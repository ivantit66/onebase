package compose

// cross.go — режим кросс-таблицы (pivot): измерения spec.Groupings разворачиваются
// в строки, spec.Columns — в колонки, на пересечении агрегируются spec.Measures.
// Режим включается, когда spec.Columns непуст (диспетчеризация — в обработчике).
// Ядро переиспользует aggregate/aggMeasure/normalizeGroupKey/alignRowKeys обычного
// режима, поэтому вычисляемые показатели (Expr) и регистронезависимость работают
// одинаково.

import (
	"sort"
	"strings"

	"github.com/ivantit66/onebase/internal/report"
)

// CrossCol — колонка кросс-таблицы: путь значений по измерениям spec.Columns
// (для многоуровневых колонок) плюс показатель. При нескольких показателях один
// путь даёт по колонке на каждый показатель.
type CrossCol struct {
	Path    []any  // значения по spec.Columns; len == len(spec.Columns)
	Measure string // Field показателя
}

// Key — стабильный строковый ключ колонки (ключ в CrossRow.Cells / RowTotal).
func (c CrossCol) Key() string { return colKey(c.Path, c.Measure) }

// CrossRow — узел дерева строковых группировок кросс-таблицы. Cells хранит
// агрегаты показателей по каждой колонке (ключ = CrossCol.Key()). Промежуточный
// узел несёт подытоги по своему поддереву и Children; лист — только Cells.
type CrossRow struct {
	Field    string
	Key      any
	Count    int
	Cells    map[string]any
	Styles   map[string]report.CellStyle // ключ = CrossCol.Key() → оформление ячейки
	Children []*CrossRow
}

// CrossResult — результат компоновки в режиме кросс-таблицы.
type CrossResult struct {
	Columns  []string       // имена измерений-колонок (spec.Columns) — для шапки
	Cols     []CrossCol     // упорядоченные колонки (путь × показатель)
	Rows     []*CrossRow    // дерево строк (верхний уровень)
	RowTotal map[string]any // итог по каждой колонке (ключ = CrossCol.Key())
	RowCount int
	Capped   bool
}

// ComposeCross строит кросс-таблицу: измерения spec.Groupings — в строки,
// spec.Columns — в колонки, на пересечении — агрегаты spec.Measures.
func ComposeCross(rows []Row, spec report.Composition, ev Evaluator) (*CrossResult, error) {
	return ComposeCrossN(rows, spec, ev, DefaultMaxRows)
}

// ComposeCrossN — ComposeCross с явным потолком строк (как ComposeN).
func ComposeCrossN(rows []Row, spec report.Composition, ev Evaluator, maxRows int) (*CrossResult, error) {
	rows = alignRowKeys(rows, spec)
	res := &CrossResult{Columns: spec.Columns, RowCount: len(rows)}
	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
		res.Capped = true
		res.RowCount = maxRows
	}
	res.Cols = crossColumns(rows, spec)
	res.Rows = buildCrossRows(rows, spec, 0, ev)
	res.RowTotal = crossCells(rows, spec, ev)
	return res, nil
}

// colPath собирает путь значений колонки из строки по spec.Columns.
func colPath(r Row, spec report.Composition) []any {
	p := make([]any, len(spec.Columns))
	for i, c := range spec.Columns {
		p[i] = normalizeGroupKey(r[c])
	}
	return p
}

// pathKey — стабильный ключ пути значений колонки.
func pathKey(path []any) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = toStr(p)
	}
	return strings.Join(parts, "\x1f")
}

// colKey — ключ ячейки: путь колонки + показатель. В пределах одного отчёта
// len(path) одинаков для всех колонок, поэтому коллизий путь/показатель нет.
func colKey(path []any, measure string) string {
	return pathKey(path) + "\x1f" + measure
}

