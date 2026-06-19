package ui

import (
	"bytes"
	"strings"
	"testing"

	reportpkg "github.com/ivantit66/onebase/internal/report"
)

func TestEffectiveComposition(t *testing.T) {
	main := &reportpkg.Composition{Groupings: []string{"Основной"}}
	variantComp := &reportpkg.Composition{Groupings: []string{"Вариант"}}
	override := &reportpkg.Composition{Groupings: []string{"Override"}}
	rep := &reportpkg.Report{
		Composition: main,
		Variants:    []reportpkg.ReportVariant{{Name: "V", Composition: variantComp}},
	}

	// 1) override (settings.Composition) перекрывает и вариант, и основной.
	if got := effectiveComposition(rep, &reportpkg.UserReportSettings{Variant: "V", Composition: override}); got != override {
		t.Fatalf("override: %+v", got)
	}
	// 2) settings без Composition → активный вариант по имени.
	if got := effectiveComposition(rep, &reportpkg.UserReportSettings{Variant: "V"}); got != variantComp {
		t.Fatalf("variant: %+v", got)
	}
	// 3) settings == nil → основной composition.
	if got := effectiveComposition(rep, nil); got != main {
		t.Fatalf("main: %+v", got)
	}
}

// TestReportSettingsPanel: панель «Настройки» рендерится при наличии ReportCols,
// содержит чекбоксы доступных полей и скрытое поле __settings.
func TestReportSettingsPanel(t *testing.T) {
	rep := &reportpkg.Report{Name: "sales", Title: "Продажи"}
	var buf bytes.Buffer
	data := map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": []reportParamUI{},
		"ReportCols":   []string{"Товар", "Сумма"},
		"UserSettings": &reportpkg.UserReportSettings{
			Composition: &reportpkg.Composition{
				Groupings: []string{"Товар"},
				Measures:  []reportpkg.Measure{{Field: "Сумма", Agg: "sum"}},
			},
		},
		"Cfg":  Config{},
		"Lang": "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-report", data); err != nil {
		t.Fatalf("execute page-report: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `data-block="settings"`) {
		t.Fatalf("нет блока настроек data-block=settings")
	}
	if !strings.Contains(out, `name="__settings"`) {
		t.Fatalf("нет скрытого поля __settings")
	}
	for _, want := range []string{"Товар", "Сумма"} {
		if !strings.Contains(out, want) {
			t.Errorf("в панели нет поля %q", want)
		}
	}
}

// TestReportSettingsPanelHidden: без ReportCols панель не рендерится
// (обратная совместимость с отчётами без компоновки).
func TestReportSettingsPanelHidden(t *testing.T) {
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
	if strings.Contains(buf.String(), `data-block="settings"`) {
		t.Errorf("панель настроек не должна рендериться без ReportCols")
	}
}

// TestReportSettingsFilters: сохранённые отборы предзаполняют строки панели —
// select поля, select оператора (с выбранным gt) и значение.
func TestReportSettingsFilters(t *testing.T) {
	rep := &reportpkg.Report{Name: "sales", Title: "Продажи"}
	var buf bytes.Buffer
	data := map[string]any{
		"Report":       rep,
		"ParamValues":  map[string]any{},
		"ReportParams": []reportParamUI{},
		"ReportCols":   []string{"Товар", "Сумма"},
		"UserSettings": &reportpkg.UserReportSettings{
			Filters: []reportpkg.Filter{{Field: "Сумма", Op: "gt", Value: "100"}},
		},
		"Cfg":  Config{},
		"Lang": "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-report", data); err != nil {
		t.Fatalf("execute page-report: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="rs-f-field"`) {
		t.Errorf("нет select поля отбора")
	}
	if !strings.Contains(out, `class="rs-f-op"`) {
		t.Fatalf("нет select оператора отбора")
	}
	if !strings.Contains(out, `value="gt" selected`) {
		t.Errorf("оператор gt не помечен selected")
	}
	if !strings.Contains(out, `value="100"`) {
		t.Errorf("значение отбора 100 не предзаполнено")
	}
}
