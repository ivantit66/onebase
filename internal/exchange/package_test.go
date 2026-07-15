package exchange_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

type fakeResolver map[string]*metadata.Entity

func (f fakeResolver) GetEntity(name string) *metadata.Entity {
	if e, ok := f[name]; ok {
		return e
	}
	for k, e := range f {
		if strings.EqualFold(k, name) {
			return e
		}
	}
	return nil
}

func newBase(t *testing.T, entities ...*metadata.Entity) (*storage.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureExchangeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, entities); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func catalogTovar() *metadata.Entity {
	return &metadata.Entity{
		Name: "Товар", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
			{Name: "Дата", Type: metadata.FieldTypeDate},
			{Name: "Активен", Type: metadata.FieldTypeBool},
		},
	}
}

func planTovar() *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name:    "Обмен",
		Content: []string{"Справочник.Товар"},
		Nodes:   []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	p.Normalize()
	return p
}

// registerObj создаёт объект в базе и регистрирует его изменение для узла с
// заданным временем (для детерминированного порядка в пакете).
func registerObj(t *testing.T, db *storage.DB, ctx context.Context, ent *metadata.Entity, id uuid.UUID, fields map[string]any, node string, changedAt int64) {
	t.Helper()
	if err := db.Upsert(ctx, ent.Name, id, fields, ent); err != nil {
		t.Fatal(err)
	}
	v, err := db.EntityVersion(ctx, ent.Name, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterExchangeChange(ctx, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: ent.Name, ObjectID: id.String(),
		NodeCode: node, Version: v, ChangedAt: changedAt,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPackageRoundTrip(t *testing.T) {
	ent := catalogTovar()
	res := fakeResolver{"Товар": ent}
	a, ctxA := newBase(t, ent)
	b, ctxB := newBase(t, ent)
	_ = a.SaveExchangeThisNode(ctxA, "Обмен", "center")
	_ = b.SaveExchangeThisNode(ctxB, "Обмен", "fil01")

	id := uuid.New()
	registerObj(t, a, ctxA, ent, id, map[string]any{
		"Наименование": "Гвоздь", "Цена": "123.45", "Дата": "2024-03-15", "Активен": true,
	}, "fil01", 1000)

	data, err := exchange.BuildPackage(ctxA, a, res, planTovar(), "fil01")
	if err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}
	lr, err := exchange.ApplyPackage(ctxB, b, res, planTovar(), data, exchange.ApplyOptions{})
	if err != nil {
		t.Fatalf("ApplyPackage: %v", err)
	}
	if lr.Applied != 1 || lr.Skipped != 0 {
		t.Fatalf("первичная загрузка: %+v", lr)
	}

	row, err := b.GetByID(ctxB, ent.Name, id, ent)
	if err != nil {
		t.Fatalf("объект не загружен: %v", err)
	}
	if row["Наименование"] != "Гвоздь" {
		t.Errorf("Наименование = %v", row["Наименование"])
	}
	if got := strFmt(row["Цена"]); got != "123.45" {
		t.Errorf("Цена = %q, want 123.45 (без потери точности)", got)
	}
	if got := toBoolT(row["Активен"]); !got {
		t.Errorf("Активен = %v, want true", row["Активен"])
	}
	if v, _ := b.EntityVersion(ctxB, ent.Name, id); v != 1 {
		t.Errorf("_version на приёмнике = %d, want 1 (версия источника)", v)
	}

	// Идемпотентность: повторная загрузка того же пакета — без изменений.
	lr2, err := exchange.ApplyPackage(ctxB, b, res, planTovar(), data, exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lr2.Applied != 0 || lr2.Skipped != 1 {
		t.Errorf("повторная загрузка не идемпотентна: %+v", lr2)
	}
	if v, _ := b.EntityVersion(ctxB, ent.Name, id); v != 1 {
		t.Errorf("_version после повтора = %d, want 1 (не наращивается)", v)
	}
}

func TestPackageVersionRule(t *testing.T) {
	ent := catalogTovar()
	res := fakeResolver{"Товар": ent}
	a, ctxA := newBase(t, ent)
	b, ctxB := newBase(t, ent)
	_ = a.SaveExchangeThisNode(ctxA, "Обмен", "center")
	_ = b.SaveExchangeThisNode(ctxB, "Обмен", "fil01")

	id := uuid.New()
	// В B заранее лежит объект версии 3.
	if err := b.Upsert(ctxB, ent.Name, id, map[string]any{"Наименование": "старое"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := b.SetExchangeObjectState(ctxB, ent, id, 3, false); err != nil {
		t.Fatal(err)
	}

	// Пакет из A с версией 2 (не новее) — должен быть пропущен.
	registerObj(t, a, ctxA, ent, id, map[string]any{"Наименование": "версия2"}, "fil01", 1000)
	if err := markVersion(a, ctxA, ent, id, 2); err != nil {
		t.Fatal(err)
	}
	data, _ := exchange.BuildPackage(ctxA, a, res, planTovar(), "fil01")
	lr, _ := exchange.ApplyPackage(ctxB, b, res, planTovar(), data, exchange.ApplyOptions{})
	if lr.Skipped != 1 || lr.Applied != 0 {
		t.Errorf("версия ≤ локальной должна пропускаться: %+v", lr)
	}
	if row, _ := b.GetByID(ctxB, ent.Name, id, ent); row["Наименование"] != "старое" {
		t.Errorf("объект перезаписан более старой версией: %v", row["Наименование"])
	}
}

func TestPackageTableParts(t *testing.T) {
	doc := &metadata.Entity{
		Name: "Продажа", Kind: metadata.KindDocument,
		Fields: []metadata.Field{{Name: "Номер", Type: metadata.FieldTypeString}},
		TableParts: []metadata.TablePart{{
			Name: "Строки",
			Fields: []metadata.Field{
				{Name: "Наименование", Type: metadata.FieldTypeString},
				{Name: "Количество", Type: metadata.FieldTypeNumber},
			},
		}},
	}
	res := fakeResolver{"Продажа": doc}
	a, ctxA := newBase(t, doc)
	b, ctxB := newBase(t, doc)
	_ = a.SaveExchangeThisNode(ctxA, "Обмен", "center")
	_ = b.SaveExchangeThisNode(ctxB, "Обмен", "fil01")

	id := uuid.New()
	if err := a.Upsert(ctxA, doc.Name, id, map[string]any{"Номер": "0001"}, doc); err != nil {
		t.Fatal(err)
	}
	if err := a.UpsertTablePartRows(ctxA, doc.Name, "Строки", id, []map[string]any{
		{"Наименование": "Гвоздь", "Количество": "10"},
		{"Наименование": "Шуруп", "Количество": "5"},
	}, doc.TableParts[0]); err != nil {
		t.Fatal(err)
	}
	v, _ := a.EntityVersion(ctxA, doc.Name, id)
	if err := a.RegisterExchangeChange(ctxA, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: doc.Name, ObjectID: id.String(), NodeCode: "fil01", Version: v, ChangedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	plan := &metadata.ExchangePlan{Name: "Обмен", Content: []string{"Документ.Продажа"},
		Nodes: []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}}}
	plan.Normalize()

	data, err := exchange.BuildPackage(ctxA, a, res, plan, "fil01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.ApplyPackage(ctxB, b, res, plan, data, exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	rows, err := b.GetTablePartRows(ctxB, doc.Name, "Строки", id, doc.TableParts[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("строк ТЧ на приёмнике = %d, want 2", len(rows))
	}
	if rows[0]["Наименование"] != "Гвоздь" || strFmt(rows[1]["Количество"]) != "5" {
		t.Errorf("ТЧ не совпала: %+v", rows)
	}
	// Документ приходит непроведённым.
	if row, _ := b.GetByID(ctxB, doc.Name, id, doc); toBoolT(row["posted"]) {
		t.Error("документ должен прийти непроведённым (posted=false)")
	}

	// Очистка ТЧ тоже должна реплицироваться. Пустой слайс в пакете отличает
	// «очистить» от старого/частичного пакета, где ключ ТЧ отсутствует.
	if err := a.Upsert(ctxA, doc.Name, id, map[string]any{"Номер": "0001"}, doc); err != nil {
		t.Fatal(err)
	}
	if err := a.UpsertTablePartRows(ctxA, doc.Name, "Строки", id, []map[string]any{}, doc.TableParts[0]); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctxA, a, []*metadata.ExchangePlan{plan}, doc, id, false); err != nil {
		t.Fatal(err)
	}
	data, err = exchange.BuildPackage(ctxA, a, res, plan, "fil01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.ApplyPackage(ctxB, b, res, plan, data, exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	rows, err = b.GetTablePartRows(ctxB, doc.Name, "Строки", id, doc.TableParts[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("очистка ТЧ не реплицировалась: %+v", rows)
	}
}

func TestPackageRejectsEntityOutsidePlanContent(t *testing.T) {
	allowed := catalogTovar()
	other := &metadata.Entity{
		Name: "Секрет", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	db, ctx := newBase(t, allowed, other)
	_ = db.SaveExchangeThisNode(ctx, "Обмен", "fil01")
	pkg := exchange.Package{
		Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", ToNode: "fil01", MessageNo: 1,
		Objects: []exchange.PackageObject{{
			Type: other.Name, ID: uuid.NewString(), Version: 1, ChangedAt: 1,
			Fields: map[string]any{"Наименование": "нельзя"},
		}},
	}
	data, _ := json.Marshal(pkg)
	_, err := exchange.ApplyPackage(ctx, db, fakeResolver{"Товар": allowed, "Секрет": other}, planTovar(), data, exchange.ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "не входит в состав") {
		t.Fatalf("ожидался отказ для сущности вне состава плана, got %v", err)
	}
}

func TestPackageAckDrains(t *testing.T) {
	ent := catalogTovar()
	res := fakeResolver{"Товар": ent}
	a, ctxA := newBase(t, ent)
	b, ctxB := newBase(t, ent)
	if err := a.SaveExchangeThisNode(ctxA, "Обмен", "center"); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveExchangeThisNode(ctxB, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}
	plan := planTovar()

	// A правит X → очередь к fil01.
	id := uuid.New()
	registerObj(t, a, ctxA, ent, id, map[string]any{"Наименование": "X"}, "fil01", 1000)
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "fil01"); len(p) != 1 {
		t.Fatalf("ожидали 1 в очереди A→fil01, got %d", len(p))
	}

	// A→fil01 (msg1, ack=0); B принимает.
	pkgA, err := exchange.BuildPackage(ctxA, a, res, plan, "fil01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.ApplyPackage(ctxB, b, res, plan, pkgA, exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	// Очередь A→fil01 ещё не пуста (нет подтверждения).
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "fil01"); len(p) != 1 {
		t.Fatalf("до ack очередь A→fil01 должна оставаться, got %d", len(p))
	}

	// Обратный пакет B→center несёт ack=1 (B принял сообщение №1 от center).
	pkgB, err := exchange.BuildPackage(ctxB, b, res, plan, "center")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.ApplyPackage(ctxA, a, res, plan, pkgB, exchange.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	// Теперь очередь A→fil01 очищена подтверждением.
	if p, _ := a.PendingExchangeChanges(ctxA, "Обмен", "fil01"); len(p) != 0 {
		t.Errorf("после ack очередь A→fil01 должна быть пуста, got %+v", p)
	}
}

func TestPhysicalDeleteBuildsTombstoneAndPreservesRemoteFields(t *testing.T) {
	ent := catalogTovar()
	res := fakeResolver{"Товар": ent}
	plan := planTovar()
	source, sourceCtx := newBase(t, ent)
	target, targetCtx := newBase(t, ent)
	if err := source.SaveExchangeThisNode(sourceCtx, plan.Name, "center"); err != nil {
		t.Fatal(err)
	}
	if err := target.SaveExchangeThisNode(targetCtx, plan.Name, "fil01"); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	if err := source.Upsert(sourceCtx, ent.Name, id, map[string]any{"Наименование": "на источнике"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := target.Upsert(targetCtx, ent.Name, id, map[string]any{"Наименование": "сохранить на приёмнике"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := source.WithTx(sourceCtx, func(txCtx context.Context) error {
		if err := exchange.RegisterOnDelete(txCtx, source, []*metadata.ExchangePlan{plan}, ent, id); err != nil {
			return err
		}
		return source.Delete(txCtx, ent.Name, id)
	}); err != nil {
		t.Fatal(err)
	}

	data, err := exchange.BuildPackage(sourceCtx, source, res, plan, "fil01")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := exchange.ParsePackage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Objects) != 1 || !pkg.Objects[0].Tombstone {
		t.Fatalf("физическое удаление должно выгружаться tombstone: %+v", pkg.Objects)
	}
	result, err := exchange.ApplyPackage(targetCtx, target, res, plan, data, exchange.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 {
		t.Fatalf("ожидалась одна пометка удаления: %+v", result)
	}
	row, err := target.GetByID(targetCtx, ent.Name, id, ent)
	if err != nil {
		t.Fatal(err)
	}
	if row["Наименование"] != "сохранить на приёмнике" || !toBoolT(row["deletion_mark"]) {
		t.Fatalf("tombstone должен сохранить данные и поставить пометку: %+v", row)
	}
}

func TestInvalidObjectDoesNotApplyAcknowledgement(t *testing.T) {
	ent := catalogTovar()
	db, ctx := newBase(t, ent)
	if err := db.SaveExchangeThisNode(ctx, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}
	change := storage.ExchangeChange{
		Plan: "Обмен", ObjectType: ent.Name, ObjectID: uuid.NewString(), NodeCode: "center",
		Version: 1, ChangedAt: 1000,
	}
	if err := db.RegisterExchangeChange(ctx, change); err != nil {
		t.Fatal(err)
	}
	messageNo, err := db.NextExchangeMessageNo(ctx, "Обмен", "center")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkExchangeChangesSent(ctx, []storage.ExchangeChange{change}, messageNo); err != nil {
		t.Fatal(err)
	}

	pkg := exchange.Package{
		Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", ToNode: "fil01",
		MessageNo: 1, AckNo: messageNo,
		Objects: []exchange.PackageObject{{
			Type: ent.Name, ID: uuid.NewString(), Version: 1, ChangedAt: 1000,
			Fields: map[string]any{"Наименование": "x", "НеизвестноеПоле": "y"},
		}},
	}
	data, _ := json.Marshal(pkg)
	if _, err := exchange.ApplyPackage(ctx, db, fakeResolver{"Товар": ent}, planTovar(), data, exchange.ApplyOptions{}); err == nil {
		t.Fatal("несовместимый объект должен отклоняться")
	}
	pending, err := db.PendingExchangeChanges(ctx, "Обмен", "center")
	if err != nil || len(pending) != 1 {
		t.Fatalf("ACK из отклонённого пакета не должен очищать очередь: %+v, %v", pending, err)
	}
}

func TestParsePackageRejectsNonDeletingTombstone(t *testing.T) {
	pkg := exchange.Package{
		Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", ToNode: "fil01", MessageNo: 1,
		Objects: []exchange.PackageObject{{
			Type: "Товар", ID: uuid.NewString(), Version: 1, ChangedAt: 1,
			Tombstone: true,
		}},
	}
	data, _ := json.Marshal(pkg)
	if _, err := exchange.ParsePackage(data); err == nil {
		t.Fatal("tombstone без deletion=true должен отклоняться")
	}
}

func markVersion(db *storage.DB, ctx context.Context, ent *metadata.Entity, id uuid.UUID, v int64) error {
	// Обновляем и объект, и строку очереди до одной версии.
	if err := db.SetExchangeObjectState(ctx, ent, id, v, false); err != nil {
		return err
	}
	return db.RegisterExchangeChange(ctx, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: ent.Name, ObjectID: id.String(), NodeCode: "fil01", Version: v, ChangedAt: 1000,
	})
}

func strFmt(v any) string {
	if v == nil {
		return ""
	}
	type stringer interface{ String() string }
	if s, ok := v.(stringer); ok {
		return s.String()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toBoolT(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	}
	return false
}
