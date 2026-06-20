package launcher

import (
	"bytes"
	"strings"
	"testing"
)

// TestConfigurator_XSS_Escaped: имена объектов конфигурации экранируются и в
// HTML-, и в JS-контексте (план 55 этап 2 — закрывает XSS-долг плана 47 §1.3).
// До перехода на html/template падает: text/template вставляет payload сырым.
func TestConfigurator_XSS_Escaped(t *testing.T) {
	const payload = `<img src=x onerror=alert(1)>'";</script>`
	// M2: брейкаут-литерал, рендерящийся через {{js .GlobalHome.Rows}} внутри
	// <script> — проверяет funcmap "js" (json.Marshal → template.JS).
	const jsBreakout = `</script><script>alert(1)</script>`
	data := &configuratorData{
		Base:           &Base{ID: "b", Name: "Тест", ConfigSource: "file"},
		Lang:           "ru",
		Tab:            "tree",
		Catalogs:       []cfgEntity{{Name: payload, Kind: "Справочник"}},
		AllEntityNames: []string{payload},
		// Виджет нужен, чтобы сработал {{if .Widgets}} вокруг строки с {{js .GlobalHome.Rows}}.
		Widgets:    []cfgWidget{{Name: "w", Type: "kpi"}},
		GlobalHome: cfgHomePage{Rows: [][]string{{jsBreakout}}},
	}
	// Bootstrap заполняет window.__cfg (как в renderCfg) — иначе payload из
	// AllEntityNames не попадёт в вывод, и JS-гард станет вакуумным.
	populateBootstrap(data, "ru")
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "cfg-main", data); err != nil {
		t.Fatalf("ExecuteTemplate cfg-main: %v", err)
	}
	out := buf.String()
	// Нигде не должно быть сырого payload (грубый брейкаут-гард).
	if strings.Contains(out, payload) {
		t.Error("сырой XSS-payload в выводе")
	}
	// HTML-контекст (текст/атрибут дерева): имя должно быть HTML-экранировано.
	if !strings.Contains(out, "&lt;img") {
		t.Error("HTML-контекст: ожидалась esc-форма &lt;img — её нет")
	}
	// JS-контекст: payload из AllEntityNames теперь уходит в window.__cfg
	// (bootstrap-JSON), где json.Marshal экранирует '<' как <. Проверяем и
	// весь вывод, и сам блоб — чтобы гард был неваккуумным (а не проходил через
	// несвязанный HTML дерева).
	if !strings.Contains(out, "\\u003cimg") {
		t.Error("JS-контекст: ожидалась esc-форма \\u003cimg в выводе — её нет")
	}
	boot := string(data.Bootstrap)
	if !strings.Contains(boot, "\\u003cimg") {
		t.Error("bootstrap: payload в window.__cfg не экранирован (\\u003cimg)")
	}
	if strings.Contains(boot, "<img") {
		t.Error("bootstrap: сырой <img в window.__cfg")
	}
	// M2: funcmap "js" (json.Marshal) экранирует < как < — брейкаут из
	// <script> закрыт, и нет двойного экранирования (\\u003c).
	if strings.Contains(out, jsBreakout) {
		t.Error("js-funcmap: сырой </script>-брейкаут в выводе")
	}
	if !strings.Contains(out, "\\u003c/script") {
		t.Error("js-funcmap: ожидалась esc-форма \\u003c/script — её нет")
	}
	if strings.Contains(out, `\\u003c`) {
		t.Error("js-funcmap: двойное экранирование \\\\u003c")
	}
}
