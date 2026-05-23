package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ivantit66/onebase/internal/metadata"
)

// ListParams controls filtering, search, sorting and pagination for List queries.
type ListParams struct {
	Filters   map[string]FilterValue
	Sort      string // field Name (empty = default sort by id)
	Dir       string // "asc" or "desc"
	ParentStr string // "" = no filter; "root" = parent IS NULL; "<uuid>" = parent = uuid
	Search    string // full-text search: ILIKE across all string fields
	Limit     int    // 0 = no limit
	Offset    int    // for pagination
}

// FilterValue holds a filter for one field.
type FilterValue struct {
	Value string // used for string and reference equality
	From  string // used for date range start (inclusive)
	To    string // used for date range end (inclusive)
}

// Upsert inserts or updates the object fields.
func (db *DB) Upsert(ctx context.Context, entityName string, id uuid.UUID, fields map[string]any, entity *metadata.Entity) error {
	d := db.dialect
	// Read old value for audit diff (best-effort, ignore errors)
	var oldRow map[string]any
	isNew := false
	if existing, err := db.GetByID(ctx, entityName, id, entity); err != nil {
		isNew = true
	} else {
		oldRow = existing
	}

	table := metadata.TableName(entityName)
	cols := []string{"id"}
	placeholders := []string{d.Placeholder(1)}
	args := []any{idArg(d, id)}
	updates := []string{}

	argIdx := 2
	for _, f := range entity.Fields {
		col := metadata.ColumnName(f)
		ph := d.Placeholder(argIdx)
		argIdx++
		cols = append(cols, col)
		placeholders = append(placeholders, ph)
		args = append(args, fieldValueDialect(d, f, fields))
		updates = append(updates, col+" = EXCLUDED."+col)
	}

	if entity.Hierarchical {
		parentIDStr := ""
		if v := fields["parent_id"]; v != nil {
			parentIDStr = fmt.Sprintf("%v", v)
		}
		if pID, err := uuid.Parse(parentIDStr); err == nil {
			if pID != id {
				if cycle, _ := db.WouldCycle(ctx, table, id, pID); cycle {
					return fmt.Errorf("нельзя переместить группу в её подчинённую группу")
				}
			}
			cols = append(cols, "parent_id")
			placeholders = append(placeholders, d.Placeholder(argIdx))
			args = append(args, idArg(d, pID))
			argIdx++
			updates = append(updates, "parent_id = EXCLUDED.parent_id")
		} else {
			cols = append(cols, "parent_id")
			placeholders = append(placeholders, "NULL")
			updates = append(updates, "parent_id = NULL")
		}
		isFolder := false
		if v := fields["is_folder"]; v != nil {
			switch tv := v.(type) {
			case bool:
				isFolder = tv
			case string:
				isFolder = tv == "true"
			}
		}
		cols = append(cols, "is_folder")
		placeholders = append(placeholders, d.Placeholder(argIdx))
		args = append(args, isFolder)
		argIdx++
		updates = append(updates, "is_folder = EXCLUDED.is_folder")
	}
	_ = argIdx

	// Оптимистическая блокировка: на каждом UPDATE инкрементируем _version.
	// На INSERT — DEFAULT 1 из DDL. См. UpsertVersioned для проверки ожидаемой
	// ревизии перед записью.
	updates = append(updates, "_version = "+table+"._version + 1")

	var sql string
	if len(updates) == 0 {
		sql = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (id) DO NOTHING",
			table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	} else {
		sql = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (id) DO UPDATE SET %s",
			table, strings.Join(cols, ", "), strings.Join(placeholders, ", "), strings.Join(updates, ", "))
	}
	if err := db.exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("upsert %s: %w", entityName, err)
	}

	// Audit (best-effort, non-blocking)
	kind := string(entity.Kind)
	if isNew {
		db.logCreate(ctx, kind, entityName, id)
	} else if oldRow != nil {
		changes := AuditDiff(oldRow, fields, entity)
		if len(changes) > 0 {
			db.logUpdate(ctx, kind, entityName, id, changes)
		}
	}
	return nil
}

