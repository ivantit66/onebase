package runtime

import (
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/printform"
	"github.com/ivantit66/onebase/internal/processor"
	"github.com/ivantit66/onebase/internal/report"
)

func TestGetProcedureResolvesOnUnpostAlias(t *testing.T) {
	prog, err := parser.New(lexer.New(`
Процедура ОбработкаУдаленияПроведения()
КонецПроцедуры`, "unpost.os")).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	r.Load(LoadOptions{Programs: map[string]*ast.Program{"Документ": prog}})
	if proc := r.GetProcedure("Документ", "OnUnpost"); proc == nil {
		t.Fatal("OnUnpost не разрешился в ОбработкаУдаленияПроведения")
	}
}

// Legacy YAML-форма СРАЗУ конвертируется в макет v2 при обращении к реестру:
// PrintFormRef.Decl несёт результат ConvertLegacy (план 64, этап 4).
func TestGetAllPrintForms_LegacyConvertedToDecl(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.printForms["реализация"] = []*printform.PrintForm{{
		Name:     "Накладная",
		Document: "Реализация",
		Title:    "Накладная № {{Номер}}",
		Table: &printform.TableSection{
			Source:  "Товары",
			Columns: []printform.Column{{Field: "Сумма", Label: "Сумма"}},
			Totals:  []printform.TotalSpec{{Field: "Сумма", Sum: true, Label: "Итого"}},
		},
	}}
	r.mu.Unlock()

	refs := r.GetAllPrintForms("Реализация")
	if len(refs) != 1 {
		t.Fatalf("ожидалась 1 форма, получили %d", len(refs))
	}
	ref := refs[0]
	if ref.Kind != PrintFormLegacy {
		t.Errorf("Kind = %v, ожидался Legacy", ref.Kind)
	}
	if ref.Legacy == nil {
		t.Error("Legacy не сохранён (нужен для справки/валидации)")
	}
	if ref.Decl == nil || ref.Decl.Layout == nil {
		t.Fatal("Decl с конвертированным макетом не заполнен")
	}
	if ref.Decl.Layout.Area("Заголовок") == nil {
		t.Error("в конвертированном макете нет области Заголовок")
	}
	if ref.Decl.Layout.Binding == nil || len(ref.Decl.Layout.Binding.Repeat) != 1 {
		t.Error("в конвертированном макете нет repeat-binding по ТЧ")
	}
}

// Внешняя v2-форма (макет из БД) отдаётся как Declarative с External=true и
// приоритетом ниже формы конфигурации (план 64, этап 4.5).
func TestSetExternalLayoutForms_DeclarativeExternal(t *testing.T) {
	r := NewRegistry()
	r.SetExternalLayoutForms([]*printform.LayoutForm{{
		Name:     "ВнешняяНакладная",
		Document: "Реализация",
		Layout:   &printform.LayoutTemplate{Name: "ВнешняяНакладная"},
	}})

	refs := r.GetAllPrintForms("Реализация")
	if len(refs) != 1 {
		t.Fatalf("ожидалась 1 форма, получили %d", len(refs))
	}
	ref := refs[0]
	if ref.Kind != PrintFormDeclarative {
		t.Errorf("Kind = %v, ожидался Declarative", ref.Kind)
	}
	if !ref.External {
		t.Error("внешняя форма должна иметь External=true")
	}
	if ref.Decl == nil || ref.Decl.Layout == nil {
		t.Error("Decl с макетом не заполнен")
	}
}

// Форма конфигурации перебивает одноимённую внешнюю v2-форму.
func TestSetExternalLayoutForms_ConfigWins(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.layoutForms["реализация"] = []*printform.LayoutForm{{
		Name:     "Накладная",
		Document: "Реализация",
		Layout:   &printform.LayoutTemplate{Name: "Накладная"},
	}}
	r.mu.Unlock()
	r.SetExternalLayoutForms([]*printform.LayoutForm{{
		Name:     "Накладная",
		Document: "Реализация",
		Layout:   &printform.LayoutTemplate{Name: "Накладная"},
	}})

	refs := r.GetAllPrintForms("Реализация")
	if len(refs) != 1 {
		t.Fatalf("ожидалась 1 форма (коллизия имени), получили %d", len(refs))
	}
	if refs[0].External {
		t.Error("при коллизии должна остаться форма конфигурации (External=false)")
	}
}

