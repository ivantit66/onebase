// Package compose сворачивает плоские строки отчёта в дерево групп с итогами.
// Чистый слой: без БД/HTTP. Условия оформления вычисляются через Evaluator.
package compose

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/ivantit66/onebase/internal/report"
)

type Row = map[string]any

// Evaluator вычисляет DSL-выражения при значениях полей строки.
type Evaluator interface {
	EvalBool(expr string, row Row) (bool, error)
	// EvalNum вычисляет числовое выражение (для вычисляемых показателей).
	// Возвращает (значение, удалось, ошибка). При ok=false результат не используется.
	EvalNum(expr string, row Row) (decimal.Decimal, bool, error)
}

const DefaultMaxRows = 50000

type Result struct {
	Columns  []string
	Groups   []*Group
	Grand    map[string]any
	RowCount int
	Capped   bool
}

type Group struct {
	Field     string
	Key       any
	Subtotals map[string]any
	Count     int
	Children  []*Group
	Details   []DetailRow
	Styles    map[string]report.CellStyle // условное оформление по подытогам; ключ "" = вся строка
}

type DetailRow struct {
	Values map[string]any
	Styles map[string]report.CellStyle // ключ "" = стиль на всю строку
}

func Compose(rows []Row, spec report.Composition, ev Evaluator) (*Result, error) {
	return ComposeN(rows, spec, ev, DefaultMaxRows)
}

func ComposeN(rows []Row, spec report.Composition, ev Evaluator, maxRows int) (*Result, error) {
	rows = alignRowKeys(rows, spec)
	res := &Result{RowCount: len(rows)}
	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
		res.Capped = true
		res.RowCount = maxRows
	}
	res.Groups = buildGroups(rows, spec, 0, ev)
	res.Grand = aggregate(rows, spec.Measures, ev)
	return res, nil
}

// alignRowKeys добавляет каждой строке ключи под именами полей из spec, находя
// соответствующие колонки регистронезависимо. Компилятор запросов отдаёт имена
// колонок в нижнем регистре (ВЫБРАТЬ Выручка КАК Выручка → колонка "выручка"),
// тогда как composition ссылается на поля в исходном регистре; без выравнивания
// доступ row[field] не находил бы данные и все строки схлопывались бы в одну
// пустую группу с нулевыми итогами. Исходные ключи сохраняются. Согласуется с
// тем, что DSL OneBase регистронезависим.
func alignRowKeys(rows []Row, spec report.Composition) []Row {
	targets := map[string]bool{}
	add := func(s string) {
		if s != "" {
			targets[s] = true
		}
	}
	for _, g := range spec.Groupings {
		add(g)
	}
	for _, m := range spec.Measures {
		add(m.Field)
	}
	for _, s := range spec.Sort {
		add(s.Field)
	}
	for _, c := range spec.Conditional {
		add(c.Field)
	}
	if spec.Chart != nil {
		add(spec.Chart.Category)
		for _, s := range spec.Chart.Series {
			add(s)
		}
	}
	// DetailLink — колонка строки, из которой берётся UUID для ссылки на документ.
	// DetailEntity — имя сущности (не поле строки), в alignRowKeys не добавляем.
	add(spec.DetailLink)
	if len(targets) == 0 {
		return rows
	}
	byLower := make(map[string]string, len(targets))
	for t := range targets {
		byLower[strings.ToLower(t)] = t
	}
	out := make([]Row, len(rows))
	for i, r := range rows {
		nr := make(Row, len(r)+len(targets))
		for k, v := range r {
			nr[k] = v
			if t, ok := byLower[strings.ToLower(k)]; ok && t != k {
				nr[t] = v
			}
		}
		out[i] = nr
	}
	return out
}

func buildGroups(rows []Row, spec report.Composition, level int, ev Evaluator) []*Group {
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
	groups := make([]*Group, 0, len(order))
	for _, k := range order {
		gr := &Group{
			Field:     field,
			Key:       k,
			Count:     len(buckets[k]),
			Subtotals: aggregate(buckets[k], spec.Measures, ev),
		}
		// Условное оформление группы/подытога вычисляется по её подытогам —
		// так убыточная группа подсвечивается целиком, без detail-строк.
		gr.Styles = evalStyles(gr.Subtotals, spec, ev)
		if level+1 < len(spec.Groupings) {
			gr.Children = buildGroups(buckets[k], spec, level+1, ev)
		} else if spec.Detail {
			gr.Details = buildDetails(buckets[k], spec, ev)
		}
		groups = append(groups, gr)
	}
	sortGroups(groups, spec)
	return groups
}

// evalStyles вычисляет условное оформление для строки значений (детальной или
// подытога группы): первое сработавшее правило на целевое поле задаёт стиль.
func evalStyles(row Row, spec report.Composition, ev Evaluator) map[string]report.CellStyle {
	if len(spec.Conditional) == 0 || ev == nil {
		return nil
	}
	var styles map[string]report.CellStyle
	for _, rule := range spec.Conditional {
		if _, done := styles[rule.Field]; done {
			continue // первое сработавшее правило на целевое поле
		}
		ok, err := ev.EvalBool(rule.When, row)
		if err != nil || !ok {
			continue
		}
		if styles == nil {
			styles = map[string]report.CellStyle{}
		}
		styles[rule.Field] = rule.Style
	}
	return styles
}

func buildDetails(rows []Row, spec report.Composition, ev Evaluator) []DetailRow {
	out := make([]DetailRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, DetailRow{Values: r, Styles: evalStyles(r, spec, ev)})
	}
	sortDetails(out, spec)
	return out
}

