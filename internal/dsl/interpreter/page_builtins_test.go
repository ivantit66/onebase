package interpreter

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/shopspring/decimal"
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

func TestPageBuilder_ActionButton(t *testing.T) {
	b := NewPageBuilder()
	code := `Процедура Тест()
  Страница.Кнопка("Открыть", "/ui/catalog/Товар");
  Страница.КнопкаДействие("Пересчитать", "ПересчитатьИтоги");
КонецПроцедуры`
	runPage(t, code, b)

	blocks := b.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("ожидалось 2 блока, получено %d", len(blocks))
	}
	// Обычная кнопка — переход по URL, без Action.
	if blocks[0].Kind != "button" || blocks[0].URL != "/ui/catalog/Товар" || blocks[0].Action != "" {
		t.Errorf("кнопка-ссылка: %+v", blocks[0])
	}
	// Кнопка-действие — имя серверной процедуры в Action, без URL.
	if blocks[1].Kind != "button" || blocks[1].Action != "ПересчитатьИтоги" || blocks[1].URL != "" {
		t.Errorf("кнопка-действие: %+v", blocks[1])
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

// TestSanitizePageHTML_CSSWhitelist — улучшение #29: глобальный style без
// валидации CSS открывал инъекцию (position:fixed overlay для кликджекинга,
// background:url(...) для утечки). Теперь style проходит через whitelist
// безопасных свойств: безопасное оформление (color) сохраняется, а опасные
// свойства/значения вырезаются.
func TestSanitizePageHTML_CSSWhitelist(t *testing.T) {
	// Безопасное свойство и класс должны проходить.
	safe := sanitizePageHTML(`<div class="card" style="color: red">x</div>`)
	if !strings.Contains(safe, `class="card"`) {
		t.Errorf("class вырезан: %q", safe)
	}
	if !strings.Contains(strings.ToLower(safe), "color") {
		t.Errorf("безопасное свойство color вырезано: %q", safe)
	}

	// Опасные значения/свойства должны быть отсечены.
	cases := []struct {
		name string
		html string
		bad  string
	}{
		{"position:fixed overlay (кликджекинг)", `<div style="position: fixed; top: 0; left: 0; width: 100%; height: 100%">x</div>`, "fixed"},
		{"background url() (утечка)", `<div style="background: url(https://evil.example/p.gif)">x</div>`, "url("},
		{"background-color url() (утечка)", `<div style="background-color: url(https://evil.example/p.gif)">x</div>`, "url("},
		{"expression() (legacy IE)", `<div style="width: expression(alert(1))">x</div>`, "expression"},
	}
	for _, c := range cases {
		got := strings.ToLower(sanitizePageHTML(c.html))
		if strings.Contains(got, c.bad) {
			t.Errorf("%s: опасное значение %q не вырезано: %q", c.name, c.bad, got)
		}
	}
}

// TestPageKPIDisplay_MoneyPrecision — улучшение #28: денежная сумма-decimal
// форматировалась через float64 (InexactFloat64), теряя младшие разряды на
// крупных суммах. Теперь decimal форматируется напрямую.
func TestPageKPIDisplay_MoneyPrecision(t *testing.T) {
	// nbsp — узкий неразрывный пробел (U+00A0), разделитель тысяч из pageGroupDigits.
	const nbsp = " "

	d, err := decimal.NewFromString("1234567890.12")
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	got := pageKPIDisplay(d, "money")
	// Стиль группировки/разделителей сохранён (pageGroupDigits): nbsp —
	// разделитель тысяч, запятая — десятичный.
	want := "1" + nbsp + "234" + nbsp + "567" + nbsp + "890,12 ₽"
	if got != want {
		t.Errorf("money decimal: получено %q, ожидалось %q", got, want)
	}

	// Сумма, которую float64 представляет неточно, не должна терять копейки.
	d2, _ := decimal.NewFromString("100000000000000.07")
	got2 := pageKPIDisplay(d2, "money")
	if !strings.HasSuffix(got2, ",07 ₽") {
		t.Errorf("копейки потеряны при крупной сумме: %q", got2)
	}

	// number: целочисленная группировка decimal без потери порядка.
	d3, _ := decimal.NewFromString("1234567890")
	got3 := pageKPIDisplay(d3, "number")
	want3 := "1" + nbsp + "234" + nbsp + "567" + nbsp + "890"
	if got3 != want3 {
		t.Errorf("number decimal: получено %q, ожидалось %q", got3, want3)
	}
}
