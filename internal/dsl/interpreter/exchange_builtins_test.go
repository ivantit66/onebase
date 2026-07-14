package interpreter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

type fakeExchangeReg struct {
	plans map[string]*metadata.ExchangePlan
	ents  map[string]*metadata.Entity
}

func (f fakeExchangeReg) GetExchangePlan(name string) *metadata.ExchangePlan {
	for k, v := range f.plans {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

func (f fakeExchangeReg) GetEntity(name string) *metadata.Entity {
	for k, v := range f.ents {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

func exchDB(t *testing.T, ent *metadata.Entity) (*storage.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureExchangeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, []*metadata.Entity{ent}); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func TestExchangeDSLRoundTrip(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Товар", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
		},
	}
	plan := &metadata.ExchangePlan{
		Name: "Обмен", Content: []string{"Справочник.Товар"},
		Nodes: []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	plan.Normalize()
	reg := fakeExchangeReg{
		plans: map[string]*metadata.ExchangePlan{"Обмен": plan},
		ents:  map[string]*metadata.Entity{"Товар": ent},
	}

	// База A: узел center, объект зарегистрирован для fil01.
	a, ctx := exchDB(t, ent)
	if err := a.SaveExchangeThisNode(ctx, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := a.Upsert(ctx, "Товар", id, map[string]any{"Наименование": "Болт", "Цена": "7"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctx, a, []*metadata.ExchangePlan{plan}, ent, id, false); err != nil {
		t.Fatal(err)
	}

	rootA := NewExchangePlansRoot(ctx, a, reg)
	proxyA := rootA.Get("Обмен")
	if proxyA == nil {
		t.Fatal("ПланыОбмена.Обмен вернул nil")
	}
	// ВыгрузитьИзменения("fil01") → строка пакета.
	res := proxyA.(*exchangePlanProxy).CallMethod("ВыгрузитьИзменения", []any{"fil01"})
	pkg, ok := res.(string)
	if !ok || pkg == "" {
		t.Fatalf("ВыгрузитьИзменения вернул %T %v, ждали непустую строку", res, res)
	}

	// База B: узел fil01, загрузка пакета.
	b, ctxB := exchDB(t, ent)
	_ = b.SaveExchangeThisNode(ctxB, "Обмен", "fil01")
	rootB := NewExchangePlansRoot(ctxB, b, reg)
	applied := rootB.Get("Обмен").(*exchangePlanProxy).CallMethod("ЗагрузитьПакет", []any{pkg})
	if applied != float64(1) {
		t.Fatalf("ЗагрузитьПакет применил %v, ждали 1", applied)
	}
	row, err := b.GetByID(ctxB, "Товар", id, ent)
	if err != nil || row["Наименование"] != "Болт" {
		t.Fatalf("объект не загрузился в B: row=%v err=%v", row, err)
	}

	// Неизвестный план → nil.
	if rootA.Get("НетТакого") != nil {
		t.Error("несуществующий план должен давать nil")
	}
}

// Прямая запись справочника из DSL (Создать().Записать()) идёт мимо
// entityservice.Save — регистрация обмена должна отработать через ExchangeRegistrar.
func TestCatalogWriteRegistersExchange(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Товар", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	plan := &metadata.ExchangePlan{
		Name: "Обмен", Content: []string{"Справочник.Товар"},
		Nodes: []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	plan.Normalize()

	db, ctx := exchDB(t, ent)
	if err := db.SaveExchangeThisNode(ctx, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	lookup := fakeExchangeReg{ents: map[string]*metadata.Entity{"Товар": ent}}
	registrar := func(ctx context.Context, entity *metadata.Entity, id uuid.UUID) error {
		return exchange.RegisterOnSave(ctx, db, []*metadata.ExchangePlan{plan}, entity, id, false)
	}
	root := NewCatalogsRoot(NewStaticCtx(ctx), db, lookup).WithExchangeRegistrar(registrar)

	proxy := root.Get("Товар").(*CatalogProxy)
	w := proxy.CallMethod("создать", nil).(*CatalogRecordWriter)
	w.CallMethod("установитьзначение", []any{"Наименование", "Гвоздь"})
	if ref := w.CallMethod("записать", nil); ref == nil {
		t.Fatal("Записать вернул nil")
	}

	// Изменение зарегистрировано для fil01 (не для center — это наш узел).
	pend, err := db.PendingExchangeChanges(ctx, "Обмен", "fil01")
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 || pend[0].ObjectType != "Товар" {
		t.Fatalf("прямая запись справочника не зарегистрирована: %+v", pend)
	}
}
