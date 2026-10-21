package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// Управляемая форма решает, каким ссылочным реквизитам нужна первая страница
// справочника: тем, что нарисованы выпадающим списком. Реквизиту с
// choice_dropdown: false и реквизиту без элемента на форме нужна только подпись
// текущего значения — первая страница большого справочника стоит полного
// прохода таблицы с сортировкой, а шаблон её всё равно выбросил бы.
func TestLoadInitialRefOptionsSkipsListWhereFormShowsNoDropdown(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	refEnt := &metadata.Entity{
		Name:   "Товары",
		Kind:   metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	ref := func(name string) metadata.Field {
		return metadata.Field{Name: name, Type: metadata.FieldType("reference:Товары"), RefEntity: "Товары"}
	}
	off := false
	doc := &metadata.Entity{
		Name:   "Заказ",
		Kind:   metadata.KindDocument,
		Fields: []metadata.Field{ref("СоСписком"), ref("БезСписка"), ref("НеНаФорме"), ref("СОтбором")},
		Forms: []*metadata.FormModule{{
			Name:       "ФормаОбъекта",
			Kind:       "object",
			LayoutKind: metadata.FormLayoutManaged,
			Elements: []*metadata.FormElement{{
				Kind: metadata.FormElementGroupBox,
				Name: "Группа",
				Children: []*metadata.FormElement{
					{Kind: metadata.FormElementField, Name: "ПолеСоСписком", DataPath: "Объект.СоСписком"},
					{Kind: metadata.FormElementField, Name: "ПолеБезСписка", DataPath: "Объект.БезСписка", ChoiceDropdown: &off},
					// Свой отбор: варианты считает applyChoiceFilters по id элемента.
					{Kind: metadata.FormElementField, ID: "эл-отбор", Name: "ПолеСОтбором", DataPath: "Объект.СОтбором",
						ChoiceFilter: []metadata.FormChoiceCondition{{Field: "Наименование", Op: metadata.FormChoiceOpEqual, From: "Объект.СоСписком"}}},
				},
			}},
		}},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{refEnt, doc}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		id := uuid.MustParse("00000000-0000-0000-0000-" + fmt12(i))
		if err := db.Upsert(ctx, refEnt.Name, id, map[string]any{"Наименование": "Товар"}, refEnt); err != nil {
			t.Fatal(err)
		}
	}
	selected := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	if err := db.Upsert(ctx, refEnt.Name, selected, map[string]any{"Наименование": "Выбранный"}, refEnt); err != nil {
		t.Fatal(err)
	}
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: []*metadata.Entity{refEnt, doc}})
	s := &Server{reg: reg, store: db}

	values := map[string]string{
		"СоСписком": selected.String(),
		"БезСписка": selected.String(),
		"НеНаФорме": selected.String(),
		"СОтбором":  selected.String(),
	}
	opts, err := s.loadInitialRefOptions(ctx, doc, values)
	if err != nil {
		t.Fatal(err)
	}

	// Поле со списком получает первую страницу — как раньше.
	if got := len(opts["СоСписком"]); got < 6 {
		t.Fatalf("СоСписком: %d вариантов, ждали первую страницу (6): %#v", got, opts["СоСписком"])
	}
	// Без списка, вне формы и со своим отбором — только текущее значение,
	// без запроса нефильтрованного списка.
	for _, field := range []string{"БезСписка", "НеНаФорме", "СОтбором"} {
		rows := opts[field]
		if len(rows) != 1 || !hasOptionWithLabel(rows, selected.String(), "Выбранный") {
			t.Fatalf("%s: ждали только текущее значение, получили %#v", field, rows)
		}
	}

	// Пустое значение поля без списка — пустые варианты, а не первая страница.
	values["БезСписка"] = ""
	opts, err = s.loadInitialRefOptions(ctx, doc, values)
	if err != nil {
		t.Fatal(err)
	}
	if rows := opts["БезСписка"]; len(rows) != 0 {
		t.Fatalf("БезСписка без значения: ждали пусто, получили %#v", rows)
	}
}

// Без управляемой формы (авто-форма рисует списком каждое ссылочное поле)
// предзагрузка остаётся прежней для всех реквизитов.
func TestLoadInitialRefOptionsWithoutManagedFormPreloadsAll(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	refEnt := &metadata.Entity{
		Name:   "Товары",
		Kind:   metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	doc := &metadata.Entity{
		Name: "Заказ",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Товар", Type: metadata.FieldType("reference:Товары"), RefEntity: "Товары"},
		},
	}
	if err := db.Migrate(ctx, []*metadata.Entity{refEnt, doc}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		id := uuid.MustParse("00000000-0000-0000-0000-" + fmt12(i))
		if err := db.Upsert(ctx, refEnt.Name, id, map[string]any{"Наименование": "Товар"}, refEnt); err != nil {
			t.Fatal(err)
		}
	}
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: []*metadata.Entity{refEnt, doc}})
	s := &Server{reg: reg, store: db}

	opts, err := s.loadInitialRefOptions(ctx, doc, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(opts["Товар"]); got != 3 {
		t.Fatalf("без управляемой формы ждали первую страницу (3), получили %d", got)
	}
}
