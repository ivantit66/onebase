package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

func TestSQLiteSmoke(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := ConnectSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()

	if !db.IsSQLite() || db.IsPostgres() {
		t.Fatalf("unexpected backend flags: sqlite=%v pg=%v", db.IsSQLite(), db.IsPostgres())
	}
	if db.Dialect().Name() != "sqlite" {
		t.Fatalf("dialect name = %q, want sqlite", db.Dialect().Name())
	}
	if got := db.Dialect().Placeholder(3); got != "?" {
		t.Fatalf("Placeholder(3) = %q, want ?", got)
	}

	// DDL — use dialect types so the same source works on PG too.
	d := db.Dialect()
	createSQL := "CREATE TABLE t (id " + d.TypeUUID() + " PRIMARY KEY, name " + d.TypeText() +
		", amount " + d.TypeNumber(18, 4) + ", created_at " + d.TypeTimestamp() + " DEFAULT " + d.CurrentTimestampTZ() + ")"
	if _, err := db.Exec(ctx, createSQL); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert via placeholders.
	ph1, ph2, ph3 := d.Placeholder(1), d.Placeholder(2), d.Placeholder(3)
	insertSQL := "INSERT INTO t(id,name,amount) VALUES(" + ph1 + "," + ph2 + "," + ph3 + ")"
	tag, err := db.Exec(ctx, insertSQL, "id-1", "alpha", "12.34")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if tag.RowsAffected != 1 {
		t.Fatalf("RowsAffected = %d, want 1", tag.RowsAffected)
	}

	// QueryRow single value.
	var name string
	if err := db.QueryRow(ctx, "SELECT name FROM t WHERE id="+ph1, "id-1").Scan(&name); err != nil {
		t.Fatalf("queryRow: %v", err)
	}
	if name != "alpha" {
		t.Fatalf("name = %q, want alpha", name)
	}

	// Query rows.
	rows, err := db.Query(ctx, "SELECT id, name FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, n string
		if err := rows.Scan(&id, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows count = %d, want 1", count)
	}

	// Transaction — insert two rows, rollback, expect still 1 total.
	tx, txCtx, err := db.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := db.Exec(txCtx, insertSQL, "id-2", "beta", "0"); err != nil {
		t.Fatalf("tx insert 1: %v", err)
	}
	if _, err := db.Exec(txCtx, insertSQL, "id-3", "gamma", "0"); err != nil {
		t.Fatalf("tx insert 2: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	var total int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM t").Scan(&total); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if total != 1 {
		t.Fatalf("after rollback total = %d, want 1", total)
	}

	// ColumnExists via dialect.
	exists, err := d.ColumnExists(ctx, db, "t", "name")
	if err != nil {
		t.Fatalf("ColumnExists: %v", err)
	}
	if !exists {
		t.Fatal("column 'name' not found via PRAGMA")
	}
	exists, err = d.ColumnExists(ctx, db, "t", "missing")
	if err != nil {
		t.Fatalf("ColumnExists missing: %v", err)
	}
	if exists {
		t.Fatal("column 'missing' should not exist")
	}
}

func TestSQLiteMigrateMinimal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "migrate.db")

	db, err := ConnectSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()

	if err := db.EnsureSeqTable(ctx); err != nil {
		t.Fatalf("EnsureSeqTable: %v", err)
	}
	if err := db.EnsureNumeratorSchema(ctx); err != nil {
		t.Fatalf("EnsureNumeratorSchema: %v", err)
	}

	// Run a real migration for two simple entities (catalog + document).
	entities := []*metadata.Entity{
		{
			Name: "Counterparty",
			Kind: metadata.KindCatalog,
			Fields: []metadata.Field{
				{Name: "Name", Type: metadata.FieldTypeString},
				{Name: "INN", Type: metadata.FieldTypeString},
			},
		},
		{
			Name: "Invoice",
			Kind: metadata.KindDocument,
			Fields: []metadata.Field{
				{Name: "Number", Type: metadata.FieldTypeString},
				{Name: "Date", Type: metadata.FieldTypeDate},
				{Name: "Counterparty", Type: "reference:Counterparty", RefEntity: "Counterparty"},
				{Name: "Amount", Type: metadata.FieldTypeNumber},
			},
		},
	}
	if err := db.Migrate(ctx, entities); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify catalog table exists and has expected columns.
	exists, err := db.Dialect().ColumnExists(ctx, db, "counterparty", "inn")
	if err != nil {
		t.Fatalf("ColumnExists: %v", err)
	}
	if !exists {
		t.Fatal("counterparty.inn column missing after migrate")
	}
	exists, _ = db.Dialect().ColumnExists(ctx, db, "invoice", "posted")
	if !exists {
		t.Fatal("invoice.posted column missing after migrate")
	}
	exists, _ = db.Dialect().ColumnExists(ctx, db, "invoice", "deletion_mark")
	if !exists {
		t.Fatal("invoice.deletion_mark column missing after migrate")
	}

	// Verify system schemas (audit, attachments, scheduled, constants) work on SQLite.
	if err := db.EnsureAuditSchema(ctx); err != nil {
		t.Fatalf("EnsureAuditSchema: %v", err)
	}
	if err := db.EnsureAttachmentTable(ctx); err != nil {
		t.Fatalf("EnsureAttachmentTable: %v", err)
	}
	if err := db.EnsureScheduledRunsTable(ctx); err != nil {
		t.Fatalf("EnsureScheduledRunsTable: %v", err)
	}
	if err := db.MigrateConstants(ctx, nil); err != nil {
		t.Fatalf("MigrateConstants: %v", err)
	}

	// Test seq numbering — RETURNING + ON CONFLICT must work on SQLite.
	n1, err := db.NextNum(ctx, "Invoice")
	if err != nil {
		t.Fatalf("NextNum first: %v", err)
	}
	n2, _ := db.NextNum(ctx, "Invoice")
	if n2 != n1+1 {
		t.Fatalf("NextNum: %d → %d, expected sequential", n1, n2)
	}

	// Constant set/get with JSON-roundtrip.
	if err := db.SetConstant(ctx, "TestKey", "hello"); err != nil {
		t.Fatalf("SetConstant: %v", err)
	}
	v, err := db.GetConstant(ctx, "TestKey")
	if err != nil {
		t.Fatalf("GetConstant: %v", err)
	}
	if v != "hello" {
		t.Fatalf("constant = %v, want hello", v)
	}

	// End-to-end CRUD on SQLite: insert catalog entry, fetch by id, list with
	// search filter, count.
	cat := entities[0] // Counterparty
	id := uuid.New()
	if err := db.Upsert(ctx, cat.Name, id, map[string]any{"Name": "Alfa", "INN": "1234567890"}, cat); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := db.GetByID(ctx, cat.Name, id, cat)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got["Name"] != "Alfa" {
		t.Fatalf("GetByID Name = %v, want Alfa", got["Name"])
	}

	// Add second row and verify List + filter.
	id2 := uuid.New()
	if err := db.Upsert(ctx, cat.Name, id2, map[string]any{"Name": "Beta", "INN": "9876543210"}, cat); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	rows, err := db.List(ctx, cat.Name, cat, ListParams{Search: "alfa"})
	if err != nil {
		t.Fatalf("List with search: %v", err)
	}
	if len(rows) != 1 || rows[0]["Name"] != "Alfa" {
		t.Fatalf("List search alfa: got %d rows, expected 1 with Name=Alfa: %v", len(rows), rows)
	}

	total, err := db.CountList(ctx, cat.Name, cat, ListParams{})
	if err != nil {
		t.Fatalf("CountList: %v", err)
	}
	if total != 2 {
		t.Fatalf("CountList = %d, want 2", total)
	}
}

