package ui

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

func exchTovar() *metadata.Entity {
	return &metadata.Entity{
		Name: "Товар", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
}

func exchPlan() *metadata.ExchangePlan {
	p := &metadata.ExchangePlan{
		Name: "Обмен", Content: []string{"Справочник.Товар"},
		Nodes: []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	p.Normalize()
	return p
}

func newExchangeBaseDB(t *testing.T) (*storage.DB, *runtime.Registry, context.Context, *metadata.Entity) {
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
	ent := exchTovar()
	if err := db.Migrate(ctx, []*metadata.Entity{ent}); err != nil {
		t.Fatal(err)
	}
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: []*metadata.Entity{ent}})
	reg.LoadExchangePlans([]*metadata.ExchangePlan{exchPlan()})
	return db, reg, ctx, ent
}

func TestExchangeHTTPPushPull(t *testing.T) {
	// Приёмник (fil01): сервер с эндпоинтами обмена.
	bDB, bReg, ctx, ent := newExchangeBaseDB(t)
	if err := bDB.SaveExchangeThisNode(ctx, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: bDB, reg: bReg}
	r := chi.NewRouter()
	s.MountExchange(r)
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	// Источник (center): готовим пакет с изменением для fil01.
	aDB, aReg, _, _ := newExchangeBaseDB(t)
	_ = aDB.SaveExchangeThisNode(ctx, "Обмен", "center")
	id := uuid.New()
	if err := aDB.Upsert(ctx, "Товар", id, map[string]any{"Наименование": "Болт"}, ent); err != nil {
		t.Fatal(err)
	}
	if err := exchange.RegisterOnSave(ctx, aDB, aReg.ExchangePlans(), ent, id, false); err != nil {
		t.Fatal(err)
	}
	data, err := exchange.BuildPackage(ctx, aDB, aReg, exchPlan(), "fil01")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Токен на приёмнике не задан → 403.
	if _, err := exchange.PushPackage(ctx, ts.URL, "Обмен", "любой", data); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("без токена ожидали 403, got %v", err)
	}

	// 2. Токен задан, но неверный → 401.
	if err := bDB.SaveExchangeToken(ctx, "Обмен", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.PushPackage(ctx, ts.URL, "Обмен", "wrong", data); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("с неверным токеном ожидали 401, got %v", err)
	}

	// 3. Верный токен → пакет применён.
	res, err := exchange.PushPackage(ctx, ts.URL, "Обмен", "s3cret", data)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("применено %d, want 1", res.Applied)
	}
	if row, err := bDB.GetByID(ctx, "Товар", id, ent); err != nil || row["Наименование"] != "Болт" {
		t.Fatalf("объект не применён на приёмнике: row=%v err=%v", row, err)
	}

	// 4. Pull возвращает пакет (для center у fil01 изменений нет — но конверт валиден).
	pulled, err := exchange.PullPackage(ctx, ts.URL, "Обмен", "s3cret", "center")
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, err := exchange.ParsePackage(pulled); err != nil {
		t.Fatalf("pull вернул невалидный пакет: %v", err)
	}

	// Общий токен плана не должен позволять подменить направление обмена.
	if _, err := exchange.PullPackage(ctx, ts.URL, "Обмен", "s3cret", "fil01"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("pull от имени текущего узла должен быть отклонён, got %v", err)
	}
	forged, err := exchange.ParsePackage(data)
	if err != nil {
		t.Fatal(err)
	}
	forged.FromNode = "fil01"
	forgedData, _ := json.Marshal(forged)
	if _, err := exchange.PushPackage(ctx, ts.URL, "Обмен", "s3cret", forgedData); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("push с неверной парой узлов должен быть отклонён, got %v", err)
	}
}
