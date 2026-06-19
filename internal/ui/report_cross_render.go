package ui

// report_cross_render.go — HTML-рендер кросс-таблицы (pivot). Строковые измерения
// уходят влево, значения измерений-колонок — в шапку, на пересечении выводятся
// агрегаты показателей. Выравнивание, формат и условное оформление — общие с
// обычным режимом (measureAlign/fmtMeasure/cssOf), чтобы вид был единым.

import (
	"fmt"
	"html"
	"html/template"
	"strings"

	"github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/report/compose"
)

func renderCrossTable(cr *compose.CrossResult, spec *report.Composition) template.HTML {
	byField := make(map[string]report.Measure, len(spec.Measures))
	for _, m := range spec.Measures {
		byField[m.Field] = m
	}
	multiMeasure := len(spec.Measures) > 1

	var b strings.Builder
	b.WriteString(`<table class="report-composed report-cross">`)

	// Шапка: первый столбец — строковые измерения; далее по колонке на CrossCol.
	b.WriteString(`<thead><tr><th>` + html.EscapeString(strings.Join(spec.Groupings, " / ")) + `</th>`)
	for _, c := range cr.Cols {
		m := byField[c.Measure]
		b.WriteString(`<th class="num" style="` + html.EscapeString(measureAlign(m)) + `">` +
			html.EscapeString(crossColTitle(c, m, multiMeasure)) + `</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)

	// Тело: дерево строк.
	for _, row := range cr.Rows {
		writeCrossRow(&b, row, cr.Cols, byField, 0)
	}

	// Нижняя строка ВСЕГО — итоги по колонкам.
	b.WriteString(`<tr class="grand"><td>ВСЕГО</td>`)
	for _, c := range cr.Cols {
		m := byField[c.Measure]
		b.WriteString(`<td class="num" style="` + html.EscapeString(measureAlign(m)) + `">` +
			html.EscapeString(fmtMeasure(cr.RowTotal[c.Key()], m)) + `</td>`)
	}
	b.WriteString(`</tr></tbody></table>`)
	return template.HTML(b.String())
}

// writeCrossRow рисует строку дерева и рекурсивно её детей с нарастающим отступом.
// Узлы с детьми (промежуточные группы) несут подытоги по колонкам.
func writeCrossRow(b *strings.Builder, row *compose.CrossRow, cols []compose.CrossCol, byField map[string]report.Measure, level int) {
	pad := fmt.Sprintf("padding-left:%dpx", 8+level*18)
	cls := "rc-leaf"
	if len(row.Children) > 0 {
		cls = "rc-group"
	}
	fmt.Fprintf(b, `<tr class="%s"><td style="%s">%s</td>`, cls, pad, html.EscapeString(fmtVal(row.Key)))
	for _, c := range cols {
		m := byField[c.Measure]
		ck := c.Key()
		cell := joinStyles(measureAlign(m), cssOf(row.Styles[ck]))
		fmt.Fprintf(b, `<td class="num" style="%s">%s</td>`, html.EscapeString(cell), html.EscapeString(fmtMeasure(row.Cells[ck], m)))
	}
	b.WriteString(`</tr>`)
	for _, ch := range row.Children {
		writeCrossRow(b, ch, cols, byField, level+1)
	}
}

// crossColTitle — подпись колонки: значение(я) пути через « / »; при нескольких
// показателях добавляется название показателя, чтобы колонки различались.
func crossColTitle(c compose.CrossCol, m report.Measure, multiMeasure bool) string {
	parts := make([]string, len(c.Path))
	for i, p := range c.Path {
		parts[i] = fmtVal(p)
	}
	title := strings.Join(parts, " / ")
	if multiMeasure {
		title += " · " + measureTitle(m)
	}
	return title
}