// при коллизии YAML/.os одноимённой печатной формы
// .os должна перебивать, YAML — удаляться из реестра, в лог идёт warning.
func TestLoadDSLPrintForms_OverridesYAML(t *testing.T) {
	r := NewRegistry()

	// сначала загружаем YAML-форму (через прямую установку, чтобы не тянуть всю
	// Load-цепочку — она требует entities/programs и др.)
	r.mu.Lock()
	r.printForms["реализациятоваров"] = []*printform.PrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров"},
		{Name: "Счёт-фактура", Document: "РеализацияТоваров"},
	}
	r.mu.Unlock()

	// теперь регистрируем .os с тем же именем «Накладная»
	r.LoadDSLPrintForms([]*printform.DSLPrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров", LayoutPath: "/proj/printforms/РеализацияТоваров/накладная.layout.yaml"},
	})

	kept := r.GetPrintForms("РеализацияТоваров")
	if len(kept) != 1 {
		t.Fatalf("ожидалась 1 YAML-форма после дедупа, получили %d", len(kept))
	}
	if kept[0].Name != "Счёт-фактура" {
		t.Errorf("ожидался Счёт-фактура (.os не было коллизии), получили %s", kept[0].Name)
	}

	dsl := r.GetDSLPrintForms("РеализацияТоваров")
	if len(dsl) != 1 || dsl[0].Name != "Накладная" {
		t.Errorf("ожидалась 1 DSL-форма Накладная, получили %v", dsl)
	}
}

// Если нет коллизии, ни одна форма не удаляется.
func TestLoadDSLPrintForms_NoCollision(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.printForms["реализациятоваров"] = []*printform.PrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров"},
	}
	r.mu.Unlock()

	r.LoadDSLPrintForms([]*printform.DSLPrintForm{
		{Name: "Счёт", Document: "РеализацияТоваров"},
	})

	if len(r.GetPrintForms("РеализацияТоваров")) != 1 {
		t.Error("YAML-форма не должна была удалиться без коллизии")
	}
	if len(r.GetDSLPrintForms("РеализацияТоваров")) != 1 {
		t.Error(".os должна остаться")
	}
}

// Внешние формы дополняют формы конфигурации и помечаются External=true;
// reload конфигурации (printForms) их не затирает.
func TestSetExternalPrintForms_MergeAndFlag(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.printForms["реализациятоваров"] = []*printform.PrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров"},
	}
	r.mu.Unlock()

	r.SetExternalPrintForms([]*printform.PrintForm{
		{Name: "Накладная-А4", Document: "РеализацияТоваров"},
	})

	forms := r.GetPrintForms("РеализацияТоваров")
	if len(forms) != 2 {
		t.Fatalf("ожидались 2 формы (конфиг + внешняя), получили %d", len(forms))
	}
	// Конфиг-форма идёт первой и не помечена External.
	if forms[0].Name != "Накладная" || forms[0].External {
		t.Errorf("первой должна быть форма конфигурации без External, получили %+v", forms[0])
	}
	if forms[1].Name != "Накладная-А4" || !forms[1].External {
		t.Errorf("второй должна быть внешняя форма с External=true, получили %+v", forms[1])
	}
}

// При совпадении имени внешней формы с конфиг-формой обе остаются в списке,
// но конфигурация идёт первой (приоритет), а внешняя помечена External.
func TestSetExternalPrintForms_NameCollisionKeepsConfigFirst(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.printForms["реализациятоваров"] = []*printform.PrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров"},
	}
	r.mu.Unlock()

	r.SetExternalPrintForms([]*printform.PrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров"},
	})

	forms := r.GetPrintForms("РеализацияТоваров")
	if len(forms) != 2 {
		t.Fatalf("ожидались 2 формы при коллизии имени, получили %d", len(forms))
	}
	if forms[0].External {
		t.Error("первой (основной) должна оставаться форма конфигурации")
	}
	if !forms[1].External {
		t.Error("второй должна быть внешняя форма")
	}
}

// Повторный вызов SetExternalPrintForms полностью заменяет набор внешних форм.
func TestSetExternalPrintForms_Replaces(t *testing.T) {
	r := NewRegistry()
	r.SetExternalPrintForms([]*printform.PrintForm{
		{Name: "A", Document: "Док"},
	})
	r.SetExternalPrintForms([]*printform.PrintForm{
		{Name: "B", Document: "Док"},
	})
	forms := r.GetPrintForms("Док")
	if len(forms) != 1 || forms[0].Name != "B" {
		t.Fatalf("ожидалась только форма B после замены, получили %+v", forms)
	}
}

