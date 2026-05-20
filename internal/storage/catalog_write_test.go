package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// Замечание #25: WriteCatalogRecord должен реально персистить запись
// справочника (раньше путь Справочники.X.Создать().Записать() был no-op).
func TestWriteCatalogRecord_Persists(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entity := &metadata.Entity{
		Name: "Контрагент",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "ИНН", Type: metadata.FieldTypeString},
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
		t.Fatal(err)
	}

	// записываем через DSL-путь
	id, err := db.WriteCatalogRecord(ctx, entity, "", map[string]any{
		"наименование": "ООО Ромашка",
		"инн":          "7701234567",
	})
	if err != nil {
		t.Fatalf("WriteCatalogRecord: %v", err)
	}
	if id == "" {
		t.Fatal("вернулся пустой id")
	}

	// проверяем что запись видна (тот же сценарий что у пользователя:
	// записать и тут же прочитать)
	var count int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM контрагент").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("ожидалась 1 запись, получили %d (запись = no-op?)", count)
	}

	var name, inn string
	if err := db.QueryRow(ctx, "SELECT наименование, инн FROM контрагент LIMIT 1").Scan(&name, &inn); err != nil {
		t.Fatal(err)
	}
	if name != "ООО Ромашка" || inn != "7701234567" {
		t.Errorf("неверные данные: name=%q inn=%q", name, inn)
	}
}

// Несколько записей подряд — все персистятся (сценарий seed-обработки
// «создаём 6 контрагентов»).
func TestWriteCatalogRecord_Multiple(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entity := &metadata.Entity{
		Name:   "Контрагент",
		Kind:   metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
		t.Fatal(err)
	}

	for _, n := range []string{"А", "Б", "В", "Г", "Д", "Е"} {
		if _, err := db.WriteCatalogRecord(ctx, entity, "", map[string]any{"наименование": n}); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}

	var count int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM контрагент").Scan(&count)
	if count != 6 {
		t.Errorf("ожидалось 6 записей, получили %d", count)
	}
}
