package launcher

import (
	"bytes"
	"strings"
	"testing"
)

// План 64, этап 5a: визуальный редактор макетов на модели v2. Фиксируем наличие
// новых элементов панели редактора (HTML тоггл-кнопок границ по сторонам) и
// JS-функций (порядок областей, линейка колонок, высоты строк, границы).

// renderLayoutPanelTree рендерит tab-tree с одной DSL-печатной формой, у которой
// привязан макет, — чтобы попала панель редактора макета (mkt-*).
func renderLayoutPanelTree(t *testing.T) string {
	t.Helper()
	data := &configuratorData{
		Base: &Base{ID: "test-base", Name: "Тест", ConfigSource: "file"},
		Lang: "ru",
		Tab:  "tree",
		DSLPrintForms: []cfgDSLPrintForm{{
			Name:       "Накладная",
			Document:   "Реализация",
			Source:     "// форма",
			FileName:   "Накладная.os",
			HasLayout:  true,
			LayoutYAML: "areas:\n  - name: Заголовок\n    rows: []\n",
		}},
	}
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "tab-tree", data); err != nil {
		t.Fatalf("ExecuteTemplate tab-tree: %v", err)
	}
	return buf.String()
}

// JS редактора макетов вынесен в /static/configurator.js (план 55, фаза 2b-2);
// тесты на тела JS-функций читают его через configuratorJS(t) из
// langref_render_test.go.

// 6.3: панель свойств ячейки содержит тоггл-кнопки границ по сторонам, select
// толщины и кнопки-пресеты.
func TestLayoutEditor_PerSideBorderControls(t *testing.T) {
	html := renderLayoutPanelTree(t)
	for _, sub := range []string{
		`id="vp-bd-left-Накладная"`,
		`id="vp-bd-top-Накладная"`,
		`id="vp-bd-right-Накладная"`,
		`id="vp-bd-bottom-Накладная"`,
		`id="vp-bw-Накладная"`,
		`ldToggleBorderSide('Накладная','left')`,
		`ldBorderPreset('Накладная','all')`,
		`ldBorderPreset('Накладная','none')`,
		`ldBorderGridArea('Накладная')`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML панели редактора нет фрагмента: %q", sub)
		}
	}
}

// 6.1/6.2/6.3: JS-редактор содержит функции порядка областей, линейки колонок,
// высот строк и границ по сторонам.
func TestLayoutEditor_V2JSFunctions(t *testing.T) {
	js := configuratorJS(t)
	for _, sub := range []string{
		"function moveLayoutArea",     // 6.1 порядок областей
		"function _ldNormAreas",       // 6.1 чтение map → массив
		"function ldColWidth",         // 6.2 ширины колонок
		"function ldRowHeight",        // 6.2 высоты строк
		"function _ldRuler",           // 6.2 линейка
		"function ldToggleBorderSide", // 6.3 границы по сторонам
		"function ldBorderPreset",     // 6.3 пресеты
		"function ldBorderGridArea",   // 6.3 сетка области
	} {
		if !strings.Contains(js, sub) {
			t.Errorf("в JS редактора нет функции: %q", sub)
		}
	}
}

// 5b блок A: операции многоуровневых шапок — удаление ячейки, вертикальный
// merge/unmerge, раскладка по канону модели, отказ %-ширин.
func TestLayoutEditor_StageBJSFunctions(t *testing.T) {
	js := configuratorJS(t)
	for _, sub := range []string{
		"function ldDelCell",         // A.1 удаление одиночной ячейки
		"function ldMergeDown",       // A.2 вертикальный merge
		"function ldUnmergeVertical", // A.2 разъединение вниз
		"function _ldColLayout",      // канон раскладки спанов
		"function _ldVisualCol",
		"function _ldCellIndexAtCol",
	} {
		if !strings.Contains(js, sub) {
			t.Errorf("в JS редактора нет функции: %q", sub)
		}
	}
	// %-ширины отклоняются в ldColWidth.
	if !strings.Contains(js, "indexOf('%')") {
		t.Error("ldColWidth не отклоняет %-ширины")
	}
}