// Внешние отчёты дополняют список конфигурации и помечаются External;
// при коллизии имени приоритет у конфигурации.
func TestSetExternalReports_MergeAndPriority(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.reports["ОстаткиТоваров"] = &report.Report{Name: "ОстаткиТоваров", Query: "ВЫБРАТЬ 1"}
	r.mu.Unlock()

	r.SetExternalReports([]*report.Report{
		{Name: "ВнешнийОтчёт", Query: "ВЫБРАТЬ 2"},
		{Name: "ОстаткиТоваров", Query: "ВЫБРАТЬ 999"}, // коллизия
	})

	// GetReport: коллизия → отдаётся конфиг-отчёт.
	if rep := r.GetReport("ОстаткиТоваров"); rep == nil || rep.Query != "ВЫБРАТЬ 1" || rep.External {
		t.Errorf("при коллизии должен отдаваться отчёт конфигурации, got %+v", rep)
	}
	// Внешний уникальный отчёт доступен и помечен External.
	if rep := r.GetReport("ВнешнийОтчёт"); rep == nil || !rep.External {
		t.Errorf("ожидался внешний отчёт с External=true, got %+v", rep)
	}
	// Reports(): конфиг + только не конфликтующие внешние = 2.
	if got := len(r.Reports()); got != 2 {
		t.Errorf("ожидалось 2 отчёта (конфиг + 1 внешний без коллизии), got %d", got)
	}
}

// Внешняя обработка регистрируется вместе с кодом; GetProcessor/GetProcedure
// её находят, External выставляется, при коллизии имени приоритет у конфигурации.
func TestSetExternalProcessors(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.processors["КонфигОбр"] = &processor.Processor{Name: "КонфигОбр"}
	r.mu.Unlock()

	parse := func(src string) *ast.Program {
		prog, err := parser.New(lexer.New(src, "x.proc.os")).ParseProgram()
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return prog
	}
	r.SetExternalProcessors(
		[]*processor.Processor{{Name: "ВнешняяОбр", Trusted: true}},
		map[string]*ast.Program{
			"ВнешняяОбр": parse("Процедура Выполнить()\nКонецПроцедуры\n"),
		},
	)

	p := r.GetProcessor("ВнешняяОбр")
	if p == nil || !p.External || !p.Trusted {
		t.Errorf("ожидалась внешняя доверенная обработка, got %+v", p)
	}
	if d := r.GetProcedure("ВнешняяОбр", "Выполнить"); d == nil {
		t.Error("код внешней обработки не зарегистрирован (GetProcedure nil)")
	}
	if got := len(r.Processors()); got != 2 {
		t.Errorf("ожидалось 2 обработки (конфиг + внешняя), got %d", got)
	}

	// Коллизия имени: внешняя «КонфигОбр» не перехватывает конфигурацию.
	r.SetExternalProcessors(
		[]*processor.Processor{{Name: "КонфигОбр"}},
		map[string]*ast.Program{"КонфигОбр": parse("Процедура Выполнить()\nКонецПроцедуры\n")},
	)
	if p := r.GetProcessor("КонфигОбр"); p == nil || p.External {
		t.Errorf("при коллизии должна отдаваться обработка конфигурации, got %+v", p)
	}
}

// ReceiversOf возвращает все entity, у которых текущий объект указан в
// based_on. Это инверсия данных, которую UI использует для рендеринга
// меню «Ввести на основании ▾» на форме источника.
func TestReceiversOf(t *testing.T) {
	r := NewRegistry()
	src := &metadata.Entity{Name: "РеализацияТоваров", Kind: metadata.KindDocument}
	recv1 := &metadata.Entity{Name: "ВозвратОтПокупателя", Kind: metadata.KindDocument, BasedOn: []string{"РеализацияТоваров"}}
	recv2 := &metadata.Entity{Name: "Счёт", Kind: metadata.KindDocument, BasedOn: []string{"РеализацияТоваров", "Контрагент"}}
	unrelated := &metadata.Entity{Name: "Поступление", Kind: metadata.KindDocument}

	r.Load(LoadOptions{Entities: []*metadata.Entity{src, recv1, recv2, unrelated}})

	got := r.ReceiversOf("РеализацияТоваров")
	if len(got) != 2 {
		t.Fatalf("ReceiversOf вернул %d сущностей, ожидалось 2", len(got))
	}
	names := map[string]bool{}
	for _, e := range got {
		names[e.Name] = true
	}
	if !names["ВозвратОтПокупателя"] || !names["Счёт"] {
		t.Errorf("ReceiversOf вернул %v, ожидались ВозвратОтПокупателя и Счёт", names)
	}

	// Регистронезависимый поиск.
	if r.ReceiversOf("реализациятоваров") == nil {
		t.Error("ReceiversOf должен быть регистронезависимым")
	}

	// Нет приёмников — nil.
	if got := r.ReceiversOf("Поступление"); got != nil {
		t.Errorf("Для сущности без приёмников ожидался nil, получили %v", got)
	}
}

// Коллизия регистронезависима: накладная == Накладная.
func TestLoadDSLPrintForms_CaseInsensitiveCollision(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.printForms["реализациятоваров"] = []*printform.PrintForm{
		{Name: "накладная", Document: "РеализацияТоваров"},
	}
	r.mu.Unlock()

	r.LoadDSLPrintForms([]*printform.DSLPrintForm{
		{Name: "НАКЛАДНАЯ", Document: "РеализацияТоваров"},
	})

	if len(r.GetPrintForms("РеализацияТоваров")) != 0 {
		t.Error("YAML-форма должна была удалиться (case-insensitive collision)")
	}
}

