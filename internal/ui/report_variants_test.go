package ui

import (
	"bytes"
	"strings"
	"testing"

	reportpkg "github.com/ivantit66/onebase/internal/report"
)

// TestReportVariantsSelect проверяет, что форма отчёта с вариантами компоновки
// рендерит выпадающий список __variant с именами вариантов и помечает активный.
func TestReportVariantsSelect(t *testing.T) {
	rep := &reportpkg.Report{
		Name:        "sales",
		Title:       "Продажи",
		Composition: &reportpkg.Composition{Groupings: []string{"Менеджер"}},
		Variants: []reportpkg.ReportVariant{
			{Name: "По складам", Composition: &reportpkg.Composition{Groupings: []string{"Склад"}}},
			{Name: "Кросс", Composition: &reportpkg.Composition{Groupings: []string{"Номенклатура"}, Columns: []string{"Месяц"}}},
		},
	}
	var buf bytes.Buffer
	data := map[string]any{
		"Report":        rep,
		"ParamValues":   map[string]any{},
		"ReportParams":  []reportParamUI{},
		"ActiveVariant": "По складам",
		"Cfg":           Config{},
		"Lang":          "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-report", data); err != nil {
		t.Fatalf("execute page-report: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `name="__variant"`) {
		t.Fatalf("нет селектора вариантов __variant")
	}
	for _, want := range []string{"По складам", "Кросс"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет варианта %q", want)
		}
	}
	// Активный вариант должен быть помечен selected.
	if !strings.Contains(out, `value="По складам" selected`) {
		t.Errorf("активный вариант \"По складам\" не помечен selected")
	}
}

// TestReportNoVariants: без вариантов селектор не рендерится (обратная совместимость).
func TestReportNoVariants(t *testing.T) {
	rep := &reportpkg.Report{Name: "plain", Title: "Простой"}
	var buf bytes.Buffer
	data := map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": []reportParamUI{},
		"Cfg":          Config{},
		"Lang":         "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-report", data); err != nil {
		t.Fatalf("execute page-report: %v", err)
	}
	if strings.Contains(buf.String(), `name="__variant"`) {
		t.Errorf("селектор вариантов не должен рендериться без вариантов")
	}
}
