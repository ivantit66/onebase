package runtime

import (
	"testing"

	"github.com/ivantit66/onebase/internal/printform"
)

// Замечание #10: при коллизии YAML/.os одноимённой печатной формы
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
