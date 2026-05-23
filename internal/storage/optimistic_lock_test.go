package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

// TestUpsertVersioned_HappyPath: запись новой записи → Get → запись с
// ожидаемой версией = текущей → успех, версия инкрементировалась.
func TestUpsertVersioned_HappyPath(t *testing.T) {
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
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	if err := db.Upsert(ctx, "Контрагент", id, map[string]any{"Наименование": "v1"}, entity); err != nil {
		t.Fatalf("initial Upsert: %v", err)
	}

	row, err := db.GetByID(ctx, "Контрагент", id, entity)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	v, ok := row["_version"].(int64)
	if !ok {
		t.Fatalf("_version = %T %v, want int64", row["_version"], row["_version"])
	}
	if v != 1 {
		t.Fatalf("initial _version = %d, want 1", v)
	}

	// Запись с правильной ожидаемой версией — успех.
	if err := db.UpsertVersioned(ctx, "Контрагент", id, map[string]any{"Наименование": "v2"}, entity, &v); err != nil {
		t.Fatalf("UpsertVersioned matching: %v", err)
	}

	row2, _ := db.GetByID(ctx, "Контрагент", id, entity)
	if row2["Наименование"] != "v2" {
		t.Errorf("Наименование = %v, want v2", row2["Наименование"])
	}
	v2, _ := row2["_version"].(int64)
	if v2 != 2 {
		t.Errorf("после второй записи _version = %d, want 2", v2)
	}
}

// TestUpsertVersioned_Conflict: эмулирует двух пользователей. Оба загрузили
// версию 1. Первый сохраняет → версия становится 2. Второй пытается
// сохранить с ожидаемой версией 1 → ErrVersionConflict, данные второго
// НЕ перетирают данные первого.
func TestUpsertVersioned_Conflict(t *testing.T) {
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
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	if err := db.Upsert(ctx, "Контрагент", id, map[string]any{"Наименование": "исходное"}, entity); err != nil {
		t.Fatal(err)
	}

	// Оба пользователя «открыли форму» — увидели версию 1.
	rowUser1, _ := db.GetByID(ctx, "Контрагент", id, entity)
	rowUser2, _ := db.GetByID(ctx, "Контрагент", id, entity)
	expectedV1, _ := rowUser1["_version"].(int64)
	expectedV2, _ := rowUser2["_version"].(int64)
	if expectedV1 != 1 || expectedV2 != 1 {
		t.Fatalf("обе сессии должны видеть версию 1, получили v1=%d v2=%d", expectedV1, expectedV2)
	}

	// Первый сохраняет — успешно, версия становится 2.
	if err := db.UpsertVersioned(ctx, "Контрагент", id, map[string]any{"Наименование": "от пользователя 1"}, entity, &expectedV1); err != nil {
		t.Fatalf("первый сохраняет: %v", err)
	}

	// Второй пытается сохранить с уже устаревшей версией 1 — конфликт.
	err = db.UpsertVersioned(ctx, "Контрагент", id, map[string]any{"Наименование": "от пользователя 2"}, entity, &expectedV2)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("ожидался ErrVersionConflict, получили: %v", err)
	}

	// Главное — данные первого пользователя НЕ перетёрты.
	row, _ := db.GetByID(ctx, "Контрагент", id, entity)
	if row["Наименование"] != "от пользователя 1" {
		t.Errorf("данные первого пользователя перетёрты! Наименование = %v", row["Наименование"])
	}
	v, _ := row["_version"].(int64)
	if v != 2 {
		t.Errorf("_version = %d, want 2 (только одна успешная запись после исходной)", v)
	}
}

// TestUpsertVersioned_NilExpected: без expectedVersion поведение
// идентично Upsert — никакой проверки нет.
func TestUpsertVersioned_NilExpected(t *testing.T) {
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
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	if err := db.UpsertVersioned(ctx, "Контрагент", id, map[string]any{"Наименование": "А"}, entity, nil); err != nil {
		t.Fatalf("first nil-expected: %v", err)
	}
	if err := db.UpsertVersioned(ctx, "Контрагент", id, map[string]any{"Наименование": "Б"}, entity, nil); err != nil {
		t.Fatalf("second nil-expected: %v", err)
	}
	row, _ := db.GetByID(ctx, "Контрагент", id, entity)
	if row["Наименование"] != "Б" {
		t.Errorf("Наименование = %v, want Б", row["Наименование"])
	}
	v, _ := row["_version"].(int64)
	if v != 2 {
		t.Errorf("_version = %d, want 2", v)
	}
}

// TestUpsertVersioned_DeletedRow: если объект удалили физически до
// нашей записи, попытка сохранить с любой ожидаемой версией — конфликт.
func TestUpsertVersioned_DeletedRow(t *testing.T) {
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
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	if err := db.Upsert(ctx, "Контрагент", id, map[string]any{"Наименование": "тест"}, entity); err != nil {
		t.Fatal(err)
	}
	row, _ := db.GetByID(ctx, "Контрагент", id, entity)
	expected, _ := row["_version"].(int64)

	// Физически удаляем строку (как если бы другой пользователь сделал
	// hard-delete через REST API или прямой SQL).
	if _, err := db.Exec(ctx, "DELETE FROM контрагент WHERE id = ?", id.String()); err != nil {
		t.Fatal(err)
	}

	err = db.UpsertVersioned(ctx, "Контрагент", id, map[string]any{"Наименование": "обновление"}, entity, &expected)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("ожидался ErrVersionConflict для удалённой записи, получили: %v", err)
	}
}