// GetAllPrintForms объединяет все виды форм и применяет приоритет коллизий
// Declarative > DSL > Legacy (план 64, этап 3).
func TestGetAllPrintForms_CollisionPriority(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	// Legacy YAML: две формы.
	r.printForms["реализациятоваров"] = []*printform.PrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров"},
		{Name: "ТолькоLegacy", Document: "РеализацияТоваров"},
	}
	// DSL: одна коллизия с YAML («Накладная»), одна уникальная.
	r.dslPrintForms["реализациятоваров"] = []*printform.DSLPrintForm{
		{Name: "Накладная", Document: "РеализацияТоваров"},
		{Name: "ТолькоDSL", Document: "РеализацияТоваров"},
	}
	r.mu.Unlock()

	// Declarative перебивает «Накладная» (и DSL, и Legacy).
	r.LoadLayoutForms([]*printform.LayoutForm{
		{Name: "Накладная", Document: "РеализацияТоваров", Layout: &printform.LayoutTemplate{Name: "Накладная"}},
	})

	refs := r.GetAllPrintForms("РеализацияТоваров")
	byName := map[string]PrintFormRef{}
	for _, ref := range refs {
		if _, dup := byName[ref.Name]; dup {
			t.Fatalf("дубль формы %q в GetAllPrintForms", ref.Name)
		}
		byName[ref.Name] = ref
	}
	if len(byName) != 3 {
		t.Fatalf("ожидалось 3 уникальных формы (Накладная+ТолькоDSL+ТолькоLegacy), получили %d: %v", len(byName), byName)
	}
	if byName["Накладная"].Kind != PrintFormDeclarative {
		t.Errorf("Накладная должна быть Declarative, получили %v", byName["Накладная"].Kind)
	}
	if byName["ТолькоDSL"].Kind != PrintFormDSL {
		t.Errorf("ТолькоDSL должна быть DSL")
	}
	if byName["ТолькоLegacy"].Kind != PrintFormLegacy {
		t.Errorf("ТолькоLegacy должна быть Legacy")
	}
}

// GetPrintFormRef ищет форму по имени с учётом приоритета.
func TestGetPrintFormRef(t *testing.T) {
	r := NewRegistry()
	r.mu.Lock()
	r.printForms["реализациятоваров"] = []*printform.PrintForm{
		{Name: "Счёт", Document: "РеализацияТоваров"},
	}
	r.mu.Unlock()

	ref, ok := r.GetPrintFormRef("реализациятоваров", "счёт")
	if !ok || ref.Kind != PrintFormLegacy || ref.Legacy == nil {
		t.Fatalf("GetPrintFormRef(счёт) = %+v ok=%v", ref, ok)
	}
	if _, ok := r.GetPrintFormRef("РеализацияТоваров", "нет"); ok {
		t.Error("несуществующая форма не должна находиться")
	}
}

func TestReplaceProjectFromSwapsProjectAndPreservesExternalObjects(t *testing.T) {
	current := NewRegistry()
	current.Load(LoadOptions{
		Entities: []*metadata.Entity{{Name: "Старый", Kind: metadata.KindCatalog}},
		Reports:  []*report.Report{{Name: "СтарыйОтчёт"}},
	})
	current.SetExternalReports([]*report.Report{{Name: "ВнешнийОтчёт"}})
	current.SetExternalProcessors([]*processor.Processor{{Name: "ВнешняяОбработка"}}, nil)

	next := NewRegistry()
	next.Load(LoadOptions{
		Entities: []*metadata.Entity{{Name: "Новый", Kind: metadata.KindDocument}},
		Reports:  []*report.Report{{Name: "НовыйОтчёт"}},
	})
	next.LoadProcessors([]*processor.Processor{{Name: "НоваяОбработка"}})
	next.LoadExchangePlans([]*metadata.ExchangePlan{{Name: "НовыйОбмен"}})

	current.ReplaceProjectFrom(next)
	if current.GetEntity("Старый") != nil || current.GetReport("СтарыйОтчёт") != nil {
		t.Fatal("старые project-owned объекты остались после reload")
	}
	if current.GetEntity("Новый") == nil || current.GetReport("НовыйОтчёт") == nil || current.GetProcessor("НоваяОбработка") == nil {
		t.Fatal("новый проект опубликован не полностью")
	}
	if current.GetExchangePlan("НовыйОбмен") == nil {
		t.Fatal("планы обмена не попали в атомарную замену")
	}
	if current.GetReport("ВнешнийОтчёт") == nil || current.GetProcessor("ВнешняяОбработка") == nil {
		t.Fatal("внешние объекты не должны исчезать при reload проекта")
	}
}
