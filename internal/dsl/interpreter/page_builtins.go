package interpreter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Объект-построитель «Страница» (план 66). Передаётся в обработчик страницы
// (src/<имя>.page.os, Процедура ПриФормировании(Страница, Параметры)) как
// мутируемый объект — обработчик наполняет его блоками, а UI-слой
// (internal/ui) рендерит собранные блоки в оболочку приложения с
// автоэкранированием и i18n. Строится UI-слоем через NewPageBuilder().
//
// Сознательно НЕ принимаем «строку HTML» как основной путь: блоки описывают
// структуру (заголовок/текст/показатель/таблица/кнопка), которую шаблон
// экранирует. Произвольный HTML доступен только явным ДобавитьСыройHTML и
// помечается «на ответственности автора».

// PageBlock — один отрендеренный блок страницы. Экспортирован, чтобы UI-слой
// мог пройтись по результату (PageBuilder.Blocks()). Поля заполняются по Kind.
type PageBlock struct {
	Kind string // heading | paragraph | kpi | table | button | divider | raw

	Text  string // heading/paragraph/button: текст; kpi: подпись не здесь (см. Label)
	URL   string // button: адрес перехода
	Label string // kpi: подпись
	Value string // kpi: уже отформатированное значение
	Title string // table: заголовок таблицы

	Columns []string  // table: заголовки колонок
	Rows    []PageRow // table: строки

	Items []PageListItem // list: пункты списка
	Chart *PageChart     // chart: данные графика

	HTML string // raw: санитизированный HTML (только ДобавитьСыройHTML)
}

// PageListItem — пункт списка: текст и необязательная ссылка.
type PageListItem struct {
	Text string
	URL  string
}

// PageChart — данные графика (план 66). Сериализуется в опции ECharts тем же
// EChartsOption, что и виджеты рабочего стола.
type PageChart struct {
	Kind   string // bar | line | pie
	XAxis  []string
	Series []PageSeries
}

// PageSeries — одна серия графика, выровненная по XAxis.
type PageSeries struct {
	Name string
	Data []float64
}

// PageRow — строка таблицы. Ячейки адресуются по имени колонки.
type PageRow struct {
	Cells map[string]PageCell
}

// PageCell — ячейка таблицы: текст и необязательная ссылка (кликабельная ячейка).
type PageCell struct {
	Text string
	URL  string
}

// DSLPageBuilder — объект «Страница» в DSL. Реализует This (Get/Set) и
// MethodCallable (CallMethod).
type DSLPageBuilder struct {
	blocks []PageBlock
}

// NewPageBuilder создаёт пустой построитель страницы (для UI-роутера).
func NewPageBuilder() *DSLPageBuilder { return &DSLPageBuilder{} }

// NewStringMap строит Соответствие (Map) из строковых пар — UI-слой передаёт им
// «Параметры» страницы в обработчик.
func NewStringMap(src map[string]string) *Map { return mapFromStrings(src) }

// Blocks возвращает собранные блоки в порядке добавления (для рендера).
func (b *DSLPageBuilder) Blocks() []PageBlock { return b.blocks }

func (b *DSLPageBuilder) Get(string) any  { return nil }
func (b *DSLPageBuilder) Set(string, any) {}

func (b *DSLPageBuilder) CallMethod(name string, args []any) any {
	switch name {
	case "заголовок", "heading":
		b.blocks = append(b.blocks, PageBlock{Kind: "heading", Text: argStr(args, 0)})
		return b
	case "абзац", "текст", "paragraph", "text":
		b.blocks = append(b.blocks, PageBlock{Kind: "paragraph", Text: argStr(args, 0)})
		return b
	case "показатель", "kpi":
		format := ""
		if len(args) >= 3 {
			format = argStr(args, 2)
		}
		var val any
		if len(args) >= 2 {
			val = args[1]
		}
		b.blocks = append(b.blocks, PageBlock{
			Kind:  "kpi",
			Label: argStr(args, 0),
			Value: pageKPIDisplay(val, format),
		})
		return b
	case "кнопка", "ссылка", "button", "link":
		b.blocks = append(b.blocks, PageBlock{Kind: "button", Text: argStr(args, 0), URL: argStr(args, 1)})
		return b
	case "разделитель", "divider":
		b.blocks = append(b.blocks, PageBlock{Kind: "divider"})
		return b
	case "сыройhtml", "добавитьсыройhtml", "rawhtml", "addrawhtml":
		b.blocks = append(b.blocks, PageBlock{Kind: "raw", HTML: sanitizePageHTML(argStr(args, 0))})
		return b
	case "таблица", "table":
		b.blocks = append(b.blocks, PageBlock{Kind: "table", Title: argStr(args, 0)})
		return &DSLPageTable{builder: b, idx: len(b.blocks) - 1}
	case "список", "list":
		b.blocks = append(b.blocks, PageBlock{Kind: "list", Title: argStr(args, 0)})
		return &DSLPageList{builder: b, idx: len(b.blocks) - 1}
	case "график", "chart":
		kind := "bar"
		if len(args) >= 2 {
			kind = strings.ToLower(argStr(args, 1))
		}
		b.blocks = append(b.blocks, PageBlock{Kind: "chart", Title: argStr(args, 0), Chart: &PageChart{Kind: kind}})
		return &DSLPageChart{builder: b, idx: len(b.blocks) - 1}
	}
	panic(userError{Msg: "Страница: неизвестный метод " + name})
}