func measureSet(spec report.Composition) map[string]bool {
	m := map[string]bool{}
	for _, x := range spec.Measures {
		m[x.Field] = true
	}
	return m
}

func sortGroups(groups []*Group, spec report.Composition) {
	if len(spec.Sort) == 0 {
		return
	}
	meas := measureSet(spec)
	sort.SliceStable(groups, func(i, j int) bool {
		for _, sk := range spec.Sort {
			var vi, vj any
			switch {
			case meas[sk.Field]:
				vi, vj = groups[i].Subtotals[sk.Field], groups[j].Subtotals[sk.Field]
			case sk.Field == groups[i].Field:
				vi, vj = groups[i].Key, groups[j].Key
			default:
				continue
			}
			if c := compareVals(vi, vj); c != 0 {
				if sk.Dir == "desc" {
					return c > 0
				}
				return c < 0
			}
		}
		return false
	})
}

func sortDetails(rows []DetailRow, spec report.Composition) {
	if len(spec.Sort) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for _, sk := range spec.Sort {
			if c := compareVals(rows[i].Values[sk.Field], rows[j].Values[sk.Field]); c != 0 {
				if sk.Dir == "desc" {
					return c > 0
				}
				return c < 0
			}
		}
		return false
	})
}

// compareVals: -1/0/1. Числа сравниваются как decimal, иначе как строки.
// Если одно значение числовое, а другое нет (nil-подытог пустой группы,
// нечисловой текст) — числовое считается «меньше», то есть непарсимое уходит
// в конец при сортировке по возрастанию, а не сравнивается лексикографически
// с числом (issue #90).
func compareVals(a, b any) int {
	da, oka := toDecimal(a)
	db, okb := toDecimal(b)
	switch {
	case oka && okb:
		return da.Cmp(db)
	case oka:
		return -1
	case okb:
		return 1
	}
	sa, sb := toStr(a), toStr(b)
	switch {
	case sa < sb:
		return -1
	case sa > sb:
		return 1
	default:
		return 0
	}
}

func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// normalizeGroupKey приводит значение группировки к надёжному ключу map.
// Числа любого типа (int/int64/float64/decimal) канонизируются в одну строку —
// иначе равные по значению ключи разных типов попали бы в разные группы, а
// decimal.Decimal как ключ map сравнивался бы по указателю big.Int. []byte
// (его может вернуть драйвер БД) неэшируем как ключ map — приводим к строке,
// иначе buildGroups паникует. compareVals при сортировке всё равно распарсит
// числовую строку обратно в decimal, так что числовой порядок сохраняется.
func normalizeGroupKey(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	case decimal.Decimal:
		return x.String()
	case int:
		return decimal.NewFromInt(int64(x)).String()
	case int64:
		return decimal.NewFromInt(x).String()
	case float64:
		return decimal.NewFromFloat(x).String()
	}
	return v
}

func aggregate(rows []Row, measures []report.Measure, ev Evaluator) map[string]any {
	out := map[string]any{}
	// Первый проход: обычные показатели (не вычисляемые).
	for _, m := range measures {
		if m.Expr != "" {
			continue // вычисляемые — во второй проход
		}
		out[m.Field] = aggMeasure(rows, m)
	}
	// Второй проход: вычисляемые показатели по уже агрегированным значениям.
	// Контракт порядка: Expr-показатели вычисляются в порядке объявления в measures.
	// Если один Expr ссылается на результат другого Expr, зависимый обязан быть
	// объявлен ПОЗЖЕ — только тогда его зависимость уже присутствует в out.
	for _, m := range measures {
		if m.Expr == "" {
			continue
		}
		if ev == nil {
			out[m.Field] = nil
			continue
		}
		// ok=false означает неопределённое значение (например, деление на ноль) →
		// пустая ячейка, как в 1С. Синтаксические ошибки выражения перехватываются
		// заранее на этапе `onebase check`, поэтому ошибка здесь не является
		// программной ошибкой и намеренно приводит к пустой ячейке (nil).
		d, ok, _ := ev.EvalNum(m.Expr, out)
		if ok {
			out[m.Field] = d
		} else {
			out[m.Field] = nil
		}
	}
	return out
}

func aggMeasure(rows []Row, m report.Measure) any {
	if m.Agg == "count" {
		return int64(len(rows))
	}
	var acc, mn, mx decimal.Decimal
	cnt := 0
	first := true
	for _, r := range rows {
		d, ok := toDecimal(r[m.Field])
		if !ok {
			continue
		}
		cnt++
		acc = acc.Add(d)
		if first {
			mn, mx = d, d
			first = false
		} else {
			if d.LessThan(mn) {
				mn = d
			}
			if d.GreaterThan(mx) {
				mx = d
			}
		}
	}
	switch m.Agg {
	case "min":
		if first {
			return nil
		}
		return mn
	case "max":
		if first {
			return nil
		}
		return mx
	case "avg":
		if cnt == 0 {
			return nil
		}
		return acc.Div(decimal.NewFromInt(int64(cnt)))
	default: // sum / ""
		return acc
	}
}

// ExportToDecimal — toDecimal для внешних пакетов (ui-рендер графика).
func ExportToDecimal(v any) (decimal.Decimal, bool) { return toDecimal(v) }

func toDecimal(v any) (decimal.Decimal, bool) {
	switch x := v.(type) {
	case decimal.Decimal:
		return x, true
	case int:
		return decimal.NewFromInt(int64(x)), true
	case int64:
		return decimal.NewFromInt(x), true
	case float64:
		return decimal.NewFromFloat(x), true
	case string:
		d, err := decimal.NewFromString(x)
		if err != nil {
			return decimal.Zero, false
		}
		return d, true
	}
	return decimal.Zero, false
}
