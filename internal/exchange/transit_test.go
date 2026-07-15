package exchange_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func starPlan() *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name:    "Обмен",
		Content: []string{"Справочник.Товар"},
		Nodes: []metadata.ExchangeNode{
			{Code: "hub", Role: metadata.RoleHub},
			{Code: "filA", Role: metadata.RoleSpoke},
			{Code: "filB", Role: metadata.RoleSpoke},
		},
	}
	p.Normalize()
	return p
}

func mustNode(t *testing.T, db *storage.DB, ctx context.Context, node string) {
	t.Helper()
	if err := db.SaveExchangeThisNode(ctx, "Обмен", node); err != nil {
		t.Fatal(err)
	}
}

// Спица A → хаб → спица B: изменение спицы регистрируется только хабу, хаб
// ретранслирует его остальным спицам, и оно доходит до другой спицы.
func TestHubTransitSpokeToSpoke(t *testing.T) {
	ent := catalogTovar()
	res := fakeResolver{"Товар": ent}
	a, ctxA := newBase(t, ent) // спица filA
	h, ctxH := newBase(t, ent) // хаб
	b, ctxB := newBase(t, ent) // спица filB
	plan := starPlan()
	mustNode(t, a, ctxA, "filA")
	mustNode(t, h, ctxH, "hub")
	mustNode(t, b, ctxB, "filB")

	// 1. Спица A правит товар → регистрируется ТОЛЬКО хабу, не другой спице.
	id := uuid.New()
	if err := a.Upsert(ctxA, ent.Name, id, map[string]any{"Наименование": "Болт"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctxA, a, []*metadata.ExchangePlan{plan}, ent, id, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "hub"); len(p) != 1 {
		t.Fatalf("спица должна регистрировать изменение хабу, got hub=%d", len(p))
	}
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "filB"); len(p) != 0 {
		t.Fatalf("спица не должна регистрировать изменение другой спице, got filB=%d", len(p))
	}

	// 2. A → hub. Хаб применяет и ретранслирует изменение спице B (не обратно A).
	data1, err := exchange.BuildPackage(ctxA, a, res, plan, "hub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.ApplyPackage(ctxH, h, res, plan, data1, exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if p, _ := h.PendingExchangeChanges(ctxH, "Обмен", "filB"); len(p) != 1 {
		t.Fatalf("хаб должен ретранслировать изменение спице filB, got %d", len(p))
	}
	if p, _ := h.PendingExchangeChanges(ctxH, "Обмен", "filA"); len(p) != 0 {
		t.Fatalf("хаб не должен слать изменение обратно источнику filA, got %d", len(p))
	}

	// 3. hub → B. Изменение спицы A доходит до спицы B через хаб.
	data2, err := exchange.BuildPackage(ctxH, h, res, plan, "filB")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.ApplyPackage(ctxB, b, res, plan, data2, exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if row, err := b.GetByID(ctxB, ent.Name, id, ent); err != nil || row["Наименование"] != "Болт" {
		t.Fatalf("изменение не дошло до спицы B через хаб: row=%v err=%v", row, err)
	}

	// B — спица (не хаб): дальше не ретранслирует (нет петли).
	if p, _ := b.PendingExchangeChanges(ctxB, "Обмен", "hub"); len(p) != 0 {
		t.Errorf("спица B не должна ничего ретранслировать, got hub=%d", len(p))
	}
}
