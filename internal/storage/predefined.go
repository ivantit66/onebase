package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

// EnsurePredefinedColumns adds _predefined_name and _is_predefined columns to
// catalog tables that declare predefined items. Safe to call repeatedly.
func (db *DB) EnsurePredefinedColumns(ctx context.Context, entities []*metadata.Entity) error {
	d := db.dialect
	for _, e := range entities {
		if e.Kind != metadata.KindCatalog || len(e.Predefined) == 0 {
			continue
		}
		table := metadata.TableName(e.Name)
		if err := db.AddColumnIfMissing(ctx, table, "_predefined_name", d.TypeText()); err != nil {
			return fmt.Errorf("ensure predefined cols %s._predefined_name: %w", e.Name, err)
		}
		if err := db.AddColumnIfMissing(ctx, table, "_is_predefined", d.TypeBool()+" NOT NULL DEFAULT "+boolFalseLit(d)); err != nil {
			return fmt.Errorf("ensure predefined cols %s._is_predefined: %w", e.Name, err)
		}
		// Partial unique index — both PG and SQLite support WHERE on indexes.
		// Boolean literal differs: TRUE for PG, 1 for SQLite.
		boolTrue := "TRUE"
		if d.Name() == "sqlite" {
			boolTrue = "1"
		}
		idxName := "idx_" + strings.ToLower(e.Name) + "_predefined"
		idxSQL := fmt.Sprintf(
			`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (_predefined_name) WHERE _is_predefined = %s`,
			idxName, table, boolTrue)
		if _, err := db.Exec(ctx, idxSQL); err != nil {
			return fmt.Errorf("ensure predefined index %s: %w", e.Name, err)
		}
	}
	return nil
}

// SyncPredefined upserts all predefined items declared in the entity YAML into
// the database. The _predefined_name is used as the conflict target so the UUID
// never changes on subsequent syncs — only field values are updated.
//
// поддерживаются cross-ref внутри одного справочника
// (Розничная.БазовыйТип: Закупочная). Алгоритм:
//   1. Пре-аллоцировать UUID для каждого item (переиспользуя из БД при upsert).
//   2. Топологическая сортировка по self-reference полям.
//   3. INSERT в порядке зависимостей, заменяя имя на UUID для self-ref полей.
// При цикле в зависимостях возвращается ошибка.
func (db *DB) SyncPredefined(ctx context.Context, e *metadata.Entity) error {
	if len(e.Predefined) == 0 {
		return nil
	}
	d := db.dialect
	boolTrue := "TRUE"
	if d.Name() == "sqlite" {
		boolTrue = "1"
	}
	table := metadata.TableName(e.Name)

	// Шаг 1: для каждого predefined — собираем UUID. Если в БД уже есть
	// запись с тем же _predefined_name — берём её UUID, иначе генерим.
	nameToUUID := make(map[string]uuid.UUID, len(e.Predefined))
	for _, item := range e.Predefined {
		var idStr string
		err := db.QueryRow(ctx, fmt.Sprintf(
			`SELECT id FROM %s WHERE _predefined_name = %s AND _is_predefined = %s LIMIT 1`,
			table, d.Placeholder(1), boolTrue),
			item.Name,
		).Scan(&idStr)
		if err == nil {
			if id, err := uuid.Parse(idStr); err == nil {
				nameToUUID[item.Name] = id
				continue
			}
		}
		nameToUUID[item.Name] = uuid.New()
	}

	// Шаг 2: топологическая сортировка. Граф ориентирован: item → items на
	// которые он ссылается через self-ref поля. Вставляем в порядке
	// «зависимости первыми». Карта имён → item для быстрого lookup.
	byName := make(map[string]*metadata.PredefinedItem, len(e.Predefined))
	for _, it := range e.Predefined {
		byName[it.Name] = it
	}
	// Определяем self-ref поля.
	selfRefFields := make(map[string]bool)
	for _, f := range e.Fields {
		if f.RefEntity == e.Name {
			selfRefFields[strings.ToLower(f.Name)] = true
		}
	}
	ordered, err := topoSortPredefined(e.Predefined, selfRefFields)
	if err != nil {
		return fmt.Errorf("sync predefined %s: %w", e.Name, err)
	}

	// Шаг 3: вставка в порядке зависимостей.
	for _, item := range ordered {
		cols := []string{"id", "_predefined_name", "_is_predefined"}
		phs := []string{d.Placeholder(1), d.Placeholder(2), boolTrue}
		args := []any{idArg(d, nameToUUID[item.Name]), item.Name}
		updates := []string{"_is_predefined = " + boolTrue}
		argIdx := 3

		for _, f := range e.Fields {
			col := metadata.ColumnName(f)
			val, ok := item.Fields[f.Name]
			if !ok {
				continue
			}
			// Self-ref поле: если значение — имя другого predefined,
			// подменяем на его UUID.
			if selfRefFields[strings.ToLower(f.Name)] {
				if refName, ok := val.(string); ok {
					if refUUID, found := nameToUUID[refName]; found {
						val = idArg(d, refUUID)
					}
				}
			}
			cols = append(cols, col)
			phs = append(phs, d.Placeholder(argIdx))
			args = append(args, val)
			updates = append(updates, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
			argIdx++
		}

		sql := fmt.Sprintf(
			`INSERT INTO %s (%s) VALUES (%s)
			 ON CONFLICT (_predefined_name) WHERE _is_predefined = %s
			 DO UPDATE SET %s`,
			table,
			strings.Join(cols, ", "),
			strings.Join(phs, ", "),
			boolTrue,
			strings.Join(updates, ", "),
		)
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			return fmt.Errorf("sync predefined %s.%s: %w", e.Name, item.Name, err)
		}
	}
	return nil
}

