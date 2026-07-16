package ui

import (
	"strings"
	"testing"
)

// Ответ ИИ-ассистента (пользовательский режим) должен рендериться как Markdown
// → HTML (таблицы/жирный/списки), а не вставляться сырым текстом. Регресс: до
// этого `pend.textContent = d.text` показывал markdown-таблицу как «|---|».
func TestAIChat_RendersMarkdown(t *testing.T) {
	js := string(uiJS)
	for _, want := range []string{
		"function mdToHtml",
		"pend.innerHTML = mdToHtml(",
		"role === 'assistant'",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("в ui.js нет %q — ответ ассистента не рендерит markdown", want)
		}
	}
	// Регресс на баг сентинела: в файле не должно быть сырых null-байтов —
	// сентинел инлайн-кода в mdToHtml должен быть -эскейпом, не 0x00.
	if strings.ContainsRune(js, 0) {
		t.Error("в ui.js есть сырой null-байт — сентинел mdToHtml должен быть \\u0000-эскейпом")
	}

	// Оформление таблиц/кода ответа присутствует в общем layout.
	html := renderPage(t, "page-index")
	for _, want := range []string{
		"#ob-ai-log .m.a table",
		"#ob-ai-log .m.a th",
		"#ob-ai-log .m.a code",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в стилях нет %q — оформление ответа ассистента не подключено", want)
		}
	}
}
