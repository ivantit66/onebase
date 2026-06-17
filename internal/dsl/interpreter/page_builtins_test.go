package interpreter

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
)

// runPage исполняет процедуру с инжектированным построителем «Страница».
func runPage(t *testing.T, code string, b *DSLPageBuilder) {
	t.Helper()
	l := lexer.New(code, "<test>")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	i := New()
	var result any
	if err := i.RunWithResult(prog.Procedures[0], &MapThis{M: map[string]any{}}, &result, map[string]any{"Страница": b}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestPageBuilder_Blocks(t *testing.T) {
	b := NewPageBuilder()
	code := `Процедура Тест()
  Страница.Заголовок("Привет");
  Страница.Абзац("Текст");
  Страница.Показатель("Выручка", 1234.5, "money");
  Страница.Кнопка("Создать", "/ui/document/Счёт/new");
  Т = Страница.Таблица("Список");
  Т.Колонки("A", "B");
  С = Т.ДобавитьСтроку();
  С.Установить("A", "x");
  С.Ссылка("A", "/ui/catalog/Товар/1");
  С.Установить("B", 42);
КонецПроцедуры`
	runPage(t, code, b)

	blocks := b.Blocks()
	if len(blocks) != 5 {
		t.Fatalf("ожидалось 5 блоков, получено %d", len(blocks))
	}
	if blocks[0].Kind != "heading" || blocks[0].Text != "Привет" {
		t.Errorf("heading: %+v", blocks[0])
	}
	if blocks[1].Kind != "paragraph" || blocks[1].Text != "Текст" {
		t.Errorf("paragraph: %+v", blocks[1])
	}
	if blocks[2].Kind != "kpi" || blocks[2].Label != "Выручка" {
		t.Errorf("kpi: %+v", blocks[2])
	}
	if !strings.HasSuffix(blocks[2].Value, "₽") {
		t.Errorf("money формат, value=%q", blocks[2].Value)
	}
	if blocks[3].Kind != "button" || blocks[3].URL != "/ui/document/Счёт/new" {
		t.Errorf("button: %+v", blocks[3])
	}

	tbl := blocks[4]
	if tbl.Kind != "table" || tbl.Title != "Список" || len(tbl.Columns) != 2 || len(tbl.Rows) != 1 {
		t.Fatalf("table: %+v", tbl)
	}
	if c := tbl.Rows[0].Cells["A"]; c.Text != "x" || c.URL != "/ui/catalog/Товар/1" {
		t.Errorf("ячейка A: %+v", c)
	}
	if c := tbl.Rows[0].Cells["B"]; c.Text != "42" {
		t.Errorf("ячейка B: %+v", c)
	}
}

func TestPageBuilder_ChartAndList(t *testing.T) {
	b := NewPageBuilder()
	code := `Процедура Тест()
  Гр = Страница.График("Продажи", "line");
  Гр.Категории("Янв", "Фев");
  М = Новый Массив;
  М.Добавить(10);
  М.Добавить(20);
  Гр.Серия("Выручка", М);
  Сп = Страница.Список("Ссылки");
  Сп.Пункт("Главная", "/ui/");
  Сп.Пункт("Без ссылки");
КонецПроцедуры`
	runPage(t, code, b)

	blocks := b.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("ожидалось 2 блока, получено %d", len(blocks))
	}
	ch := blocks[0]
	if ch.Kind != "chart" || ch.Chart == nil {
		t.Fatalf("chart: %+v", ch)
	}
	if ch.Chart.Kind != "line" || len(ch.Chart.XAxis) != 2 || len(ch.Chart.Series) != 1 {
		t.Fatalf("данные графика: %+v", ch.Chart)
	}
	s := ch.Chart.Series[0]
	if s.Name != "Выручка" || len(s.Data) != 2 || s.Data[1] != 20 {
		t.Errorf("серия: %+v", s)
	}
	lst := blocks[1]
	if lst.Kind != "list" || len(lst.Items) != 2 {
		t.Fatalf("list: %+v", lst)
	}
	if lst.Items[0].Text != "Главная" || lst.Items[0].URL != "/ui/" {
		t.Errorf("пункт 0: %+v", lst.Items[0])
	}
	if lst.Items[1].URL != "" {
		t.Errorf("пункт 1 не должен иметь ссылку: %+v", lst.Items[1])
	}
}

func TestSanitizePageHTML(t *testing.T) {
	got := sanitizePageHTML(`<b>ok</b><script>alert(1)</script><a href="javascript:bad()" onclick="x()">t</a>`)
	for _, bad := range []string{"<script", "onclick", "javascript:"} {
		if strings.Contains(strings.ToLower(got), bad) {
			t.Errorf("санитайзер пропустил %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "<b>ok</b>") {
		t.Errorf("безопасный тег вырезан: %q", got)
	}
}
