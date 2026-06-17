package ui

// report_compose_excel.go — экспорт скомпонованного отчёта (СКД) в Excel.
// Обход дерева аналогичен renderComposedTable, но вместо HTML строит [][]any
// для передачи в excel.ExportList.

import (
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/report/compose"
)

// composedRows строит заголовки и строки данных для excel.ExportList
// из скомпонованного результата. Порядок строк совпадает с renderComposedTable:
//
//	группа → [дети/детали] → подытог … → ВСЕГО
//
// Первая колонка — имена/ключи групп с текстовым отступом (2 пробела × уровень).
// Значения показателей передаются как float64, чтобы Excel видел числа, а не текст.
func composedRows(res *compose.Result, spec *report.Composition) (headers []string, rows [][]any) {
	// Формируем заголовок: группировки через " / ", затем показатели.
	headers = make([]string, 0, 1+len(spec.Measures))
	headers = append(headers, strings.Join(spec.Groupings, " / "))
	for _, m := range spec.Measures {
		headers = append(headers, measureTitle(m))
	}

	// Обходим дерево групп.
	for _, g := range res.Groups {
		rows = appendGroup(rows, g, spec, 0)
	}

	// Строка общего итога «ВСЕГО».
	if spec.Totals.Grand {
		row := make([]any, 1+len(spec.Measures))
		row[0] = "ВСЕГО"
		for i, m := range spec.Measures {
			row[i+1] = numFor(res.Grand[m.Field])
		}
		rows = append(rows, row)
	}
	return headers, rows
}

// appendGroup рекурсивно добавляет строки группы: строку группы, дочерние группы
// (или детали), и при Totals.Subtotals — строку подытога.
func appendGroup(rows [][]any, g *compose.Group, spec *report.Composition, level int) [][]any {
	indent := strings.Repeat("  ", level)
	colCount := 1 + len(spec.Measures)

	// Строка группы: ключ с отступом + значения показателей.
	grpRow := make([]any, colCount)
	grpRow[0] = indent + fmtVal(g.Key)
	for i, m := range spec.Measures {
		grpRow[i+1] = numFor(g.Subtotals[m.Field])
	}
	rows = append(rows, grpRow)

	// Дочерние группы (рекурсия).
	for _, ch := range g.Children {
		rows = appendGroup(rows, ch, spec, level+1)
	}

	// Детальные строки (если Detail=true). По дизайну compose, g.Details заполняется
	// только у листовых групп (level == последний уровень группировки); у внутренних
	// групп g.Details всегда пуст, поэтому явная проверка Children не нужна.
	if spec.Detail {
		childIndent := strings.Repeat("  ", level+1)
		for _, d := range g.Details {
			detRow := make([]any, colCount)
			// Первая ячейка детали намеренно содержит только отступ — зеркалит
			// HTML-рендер (writeDetail). Осмысленный идентификатор строки (ссылка на
			// документ) появится в задаче B3 (detail_link).
			detRow[0] = childIndent
			for i, m := range spec.Measures {
				detRow[i+1] = numFor(d.Values[m.Field])
			}
			rows = append(rows, detRow)
		}
	}

	// Строка подытога.
	if spec.Totals.Subtotals {
		subIndent := strings.Repeat("  ", level+1)
		subRow := make([]any, colCount)
		subRow[0] = fmt.Sprintf("%s··· Итого: %s ···", subIndent, fmtVal(g.Key))
		for i, m := range spec.Measures {
			subRow[i+1] = numFor(g.Subtotals[m.Field])
		}
		rows = append(rows, subRow)
	}

	return rows
}
