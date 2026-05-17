// Package widget runs dashboard widgets defined as metadata.Widget on top of
// the existing Query Language pipeline.
package widget

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/scheduler"
	"github.com/ivantit66/onebase/internal/storage"
)

// Result is the rendered output of a single widget. Type mirrors metadata.WidgetType
// so templates can switch on a single field; other fields are populated based on type.
type Result struct {
	Name    string
	Type    string
	Title   string
	Span    int
	Error   string

	// kpi
	KPI *KPIResult

	// list / recent
	Rows    []map[string]any
	Columns []ColumnSpec

	// chart
	Chart *ChartData

	// actions
	Actions []ActionLink
}

// KPIResult holds the single numeric value rendered by a KPI widget.
type KPIResult struct {
	Value   any    // primary number (or string if textual)
	Display string // pre-formatted text for direct rendering
}

// ColumnSpec describes how to render a column for list/recent widgets.
type ColumnSpec struct {
	Field  string
	Label  string
	Format string
	Align  string
}

// ChartData carries pre-shaped data for the chart widget. The UI layer turns
// this into an ECharts options object client-side.
type ChartData struct {
	Kind   string   // bar | line | pie
	XAxis  []string // category labels (bar/line) or slice labels (pie)
	Series []ChartSeries
}

// ChartSeries holds one series of values aligned with ChartData.XAxis.
type ChartSeries struct {
	Name string
	Data []float64
}

// ActionLink is a single rendered button on the actions widget.
type ActionLink struct {
	Label string
	URL   string
}

// Runner executes widgets against the database. It holds references to the
// shared registry and storage; it is safe to reuse per request.
type Runner struct {
	Reg         *runtime.Registry
	Store       *storage.DB
	CurrentUser string // login of the user looking at the dashboard (for recent.scope=current_user)
	Cache       *Cache // optional — when set, results are cached by widget name + user
}

// New creates a Runner. The Resolve hook is optional — when non-nil it is
// invoked on every row of list/chart widgets to map raw UUIDs back to display
// names, similar to what reports do.
func New(reg *runtime.Registry, store *storage.DB) *Runner {
	return &Runner{Reg: reg, Store: store}
}

// Run executes a single widget. It never returns an error: any failure is
// captured in Result.Error so the dashboard keeps rendering other widgets.
// When a Cache is configured, results are reused inside its TTL window. The
// "actions" widget type is purely declarative so it skips the cache.
func (r *Runner) Run(ctx context.Context, w *metadata.Widget) Result {
	if r.Cache != nil && w.Type != metadata.WidgetTypeActions {
		key := cacheKey(w.Name, r.CurrentUser)
		if cached, ok := r.Cache.get(key); ok {
			return cached
		}
		res := r.runOnce(ctx, w)
		// Don't cache transient errors — they're often "compile" errors during
		// the editing loop, and a stale failure looks worse than a fresh retry.
		if res.Error == "" {
			r.Cache.put(key, res)
		}
		return res
	}
	return r.runOnce(ctx, w)
}

func (r *Runner) runOnce(ctx context.Context, w *metadata.Widget) Result {
	res := Result{Name: w.Name, Type: string(w.Type), Title: w.Title}
	switch w.Type {
	case metadata.WidgetTypeKPI:
		r.runKPI(ctx, w, &res)
	case metadata.WidgetTypeList:
		r.runList(ctx, w, &res)
	case metadata.WidgetTypeChart:
		r.runChart(ctx, w, &res)
	case metadata.WidgetTypeActions:
		r.runActions(w, &res)
	case metadata.WidgetTypeRecent:
		r.runRecent(ctx, w, &res)
	default:
		res.Error = "неизвестный тип виджета: " + string(w.Type)
	}
	return res
}

func (r *Runner) runKPI(ctx context.Context, w *metadata.Widget, res *Result) {
	rows, _, err := r.runQuery(ctx, w)
	if err != nil {
		res.Error = err.Error()
		return
	}
	if len(rows) == 0 {
		res.KPI = &KPIResult{Display: formatKPI(0, w.Format)}
		return
	}
	val := firstScalar(rows[0])
	res.KPI = &KPIResult{Value: val, Display: formatKPI(val, w.Format)}
}

func (r *Runner) runList(ctx context.Context, w *metadata.Widget, res *Result) {
	rows, cols, err := r.runQuery(ctx, w)
	if err != nil {
		res.Error = err.Error()
		return
	}
	if w.Limit > 0 && len(rows) > w.Limit {
		rows = rows[:w.Limit]
	}
	r.resolveUUIDs(ctx, rows)
	res.Rows = rows
	res.Columns = columnsForList(w, cols)
}