// TestSQLiteCyrillicCaseInsensitive проверяет, что отбор и полнотекстовый
// поиск по кириллице регистронезависимы на SQLite. Встроенная LOWER() в
// SQLite приводит к нижнему регистру только ASCII — без ob_lower (см. init в
// sqlite.go) этот тест падал бы для русского текста.
func TestSQLiteCyrillicCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := ConnectSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()

	cat := &metadata.Entity{
		Name: "Counterparty",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Name", Type: metadata.FieldTypeString},
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{cat}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Upsert(ctx, cat.Name, uuid.New(), map[string]any{"Name": "Иванов"}, cat); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Поиск в нижнем регистре должен найти запись «Иванов».
	rows, err := db.List(ctx, cat.Name, cat, ListParams{Search: "иванов"})
	if err != nil {
		t.Fatalf("List search: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Search 'иванов': got %d rows, want 1 (Иванов)", len(rows))
	}

	// Отбор по полю Name в верхнем регистре должен найти ту же запись.
	rows, err = db.List(ctx, cat.Name, cat, ListParams{
		Filters: map[string]FilterValue{"Name": {Value: "ИВАН"}},
	})
	if err != nil {
		t.Fatalf("List filter: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Filter Name~'ИВАН': got %d rows, want 1 (Иванов)", len(rows))
	}
}

func TestSQLiteDialectLatestPerKey(t *testing.T) {
	d := SQLiteDialect{}
	sql := d.LatestPerKey(
		[]string{"k", "v"},
		[]string{"k"},
		[]string{"ts DESC"},
		"reg",
		"r",
		"k IS NOT NULL",
	)
	want := "SELECT k, v FROM (SELECT k, v, ROW_NUMBER() OVER (PARTITION BY k ORDER BY ts DESC) AS _rn FROM reg AS r WHERE k IS NOT NULL) _w WHERE _rn = 1"
	if sql != want {
		t.Fatalf("LatestPerKey:\n  got:  %s\n  want: %s", sql, want)
	}
}