// GetByID retrieves a single object by ID, returning fields as map[string]any.
// For documents, also returns "posted" bool.
func (db *DB) GetByID(ctx context.Context, entityName string, id uuid.UUID, entity *metadata.Entity) (map[string]any, error) {
	d := db.dialect
	table := metadata.TableName(entityName)
	cols := []string{"id"}
	for _, f := range entity.Fields {
		cols = append(cols, metadata.ColumnName(f))
	}
	if entity.Kind == metadata.KindDocument {
		cols = append(cols, "posted")
	}
	cols = append(cols, "deletion_mark", "_version")
	if entity.Hierarchical {
		cols = append(cols, "is_folder", "parent_id")
	}
	sql := fmt.Sprintf("SELECT %s FROM %s WHERE id = %s", strings.Join(cols, ", "), table, d.Placeholder(1))
	row := db.QueryRow(ctx, sql, idArg(d, id))

	dest := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		return nil, fmt.Errorf("getbyid %s: %w", entityName, err)
	}

	result := make(map[string]any, len(cols))
	result["id"] = normalizeValue(dest[0])
	for i, f := range entity.Fields {
		result[f.Name] = normalizeValue(dest[i+1])
	}
	off := len(entity.Fields) + 1
	if entity.Kind == metadata.KindDocument {
		result["posted"] = normalizeBool(dest[off])
		off++
	}
	result["deletion_mark"] = normalizeValue(dest[off])
	off++
	result["_version"] = normalizeValue(dest[off])
	off++
	if entity.Hierarchical {
		result["is_folder"] = normalizeValue(dest[off])
		off++
		result["parent_id"] = normalizeValue(dest[off])
	}
	return result, nil
}

// normalizeValue converts pgx scan results to display-friendly Go types.
func normalizeValue(v any) any {
	switch t := v.(type) {
	case [16]byte:
		return uuid.UUID(t).String()
	case uuid.UUID:
		return t.String()
	case pgtype.Numeric:
		if !t.Valid || t.NaN {
			return nil
		}
		f, err := t.Float64Value()
		if err == nil && f.Valid {
			return f.Float64
		}
		return nil
	case int64:
		return t
	}
	return v
}

// normalizeBool converts any DB boolean representation (bool, int64 0/1) to bool.
// SQLite stores booleans as integers; PostgreSQL returns bool directly.
func normalizeBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	}
	return false
}

// normalizeUUID is a convenience alias for UUID normalization only.
func normalizeUUID(v any) any {
	return normalizeValue(v)
}

