package dslvars

// Проверяет, что правило конфликта hook (ПриКонфликтеОбмена) срабатывает при
// загрузке пакета ИЗ DSL (ПланыОбмена.X.ЗагрузитьПакет), а не только на Go-путях.
// Раньше DSL-путь молча откатывался к by_time. Дискриминатор: у входящего объекта
// changed_at РАНЬШЕ локального — при by_time он бы проиграл; если объект всё-таки
// заменён входящим, значит отработал именно hook.

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

const exchangeHookModuleSrc = `
Функция ПриКонфликтеОбмена(Локальный, Входящий) Экспорт
    Возврат Входящий.Наименование = "победитель";
КонецФункции

Функция ЗагрузитьВПлан() Экспорт
    Возврат ПланыОбмена.Обмен.ЗагрузитьПакет(Пакет);
КонецФункции
`

func TestExchangeHookFiresOnDSLLoad(t *testing.T) {
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

	prog, err := parser.New(lexer.New(exchangeHookModuleSrc, "общий.module.os")).ParseProgram()
	if err != nil {
		t.Fatalf("parse hook module: %v", err)
	}
	plan := &metadata.ExchangePlan{
		Name: "Обмен", Conflict: "hook", Content: []string{"Справочник.Товар"},
		Nodes: []metadata.ExchangeNode{{Code: "center"}, {Code: "fil01"}},
	}
	plan.Normalize()

	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: []*metadata.Entity{ent}})
	reg.LoadModules(map[string]*ast.Program{"Общий": prog})
	reg.LoadExchangePlans([]*metadata.ExchangePlan{plan})
	interp := interpreter.New()
	interp.LookupProc = reg.GetModuleProc

	if err := db.SaveExchangeThisNode(ctx, "Обмен", "fil01"); err != nil {
		t.Fatal(err)
	}

	// Готовим конфликт: локальный объект + неотправленная правка узлу center.
	id := uuid.New()
	if err := db.Upsert(ctx, "Товар", id, map[string]any{"Наименование": "локальное"}, ent); err != nil {
		t.Fatal(err)
	}
	v, _ := db.EntityVersion(ctx, "Товар", id)
	if err := db.RegisterExchangeChange(ctx, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: "Товар", ObjectID: id.String(), NodeCode: "center", Version: v, ChangedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	// Входящий: имя "победитель", но changed_at=500 < локального 1000 (by_time проиграл бы).
	pkg := exchange.Package{
		Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", ToNode: "fil01", MessageNo: 1,
		Objects: []exchange.PackageObject{{Type: "Товар", ID: id.String(), Version: 2, ChangedAt: 500,
			Fields: map[string]any{"Наименование": "победитель"}}},
	}
	data, _ := json.Marshal(pkg)

	// Common с непустым Interp → ПланыОбмена получает hook.
	vars := Common{Ctx: ctx, Reg: reg, Store: db, Interp: interp}.Build()
	vars["Пакет"] = string(data)

	proc := reg.GetModuleProc("ЗагрузитьВПлан")
	if proc == nil {
		t.Fatal("процедура ЗагрузитьВПлан не найдена")
	}
	var result any
	if err := interp.RunWithResult(proc, nil, &result, vars); err != nil {
		t.Fatalf("выполнение DSL-загрузки: %v", err)
	}

	row, err := db.GetByID(ctx, "Товар", id, ent)
	if err != nil {
		t.Fatal(err)
	}
	if row["Наименование"] != "победитель" {
		t.Fatalf("hook на DSL-пути не сработал: объект = %q (ожидали \"победитель\")", row["Наименование"])
	}

	// Контроль: без Interp тот же конфликт разрешается by_time — входящий проигрывает.
	id2 := uuid.New()
	if err := db.Upsert(ctx, "Товар", id2, map[string]any{"Наименование": "локальное2"}, ent); err != nil {
		t.Fatal(err)
	}
	v2, _ := db.EntityVersion(ctx, "Товар", id2)
	if err := db.RegisterExchangeChange(ctx, storage.ExchangeChange{
		Plan: "Обмен", ObjectType: "Товар", ObjectID: id2.String(), NodeCode: "center", Version: v2, ChangedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	pkg2 := exchange.Package{
		Format: exchange.FormatV1, Plan: "Обмен", FromNode: "center", ToNode: "fil01", MessageNo: 2,
		Objects: []exchange.PackageObject{{Type: "Товар", ID: id2.String(), Version: 2, ChangedAt: 500,
			Fields: map[string]any{"Наименование": "победитель"}}},
	}
	data2, _ := json.Marshal(pkg2)
	varsNoHook := Common{Ctx: ctx, Reg: reg, Store: db}.Build() // Interp == nil
	varsNoHook["Пакет"] = string(data2)
	if err := interp.RunWithResult(proc, nil, &result, varsNoHook); err != nil {
		t.Fatalf("выполнение DSL-загрузки без hook: %v", err)
	}
	row2, _ := db.GetByID(ctx, "Товар", id2, ent)
	if row2["Наименование"] != "локальное2" {
		t.Fatalf("без Interp ожидали by_time (локальный побеждает), объект = %q", row2["Наименование"])
	}
}
