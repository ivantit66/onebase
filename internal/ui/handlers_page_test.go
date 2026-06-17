package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
)

// TestPageCustomTemplate отрисовывает шаблон page-custom со всеми типами блоков.
// Ловит ошибки доступа к полям и вызовов FuncMap (pageRaw/pageChart/echartsJSON)
// без поднятия полноценного сервера.
func TestPageCustomTemplate(t *testing.T) {
	b := interpreter.NewPageBuilder()
	b.CallMethod("заголовок", []any{"Заголовок"})
	b.CallMethod("абзац", []any{"Абзац"})
	b.CallMethod("показатель", []any{"KPI", 42.0, "number"})
	b.CallMethod("кнопка", []any{"Кнопка", "/ui/"})
	b.CallMethod("разделитель", nil)
	b.CallMethod("добавитьсыройhtml", []any{"<b>ok</b>"})

	tbl := b.CallMethod("таблица", []any{"Таблица"}).(*interpreter.DSLPageTable)
	tbl.CallMethod("колонки", []any{"A"})
	row := tbl.CallMethod("добавитьстроку", nil).(*interpreter.DSLPageRow)
	row.CallMethod("установить", []any{"A", "x"})
	row.CallMethod("ссылка", []any{"A", "/ui/catalog/Товар/1"})

	lst := b.CallMethod("список", []any{"Список"}).(*interpreter.DSLPageList)
	lst.CallMethod("пункт", []any{"Пункт", "/ui/"})

	ch := b.CallMethod("график", []any{"График", "line"}).(*interpreter.DSLPageChart)
	ch.CallMethod("категории", []any{"Янв", "Фев"})
	ch.CallMethod("серия", []any{"S", interpreter.NewArray([]any{1.0, 2.0})})

	var buf bytes.Buffer
	data := map[string]any{
		"PageTitle":    "Тест",
		"PageBlocks":   b.Blocks(),
		"PageHasChart": true,
		"Cfg":          Config{},
		"Lang":         "ru",
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-custom", data); err != nil {
		t.Fatalf("execute page-custom: %v", err)
	}
	out := buf.String()
	// URL в href нормализуется html/template (кириллица percent-кодируется),
	// поэтому проверяем ASCII-префикс пути ячейки-ссылки.
	for _, want := range []string{"Заголовок", "<b>ok</b>", "/ui/catalog/", "data-pagechart", "echarts.min.js"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q", want)
		}
	}
	// Сырой HTML не должен пройти экранирование (pageRaw), а текст блоков —
	// должен (нет «живого» тега из текста).
	if strings.Contains(out, "&lt;b&gt;ok") {
		t.Errorf("сырой HTML был экранирован")
	}
}
