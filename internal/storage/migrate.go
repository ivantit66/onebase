package storage

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/ivantit66/onebase/internal/metadata"
)

// toSnakeCase converts CamelCase (including Cyrillic) to snake_case.
// Used to detect and rename columns created by older schema versions.
func toSnakeCase(s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes)+4)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(runes[i-1]) {
			out = append(out, '_')
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}

// renameSnakeCols renames old snake_case columns (e.g. тип_контрагента)
// to the current lowercase style (типконтрагента) if they exist in the table.
// PG-only: uses information_schema. No-op on SQLite (legacy data isn't a concern there).
func (db *DB) renameSnakeCols(ctx context.Context, table string, fields []metadata.Field) {
	if db.IsSQLite() {
		return
	}
	for _, f := range fields {
		newCol := metadata.ColumnName(f)
		oldCol := toSnakeCase(f.Name)
		if oldCol == newCol {
			continue
		}
		var oldExists bool
		db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`,
			table, oldCol).Scan(&oldExists)
		if !oldExists {
			continue
		}
		var newExists bool
		db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`,
			table, newCol).Scan(&newExists)
		if newExists {
			db.Exec(ctx, fmt.Sprintf(
				"UPDATE %s SET %s = %s WHERE %s IS NOT NULL AND %s IS NULL",
				table, newCol, oldCol, oldCol, newCol))
			db.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, oldCol))
		} else {
			db.Exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", table, oldCol, newCol))
		}
	}
}

// MigrateRegisters creates register tables.
func (db *DB) MigrateRegisters(ctx context.Context, registers []*metadata.Register) error {
	d := db.dialect
	for _, reg := range registers {
		if _, err := db.Exec(ctx, CreateRegisterSQL(d, reg)); err != nil {
			return fmt.Errorf("migrate register %s: %w", reg.Name, err)
		}
		table := metadata.RegisterTableName(reg.Name)
		if err := db.AddColumnIfMissing(ctx, table, "period", d.TypeTimestamp()); err != nil {
			return fmt.Errorf("migrate register %s.period: %w", reg.Name, err)
		}
		allFields := append(append([]metadata.Field{}, reg.Dimensions...), append(reg.Resources, reg.Attributes...)...)
		db.renameSnakeCols(ctx, table, allFields)
		for _, f := range allFields {
			if err := db.AddColumnIfMissing(ctx, table, metadata.ColumnName(f), fieldType(d, f)); err != nil {
				return fmt.Errorf("migrate register %s.%s: %w", reg.Name, f.Name, err)
			}
		}
	}
	return nil
}

// MigrateInfoRegisters creates tables for info registers and дотягивает схему
// существующих таблиц — добавляет недостающие колонки. Так регистры можно
// расширять (добавление periodic, новое измерение / ресурс) без ручной
// миграции БД.
//
// Что НЕ покрыто: изменение PRIMARY KEY (нельзя через ALTER в SQLite,
// нужен пересоздаваемый ALTER TABLE RENAME + INSERT SELECT). Поэтому если
// добавляется periodic-флаг к существующей непустой таблице — данные
// останутся, но PK не обновится; могут быть дубли по (dim) ключу.
func (db *DB) MigrateInfoRegisters(ctx context.Context, regs []*metadata.InfoRegister) error {
	d := db.dialect
	for _, ir := range regs {
		if _, err := db.Exec(ctx, CreateInfoRegisterSQL(d, ir)); err != nil {
			return fmt.Errorf("migrate info register %s: %w", ir.Name, err)
		}
		table := metadata.InfoRegTableName(ir.Name)
		// period колонка для periodic-регистров — может отсутствовать,
		// если в YAML только что добавили `periodic: true`. ALLOW NULL,
		// потому что existing rows иначе не вставить.
		if ir.Periodic {
			if err := db.AddColumnIfMissing(ctx, table, "period", d.TypeTimestamp()); err != nil {
				return fmt.Errorf("migrate info register %s.period: %w", ir.Name, err)
			}
		}
		// Измерения и ресурсы — добавляем если их нет.
		for _, f := range ir.Dimensions {
			if err := db.AddColumnIfMissing(ctx, table, metadata.ColumnName(f), fieldType(d, f)); err != nil {
				return fmt.Errorf("migrate info register %s.%s: %w", ir.Name, f.Name, err)
			}
		}
		if err := db.AddColumnIfMissing(ctx, table, "updated_at", d.TypeTimestamp()); err != nil {
			return fmt.Errorf("migrate info register %s.updated_at: %w", ir.Name, err)
		}
		// Замечание #23: recorder/recorder_type для записи из документа.
		if err := db.AddColumnIfMissing(ctx, table, "recorder", d.TypeUUID()); err != nil {
			return fmt.Errorf("migrate info register %s.recorder: %w", ir.Name, err)
		}
		if err := db.AddColumnIfMissing(ctx, table, "recorder_type", d.TypeText()); err != nil {
			return fmt.Errorf("migrate info register %s.recorder_type: %w", ir.Name, err)
		}
		for _, f := range ir.Resources {
			if err := db.AddColumnIfMissing(ctx, table, metadata.ColumnName(f), fieldType(d, f)); err != nil {
				return fmt.Errorf("migrate info register %s.%s: %w", ir.Name, f.Name, err)
			}
		}
		// Замечание #23 (продолжение): фактический PK таблицы может не
		// совпадать с pkCols(ir) — наследие старого CREATE до того как
		// регистр стал periodic / добавили измерения. SQLite не позволяет
		// ALTER PK, поэтому при mismatch пересоздаём таблицу через
		// CREATE + INSERT SELECT + DROP + RENAME.
		if err := db.fixInfoRegPK(ctx, ir); err != nil {
			return fmt.Errorf("migrate info register %s PK: %w", ir.Name, err)
		}
	}
	return nil
}

