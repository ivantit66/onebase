package exchange_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func setupReg(t *testing.T) (*storage.DB, context.Context, *metadata.Entity) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureExchangeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	ent := &metadata.Entity{
		Name: "Номенклатура", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{ent}); err != nil {
		t.Fatal(err)
	}
	return db, ctx, ent
}

func plan2(content []string) *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name:    "Обмен",
		Content: content,
		Nodes:   []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	p.Normalize()
	return p
}

func TestRegisterOnSave(t *testing.T) {
	db, ctx, ent := setupReg(t)
	id := uuid.New()
	if err := db.Upsert(ctx, ent.Name, id, map[string]any{"Наименование": "A"}, ent); err != nil {
		t.Fatal(err)
	}
	plans := []*metadata.ExchangePlan{plan2([]string{"Справочник.Номенклатура"})}

	// Пока this_node не задан — база не участник, регистрации нет.
	if err := exchange.RegisterOnSave(ctx, db, plans, ent, id, false); err != nil {
		t.Fatal(err)
	}
	if pend, _ := db.PendingExchangeChanges(ctx, "Обмен", "fil01"); len(pend) != 0 {
		t.Fatalf("без this_node регистрации быть не должно: %+v", pend)
	}

	// Задаём this_node=center → строка ставится fil01, но НЕ center (себе).
	if err := db.SaveExchangeThisNode(ctx, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctx, db, plans, ent, id, false); err != nil {
		t.Fatal(err)
	}
	if self, _ := db.PendingExchangeChanges(ctx, "Обмен", "center"); len(self) != 0 {
		t.Errorf("источнику (center) регистрировать не нужно: %+v", self)
	}
	pend, _ := db.PendingExchangeChanges(ctx, "Обмен", "fil01")
	if len(pend) != 1 || pend[0].ObjectType != "Номенклатура" || pend[0].Version < 1 || pend[0].Deletion {
		t.Fatalf("fil01 pending: %+v", pend)
	}
}

func TestRegisterOnSaveNotInContent(t *testing.T) {
	db, ctx, ent := setupReg(t)
	if err := db.SaveExchangeThisNode(ctx, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	// Состав — только Контрагенты; наша Номенклатура не входит → нет регистрации
	// (и EntityVersion не читается, так что несуществующий объект не роняет).
	plans := []*metadata.ExchangePlan{plan2([]string{"Справочник.Контрагенты"})}
	if err := exchange.RegisterOnSave(ctx, db, plans, ent, uuid.New(), false); err != nil {
		t.Fatal(err)
	}
	if pend, _ := db.PendingExchangeChanges(ctx, "Обмен", "fil01"); len(pend) != 0 {
		t.Fatalf("сущность вне состава не должна регистрироваться: %+v", pend)
	}
}

func TestRegisterOnSaveDeletion(t *testing.T) {
	db, ctx, ent := setupReg(t)
	id := uuid.New()
	if err := db.Upsert(ctx, ent.Name, id, map[string]any{"Наименование": "A"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveExchangeThisNode(ctx, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	plans := []*metadata.ExchangePlan{plan2([]string{"Номенклатура"})}
	if err := exchange.RegisterOnSave(ctx, db, plans, ent, id, true); err != nil {
		t.Fatal(err)
	}
	pend, _ := db.PendingExchangeChanges(ctx, "Обмен", "fil01")
	if len(pend) != 1 || !pend[0].Deletion {
		t.Fatalf("ожидали одну строку с Deletion=true: %+v", pend)
	}
}
