package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/entityservice"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// newSelfRefPostingServer поднимает Server с проводимым документом «Прибор», чей
// ОбработкаПроведения создаёт связанный справочник «СобытиеПрибора» со ссылкой
// обратно на сам документ (reference:Прибор → ЭтотОбъект.Ссылка).
func newSelfRefPostingServer(t *testing.T) (context.Context, *storage.DB, *Server, *metadata.Entity, *metadata.Entity) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	doc := &metadata.Entity{
		Name:    "Прибор",
		Kind:    metadata.KindDocument,
		Posting: true,
		Fields:  []metadata.Field{{Name: "Номер", Type: metadata.FieldTypeString}},
	}
	cat := &metadata.Entity{
		Name: "СобытиеПрибора",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Прибор", Type: "reference:Прибор", RefEntity: "Прибор"},
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{doc, cat}); err != nil {
		t.Fatal(err)
	}

	onPostSrc := `Процедура ОбработкаПроведения()
  Соб = Справочники.СобытиеПрибора.Создать();
  Соб.Прибор = ЭтотОбъект.Ссылка;
  Соб.Записать();
КонецПроцедуры`
	registry := runtime.NewRegistry()
	registry.Load(runtime.LoadOptions{
		Entities: []*metadata.Entity{doc, cat},
		Programs: map[string]*ast.Program{"Прибор": mustParse(t, onPostSrc)},
	})
	interp := interpreter.New()
	interp.LookupProc = registry.GetModuleProc
	s := &Server{store: db, reg: registry, interp: interp, lockMgr: runtime.NewLockManager(), messages: NewMessageStore()}
	s.entitySvc = &entityservice.Service{
		Store:        db,
		Reg:          registry,
		Interp:       interp,
		PrepareHook:  s.enrichHeaderRefs,
		EnrichTPRows: s.enrichTPRowsWithRefs,
		BuildVars:    s.buildDSLVarsWithMessages,
		MakeThis: func(ctx context.Context, obj *runtime.Object, e *metadata.Entity) interpreter.This {
			return s.newFormObjectThis(ctx, obj, e, nil)
		},
	}
	return ctx, db, s, doc, cat
}

// Регрессия issue #360: создание+проведение нового документа одним действием
// (post/post_and_close на /new), чей ОбработкаПроведения создаёт связанный
// объект со ссылкой обратно на сам документ. До фикса шапка документа
// записывалась ПОСЛЕ хука, поэтому FK-ссылка «на себя» падала с
// FOREIGN KEY constraint failed. Теперь шапка нового объекта пишется до хука.
func TestSaveNewDocument_PostWithSelfReference(t *testing.T) {
	ctx, db, s, doc, cat := newSelfRefPostingServer(t)

	docID := uuid.New()
	res, err := s.entitySvc.Save(ctx, entityservice.SaveRequest{
		Entity: doc,
		ID:     docID,
		IsNew:  true,
		Fields: map[string]any{"Номер": "П-001"},
		Action: "post",
	})
	if err != nil {
		t.Fatalf("Save вернул технический сбой: %v", err)
	}
	if res.DSLError != "" {
		t.Fatalf("Save.DSLError = %q (ожидалось пусто) — FK-ссылка на себя при создании+проведении", res.DSLError)
	}

	// Документ существует и проведён.
	row, err := db.GetByID(ctx, "Прибор", docID, doc)
	if err != nil || row == nil {
		t.Fatalf("документ не найден: %v", err)
	}
	if !asBool(row["posted"]) {
		t.Errorf("документ не помечен проведённым: %v", row["posted"])
	}

	// Связанный объект создан и ссылается на документ.
	events, err := db.List(ctx, "СобытиеПрибора", cat, storage.ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("ожидался 1 СобытиеПрибора, создано %d", len(events))
	}
	if ref := refValueString(events[0]["Прибор"]); ref != docID.String() {
		t.Errorf("СобытиеПрибора.Прибор = %q, ожидался %q", ref, docID.String())
	}
}

// Неудачное бизнес-правило при создании+проведении не должно оставлять
// документ-сироту: пред-запись шапки нового объекта откатывается.
func TestSaveNewDocument_PostHookFailureLeavesNothing(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	doc := &metadata.Entity{
		Name:    "Прибор",
		Kind:    metadata.KindDocument,
		Posting: true,
		Fields:  []metadata.Field{{Name: "Номер", Type: metadata.FieldTypeString}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{doc}); err != nil {
		t.Fatal(err)
	}
	onPostSrc := `Процедура ОбработкаПроведения()
  ВызватьИсключение("нельзя проводить");
КонецПроцедуры`
	registry := runtime.NewRegistry()
	registry.Load(runtime.LoadOptions{
		Entities: []*metadata.Entity{doc},
		Programs: map[string]*ast.Program{"Прибор": mustParse(t, onPostSrc)},
	})
	interp := interpreter.New()
	interp.LookupProc = registry.GetModuleProc
	s := &Server{store: db, reg: registry, interp: interp, lockMgr: runtime.NewLockManager(), messages: NewMessageStore()}
	s.entitySvc = &entityservice.Service{
		Store: db, Reg: registry, Interp: interp,
		PrepareHook:  s.enrichHeaderRefs,
		EnrichTPRows: s.enrichTPRowsWithRefs,
		BuildVars:    s.buildDSLVarsWithMessages,
		MakeThis: func(ctx context.Context, obj *runtime.Object, e *metadata.Entity) interpreter.This {
			return s.newFormObjectThis(ctx, obj, e, nil)
		},
	}

	docID := uuid.New()
	res, err := s.entitySvc.Save(ctx, entityservice.SaveRequest{
		Entity: doc, ID: docID, IsNew: true,
		Fields: map[string]any{"Номер": "П-001"}, Action: "post",
	})
	if err != nil {
		t.Fatalf("Save технический сбой: %v", err)
	}
	if res.DSLError == "" {
		t.Fatal("ожидалась DSLError от ВызватьИсключение")
	}
	rows, err := db.List(ctx, "Прибор", doc, storage.ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("документ-сирота не откачен: %d строк", len(rows))
	}
}