// fixInfoRegPK сверяет фактический PRIMARY KEY таблицы регистра сведений
// с ожидаемым (pkCols(ir)). При несовпадении пересоздаёт таблицу с
// правильным PK, копируя данные. Безопасно если в существующих строках
// нет дубликатов по новому ключу — иначе INSERT упадёт с UNIQUE constraint,
// и пользователь должен будет разобраться с дубликатами.
//
// Реализация SQLite-only — PostgreSQL поддерживает ALTER TABLE для PK
// напрямую (но в onebase это пока не используется).
func (db *DB) fixInfoRegPK(ctx context.Context, ir *metadata.InfoRegister) error {
	if db.dialect.Name() != "sqlite" {
		return nil // PG: ALTER TABLE сам разрулит, но это другой код-путь
	}
	table := metadata.InfoRegTableName(ir.Name)

	// Снимаем фактический PK.
	rows, err := db.Query(ctx, "PRAGMA table_info("+sqliteIdent(table)+")")
	if err != nil {
		return err
	}
	type pkCol struct {
		name string
		pos  int
	}
	var actual []pkCol
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if pk > 0 {
			actual = append(actual, pkCol{name: name, pos: pk})
		}
	}
	rows.Close()
	// Сортируем по pos
	for i := 1; i < len(actual); i++ {
		for j := i; j > 0 && actual[j-1].pos > actual[j].pos; j-- {
			actual[j-1], actual[j] = actual[j], actual[j-1]
		}
	}

	expected := pkCols(ir)
	if len(actual) == len(expected) {
		match := true
		for i := range actual {
			if actual[i].name != expected[i] {
				match = false
				break
			}
		}
		if match {
			return nil
		}
	}

	// PK mismatch — rebuild.
	d := db.dialect
	tmp := table + "_new"
	if _, err := db.Exec(ctx, "DROP TABLE IF EXISTS "+sqliteIdent(tmp)); err != nil {
		return err
	}
	createSQL := CreateInfoRegisterSQL(d, ir)
	// Подменяем имя таблицы на _new
	createSQL = strings.Replace(createSQL, "CREATE TABLE IF NOT EXISTS "+table, "CREATE TABLE "+tmp, 1)
	if _, err := db.Exec(ctx, createSQL); err != nil {
		return err
	}

	// Копируем данные — только общие колонки. Берём пересечение
	// колонок старой и новой таблицы.
	oldCols, err := tableColumnNames(ctx, db, table)
	if err != nil {
		return err
	}
	newCols, err := tableColumnNames(ctx, db, tmp)
	if err != nil {
		return err
	}
	var common []string
	newSet := make(map[string]bool, len(newCols))
	for _, c := range newCols {
		newSet[c] = true
	}
	for _, c := range oldCols {
		if newSet[c] {
			common = append(common, c)
		}
	}
	if len(common) > 0 {
		copySQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
			sqliteIdent(tmp),
			strings.Join(common, ", "),
			strings.Join(common, ", "),
			sqliteIdent(table))
		if _, err := db.Exec(ctx, copySQL); err != nil {
			// Откатимся — дропнем tmp, оставим старую.
			_, _ = db.Exec(ctx, "DROP TABLE "+sqliteIdent(tmp))
			return fmt.Errorf("copy data (возможно дубликаты по новому PK): %w", err)
		}
	}
	if _, err := db.Exec(ctx, "DROP TABLE "+sqliteIdent(table)); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, "ALTER TABLE "+sqliteIdent(tmp)+" RENAME TO "+sqliteIdent(table)); err != nil {
		return err
	}
	return nil
}