// comparePaths сравнивает пути колонок поэлементно через compareVals
// (числа как decimal, иначе строки). Более короткий путь считается меньшим.
func comparePaths(a, b []any) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if c := compareVals(a[i], b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// crossColumns собирает уникальные пути колонок в порядке появления и
// разворачивает их в список CrossCol (путь × каждый показатель).
func crossColumns(rows []Row, spec report.Composition) []CrossCol {
	seen := map[string]bool{}
	var paths [][]any
	for _, r := range rows {
		p := colPath(r, spec)
		k := pathKey(p)
		if !seen[k] {
			seen[k] = true
			paths = append(paths, p)
		}
	}
	// Порядок колонок — по значению пути (числа как decimal, иначе строки),
	// чтобы он не зависел от порядка строк в выборке. Для корректной календарной
	// сортировки период-измерение должно иметь сортируемое значение (дата/число/ISO).
	sort.SliceStable(paths, func(i, j int) bool {
		return comparePaths(paths[i], paths[j]) < 0
	})
	cols := make([]CrossCol, 0, len(paths)*len(spec.Measures))
	for _, p := range paths {
		for _, m := range spec.Measures {
			cols = append(cols, CrossCol{Path: p, Measure: m.Field})
		}
	}
	return cols
}

// buildCrossRows строит дерево строк по spec.Groupings; на каждом узле
// агрегирует показатели по колонкам (crossCells по строкам узла).
func buildCrossRows(rows []Row, spec report.Composition, level int, ev Evaluator) []*CrossRow {
	if level >= len(spec.Groupings) {
		return nil
	}
	field := spec.Groupings[level]
	var order []any
	buckets := map[any][]Row{}
	for _, r := range rows {
		k := normalizeGroupKey(r[field])
		if _, ok := buckets[k]; !ok {
			order = append(order, k)
		}
		buckets[k] = append(buckets[k], r)
	}
	out := make([]*CrossRow, 0, len(order))
	for _, k := range order {
		cells, styles := crossAgg(buckets[k], spec, ev)
		cr := &CrossRow{
			Field:  field,
			Key:    k,
			Count:  len(buckets[k]),
			Cells:  cells,
			Styles: styles,
		}
		if level+1 < len(spec.Groupings) {
			cr.Children = buildCrossRows(buckets[k], spec, level+1, ev)
		}
		out = append(out, cr)
	}
	return out
}

// crossCells агрегирует показатели по каждому пути колонки (без оформления) —
// для итоговой строки RowTotal.
func crossCells(rows []Row, spec report.Composition, ev Evaluator) map[string]any {
	cells, _ := crossAgg(rows, spec, ev)
	return cells
}

// crossAgg агрегирует показатели по каждому пути колонки внутри подмножества
// строк и вычисляет условное оформление ячеек. Использует общий aggregate —
// вычисляемые показатели (Expr) работают так же, как в обычном режиме. Стиль
// ячейки (путь, показатель) = правило на этот показатель, иначе правило на всю
// строку (Field "") — по значениям агрегатов данного пути.
func crossAgg(rows []Row, spec report.Composition, ev Evaluator) (map[string]any, map[string]report.CellStyle) {
	byPath := map[string][]Row{}
	paths := map[string][]any{}
	for _, r := range rows {
		p := colPath(r, spec)
		k := pathKey(p)
		byPath[k] = append(byPath[k], r)
		paths[k] = p
	}
	cells := map[string]any{}
	var styles map[string]report.CellStyle
	for k, sub := range byPath {
		// wc=nil: кросс-таблица не собирает предупреждения компоновки (CrossResult
		// их не несёт) — отдельный режим вывода; ошибки показателей/условий в нём
		// не аккумулируются.
		agg := aggregate(sub, spec.Measures, ev, nil)
		st := evalStyles(agg, spec, ev, nil)
		p := paths[k]
		for _, m := range spec.Measures {
			ck := colKey(p, m.Field)
			cells[ck] = agg[m.Field]
			if len(st) == 0 {
				continue
			}
			s, ok := st[m.Field]
			if !ok {
				s, ok = st[""]
			}
			if ok {
				if styles == nil {
					styles = map[string]report.CellStyle{}
				}
				styles[ck] = s
			}
		}
	}
	return cells, styles
}
