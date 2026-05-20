package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// Замечание #26: создание/проведение документов из обработки.
// Полный сценарий: Документы.X.Создать() → заполнить шапку и ТЧ →
// Записать() → Провести(). Проверяем что документ, его ТЧ и движения
// регистра реально записались.
func TestDocsRoot_CreateWritePost(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Документ ПоступлениеТоваров с ТЧ Товары.
	doc := &metadata.Entity{
		Name:    "ПоступлениеТоваров",
		Kind:    metadata.KindDocument,
		Posting: true,
		Fields: []metadata.Field{
			{Name: "Номер", Type: metadata.FieldTypeString},
		},
		TableParts: []metadata.TablePart{
			{Name: "Товары", Fields: []metadata.Field{
				{Name: "Номенклатура", Type: metadata.FieldTypeString},
				{Name: "Количество", Type: metadata.FieldTypeNumber},
			}},
		},
	}
	reg := &metadata.Register{
		Name:       "ОстаткиТоваров",
		Dimensions: []metadata.Field{{Name: "Номенклатура", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
	}

	if err := db.Migrate(ctx, []*metadata.Entity{doc}); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatal(err)
	}

	// OnPost: пишем приход в регистр по строкам ТЧ.
	onPostSrc := `Процедура ОбработкаПроведения()
  Для Каждого Стр Из ЭтотОбъект.Товары Цикл
    Дв = Движения.ОстаткиТоваров.Добавить();
    Дв.ВидДвижения = "Приход";
    Дв.Номенклатура = Стр.Номенклатура;
    Дв.Количество = Стр.Количество;
  КонецЦикла;
КонецПроцедуры`
	prog := mustParse(t, onPostSrc)

	registry := runtime.NewRegistry()
	registry.Load(
		[]*metadata.Entity{doc},
		map[string]*ast.Program{"ПоступлениеТоваров": prog},
		[]*metadata.Register{reg},
		nil, nil, nil, nil,
	)

	interp := interpreter.New()
	interp.LookupProc = registry.GetModuleProc

	s := &Server{
		store:    db,
		reg:      registry,
		interp:   interp,
		lockMgr:  runtime.NewLockManager(),
		messages: NewMessageStore(),
	}

	// Сценарий обработки: создать документ, заполнить, записать, провести.
	root := newDocsRoot(s, interpreter.NewTxState(ctx))
	proxy := root.Get("ПоступлениеТоваров")
	if proxy == nil {
		t.Fatal("Документы.ПоступлениеТоваров → nil")
	}
	dp, ok := proxy.(*docProxy)
	if !ok {
		t.Fatalf("ожидался *docProxy, получили %T", proxy)
	}

	rec := dp.CallMethod("создать", nil)
	w, ok := rec.(*docWriter)
	if !ok {
		t.Fatalf("Создать → %T, ожидался *docWriter", rec)
	}
	w.Set("Номер", "ПОС-001")

	// Док.Товары.Добавить()
	tpAny := w.Get("Товары")
	tp, ok := tpAny.(*tpProxy)
	if !ok {
		t.Fatalf("Док.Товары → %T, ожидался *tpProxy", tpAny)
	}
	row1 := tp.CallMethod("добавить", nil).(*interpreter.MapThis)
	row1.Set("Номенклатура", "Тумбочка")
	row1.Set("Количество", float64(100))
	row2 := tp.CallMethod("добавить", nil).(*interpreter.MapThis)
	row2.Set("Номенклатура", "Стул")
	row2.Set("Количество", float64(50))

	// Записать + Провести
	if res := w.CallMethod("записать", nil); res == nil {
		t.Fatal("Записать вернул nil")
	}
	if res := w.CallMethod("провести", nil); res == nil {
		t.Fatal("Провести вернул nil")
	}

	// Проверки: документ записан
	var docCount int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM поступлениетоваров").Scan(&docCount)
	if docCount != 1 {
		t.Errorf("ожидался 1 документ, получили %d", docCount)
	}
	// ТЧ записана
	var tpCount int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM поступлениетоваров_товары").Scan(&tpCount)
	if tpCount != 2 {
		t.Errorf("ожидалось 2 строки ТЧ, получили %d", tpCount)
	}
	// движения регистра записаны
	var movCount int
	db.QueryRow(ctx, "SELECT COUNT(*) FROM рег_остаткитоваров").Scan(&movCount)
	if movCount != 2 {
		t.Errorf("ожидалось 2 движения, получили %d", movCount)
	}
	// posted = true
	var posted bool
	db.QueryRow(ctx, "SELECT posted FROM поступлениетоваров LIMIT 1").Scan(&posted)
	if !posted {
		t.Error("документ не помечен проведённым")
	}
}

// Документы.X для несуществующего/несдокументного — nil.
func TestDocsRoot_UnknownDocument(t *testing.T) {
	s := &Server{reg: runtime.NewRegistry()}
	root := newDocsRoot(s, interpreter.NewTxState(context.Background()))
	if v := root.Get("НетТакого"); v != nil {
		t.Errorf("Документы.НетТакого → %v, ожидался nil", v)
	}
}

func mustParse(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := lexer.New(src, "<test>")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}