// DSLPageList — дескриптор списка внутри построителя.
type DSLPageList struct {
	builder *DSLPageBuilder
	idx     int
}

func (l *DSLPageList) Get(string) any  { return nil }
func (l *DSLPageList) Set(string, any) {}

func (l *DSLPageList) CallMethod(name string, args []any) any {
	switch name {
	case "пункт", "добавить", "item", "add":
		it := PageListItem{Text: argStr(args, 0)}
		if len(args) >= 2 {
			it.URL = argStr(args, 1)
		}
		l.builder.blocks[l.idx].Items = append(l.builder.blocks[l.idx].Items, it)
		return l
	}
	panic(userError{Msg: "Страница.Список: неизвестный метод " + name})
}

// DSLPageChart — дескриптор графика внутри построителя.
type DSLPageChart struct {
	builder *DSLPageBuilder
	idx     int
}

func (c *DSLPageChart) Get(string) any  { return nil }
func (c *DSLPageChart) Set(string, any) {}

func (c *DSLPageChart) chart() *PageChart { return c.builder.blocks[c.idx].Chart }

func (c *DSLPageChart) CallMethod(name string, args []any) any {
	switch name {
	case "категории", "categories":
		var cols []string
		if len(args) == 1 {
			if a, ok := args[0].(*Array); ok {
				for _, it := range a.items {
					cols = append(cols, pageValueString(it))
				}
				c.chart().XAxis = cols
				return c
			}
		}
		for i := range args {
			cols = append(cols, argStr(args, i))
		}
		c.chart().XAxis = cols
		return c
	case "серия", "series":
		s := PageSeries{Name: argStr(args, 0)}
		if len(args) >= 2 {
			s.Data = toFloatSlice(args[1])
		}
		c.chart().Series = append(c.chart().Series, s)
		return c
	}
	panic(userError{Msg: "Страница.График: неизвестный метод " + name})
}

// toFloatSlice превращает DSL-Массив (или одиночное число) в []float64.
func toFloatSlice(v any) []float64 {
	var out []float64
	if a, ok := v.(*Array); ok {
		for _, it := range a.items {
			f, _ := toFloat(it)
			out = append(out, f)
		}
		return out
	}
	if f, ok := toFloat(v); ok {
		out = append(out, f)
	}
	return out
}

// DSLPageTable — дескриптор табличного блока внутри построителя. Мутирует блок
// по индексу, поэтому добавление других блоков позже его не ломает.
type DSLPageTable struct {
	builder *DSLPageBuilder
	idx     int
}

func (t *DSLPageTable) Get(string) any  { return nil }
func (t *DSLPageTable) Set(string, any) {}

func (t *DSLPageTable) CallMethod(name string, args []any) any {
	switch name {
	case "колонки", "columns":
		cols := make([]string, 0, len(args))
		for i := range args {
			cols = append(cols, argStr(args, i))
		}
		t.builder.blocks[t.idx].Columns = cols
		return t
	case "добавитьстроку", "addrow":
		row := PageRow{Cells: map[string]PageCell{}}
		t.builder.blocks[t.idx].Rows = append(t.builder.blocks[t.idx].Rows, row)
		return &DSLPageRow{builder: t.builder, block: t.idx, row: len(t.builder.blocks[t.idx].Rows) - 1}
	}
	panic(userError{Msg: "Страница.Таблица: неизвестный метод " + name})
}

