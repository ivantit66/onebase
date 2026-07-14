package ui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

const conflictHookSrc = `
Функция ПриКонфликтеОбмена(Локальный, Входящий) Экспорт
    Возврат Входящий.Наименование = "победитель";
КонецФункции
`

func TestExchangeHookResolvesConflict(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureExchangeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	ent := &metadata.Entity{
		Name: "Товар", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{ent}); err != nil {
		t.Fatal(err)
	}

	prog, err := parser.New(lexer.New(conflictHookSrc, "общий.module.os")).ParseProgram()
	if err != nil {
		t.Fatalf("parse hook module: %v", err)
	}
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: []*metadata.Entity{ent}})
	reg.LoadModules(map[string]*ast.Program{"Общий": prog})
	interp := interpreter.New()
	interp.LookupProc = reg.GetModuleProc

	plan := &metadata.ExchangePlan{
		Name: "Обмен", Conflict: "hook", Content: []string{"Справочник.Товар"},
		Nodes: []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	plan.Normalize()
	_ = db.SaveExchangeThisNode(ctx, "Обмен", "fil01")
	hook := NewExchangeHook(db, reg, interp)

	// Готовит конфликт: локальный объект + неотправленная правка узлу center.
	seedConflict := func(localName string) uuid.UUID {
		id := uuid.New()
		if err := db.Upsert(ctx, "Товар", id, map[string]any{"Наименование": localName}, ent); err != nil {
			t.Fatal(err)
		}
		v, _ := db.EntityVersion(ctx, "Товар", id)
		if err := db.RegisterExchangeChange(ctx, storage.ExchangeChange{
			Plan: "Обмен", ObjectType: "Товар", ObjectID: id.String(), NodeCode: "center", Version: v, ChangedAt: 1000,
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	incoming := func(id uuid.UUID, name string) []byte {
		pkg := exchange.Package{
			Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", ToNode: "fil01", MessageNo: 1,
			Objects: []exchange.PackageObject{{Type: "Товар", ID: id.String(), Version: 2, ChangedAt: 500,
				Fields: map[string]any{"Наименование": name}}},
		}
		data, _ := json.Marshal(pkg)
		return data
	}
	nameOf := func(id uuid.UUID) string {
		row, err := db.GetByID(ctx, "Товар", id, ent)
		if err != nil {
			t.Fatal(err)
		}
		return row["Наименование"].(string)
	}

	// Хук решает в пользу входящего (имя = "победитель").
	winID := seedConflict("локальное")
	res, err := exchange.ApplyPackage(ctx, db, reg, plan, incoming(winID, "победитель"), exchange.ApplyOptions{Hook: hook})
	if err != nil {
		t.Fatal(err)
	}
	if res.Conflicts != 1 {
		t.Fatalf("ожидали конфликт, got %+v", res)
	}
	if got := nameOf(winID); got != "победитель" {
		t.Errorf("хук решил в пользу входящего, но объект = %q", got)
	}

	// Хук решает в пользу локального (имя ≠ "победитель").
	loseID := seedConflict("локальное2")
	if _, err := exchange.ApplyPackage(ctx, db, reg, plan, incoming(loseID, "проигравший"), exchange.ApplyOptions{Hook: hook}); err != nil {
		t.Fatal(err)
	}
	if got := nameOf(loseID); got != "локальное2" {
		t.Errorf("хук решил в пользу локального, но объект = %q", got)
	}
}
