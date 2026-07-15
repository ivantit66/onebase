package exchange_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func repostDoc() *metadata.Entity {
	return &metadata.Entity{
		Name: "Продажа", Kind: metadata.KindDocument, Posting: true,
		Fields: []metadata.Field{{Name: "Номер", Type: metadata.FieldTypeString}},
	}
}

func repostPlan(repost bool) *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name:    "Обмен",
		Content: []string{"Документ.Продажа"},
		Nodes:   []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
		Repost:  repost,
	}
	p.Normalize()
	return p
}

func TestRepostFlagAndCallback(t *testing.T) {
	doc := repostDoc()
	res := fakeResolver{"Продажа": doc}
	a, ctxA := newBase(t, doc)
	if err := a.SaveExchangeThisNode(ctxA, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}

	// A: проведённый документ, зарегистрированный для fil01.
	id := uuid.New()
	if err := a.Upsert(ctxA, doc.Name, id, map[string]any{"Номер": "0001"}, doc); err != nil {
		t.Fatal(err)
	}
	if err := a.SetPosted(ctxA, doc.Name, id, true); err != nil {
		t.Fatal(err)
	}
	v, _ := a.EntityVersion(ctxA, doc.Name, id)
	if err := a.RegisterExchangeChange(ctxA, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: doc.Name, ObjectID: id.String(), NodeCode: "fil01", Version: v, ChangedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	data, err := exchange.BuildPackage(ctxA, a, res, repostPlan(true), "fil01")
	if err != nil {
		t.Fatal(err)
	}
	// Пакет несёт признак проведённости.
	pkg, _ := exchange.ParsePackage(data)
	if len(pkg.Objects) != 1 || !pkg.Objects[0].Posted {
		t.Fatalf("Posted должен переноситься в пакет: %+v", pkg.Objects)
	}

	reposter := func() (exchange.ApplyOptions, *[]string) {
		var got []string
		return exchange.ApplyOptions{Repost: func(_ context.Context, entityType string, rid uuid.UUID) error {
			got = append(got, entityType+":"+rid.String())
			return nil
		}}, &got
	}

	// С repost=true и обработчиком — документ ставится в перепроведение.
	b, ctxB := newBase(t, doc)
	optsB, gotB := reposter()
	lrB, err := exchange.ApplyPackage(ctxB, b, res, repostPlan(true), data, optsB)
	if err != nil {
		t.Fatal(err)
	}
	if lrB.Reposted != 1 || len(*gotB) != 1 || (*gotB)[0] != "Продажа:"+id.String() {
		t.Fatalf("перепроведение не вызвано: reposted=%d got=%v", lrB.Reposted, *gotB)
	}
	// Сам документ применён; проведёт его уже репостер (здесь — фейковый, поэтому
	// в БД пока непроведён).
	if row, _ := b.GetByID(ctxB, doc.Name, id, doc); toBoolT(row["posted"]) {
		t.Error("до реального перепроведения документ на приёмнике должен быть непроведён")
	}

	// С repost=false тот же пакет не вызывает перепроведение.
	c, ctxC := newBase(t, doc)
	optsC, gotC := reposter()
	lrC, err := exchange.ApplyPackage(ctxC, c, res, repostPlan(false), data, optsC)
	if err != nil {
		t.Fatal(err)
	}
	if lrC.Reposted != 0 || len(*gotC) != 0 {
		t.Errorf("без repost перепроведения быть не должно: reposted=%d got=%v", lrC.Reposted, *gotC)
	}
}