// List returns rows for an entity with optional filtering and sorting.
// For documents, also returns "posted" bool.
func (db *DB) List(ctx context.Context, entityName string, entity *metadata.Entity, params ListParams) ([]map[string]any, error) {
	d := db.dialect
	table := metadata.TableName(entityName)
	cols := []string{"id"}
	for _, f := range entity.Fields {
		cols = append(cols, metadata.ColumnName(f))
	}
	if entity.Kind == metadata.KindDocument {
		cols = append(cols, "posted")
	}
	cols = append(cols, "deletion_mark")
	hasPredefined := entity.Kind == metadata.KindCatalog && len(entity.Predefined) > 0
	if hasPredefined {
		cols = append(cols, "_is_predefined")
	}
	if entity.Hierarchical {
		cols = append(cols, "is_folder", "parent_id")
	}

	var whereParts []string
	var args []any
	argIdx := 1

	// Parent filter for hierarchical catalogs
	if entity.Hierarchical && params.ParentStr != "" {
		if params.ParentStr == "root" {
			whereParts = append(whereParts, "parent_id IS NULL")
		} else if pID, err := uuid.Parse(params.ParentStr); err == nil {
			whereParts = append(whereParts, fmt.Sprintf("parent_id = %s", d.Placeholder(argIdx)))
			args = append(args, idArg(d, pID))
			argIdx++
		}
	}

	for _, f := range entity.Fields {
		fv, ok := params.Filters[f.Name]
		if !ok {
			continue
		}
		col := metadata.ColumnName(f)
		switch {
		case f.Type == metadata.FieldTypeDate:
			if fv.From != "" {
				whereParts = append(whereParts, fmt.Sprintf("%s >= %s", col, d.Placeholder(argIdx)))
				args = append(args, fv.From)
				argIdx++
			}
			if fv.To != "" {
				whereParts = append(whereParts, fmt.Sprintf("%s <= %s", col, d.Placeholder(argIdx)))
				args = append(args, fv.To)
				argIdx++
			}
		case f.RefEntity != "":
			if fv.Value != "" {
				whereParts = append(whereParts, fmt.Sprintf("%s = %s", col, d.Placeholder(argIdx)))
				if id, err := uuid.Parse(fv.Value); err == nil {
					args = append(args, idArg(d, id))
				} else {
					args = append(args, fv.Value)
				}
				argIdx++
			}
		default:
			if fv.Value != "" {
				whereParts = append(whereParts, d.LowerLike(col)+" LIKE "+d.LowerLike(d.Placeholder(argIdx)))
				args = append(args, "%"+fv.Value+"%")
				argIdx++
			}
		}
	}

	// Full-text search across all string fields.
	// SQLite '?' placeholders are positional with no repetition; for each
	// field we allocate a fresh placeholder and bind the pattern again.
	if params.Search != "" {
		var searchParts []string
		pattern := "%" + params.Search + "%"
		for _, f := range entity.Fields {
			if f.Type == metadata.FieldTypeString && f.RefEntity == "" {
				col := metadata.ColumnName(f)
				searchParts = append(searchParts, d.LowerLike(col)+" LIKE "+d.LowerLike(d.Placeholder(argIdx)))
				args = append(args, pattern)
				argIdx++
			}
		}
		if len(searchParts) > 0 {
			whereParts = append(whereParts, "("+strings.Join(searchParts, " OR ")+")")
		}
	}

	baseQuery := fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), table)
	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = " WHERE " + strings.Join(whereParts, " AND ")
	}
	query := baseQuery + whereClause

	// sorting
	if entity.Hierarchical && params.Sort == "" {
		firstStrCol := "id"
		for _, f := range entity.Fields {
			if f.Type == metadata.FieldTypeString {
				firstStrCol = metadata.ColumnName(f)
				break
			}
		}
		query += fmt.Sprintf(" ORDER BY is_folder DESC, %s ASC", firstStrCol)
	} else {
		orderCol := "id"
		if params.Sort != "" {
			for _, f := range entity.Fields {
				if f.Name == params.Sort {
					orderCol = metadata.ColumnName(f)
					break
				}
			}
		}
		orderDir := "ASC"
		if strings.ToLower(params.Dir) == "desc" {
			orderDir = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", orderCol, orderDir)
	}

	if params.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", params.Limit, params.Offset)
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", entityName, err)
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		dest := make([]any, len(cols))
		ptrs := make([]any, len(dest))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		row["id"] = normalizeValue(dest[0])
		for i, f := range entity.Fields {
			row[f.Name] = normalizeValue(dest[i+1])
		}
		off := len(entity.Fields) + 1
		if entity.Kind == metadata.KindDocument {
			row["posted"] = normalizeBool(dest[off])
			off++
		}
		row["deletion_mark"] = normalizeValue(dest[off])
		off++
		if hasPredefined {
			row["_is_predefined"] = normalizeValue(dest[off])
			off++
		}
		if entity.Hierarchical {
			row["is_folder"] = normalizeValue(dest[off])
			off++
			row["parent_id"] = normalizeValue(dest[off])
			// off++
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// CountList returns the total number of rows matching the given params (ignoring Limit/Offset).
func (db *DB) CountList(ctx context.Context, entityName string, entity *metadata.Entity, params ListParams) (int, error) {
	d := db.dialect
	table := metadata.TableName(entityName)
	var whereParts []string
	var args []any
	argIdx := 1

	if entity.Hierarchical && params.ParentStr != "" {
		if params.ParentStr == "root" {
			whereParts = append(whereParts, "parent_id IS NULL")
		} else if pID, err := uuid.Parse(params.ParentStr); err == nil {
			whereParts = append(whereParts, fmt.Sprintf("parent_id = %s", d.Placeholder(argIdx)))
			args = append(args, idArg(d, pID))
			argIdx++
		}
	}

	for _, f := range entity.Fields {
		fv, ok := params.Filters[f.Name]
		if !ok {
			continue
		}
		col := metadata.ColumnName(f)
		switch {
		case f.Type == metadata.FieldTypeDate:
			if fv.From != "" {
				whereParts = append(whereParts, fmt.Sprintf("%s >= %s", col, d.Placeholder(argIdx)))
				args = append(args, fv.From)
				argIdx++
			}
			if fv.To != "" {
				whereParts = append(whereParts, fmt.Sprintf("%s <= %s", col, d.Placeholder(argIdx)))
				args = append(args, fv.To)
				argIdx++
			}
		case f.RefEntity != "":
			if fv.Value != "" {
				whereParts = append(whereParts, fmt.Sprintf("%s = %s", col, d.Placeholder(argIdx)))
				if id, err := uuid.Parse(fv.Value); err == nil {
					args = append(args, idArg(d, id))
				} else {
					args = append(args, fv.Value)
				}
				argIdx++
			}
		default:
			if fv.Value != "" {
				whereParts = append(whereParts, d.LowerLike(col)+" LIKE "+d.LowerLike(d.Placeholder(argIdx)))
				args = append(args, "%"+fv.Value+"%")
				argIdx++
			}
		}
	}

	if params.Search != "" {
		var searchParts []string
		pattern := "%" + params.Search + "%"
		for _, f := range entity.Fields {
			if f.Type == metadata.FieldTypeString && f.RefEntity == "" {
				col := metadata.ColumnName(f)
				searchParts = append(searchParts, d.LowerLike(col)+" LIKE "+d.LowerLike(d.Placeholder(argIdx)))
				args = append(args, pattern)
				argIdx++
			}
		}
		if len(searchParts) > 0 {
			whereParts = append(whereParts, "("+strings.Join(searchParts, " OR ")+")")
		}
	}

	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if len(whereParts) > 0 {
		q += " WHERE " + strings.Join(whereParts, " AND ")
	}
	var count int
	if err := db.QueryRow(ctx, q, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count %s: %w", entityName, err)
	}
	return count, nil
}

// GetTablePartRows returns rows of a tablepart for a given parent id, ordered by строка.
func (db *DB) GetTablePartRows(ctx context.Context, entityName, tpName string, parentID uuid.UUID, tp metadata.TablePart) ([]map[string]any, error) {
	d := db.dialect
	table := metadata.TablePartTableName(entityName, tpName)
	cols := []string{"строка"}
	for _, f := range tp.Fields {
		cols = append(cols, metadata.ColumnName(f))
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE parent_id = %s ORDER BY строка",
		strings.Join(cols, ", "), table, d.Placeholder(1))
	rows, err := db.Query(ctx, query, idArg(d, parentID))
	if err != nil {
		return nil, fmt.Errorf("get tablepart %s.%s: %w", entityName, tpName, err)
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		dest := make([]any, len(cols))
		ptrs := make([]any, len(dest))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		row["строка"] = dest[0]
		for i, f := range tp.Fields {
			row[f.Name] = normalizeValue(dest[i+1])
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// UpsertTablePartRows replaces all rows for the given parent with the provided rows.
func (db *DB) UpsertTablePartRows(ctx context.Context, entityName, tpName string, parentID uuid.UUID, rows []map[string]any, tp metadata.TablePart) error {
	d := db.dialect
	table := metadata.TablePartTableName(entityName, tpName)

	if err := db.exec(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE parent_id = %s", table, d.Placeholder(1)),
		idArg(d, parentID)); err != nil {
		return fmt.Errorf("delete tablepart %s.%s: %w", entityName, tpName, err)
	}

	for i, row := range rows {
		cols := []string{"id", "parent_id", "строка"}
		placeholders := []string{d.Placeholder(1), d.Placeholder(2), d.Placeholder(3)}
		args := []any{idArg(d, uuid.New()), idArg(d, parentID), i + 1}
		for j, f := range tp.Fields {
			cols = append(cols, metadata.ColumnName(f))
			placeholders = append(placeholders, d.Placeholder(j+4))
			args = append(args, fieldValueDialect(d, f, row))
		}
		sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
		if err := db.exec(ctx, sql, args...); err != nil {
			return fmt.Errorf("insert tablepart %s.%s row %d: %w", entityName, tpName, i+1, err)
		}
	}
	return nil
}

// Delete removes an entity record by id. Tablepart rows cascade automatically.
// Returns an error if the record is a predefined item (_is_predefined = TRUE).
func (db *DB) Delete(ctx context.Context, entityName string, id uuid.UUID) error {
	d := db.dialect
	tbl := metadata.TableName(entityName)
	// Check if this is a predefined record (column may not exist — ignore error)
	var isPredefined bool
	if err := db.QueryRow(ctx,
		fmt.Sprintf("SELECT _is_predefined FROM %s WHERE id = %s", tbl, d.Placeholder(1)),
		idArg(d, id),
	).Scan(&isPredefined); err == nil && isPredefined {
		return fmt.Errorf("нельзя удалить предопределённый элемент %s", entityName)
	}

	// For hierarchical catalogs, prevent deleting non-empty folders
	var childCount int
	if err := db.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE parent_id = %s AND deletion_mark = %s",
			tbl, d.Placeholder(1), boolFalseLit(d)),
		idArg(d, id),
	).Scan(&childCount); err == nil && childCount > 0 {
		return fmt.Errorf("нельзя удалить группу: в ней есть элементы (%d шт.)", childCount)
	}

	err := db.exec(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE id = %s", tbl, d.Placeholder(1)), idArg(d, id))
	if err == nil {
		if s := db.GetAuditSettings(ctx); s.Enabled && s.Delete {
			u, _ := auditUserFromCtx(ctx)
			_ = db.Log(ctx, &AuditEntry{
				UserID:     u.UserID,
				UserLogin:  u.UserLogin,
				Action:     "delete",
				EntityName: entityName,
				RecordID:   id.String(),
			})
		}
	}
	return err
}

// SetPosted sets the posted flag on a document.
func (db *DB) SetPosted(ctx context.Context, entityName string, id uuid.UUID, posted bool) error {
	d := db.dialect
	err := db.exec(ctx,
		fmt.Sprintf("UPDATE %s SET posted = %s WHERE id = %s",
			metadata.TableName(entityName), d.Placeholder(1), d.Placeholder(2)),
		posted, idArg(d, id))
	if err == nil {
		if s := db.GetAuditSettings(ctx); s.Enabled && s.Post {
			u, _ := auditUserFromCtx(ctx)
			action := "post"
			if !posted {
				action = "unpost"
			}
			_ = db.Log(ctx, &AuditEntry{
				UserID:     u.UserID,
				UserLogin:  u.UserLogin,
				Action:     action,
				EntityKind: "document",
				EntityName: entityName,
				RecordID:   id.String(),
			})
		}
	}
	return err
}

// fieldValue extracts the value for a field from the fields map, handling reference UUID strings.
// Deprecated: use fieldValueDialect to get values typed for the active backend.
func fieldValue(f metadata.Field, fields map[string]any) any {
	return fieldValueDialect(PgDialect{}, f, fields)
}

// uuidProvider is implemented by *interpreter.Ref to expose its UUID without
// creating an import cycle between storage and interpreter packages.
type uuidProvider interface{ GetRefUUID() string }

// fieldValueDialect extracts a field value and normalizes UUIDs:
// PG accepts uuid.UUID directly; SQLite stores them as TEXT strings.
func fieldValueDialect(d Dialect, f metadata.Field, fields map[string]any) any {
	v := fields[f.Name]
	if v == nil {
		v = fields[strings.ToLower(f.Name)]
	}
	if f.RefEntity != "" {
		if v == nil {
			return nil
		}
		if rv, ok := v.(uuidProvider); ok {
			s := rv.GetRefUUID()
			if s == "" {
				return nil
			}
			if id, err := uuid.Parse(s); err == nil {
				return idArg(d, id)
			}
			return nil
		}
		if s, ok := v.(string); ok {
			if s == "" {
				return nil
			}
			if id, err := uuid.Parse(s); err == nil {
				return idArg(d, id)
			}
			return nil
		}
	}
	// SQLite stores time.Time as its .String() representation ("2006-01-02 15:04:05 -0700 MST")
	// which is unreadable by modernc. Normalize to RFC3339 for reliable round-trip.
	if f.Type == metadata.FieldTypeDate && d.Name() == "sqlite" {
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return v
}

// idArg encodes a UUID for the active backend: PG → uuid.UUID, SQLite → string.
func idArg(d Dialect, id uuid.UUID) any {
	if d.Name() == "sqlite" {
		return id.String()
	}
	return id
}