// topoSortPredefined сортирует predefined-элементы по self-reference
// зависимостям. Если A.SelfRefField = B, то B вставляется раньше A.
// При обнаружении цикла возвращает ошибку с указанием участников.
func topoSortPredefined(items []*metadata.PredefinedItem, selfRefFields map[string]bool) ([]*metadata.PredefinedItem, error) {
	byName := make(map[string]*metadata.PredefinedItem, len(items))
	for _, it := range items {
		byName[it.Name] = it
	}

	// deps[name] = список имён, от которых зависит name.
	deps := make(map[string][]string, len(items))
	for _, it := range items {
		var d []string
		for fieldName, val := range it.Fields {
			if !selfRefFields[strings.ToLower(fieldName)] {
				continue
			}
			refName, ok := val.(string)
			if !ok || refName == "" {
				continue
			}
			if _, exists := byName[refName]; exists {
				d = append(d, refName)
			}
		}
		deps[it.Name] = d
	}

	// DFS с тремя цветами.
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(items))
	var out []*metadata.PredefinedItem
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch color[name] {
		case black:
			return nil
		case gray:
			return fmt.Errorf("цикл в self-reference predefined: %s → %s",
				strings.Join(path, " → "), name)
		}
		color[name] = gray
		for _, dep := range deps[name] {
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		color[name] = black
		out = append(out, byName[name])
		return nil
	}
	for _, it := range items {
		if err := visit(it.Name, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetPredefinedID returns the UUID of a predefined item by its name.
func (db *DB) GetPredefinedID(ctx context.Context, entityName, predefinedName string) (uuid.UUID, error) {
	d := db.dialect
	boolTrue := "TRUE"
	if d.Name() == "sqlite" {
		boolTrue = "1"
	}
	table := metadata.TableName(entityName)
	var idStr string
	err := db.QueryRow(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE _predefined_name = %s AND _is_predefined = %s`,
			table, d.Placeholder(1), boolTrue),
		predefinedName,
	).Scan(&idStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("predefined %s.%s not found: %w", entityName, predefinedName, err)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("predefined %s.%s: bad uuid: %w", entityName, predefinedName, err)
	}
	return id, nil
}

// GetPredefinedIDStr is a string-returning variant of GetPredefinedID for the
// DSL interpreter interface (avoids uuid dependency in the interpreter package).
func (db *DB) GetPredefinedIDStr(ctx context.Context, entityName, predefinedName string) (string, error) {
	id, err := db.GetPredefinedID(ctx, entityName, predefinedName)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// WriteCatalogRecord upserts a catalog/document record from DSL code
// (замечание #25: Справочники.X.Создать().Записать() из обработки).
// idStr пустой или невалидный → генерируется новый UUID. Возвращает
// строковый UUID записанной записи (для построения *Ref в DSL).
func (db *DB) WriteCatalogRecord(ctx context.Context, entity *metadata.Entity, idStr string, fields map[string]any) (string, error) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		id = uuid.New()
	}
	if err := db.Upsert(ctx, entity.Name, id, fields, entity); err != nil {
		return "", err
	}
	return id.String(), nil
}

// FindCatalogByField looks up a single catalog row by exact match of fieldName.
// Returns (id, displayValue, true) on hit; ("", "", false, nil) when not found.
// displayValue is the matched field's value — handy for building a *Ref.
// fieldName lookup is case-insensitive against entity.Fields names.
func (db *DB) FindCatalogByField(ctx context.Context, entity *metadata.Entity, fieldName, value string) (string, string, bool, error) {
	var field *metadata.Field
	for i := range entity.Fields {
		if strings.EqualFold(entity.Fields[i].Name, fieldName) {
			field = &entity.Fields[i]
			break
		}
	}
	if field == nil {
		return "", "", false, fmt.Errorf("entity %s has no field %q", entity.Name, fieldName)
	}
	col := metadata.ColumnName(*field)
	table := metadata.TableName(entity.Name)
	d := db.dialect
	rows, err := db.Query(ctx,
		fmt.Sprintf(`SELECT id, %s FROM %s WHERE %s = %s LIMIT 1`, col, table, col, d.Placeholder(1)),
		value,
	)
	if err != nil {
		return "", "", false, fmt.Errorf("find %s.%s: %w", entity.Name, fieldName, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", false, nil
	}
	var idStr, display string
	if err := rows.Scan(&idStr, &display); err != nil {
		return "", "", false, fmt.Errorf("find %s.%s scan: %w", entity.Name, fieldName, err)
	}
	return idStr, display, true, nil
}
