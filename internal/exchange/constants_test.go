package exchange_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func newConstBase(t *testing.T) (*storage.DB, context.Context) {
	t.Helper()
	db, ctx := newBase(t) // без сущностей — только служебные таблицы обмена
	if err := db.MigrateConstants(ctx, []*metadata.Constant{{Name: "СтавкаНДС", Type: metadata.FieldTypeString}}); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func constPlan() *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name:    "Обмен",
		Content: []string{"Константа.СтавкаНДС"},
		Nodes:   []metadata.ExchangeNode{{Code: "center", Priority: 10}, {Code: "fil01", Priority: 1}},
	}
	p.Normalize()
	return p
}

func TestConstantRoundTripAndIdempotency(t *testing.T) {
	a, ctxA := newConstBase(t)
	b, ctxB := newConstBase(t)
	res := fakeResolver{} // константам резолвер сущностей не нужен
	plan := constPlan()

	if err := a.SaveExchangeThisNode(ctxA, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveExchangeThisNode(ctxB, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}

	if err := a.SetConstant(ctxA, "СтавкаНДС", "20"); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterConstantOnSave(ctxA, a, []*metadata.ExchangePlan{plan}, "СтавкаНДС"); err != nil {
		t.Fatal(err)
	}

	// Очередь встала к fil01 (kind=constant), источнику center — нет.
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "fil01"); len(p) != 1 || p[0].Kind != storage.ExchangeKindConstant {
		t.Fatalf("константа не встала в очередь fil01: %+v", p)
	}
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "center"); len(p) != 0 {
		t.Fatalf("источнику регистрировать не нужно: %+v", p)
	}

	data, err := exchange.BuildPackage(ctxA, a, res, plan, "fil01")
	if err != nil {
		t.Fatal(err)
	}
	lr, err := exchange.ApplyPackage(ctxB, b, res, plan, data, exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr.Applied != 1 {
		t.Fatalf("применение константы: %+v", lr)
	}
	if v, _ := b.GetConstant(ctxB, "СтавкаНДС"); v != "20" {
		t.Errorf("константа на приёмнике = %v, want 20", v)
	}

	// Идемпотентность: повторная загрузка того же пакета — без изменений.
	lr2, err := exchange.ApplyPackage(ctxB, b, res, plan, data, exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr2.Applied != 0 || lr2.Skipped != 1 {
		t.Errorf("повторная загрузка константы не идемпотентна: %+v", lr2)
	}
}

func TestConstantConflictByTime(t *testing.T) {
	b, ctxB := newConstBase(t)
	res := fakeResolver{}
	plan := constPlan() // by_time по умолчанию
	if err := b.SaveExchangeThisNode(ctxB, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}

	// B локально правит константу (значение "18") и держит неотправленную правку
	// узлу center со временем 2000.
	if err := b.SetConstant(ctxB, "СтавкаНДС", "18"); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterExchangeChange(ctxB, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: "СтавкаНДС", ObjectID: "", NodeCode: "center",
		Kind: storage.ExchangeKindConstant, Version: 2000, ChangedAt: 2000,
	}); err != nil {
		t.Fatal(err)
	}

	incoming := func(changedAt int64, value string, msgNo int64) []byte {
		pkg := exchange.Package{
			Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", MessageNo: msgNo,
			Objects: []exchange.PackageObject{{
				Kind: storage.ExchangeKindConstant, Type: "СтавкаНДС", ChangedAt: changedAt,
				Fields: map[string]any{"value": value},
			}},
		}
		data, _ := json.Marshal(pkg)
		return data
	}

	// Входящий старше (500 < 2000) → by_time: локальная правка побеждает.
	lr, err := exchange.ApplyPackage(ctxB, b, res, plan, incoming(500, "20", 1), exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr.Conflicts != 1 {
		t.Fatalf("ожидали конфликт, got %+v", lr)
	}
	if v, _ := b.GetConstant(ctxB, "СтавкаНДС"); v != "18" {
		t.Errorf("by_time: локальная (позже) должна победить, got %v", v)
	}

	// Входящий новее (3000 > 2000) → by_time: входящий побеждает.
	lr2, err := exchange.ApplyPackage(ctxB, b, res, plan, incoming(3000, "21", 2), exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr2.Applied != 1 {
		t.Fatalf("новее должно примениться: %+v", lr2)
	}
	if v, _ := b.GetConstant(ctxB, "СтавкаНДС"); v != "21" {
		t.Errorf("by_time: новее должно победить, got %v", v)
	}
}
