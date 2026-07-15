package exchange_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func planC(rule string, centerPrio, filPrio int) *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name: "Обмен", Conflict: rule, Content: []string{"Справочник.Товар"},
		Nodes: []metadata.ExchangeNode{{Code: "center", Priority: centerPrio}, {Code: "fil01", Priority: filPrio}},
	}
	p.Normalize()
	return p
}

// setupConflict готовит приёмник (this-node=fil01) с локальным объектом и его
// неотправленной правкой узлу center (changed_at=1000) — почва для конфликта.
func setupConflict(t *testing.T) (*storage.DB, context.Context, *metadata.Entity, uuid.UUID) {
	t.Helper()
	ent := catalogTovar()
	b, ctxB := newBase(t, ent)
	if err := b.SaveExchangeThisNode(ctxB, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := b.Upsert(ctxB, ent.Name, id, map[string]any{"Наименование": "локальное", "Цена": "1"}, ent); err != nil {
		t.Fatal(err)
	}
	v, _ := b.EntityVersion(ctxB, ent.Name, id)
	if err := b.RegisterExchangeChange(ctxB, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: "Товар", ObjectID: id.String(), NodeCode: "center", Version: v, ChangedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	return b, ctxB, ent, id
}

func incomingPkg(id uuid.UUID, version, changedAt int64) []byte {
	pkg := exchange.Package{
		Format: exchange.FormatV1, Plan: "Обмен", FromNode: "CENTER", ToNode: "FIL01", MessageNo: 1,
		Objects: []exchange.PackageObject{{
			Type: "товар", ID: strings.ToUpper(id.String()), Version: version, ChangedAt: changedAt,
			Fields: map[string]any{"Наименование": "изЦентра", "Цена": "999", "Дата": nil, "Активен": false},
		}},
	}
	data, _ := json.Marshal(pkg)
	return data
}

func name(t *testing.T, db *storage.DB, ctx context.Context, ent *metadata.Entity, id uuid.UUID) string {
	t.Helper()
	row, err := db.GetByID(ctx, ent.Name, id, ent)
	if err != nil {
		t.Fatal(err)
	}
	return row["Наименование"].(string)
}

func TestConflictByTime(t *testing.T) {
	res := fakeResolver{"Товар": catalogTovar()}

	// Входящее позже (2000 > 1000) → побеждает входящее.
	b, ctx, ent, id := setupConflict(t)
	lr, err := exchange.ApplyPackage(ctx, b, res, planC("by_time", 0, 0), incomingPkg(id, 2, 2000), exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr.Conflicts != 1 || lr.Applied != 1 {
		t.Fatalf("ожидали conflict+applied: %+v", lr)
	}
	if got := name(t, b, ctx, ent, id); got != "изЦентра" {
		t.Errorf("by_time позже → должно победить входящее, got %q", got)
	}
	if pending, _ := b.PendingExchangeChanges(ctx, "Обмен", "center"); len(pending) != 0 {
		t.Errorf("проигравшая локальная правка осталась в очереди: %+v", pending)
	}

	// Входящее раньше (500 < 1000) → побеждает локальное.
	b2, ctx2, ent2, id2 := setupConflict(t)
	lr2, err := exchange.ApplyPackage(ctx2, b2, res, planC("by_time", 0, 0), incomingPkg(id2, 2, 500), exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr2.Conflicts != 1 || lr2.Skipped != 1 {
		t.Fatalf("ожидали conflict+skipped: %+v", lr2)
	}
	if got := name(t, b2, ctx2, ent2, id2); got != "локальное" {
		t.Errorf("by_time раньше → должно победить локальное, got %q", got)
	}
}

func TestConflictByTimeEqualTimestampUsesStableNodeTieBreaker(t *testing.T) {
	b, ctx, ent, id := setupConflict(t)
	lr, err := exchange.ApplyPackage(ctx, b, fakeResolver{"Товар": ent},
		planC("by_time", 0, 0), incomingPkg(id, 2, 1000), exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// fil01 sorts after center, so both peers make the same choice: fil01 wins.
	if lr.Conflicts != 1 || lr.Skipped != 1 {
		t.Fatalf("ожидали детерминированный выбор локальной версии: %+v", lr)
	}
	if got := name(t, b, ctx, ent, id); got != "локальное" {
		t.Fatalf("при равном времени должен победить fil01, got %q", got)
	}
}

func TestAcknowledgedChangeIsNotConflict(t *testing.T) {
	b, ctx, ent, id := setupConflict(t)
	pending, err := b.PendingExchangeChanges(ctx, "Обмен", "center")
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %+v, %v", pending, err)
	}
	if n, err := b.NextExchangeMessageNo(ctx, "Обмен", "center"); err != nil || n != 1 {
		t.Fatalf("next message: %d, %v", n, err)
	}
	if err := b.MarkExchangeChangesSent(ctx, pending, 1); err != nil {
		t.Fatal(err)
	}

	pkg := exchange.Package{
		Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", ToNode: "fil01",
		MessageNo: 2, AckNo: 1,
		Objects: []exchange.PackageObject{{
			Type: "Товар", ID: id.String(), Version: 2, ChangedAt: 2000,
			Fields: map[string]any{"Наименование": "после подтверждения", "Цена": "2", "Дата": nil, "Активен": false},
		}},
	}
	data, _ := json.Marshal(pkg)
	// У fil01 приоритет выше. Если подтверждённая строка ошибочно считается
	// конфликтом, входящий объект будет отвергнут правилом by_node_priority.
	lr, err := exchange.ApplyPackage(ctx, b, fakeResolver{"Товар": ent},
		planC("by_node_priority", 1, 10), data, exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr.Conflicts != 0 || lr.Applied != 1 {
		t.Fatalf("подтверждённая правка не должна быть конфликтом: %+v", lr)
	}
	if got := name(t, b, ctx, ent, id); got != "после подтверждения" {
		t.Fatalf("входящая версия не применена: %q", got)
	}
}

func TestConflictByNodePriority(t *testing.T) {
	res := fakeResolver{"Товар": catalogTovar()}

	// center (10) > fil01 (1) → побеждает входящее (от center).
	b, ctx, ent, id := setupConflict(t)
	if _, err := exchange.ApplyPackage(ctx, b, res, planC("by_node_priority", 10, 1), incomingPkg(id, 2, 500), exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := name(t, b, ctx, ent, id); got != "изЦентра" {
		t.Errorf("приоритет center выше → должно победить входящее, got %q", got)
	}

	// center (1) < fil01 (10) → побеждает локальное, даже если входящее позже.
	b2, ctx2, ent2, id2 := setupConflict(t)
	if _, err := exchange.ApplyPackage(ctx2, b2, res, planC("by_node_priority", 1, 10), incomingPkg(id2, 2, 9999), exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := name(t, b2, ctx2, ent2, id2); got != "локальное" {
		t.Errorf("приоритет fil01 выше → должно победить локальное, got %q", got)
	}
}

type fakeHook struct {
	incomingWins bool
	sawLocal     string
	sawIncoming  string
}

func (f *fakeHook) ResolveConflict(ctx context.Context, plan *metadata.ExchangePlan, entity *metadata.Entity, local, incoming *exchange.PackageObject) (bool, error) {
	f.sawLocal, _ = local.Fields["Наименование"].(string)
	f.sawIncoming, _ = incoming.Fields["Наименование"].(string)
	return f.incomingWins, nil
}

func TestConflictByHook(t *testing.T) {
	res := fakeResolver{"Товар": catalogTovar()}
	b, ctx, ent, id := setupConflict(t)
	hook := &fakeHook{incomingWins: true}
	if _, err := exchange.ApplyPackage(ctx, b, res, planC("hook", 0, 0), incomingPkg(id, 2, 500), exchange.ApplyOptions{Hook: hook}); err != nil {
		t.Fatal(err)
	}
	if hook.sawLocal != "локальное" || hook.sawIncoming != "изЦентра" {
		t.Errorf("хук получил не те объекты: local=%q incoming=%q", hook.sawLocal, hook.sawIncoming)
	}
	if got := name(t, b, ctx, ent, id); got != "изЦентра" {
		t.Errorf("хук решил в пользу входящего, got %q", got)
	}
}

func TestConflictHookNilFallsBackToTime(t *testing.T) {
	res := fakeResolver{"Товар": catalogTovar()}
	// conflict: hook, но Hook не подключён → откат к by_time; входящее позже → wins.
	b, ctx, ent, id := setupConflict(t)
	if _, err := exchange.ApplyPackage(ctx, b, res, planC("hook", 0, 0), incomingPkg(id, 2, 2000), exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := name(t, b, ctx, ent, id); got != "изЦентра" {
		t.Errorf("без хука ожидался откат к by_time (входящее позже), got %q", got)
	}
}