func (r *Runner) runChart(ctx context.Context, w *metadata.Widget, res *Result) {
	rows, cols, err := r.runQuery(ctx, w)
	if err != nil {
		res.Error = err.Error()
		return
	}
	x := resolveFieldName(cols, w.XField)
	if x == "" && len(cols) > 0 {
		x = cols[0]
	}
	yFields := make([]string, 0, len(w.YFields))
	if len(w.YFields) > 0 {
		for _, yf := range w.YFields {
			if resolved := resolveFieldName(cols, yf); resolved != "" {
				yFields = append(yFields, resolved)
			}
		}
	}
	if len(yFields) == 0 {
		for _, c := range cols {
			if !strings.EqualFold(c, x) {
				yFields = append(yFields, c)
			}
		}
	}
	r.resolveUUIDs(ctx, rows)
	chart := &ChartData{Kind: w.ChartKind}
	for _, row := range rows {
		v := row[x]
		label := fmt.Sprintf("%v", v)
		if t, ok := v.(time.Time); ok {
			label = t.Format("02.01")
		} else if v == nil {
			label = ""
		}
		chart.XAxis = append(chart.XAxis, label)
	}
	for _, yf := range yFields {
		s := ChartSeries{Name: yf}
		for _, row := range rows {
			s.Data = append(s.Data, toFloat(row[yf]))
		}
		chart.Series = append(chart.Series, s)
	}
	res.Chart = chart
}

// resolveFieldName matches a YAML-declared field name against the columns
// actually returned by the query, case-insensitively. SQL backends often
// lowercase identifiers (PostgreSQL most aggressively), so a widget declaring
// `x_field: Дата` may have to look up "дата" in the row map.
func resolveFieldName(cols []string, declared string) string {
	if declared == "" {
		return ""
	}
	for _, c := range cols {
		if c == declared {
			return c
		}
	}
	lower := strings.ToLower(declared)
	for _, c := range cols {
		if strings.ToLower(c) == lower {
			return c
		}
	}
	return ""
}

func (r *Runner) runActions(w *metadata.Widget, res *Result) {
	for _, item := range w.Items {
		link := ActionLink{Label: item.Label}
		switch {
		case item.URL != "":
			link.URL = item.URL
		case item.Entity != "":
			ent := r.Reg.GetEntity(item.Entity)
			if ent == nil {
				continue
			}
			link.URL = "/ui/" + strings.ToLower(string(ent.Kind)) + "/" + ent.Name + "/new"
		default:
			continue
		}
		res.Actions = append(res.Actions, link)
	}
}

func (r *Runner) runRecent(ctx context.Context, w *metadata.Widget, res *Result) {
	// "Recent" is a platform widget: it reads the global _audit log to find the
	// most-recently-touched records across the chosen entities. This works for
	// any entity (documents and catalogs) without requiring a specific column
	// like дата or updated_at.
	limit := w.Limit
	if limit <= 0 {
		limit = 8
	}

	d := r.Store.Dialect()
	var where []string
	var args []any
	idx := 1

	if len(w.Entities) > 0 {
		placeholders := make([]string, len(w.Entities))
		for i, name := range w.Entities {
			placeholders[i] = d.Placeholder(idx)
			args = append(args, name)
			idx++
		}
		where = append(where, "entity_name IN ("+strings.Join(placeholders, ", ")+")")
	} else {
		where = append(where, "entity_kind = "+d.Placeholder(idx))
		args = append(args, "document")
		idx++
	}

	if strings.EqualFold(w.Scope, "current_user") && r.CurrentUser != "" {
		where = append(where, "user_login = "+d.Placeholder(idx))
		args = append(args, r.CurrentUser)
		idx++
	}

	sql := `SELECT entity_kind, entity_name, record_id, MAX(at) AS _ts
		FROM _audit
		WHERE ` + strings.Join(where, " AND ") + ` AND record_id IS NOT NULL
		GROUP BY entity_kind, entity_name, record_id
		ORDER BY _ts DESC
		LIMIT ` + fmt.Sprint(limit)

	rows, _, err := r.Store.RunQuery(ctx, sql, args)
	if err != nil {
		res.Error = err.Error()
		return
	}
	for _, row := range rows {
		entName, _ := row["entity_name"].(string)
		kind, _ := row["entity_kind"].(string)
		id := fmt.Sprintf("%v", row["record_id"])
		row["_url"] = "/ui/" + kind + "/" + entName + "/" + id
		row["_label"] = entName
		row["_title"] = recordPresentation(ctx, r, entName, id)
	}
	res.Rows = rows
	res.Columns = []ColumnSpec{
		{Field: "entity_name", Label: "Объект"},
		{Field: "_ts", Label: "Когда", Format: "date"},
	}
}

// resolveUUIDs walks rows and replaces any UUID-shaped string with the
// display name of the corresponding entity. Same idea as
// ui.resolveUUIDsInReport but kept inside the widget package to avoid a
// circular import.
func (r *Runner) resolveUUIDs(ctx context.Context, rows []map[string]any) {
	uuidToLabel := make(map[string]string)
	for _, row := range rows {
		for _, v := range row {
			if str, ok := v.(string); ok {
				if _, err := uuid.Parse(str); err == nil {
					uuidToLabel[str] = ""
				}
			}
		}
	}
	if len(uuidToLabel) == 0 {
		return
	}
	for _, entity := range r.Reg.Entities() {
		for idStr, label := range uuidToLabel {
			if label != "" {
				continue
			}
			id, perr := uuid.Parse(idStr)
			if perr != nil {
				continue
			}
			if refRow, err := r.Store.GetByID(ctx, entity.Name, id, entity); err == nil {
				for _, f := range entity.Fields {
					if s, ok := refRow[f.Name].(string); ok && strings.TrimSpace(s) != "" {
						uuidToLabel[idStr] = s
						break
					}
				}
				if uuidToLabel[idStr] == "" {
					uuidToLabel[idStr] = shortID(idStr)
				}
			}
		}
	}
	for _, row := range rows {
		for col, v := range row {
			if str, ok := v.(string); ok {
				if label, found := uuidToLabel[str]; found && label != "" {
					row[col] = label
				}
			}
		}
	}
}

