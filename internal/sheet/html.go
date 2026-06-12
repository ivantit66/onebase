package sheet

import (
	"fmt"
	"strings"
)

// HTMLOptions — параметры HTML-рендера табличного документа.
// BackURL: если задан, кнопка «Назад» ведёт по ссылке, иначе history.back().
// PDFURL: если задан, кнопка «PDF» в тулбаре ведёт на серверный PDF-endpoint;
// при пустом PDFURL вывод бит-в-бит совпадает с прежним (кнопка → window.print()).
// Точная семантика повторяет прежний toHTML из interpreter.
type HTMLOptions struct {
	BackURL string
	PDFURL  string
}

// HTMLString рендерит документ в HTML, используя BackURL из самого документа.
// Удобная обёртка над HTML(HTMLOptions{BackURL: d.BackURL}).
func (d *Document) HTMLString() string {
	return d.HTML(HTMLOptions{BackURL: d.BackURL})
}

// HTML конвертирует документ в HTML-представление (перенос toHTML без изменений
// поведения: тулбар, стили, таблица со спанами через карту covered).
func (d *Document) HTML(opts HTMLOptions) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<title>Табличный документ</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
@page{margin:1cm}
body{font-family:'Times New Roman',Times,serif;font-size:10pt;color:#000;padding:0}
.doc-toolbar{position:sticky;top:0;z-index:10;background:#f5f5f5;border-bottom:1px solid #ddd;padding:8px 16px;display:flex;gap:8px;align-items:center;font-family:Arial,sans-serif;font-size:13px}
.doc-toolbar button,.doc-toolbar a.btn{padding:6px 16px;border:1px solid #bbb;border-radius:4px;background:#fff;cursor:pointer;font-size:13px;text-decoration:none;color:#333;display:inline-block}
.doc-toolbar button:hover,.doc-toolbar a.btn:hover{background:#e8e8e8}
.doc-toolbar .btn-back{margin-right:auto}
.doc-toolbar .btn-print{background:#4a9;color:#fff;border-color:#4a9}
.doc-toolbar .btn-print:hover{background:#398}
.doc-content{padding:20px}
table{border-collapse:collapse;width:auto}
td,th{border:1px solid #cbd5e1;padding:3px 6px;empty-cells:show;font-size:13px}
tr:nth-child(even) td{background:#f8fafc}
@media print{.doc-toolbar{display:none}.doc-content{padding:0}}
</style>
</head>
<body>
<div class="doc-toolbar">
`)
	// Кнопка «Назад»: ссылка на документ если задан BackURL, иначе history.back().
	if opts.BackURL != "" {
		sb.WriteString(`<a class="btn btn-back" href="` + escapeHTML(opts.BackURL) + `">&#8592; Назад</a>`)
	} else {
		sb.WriteString(`<button class="btn-back" onclick="history.back()">&#8592; Назад</button>`)
	}
	sb.WriteString(`
<button class="btn-print" onclick="window.print()">&#128424; Печать</button>
`)
	// Кнопка «PDF»: при заданном PDFURL — ссылка на серверный PDF, иначе
	// прежнее поведение (window.print()) — бит-в-бит как раньше.
	if opts.PDFURL != "" {
		sb.WriteString(`<a class="btn" href="` + escapeHTML(opts.PDFURL) + `">&#128196; PDF</a>`)
	} else {
		sb.WriteString(`<button onclick="window.print()">&#128196; PDF</button>`)
	}
	sb.WriteString(`
</div>
<div class="doc-content">
<table>`)

	maxRow, maxCol := d.ContentBounds()

	// Ячейки, перекрытые colspan/rowspan.
	covered := make(map[CellKey]bool)

	for row := 0; row <= maxRow; row++ {
		sb.WriteString("<tr>")
		for col := 0; col <= maxCol; col++ {
			if covered[CellKey{row, col}] {
				continue
			}

			cell := d.GetCell(row, col)
			// Эффективные размеры: колоночная/строчная (1-based индексы на
			// Document) имеют приоритет, иначе — индивидуальный размер ячейки.
			width := d.ColWidths[col+1]
			if width == 0 && cell != nil {
				width = cell.Width
			}
			height := d.RowHeights[row+1]
			if height == 0 && cell != nil {
				height = cell.Height
			}

			var attrs, style string

			if cell != nil {
				style = buildCellStyle(cell, width, height)
				if cell.ColSpan > 1 {
					attrs += fmt.Sprintf(` colspan="%d"`, cell.ColSpan)
					for c := col + 1; c < col+cell.ColSpan; c++ {
						covered[CellKey{row, c}] = true
					}
				}
				if cell.RowSpan > 1 {
					attrs += fmt.Sprintf(` rowspan="%d"`, cell.RowSpan)
					for r := row + 1; r < row+cell.RowSpan; r++ {
						covered[CellKey{r, col}] = true
					}
				}
			} else {
				style = sizeStyle(width, height)
			}

			text := ""
			if cell != nil {
				text = escapeHTML(cell.Text)
			}

			if attrs != "" || style != "" {
				sb.WriteString(fmt.Sprintf("<td%s style=\"%s\">%s</td>", attrs, style, text))
			} else {
				sb.WriteString(fmt.Sprintf("<td>%s</td>", text))
			}
		}
		sb.WriteString("</tr>\n")
	}

	sb.WriteString(`</table>
</div>
</body>
</html>`)
	return sb.String()
}

// buildCellStyle строит CSS-стиль ячейки. width/height — эффективные размеры
// (колоночная/строчная с Document либо индивидуальная ячейки), вычислены вызывающим.
func buildCellStyle(cell *Cell, width, height float64) string {
	var styles []string

	if cell.Align != "" && cell.Align != "left" {
		styles = append(styles, "text-align:"+cell.Align)
	}
	if cell.Vertical != "" && cell.Vertical != "top" {
		styles = append(styles, "vertical-align:"+cell.Vertical)
	}
	if cell.Bold {
		styles = append(styles, "font-weight:bold")
	}
	if cell.Italic {
		styles = append(styles, "font-style:italic")
	}
	if cell.FontSize > 0 && cell.FontSize != 10 {
		styles = append(styles, fmt.Sprintf("font-size:%dpt", cell.FontSize))
	}
	if cell.FontFamily != "" && cell.FontFamily != "Times New Roman" {
		styles = append(styles, "font-family:"+cell.FontFamily)
	}
	if width > 0 {
		styles = append(styles, fmt.Sprintf("width:%.2fpx", width))
	}
	if height > 0 {
		styles = append(styles, fmt.Sprintf("height:%.2fpx", height))
	}
	if cell.BackColor != "" {
		styles = append(styles, "background-color:"+cell.BackColor)
	}
	if cell.TextColor != "" {
		styles = append(styles, "color:"+cell.TextColor)
	}

	return strings.Join(styles, ";")
}

// sizeStyle строит CSS только из размеров — для пустой (несуществующей) ячейки
// в пределах содержимого, где задана колоночная ширина / строчная высота.
func sizeStyle(width, height float64) string {
	var styles []string
	if width > 0 {
		styles = append(styles, fmt.Sprintf("width:%.2fpx", width))
	}
	if height > 0 {
		styles = append(styles, fmt.Sprintf("height:%.2fpx", height))
	}
	return strings.Join(styles, ";")
}

// escapeHTML экранирует спецсимволы HTML (&, <, >, ").
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