// DSLPageRow — дескриптор строки таблицы. Ячейки адресуются по имени колонки.
type DSLPageRow struct {
	builder *DSLPageBuilder
	block   int
	row     int
}

func (r *DSLPageRow) Get(string) any  { return nil }
func (r *DSLPageRow) Set(string, any) {}

func (r *DSLPageRow) cell() map[string]PageCell {
	return r.builder.blocks[r.block].Rows[r.row].Cells
}

func (r *DSLPageRow) CallMethod(name string, args []any) any {
	switch name {
	case "установить", "set":
		if len(args) >= 2 {
			col := argStr(args, 0)
			c := r.cell()[col]
			c.Text = pageValueString(args[1])
			r.cell()[col] = c
		}
		return r
	case "ссылка", "link":
		if len(args) >= 2 {
			col := argStr(args, 0)
			c := r.cell()[col]
			c.URL = argStr(args, 1)
			r.cell()[col] = c
		}
		return r
	}
	panic(userError{Msg: "Страница.Таблица.Строка: неизвестный метод " + name})
}

// ─── вспомогательные ──────────────────────────────────────────────────────────

// sanitizePageHTML — консервативная очистка произвольного HTML из
// ДобавитьСыройHTML: вырезает блоки <script>/<style>, обработчики on*= и
// javascript:-URI. Не полноценный санитайзер DOM; задаёт нижнюю планку
// безопасности для escape-hatch'а, который по умолчанию не используется.
// RE2 (пакет regexp) не поддерживает обратные ссылки, поэтому script/style
// закрываем отдельными выражениями, плюс выметаем любые «осиротевшие» теги.
var (
	reScriptBlock = regexp.MustCompile(`(?is)<\s*script\b[^>]*>.*?<\s*/\s*script\s*>`)
	reStyleBlock  = regexp.MustCompile(`(?is)<\s*style\b[^>]*>.*?<\s*/\s*style\s*>`)
	reStrayTag    = regexp.MustCompile(`(?is)<\s*/?\s*(script|style)\b[^>]*>`)
	reEventAttr   = regexp.MustCompile(`(?is)\son[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	reJSURI       = regexp.MustCompile(`(?is)(href|src)\s*=\s*("\s*javascript:[^"]*"|'\s*javascript:[^']*'|javascript:[^\s>]+)`)
)

func sanitizePageHTML(s string) string {
	s = reScriptBlock.ReplaceAllString(s, "")
	s = reStyleBlock.ReplaceAllString(s, "")
	s = reStrayTag.ReplaceAllString(s, "")
	s = reEventAttr.ReplaceAllString(s, "")
	s = reJSURI.ReplaceAllString(s, `$1="#"`)
	return s
}

func argStr(args []any, i int) string {
	if i >= len(args) {
		return ""
	}
	return pageValueString(args[i])
}

func pageValueString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// pageKPIDisplay форматирует значение показателя по подсказке формата
// (money/percent/number). Зеркалит поведение виджетов, но самодостаточно, чтобы
// не тянуть в интерпретатор пакет widget.
func pageKPIDisplay(v any, format string) string {
	switch strings.ToLower(format) {
	case "money":
		if f, ok := toFloat(v); ok {
			return pageGroupInt(f, 2) + " ₽"
		}
	case "percent":
		if f, ok := toFloat(v); ok {
			return strconv.FormatFloat(f, 'f', 1, 64) + "%"
		}
	case "number":
		if f, ok := toFloat(v); ok {
			return pageGroupInt(f, 0)
		}
	}
	return pageValueString(v)
}

// pageGroupInt форматирует число с разделителем тысяч (узкий неразрывный пробел)
// и frac знаками после запятой. Русская конвенция: десятичная запятая.
func pageGroupInt(f float64, frac int) string {
	neg := f < 0
	if neg {
		f = -f
	}
	s := strconv.FormatFloat(f, 'f', frac, 64)
	intPart, fracPart := s, ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart, fracPart = s[:dot], s[dot+1:]
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	rem := len(intPart) % 3
	if rem > 0 {
		b.WriteString(intPart[:rem])
	}
	for i := rem; i < len(intPart); i += 3 {
		if i > 0 {
			b.WriteRune(' ')
		}
		b.WriteString(intPart[i : i+3])
	}
	if fracPart != "" {
		b.WriteByte(',')
		b.WriteString(fracPart)
	}
	return b.String()
}
