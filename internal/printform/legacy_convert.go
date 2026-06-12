package printform

import (
	"regexp"
	"strings"
)

// reBoldStrip/reItalicStrip снимают inline-markdown при конвертации (заменяют
// прежние reBold/reItalic из удалённого renderer.go).
var (
	reBoldStrip   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItalicStrip = regexp.MustCompile(`\*([^*]+)\*`)
	// reMoneyFmt ловит формат money внутри {{ выражение | money }} (регистро- и
	// пробело-независимо) — заменяется на number:2 при конвертации.
	reMoneyFmt = regexp.MustCompile(`(?i)(\{\{[^}|]*\|\s*)money(\s*\}\})`)
)

// mapMoneyInText заменяет «| money» на «| number:2» внутри {{...}}-плейсхолдеров
// текстовых блоков (title/header/footer). money в legacy НЕ был реализован —
// number:2 даёт честное число с двумя знаками.
func mapMoneyInText(text string) string {
	return reMoneyFmt.ReplaceAllString(text, "${1}number:2${2}")
}

// legacy_convert.go — конвертация устаревшей YAML-формы (PrintForm) в макет v2
// (LayoutTemplate + Binding) для декларативного движка (план 64, этап 4).
// Преобразование чисто структурное: интерполяцию {{...}} и резолв выражений
// делает движок (binding.go) при рендере — здесь только раскладка областей.
//
// Соответствие областей:
//   title  → «Заголовок»  (1 ячейка, bold, по центру, крупный шрифт, colspan)
//   header → «Шапка»      (построчный разбор markdown-подмножества)
//   table  → «ШапкаТаблицы» (метки) + «Строка» (параметры) + «Итоги»
//   footer → «Подвал»     (построчный разбор markdown-подмножества)
//
// money-форматтер: в legacy-рендере он НЕ был реализован (выводился как сырое
// значение). При конвертации заменяется на number:2 — честное число с двумя
// знаками. Это улучшение: раньше «Всего к оплате» печаталось как «1234.5», теперь
// форматируется единообразно с колонками-суммами.

const (
	// gridWidth по умолчанию, если в форме нет таблицы (заголовок/шапка/подвал
	// всё равно растягиваются на одну колонку).
	legacyDefaultGrid = 1
	titleFontSize     = 14
	h1FontSize        = 16
	h2FontSize        = 14
)

// ConvertLegacy преобразует устаревшую YAML-форму в макет v2 с binding.
func ConvertLegacy(pf *PrintForm) (*LayoutTemplate, error) {
	lt := &LayoutTemplate{
		Name:     pf.Name,
		Document: pf.Document,
	}

	// Ширина сетки = число колонок таблицы (или 1, если таблицы нет).
	grid := legacyDefaultGrid
	if pf.Table != nil && len(pf.Table.Columns) > 0 {
		grid = len(pf.Table.Columns)
		for _, col := range pf.Table.Columns {
			lt.Columns = append(lt.Columns, LayoutColumn{Width: col.Width})
		}
	}

	var sequence []string

	// Заголовок.
	if strings.TrimSpace(pf.Title) != "" {
		lt.Areas = append(lt.Areas, &LayoutArea{
			Name: "Заголовок",
			Rows: []LayoutRow{{Cells: []LayoutCell{{
				Text:     mapMoneyInText(pf.Title),
				Bold:     true,
				Align:    "center",
				FontSize: titleFontSize,
				ColSpan:  grid,
			}}}},
		})
		sequence = append(sequence, "Заголовок")
	}

	// Шапка (markdown).
	if strings.TrimSpace(pf.Header) != "" {
		lt.Areas = append(lt.Areas, &LayoutArea{
			Name: "Шапка",
			Rows: markdownRows(mapMoneyInText(pf.Header), grid),
		})
		sequence = append(sequence, "Шапка")
	}

	// Таблица → ШапкаТаблицы + Строка (+ Итоги) + binding.repeat.
	if pf.Table != nil && len(pf.Table.Columns) > 0 {
		thead, rowArea, repeat := convertTable(pf.Table)
		lt.Areas = append(lt.Areas, thead, rowArea)
		sequence = append(sequence, "ШапкаТаблицы", "Строка")

		if lt.Binding == nil {
			lt.Binding = &Binding{}
		}
		lt.Binding.Repeat = []RepeatBinding{repeat}

		if totals, totParams := convertTotals(pf.Table); totals != nil {
			lt.Areas = append(lt.Areas, totals)
			sequence = append(sequence, "Итоги")
			// Итоговые ячейки — не repeat-область, резолвятся через
			// binding.Parameters (контекст документа) выражением Итог.<ТЧ>.<поле>.
			if lt.Binding.Parameters == nil {
				lt.Binding.Parameters = make(map[string]string)
			}
			for k, v := range totParams {
				lt.Binding.Parameters[k] = v
			}
		}
	}

	// Подвал (markdown).
	if strings.TrimSpace(pf.Footer) != "" {
		lt.Areas = append(lt.Areas, &LayoutArea{
			Name: "Подвал",
			Rows: markdownRows(mapMoneyInText(pf.Footer), grid),
		})
		sequence = append(sequence, "Подвал")
	}

	if lt.Binding == nil {
		lt.Binding = &Binding{}
	}
	lt.Binding.Sequence = sequence

	return lt, nil
}