// 5b блок A: HTML панели редактора содержит кнопки удаления ячейки и
// вертикального merge/unmerge.
func TestLayoutEditor_StageBControls(t *testing.T) {
	html := renderLayoutPanelTree(t)
	for _, sub := range []string{
		`ldDelCell('Накладная')`,
		`ldMergeDown('Накладная')`,
		`ldUnmergeVertical('Накладная')`,
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML панели редактора нет фрагмента: %q", sub)
		}
	}
}

// Этап 6: JS редактора содержит операцию разреза области перед строкой и
// диалог импорта макета из PDF.
func TestLayoutEditor_SplitAndImportJS(t *testing.T) {
	js := configuratorJS(t)
	if !strings.Contains(js, "function splitLayoutArea") {
		t.Error("в JS редактора нет функции splitLayoutArea (разрез области)")
	}
	if !strings.Contains(js, "function cfgImportPdfLayout") {
		t.Error("в JS нет функции cfgImportPdfLayout (диалог импорта из PDF)")
	}
}

// UX-доводка редактора печатных форм: панель свойств не проматывает страницу
// (закреплённый док + закрытие), сворачивание YAML-панели, фоновая сетка как в
// 1С, кнопка импорта из PDF в тулбаре, PDF-предпросмотр во внешнем приложении,
// подсказка про составной текст {{выражение}}.
func TestLayoutEditor_UXImprovements(t *testing.T) {
	js := configuratorJS(t)
	for _, sub := range []string{
		"function ldAddCellAt",  // клик по фоновой сетке добавляет ячейку
		"function ldDeselect",   // закрытие панели свойств
		"function ldToggleYaml", // сворачивание YAML-панели
		"function _ldGridCols",  // ширина фоновой сетки
		`cfgToast(T("PDF открыт во внешнем приложении"))`, // #5 тост вместо inline-iframe (i18n рантайм, план 55 фаза 2b-1)
		"format=pdf&open=1",        // #5 PDF открывается сервером
		"function _ldPageInfo",     // границы листа: печатная ширина
		"function ldEnsurePage",    // материализация page без смены вывода
		"function ldSetPageMargin", // поля листа
	} {
		if !strings.Contains(js, sub) {
			t.Errorf("в JS редактора нет фрагмента: %q", sub)
		}
	}

	html := renderLayoutPanelTree(t)
	for _, sub := range []string{
		`id="yamlpane-Накладная"`,                                               // #2 сворачиваемая панель
		`ldToggleYaml('Накладная')`,                                             // #2 кнопка сворачивания
		`position:fixed;left:0;right:0;bottom:0;z-index:50`,                     // #1 закреплённый док
		`ldDeselect('Накладная')`,                                               // #1 закрытие дока
		`cfgImportPdfLayout('/bases/test-base/configurator/layout/import-pdf')`, // #6 кнопка «Из PDF» в тулбаре
		`{{Номер}}`, // #3 подсказка про интерполяцию
		"Откроется во внешнем приложении",                      // #5 подсказка на кнопке PDF
		`id="pg-fmt-Накладная"`,                                // границы листа: выбор формата
		`ldSetPageField('Накладная','orientation',this.value)`, // выбор ориентации
		`ldSetPageMargin('Накладная','left',this.value)`,       // поля листа
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML панели редактора нет фрагмента: %q", sub)
		}
	}
}

// Регулируемая высота дока свойств ячейки: ручка сверху панели (drag по
// вертикали), высота в --cfg-vprops-h + localStorage, сброс двойным кликом.
func TestLayoutEditor_PropsPanelResize(t *testing.T) {
	js := configuratorJS(t)
	for _, sub := range []string{
		"function ldPropsResizeStart", // drag ручки
		"function ldPropsResizeReset", // сброс по dblclick
		"--cfg-vprops-h",              // общая CSS-переменная высоты
		"cfgVPropsH",                  // ключ localStorage (восстановление в initResizers)
	} {
		if !strings.Contains(js, sub) {
			t.Errorf("в JS редактора нет фрагмента: %q", sub)
		}
	}

	html := renderLayoutPanelTree(t)
	for _, sub := range []string{
		`class="vprops-grip"`,
		`ldPropsResizeStart(event)`,
		`ldPropsResizeReset()`,
		`var(--cfg-vprops-h,44vh)`, // высота по умолчанию как раньше
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML панели редактора нет фрагмента: %q", sub)
		}
	}
}
