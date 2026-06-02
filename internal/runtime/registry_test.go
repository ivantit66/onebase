package runtime

import (
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/printform"
)

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