// runQuery is the shared back-end for kpi/list/chart widgets.
func (r *Runner) runQuery(ctx context.Context, w *metadata.Widget) ([]map[string]any, []string, error) {
	params := make(map[string]any, len(w.Params))
	for k, v := range w.Params {
		params[k] = v
	}
	params = scheduler.ResolveParamTemplates(params)

	compiled, err := query.Compile(w.Query, query.CompileOpts{
		Params:      params,
		Registers:   r.Reg.Registers(),
		InfoRegs:    r.Reg.InfoRegisters(),
		AccountRegs: r.Reg.AccountRegisters(),
		Dialect:     r.Store.Dialect(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("compile: %w", err)
	}
	return r.Store.RunQuery(ctx, compiled.SQL, compiled.Args)
}

func columnsForList(w *metadata.Widget, cols []string) []ColumnSpec {
	if len(w.Columns) > 0 {
		out := make([]ColumnSpec, len(w.Columns))
		for i, c := range w.Columns {
			actual := resolveFieldName(cols, c.Field)
			if actual == "" {
				actual = c.Field
			}
			out[i] = ColumnSpec{Field: actual, Label: c.Label, Format: c.Format, Align: c.Align}
			if out[i].Label == "" {
				out[i].Label = c.Field
			}
		}
		return out
	}
	out := make([]ColumnSpec, len(cols))
	for i, c := range cols {
		out[i] = ColumnSpec{Field: c, Label: c}
	}
	return out
}

func firstScalar(row map[string]any) any {
	for _, v := range row {
		return v
	}
	return nil
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		var f float64
		fmt.Sscanf(t, "%f", &f)
		return f
	case nil:
		return 0
	}
	return 0
}

func formatKPI(v any, format string) string {
	f := toFloat(v)
	switch strings.ToLower(format) {
	case "money":
		return formatMoney(f)
	case "percent":
		return fmt.Sprintf("%.1f%%", f)
	case "number":
		return formatInt(f)
	}
	if isInteger(f) {
		return formatInt(f)
	}
	return fmt.Sprintf("%.2f", f)
}

func formatMoney(f float64) string {
	// Russian convention: thousands separator U+00A0, comma decimal.
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int64(f)
	frac := int64((f - float64(whole)) * 100)
	if frac < 0 {
		frac = -frac
	}
	s := groupDigits(whole)
	out := fmt.Sprintf("%s,%02d ₽", s, frac)
	if neg {
		out = "-" + out
	}
	return out
}

func formatInt(f float64) string {
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int64(f + 0.5)
	s := groupDigits(whole)
	if neg {
		return "-" + s
	}
	return s
}

func groupDigits(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteRune(' ')
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteRune(' ')
		}
	}
	return b.String()
}

func isInteger(f float64) bool {
	return f == float64(int64(f))
}

// recordPresentation returns a human-readable title for a record referenced
// by the recent-widget. For documents we prefer "Номер · Дата"; for catalogs
// we fall back to the first string-typed field (usually Наименование). On any
// lookup failure we return a shortened UUID so the row still renders.
func recordPresentation(ctx context.Context, r *Runner, entityName, idStr string) string {
	ent := r.Reg.GetEntity(entityName)
	if ent == nil {
		return shortID(idStr)
	}
	id, err := parseUUID(idStr)
	if err != nil {
		return shortID(idStr)
	}
	row, err := r.Store.GetByID(ctx, ent.Name, id, ent)
	if err != nil || row == nil {
		return shortID(idStr)
	}
	if ent.Kind == metadata.KindDocument {
		num := fmt.Sprintf("%v", firstNonEmpty(row, "Номер", "Number"))
		dateRaw := firstNonEmpty(row, "Дата", "Date")
		if dateRaw != nil {
			if t, ok := dateRaw.(time.Time); ok {
				return strings.TrimSpace(num) + " от " + t.Format("02.01.2006")
			}
		}
		if num != "<nil>" && num != "" {
			return num
		}
	}
	// catalogs and fallbacks: first non-empty string field
	for _, f := range ent.Fields {
		if v, ok := row[f.Name]; ok && v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return shortID(idStr)
}

func firstNonEmpty(row map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := row[k]; ok && v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
				continue
			}
			return v
		}
	}
	return nil
}

func shortID(idStr string) string {
	if len(idStr) > 8 {
		return idStr[:8]
	}
	return idStr
}

// parseUUID isolates the google/uuid import to a single helper so the rest of
// the runner stays driver-agnostic.
func parseUUID(s string) (uuid.UUID, error) { return uuid.Parse(s) }