// convertTable строит области меток (ШапкаТаблицы) и параметров (Строка) и
// repeat-binding по строкам табличной части.
func convertTable(ts *TableSection) (thead, rowArea *LayoutArea, repeat RepeatBinding) {
	headCells := make([]LayoutCell, 0, len(ts.Columns))
	rowCells := make([]LayoutCell, 0, len(ts.Columns))
	params := make(map[string]string, len(ts.Columns))

	for i, col := range ts.Columns {
		// Метка-колонка (bold, границы all). Выравнивание — как у колонки.
		headCells = append(headCells, LayoutCell{
			Text:   col.Label,
			Bold:   true,
			Align:  col.Align,
			Border: "all",
		})

		// Имя параметра уникально (Кол<i>): несколько колонок могут ссылаться на
		// одно поле (например @row или две суммы) — имя поля как ключ привело бы к
		// коллизии в карте параметров.
		paramName := columnParamName(i)
		rowCells = append(rowCells, LayoutCell{
			Parameter: paramName,
			Align:     col.Align,
			Border:    "all",
		})
		params[paramName] = columnExpr(col)
	}

	thead = &LayoutArea{Name: "ШапкаТаблицы", Rows: []LayoutRow{{Cells: headCells}}}
	rowArea = &LayoutArea{Name: "Строка", Rows: []LayoutRow{{Cells: rowCells}}}
	repeat = RepeatBinding{Area: "Строка", Source: ts.Source, Parameters: params}
	return thead, rowArea, repeat
}

// convertTotals строит область «Итоги» из totals формы и карту параметров для
// binding (имя параметра → выражение Итог.<ТЧ>.<поле> | формат). Возвращает
// (nil, nil), если итогов нет. Каждая суммируемая колонка получает ячейку-
// параметр; прочие — пустую ячейку с границей (выравнивание сетки).
func convertTotals(ts *TableSection) (*LayoutArea, map[string]string) {
	if len(ts.Totals) == 0 {
		return nil, nil
	}
	// Карта: индекс колонки → спецификация итога.
	totByCol := make(map[int]TotalSpec)
	for _, tot := range ts.Totals {
		for ci, col := range ts.Columns {
			if strings.EqualFold(col.Field, tot.Field) {
				totByCol[ci] = tot
			}
		}
	}
	cells := make([]LayoutCell, 0, len(ts.Columns))
	params := make(map[string]string)
	for ci, col := range ts.Columns {
		if tot, ok := totByCol[ci]; ok {
			expr := "Итог." + ts.Source + "." + tot.Field
			if f := mapFormat(col.Format); f != "" {
				expr += " | " + f
			}
			if strings.TrimSpace(tot.Label) != "" {
				// Метка итога (как в legacy: «Итого: 650.00») сохраняется через
				// интерполируемый текст — параметр не несёт произвольного префикса.
				cells = append(cells, LayoutCell{
					Text:   tot.Label + ": {{" + expr + "}}",
					Align:  col.Align,
					Bold:   true,
					Border: "all",
				})
				continue
			}
			// Без метки — простая ячейка-параметр.
			name := totalParamName(ci)
			cells = append(cells, LayoutCell{
				Parameter: name,
				Align:     col.Align,
				Bold:      true,
				Border:    "all",
			})
			params[name] = expr
			continue
		}
		// Не-итоговая колонка: пустая ячейка с границей (выравнивание сетки).
		cells = append(cells, LayoutCell{Border: "all"})
	}
	return &LayoutArea{Name: "Итоги", Rows: []LayoutRow{{Cells: cells}}}, params
}