// tableColumnNames возвращает имена колонок таблицы в порядке их объявления.
func tableColumnNames(ctx context.Context, db *DB, table string) ([]string, error) {
	rows, err := db.Query(ctx, "PRAGMA table_info("+sqliteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// Migrate applies CREATE TABLE and ADD COLUMN IF NOT EXISTS for all entities.
func (db *DB) Migrate(ctx context.Context, entities []*metadata.Entity) error {
	d := db.dialect
	if err := db.EnsureSeqTable(ctx); err != nil {
		return fmt.Errorf("migrate: sequences table: %w", err)
	}
	if err := db.EnsureNumeratorSchema(ctx); err != nil {
		return fmt.Errorf("migrate: numerators table: %w", err)
	}
	ordered := orderByDependency(entities)
	for _, e := range ordered {
		if _, err := db.Exec(ctx, CreateTableSQL(d, e)); err != nil {
			return fmt.Errorf("migrate %s: %w", e.Name, err)
		}
		if err := db.EnsurePredefinedColumns(ctx, []*metadata.Entity{e}); err != nil {
			return fmt.Errorf("migrate: predefined columns: %w", err)
		}
		table := metadata.TableName(e.Name)
		if e.Kind == metadata.KindDocument {
			if err := db.AddColumnIfMissing(ctx, table, "posted", d.TypeBool()+" NOT NULL DEFAULT "+boolFalseLit(d)); err != nil {
				return fmt.Errorf("migrate %s.posted: %w", e.Name, err)
			}
		}
		db.renameSnakeCols(ctx, table, e.Fields)
		for _, f := range e.Fields {
			if err := db.AddColumnIfMissing(ctx, table, metadata.ColumnName(f), fieldType(d, f)); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", e.Name, f.Name, err)
			}
		}
		if err := db.AddColumnIfMissing(ctx, table, "deletion_mark", d.TypeBool()+" NOT NULL DEFAULT "+boolFalseLit(d)); err != nil {
			return fmt.Errorf("migrate %s.deletion_mark: %w", e.Name, err)
		}
		if e.Hierarchical {
			if err := db.AddHierarchyColumns(ctx, table); err != nil {
				return fmt.Errorf("migrate %s hierarchy: %w", e.Name, err)
			}
		}
		for _, tp := range e.TableParts {
			if _, err := db.Exec(ctx, CreateTablePartSQL(d, e, tp)); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", e.Name, tp.Name, err)
			}
			tpTable := metadata.TablePartTableName(e.Name, tp.Name)
			for _, f := range tp.Fields {
				if err := db.AddColumnIfMissing(ctx, tpTable, metadata.ColumnName(f), fieldType(d, f)); err != nil {
					return fmt.Errorf("migrate %s.%s.%s: %w", e.Name, tp.Name, f.Name, err)
				}
			}
		}
	}
	for _, e := range ordered {
		if err := db.SyncPredefined(ctx, e); err != nil {
			return fmt.Errorf("migrate: sync predefined %s: %w", e.Name, err)
		}
	}
	return nil
}

// orderByDependency sorts entities so referenced entities come before referencing ones.
func orderByDependency(entities []*metadata.Entity) []*metadata.Entity {
	byName := make(map[string]*metadata.Entity, len(entities))
	for _, e := range entities {
		byName[e.Name] = e
	}
	visited := make(map[string]bool)
	var result []*metadata.Entity
	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		e := byName[name]
		if e == nil {
			return
		}
		for _, f := range e.Fields {
			if f.RefEntity != "" {
				visit(f.RefEntity)
			}
		}
		result = append(result, e)
	}
	for _, e := range entities {
		visit(e.Name)
	}
	return result
}
