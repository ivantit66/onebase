package launcher

import (
	"bytes"
	"strings"
	"testing"
)

// Issue #35: редактор объектов конфигуратора рендерит секции вкладками,
// а не длинным столбцом. Фиксируем наличие панели вкладок и панелей контента.
func renderTabTree(t *testing.T) string {
	t.Helper()
	data := &configuratorData{
		Base: &Base{ID: "test-base", Name: "Тест", ConfigSource: "file"},
		Lang: "ru",
		Tab:  "tree",
		Catalogs: []cfgEntity{{
			Name: "Номенклатура", Kind: "Справочник",
			Fields: []cfgField{{Name: "Цена", Type: "number"}},
		}},
		Docs: []cfgEntity{{
			Name: "Реализация", Kind: "Документ", Posting: true,
			Fields: []cfgField{{Name: "Сумма", Type: "number"}},
		}},
		Reports:    []cfgReport{{Name: "Продажи"}},
		Processors: []cfgProcessor{{Name: "Загрузка"}},
	}
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "tab-tree", data); err != nil {
		t.Fatalf("ExecuteTemplate tab-tree: %v", err)
	}
	return buf.String()
}

func TestConfigurator_EntityTabs(t *testing.T) {
	html := renderTabTree(t)
	for _, sub := range []string{
		`class="obj-tabs"`,
		`id="ot-data-Номенклатура"`,
		`id="ot-forms-Номенклатура"`,
		`id="ot-print-Номенклатура"`,
		`id="ot-modules-Номенклатура"`,
		`id="ot-data-Реализация"`,
		`id="ot-modules-Реализация"`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML нет ожидаемого фрагмента: %q", sub)
		}
	}
}

func TestConfigurator_ReportTabs(t *testing.T) {
	html := renderTabTree(t)
	for _, sub := range []string{
		`id="ot-rep-params-Продажи"`,
		`id="ot-rep-query-Продажи"`,
		`id="ot-rep-chart-Продажи"`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML нет ожидаемого фрагмента отчёта: %q", sub)
		}
	}
}

func TestConfigurator_ProcessorTabs(t *testing.T) {
	html := renderTabTree(t)
	for _, sub := range []string{
		`id="ot-proc-params-Загрузка"`,
		`id="ot-proc-code-Загрузка"`,
		`id="ot-proc-form-Загрузка"`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML нет ожидаемого фрагмента обработки: %q", sub)
		}
	}
}

// Перенос секций по вкладкам не должен вкладывать одну <form> в другую —
// браузер «разорвёт» вложенную форму и сабмиты сломаются. Сканируем
// отрендеренный HTML и проверяем, что глубина вложенности <form> ≤ 1.
func TestConfigurator_NoNestedForms(t *testing.T) {
	html := renderTabTree(t)
	depth, maxDepth := 0, 0
	for i := 0; i < len(html); {
		if strings.HasPrefix(html[i:], "<form") {
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
			i += len("<form")
		} else if strings.HasPrefix(html[i:], "</form>") {
			depth--
			i += len("</form>")
		} else {
			i++
		}
	}
	if maxDepth > 1 {
		t.Errorf("обнаружена вложенность <form> глубиной %d (ожидалось ≤1)", maxDepth)
	}
	if depth != 0 {
		t.Errorf("несбалансированные теги <form>/</form>: итоговая глубина %d", depth)
	}
}