// columnExpr строит выражение языка binding для колонки: «поле | формат».
// @row остаётся @row; ссылочные поля (Поле.ПодПоле) проходят как есть; money
// в формате заменяется на number:2.
func columnExpr(col Column) string {
	expr := col.Field
	if f := mapFormat(col.Format); f != "" {
		expr += " | " + f
	}
	return expr
}

// columnParamName — имя параметра ячейки колонки (детерминированно по индексу).
func columnParamName(i int) string { return "Кол" + itoa(i) }

// totalParamName — имя параметра итоговой ячейки колонки.
func totalParamName(i int) string { return "Итог" + itoa(i) }

// mapFormat нормализует формат колонки/выражения. money → number:2 (legacy money
// не был реализован — выводился как сырое значение; number:2 честнее). Прочие
// форматы проходят без изменений.
func mapFormat(format string) string {
	f := strings.TrimSpace(format)
	if strings.EqualFold(f, "money") {
		return "number:2"
	}
	return f
}

// markdownRows разбирает markdown-подмножество (как в legacy renderer.go) в
// строки макета: каждая строка — одна ячейка colspan на всю ширину сетки.
//   - «# » → bold + fontSize 16
//   - «## » → bold + fontSize 14
//   - «**текст**» (вся строка обёрнута) → bold-ячейка
//   - смешанный inline-markdown → текст без маркеров (как есть)
//   - «---»/«___» → пустая ячейка с нижней границей (borders.bottom: thin)
//   - пустая строка → пустая строка-ячейка
func markdownRows(text string, grid int) []LayoutRow {
	lines := strings.Split(text, "\n")
	rows := make([]LayoutRow, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		cell := LayoutCell{ColSpan: grid}

		switch {
		case line == "":
			// пустая строка-ячейка.

		case line == "---" || line == "___":
			cell.Borders = &CellBorders{Bottom: "thin"}

		case strings.HasPrefix(line, "## "):
			cell.Text = strings.TrimSpace(line[3:])
			cell.Bold = true
			cell.FontSize = h2FontSize

		case strings.HasPrefix(line, "# "):
			cell.Text = strings.TrimSpace(line[2:])
			cell.Bold = true
			cell.FontSize = h1FontSize

		case wholeLineBold(line):
			// вся строка обёрнута в **...** → bold-ячейка, маркеры сняты.
			cell.Text = line[2 : len(line)-2]
			cell.Bold = true

		default:
			// смешанный inline-markdown: удаляем маркеры, текст как есть.
			cell.Text = stripInlineMarkdown(line)
		}
		rows = append(rows, LayoutRow{Cells: []LayoutCell{cell}})
	}
	return rows
}

// wholeLineBold сообщает, что вся строка обёрнута в **...** без вложенных **.
func wholeLineBold(line string) bool {
	if !strings.HasPrefix(line, "**") || !strings.HasSuffix(line, "**") || len(line) < 4 {
		return false
	}
	inner := line[2 : len(line)-2]
	return !strings.Contains(inner, "**")
}

// stripInlineMarkdown снимает маркеры **bold**/*italic*, оставляя текст. {{...}}
// не трогается — интерполяция делается движком.
func stripInlineMarkdown(s string) string {
	s = reBoldStrip.ReplaceAllString(s, "$1")
	s = reItalicStrip.ReplaceAllString(s, "$1")
	return s
}

// itoa — маленький helper без импорта strconv в этом файле (избегаем лишнего
// импорта; число всегда неотрицательное и небольшое).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
