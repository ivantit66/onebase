package exchange_test

import (
	"context"
	"testing"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// farFuture — заведомо более поздний changed_at, чем реальный now-millis записи
// (для детерминированного порядка удаления над «водяным знаком»).
const farFuture = int64(1) << 50

func infoRegCodes() *metadata.InfoRegister {
	return &metadata.InfoRegister{
		Name:       "СоответствиеКодов",
		Dimensions: []metadata.Field{{Name: "Ключ", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Значение", Type: metadata.FieldTypeString}},
	}
}

func infoRegPlan() *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name:    "Обмен",
		Content: []string{"РегистрСведений.СоответствиеКодов"},
		Nodes:   []metadata.ExchangeNode{{Code: "center", Priority: 10}, {Code: "fil01", Priority: 1}},
	}
	p.Normalize()
	return p
}

func newInfoRegBase(t *testing.T) (*storage.DB, context.Context, *metadata.InfoRegister) {
	t.Helper()
	db, ctx := newBase(t)
	ir := infoRegCodes()
	if err := db.MigrateInfoRegisters(ctx, []*metadata.InfoRegister{ir}); err != nil {
		t.Fatal(err)
	}
	return db, ctx, ir
}

func TestInfoRegRoundTripDeleteIdempotent(t *testing.T) {
	a, ctxA, ir := newInfoRegBase(t)
	b, ctxB, _ := newInfoRegBase(t)
	res := fakeResolverIR{inforegs: map[string]*metadata.InfoRegister{"СоответствиеКодов": ir}}
	plan := infoRegPlan()
	if err := a.SaveExchangeThisNode(ctxA, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveExchangeThisNode(ctxB, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}

	dims := map[string]any{"Ключ": "A1"}
	if err := a.InfoRegSet(ctxA, ir, dims, map[string]any{"Значение": "первое"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterInfoRegOnSave(ctxA, a, []*metadata.ExchangePlan{plan}, ir, dims, false); err != nil {
		t.Fatal(err)
	}

	pend, _ := a.PendingExchangeChanges(ctxA, "Обмен", "fil01")
	if len(pend) != 1 || pend[0].Kind != storage.ExchangeKindInfoReg || pend[0].Deletion {
		t.Fatalf("запись регистра не встала в очередь: %+v", pend)
	}
	key := pend[0].ObjectID // каноничный ключ измерений
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
		t.Fatalf("применение записи регистра: %+v", lr)
	}
	if rec, err := b.InfoRegGet(ctxB, ir, dims); err != nil || rec["Значение"] != "первое" {
		t.Fatalf("запись не синхронизирована: rec=%v err=%v", rec, err)
	}

	// Идемпотентность: повтор пакета ничего не меняет.
	lr2, err := exchange.ApplyPackage(ctxB, b, res, plan, data, exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr2.Applied != 0 || lr2.Skipped != 1 {
		t.Errorf("повтор не идемпотентен: %+v", lr2)
	}

	// Удаление записи (регистрируем с явным более поздним changed_at).
	if err := a.InfoRegDelete(ctxA, ir, dims, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterExchangeChange(ctxA, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: "СоответствиеКодов", ObjectID: key, NodeCode: "fil01",
		Kind: storage.ExchangeKindInfoReg, Deletion: true, Version: farFuture, ChangedAt: farFuture,
	}); err != nil {
		t.Fatal(err)
	}
	dataDel, err := exchange.BuildPackage(ctxA, a, res, plan, "fil01")
	if err != nil {
		t.Fatal(err)
	}
	lrDel, err := exchange.ApplyPackage(ctxB, b, res, plan, dataDel, exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lrDel.Deleted != 1 {
		t.Fatalf("удаление не применилось: %+v", lrDel)
	}
	if rec, err := b.InfoRegGet(ctxB, ir, dims); err == nil && rec != nil {
		t.Errorf("запись должна быть удалена, но найдена: %v", rec)
	}
}

// Периодические регистры пока не синхронизируются — регистрация их пропускает.
func TestInfoRegPeriodicSkipped(t *testing.T) {
	a, ctxA := newBase(t)
	pir := &metadata.InfoRegister{
		Name: "Курс", Periodic: true,
		Dimensions: []metadata.Field{{Name: "Валюта", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Значение", Type: metadata.FieldTypeNumber}},
	}
	plan := &metadata.ExchangePlan{
		Name: "Обмен", Content: []string{"РегистрСведений.Курс"},
		Nodes: []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	plan.Normalize()
	if err := a.SaveExchangeThisNode(ctxA, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterInfoRegOnSave(ctxA, a, []*metadata.ExchangePlan{plan}, pir, map[string]any{"Валюта": "USD"}, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "fil01"); len(p) != 0 {
		t.Errorf("периодический регистр не должен регистрироваться, got %+v", p)
	}
}
