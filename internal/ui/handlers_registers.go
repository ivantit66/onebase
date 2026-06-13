package ui

// HTTP-обработчики регистров (накопления/сведений) и констант.
// Выделено из handlers.go (план 55, этап 1) — перенос as-is.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func (s *Server) registerMovements(w http.ResponseWriter, r *http.Request) {
	name := capitalize(chi.URLParam(r, "name"))
	reg := s.reg.GetRegister(name)
	if reg == nil {
		http.Error(w, "unknown register: "+name, 404)
		return
	}
	if !s.requirePerm(w, r, "register", reg.Name, "read") {
		return
	}
	flt := parseRegFilter(r, reg.Dimensions, true /*periodic — у движений всегда есть период*/)
	rows, err := s.store.GetMovements(r.Context(), name, reg, flt)
	if err != nil {
		http.Error(w, s.errText(r, err), 500)
		return
	}
	s.resolveRegisterRows(r.Context(), rows, reg)
	s.render(w, r, "page-register-movements", map[string]any{
		"Register":   reg,
		"Rows":       rows,
		"Filter":     filterFormValues(r, reg.Dimensions),
		"RefOpts":    s.loadRefOpts(r.Context(), reg.Dimensions),
		"HasFilters": !flt.IsEmpty(),
	})
}

func (s *Server) registerBalances(w http.ResponseWriter, r *http.Request) {
	name := capitalize(chi.URLParam(r, "name"))
	reg := s.reg.GetRegister(name)
	if reg == nil {
		http.Error(w, "unknown register: "+name, 404)
		return
	}
	if !s.requirePerm(w, r, "register", reg.Name, "read") {
		return
	}
	// Остатки: только «на дату» (to) + измерения; from игнорируется в storage.
	flt := parseRegFilter(r, reg.Dimensions, true)
	rows, err := s.store.GetBalances(r.Context(), name, reg, flt)
	if err != nil {
		http.Error(w, s.errText(r, err), 500)
		return
	}
	s.resolveRegisterRows(r.Context(), rows, reg)
	s.render(w, r, "page-register-balances", map[string]any{
		"Register":   reg,
		"Rows":       rows,
		"Filter":     filterFormValues(r, reg.Dimensions),
		"RefOpts":    s.loadRefOpts(r.Context(), reg.Dimensions),
		"HasFilters": !flt.IsEmpty(),
	})
}

// parseRegFilter собирает storage.RegFilter из query-параметров формы отбора:
// flt_<ИмяИзмерения> для измерений (для ссылочных — UUID из select), from/to —
// границы периода (формат 2006-01-02 из <input type="date">). Период читается
// только если periodic. Имена измерений берутся из fields — это же страхует
// storage от инъекции (там имена ещё раз сверяются с метаданными). #45.
func parseRegFilter(r *http.Request, fields []metadata.Field, periodic bool) storage.RegFilter {
	q := r.URL.Query()
	f := storage.RegFilter{Dims: map[string]string{}}
	for _, fld := range fields {
		v := strings.TrimSpace(q.Get("flt_" + fld.Name))
		if v != "" {
			f.Dims[fld.Name] = v
		}
	}
	if periodic {
		if t := parseFilterDate(q.Get("from")); t != nil {
			f.From = t
		}
		if t := parseFilterDate(q.Get("to")); t != nil {
			// Сдвигаем к концу дня (23:59:59.999999999), чтобы отбор «по дату»
			// включал весь день: period <= to сравнивается с TIMESTAMP (#45).
			endOfDay := t.Add(24*time.Hour - time.Nanosecond)
			f.To = &endOfDay
		}
	}
	return f
}

func parseFilterDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return &t
	}
	return nil
}

// filterFormValues возвращает текущие значения формы отбора (для сохранения
// выбора): flt_<Имя> по измерениям + from/to.
func filterFormValues(r *http.Request, fields []metadata.Field) map[string]string {
	q := r.URL.Query()
	vals := map[string]string{
		"from": strings.TrimSpace(q.Get("from")),
		"to":   strings.TrimSpace(q.Get("to")),
	}
	for _, fld := range fields {
		vals[fld.Name] = strings.TrimSpace(q.Get("flt_" + fld.Name))
	}
	return vals
}

// loadRefOpts загружает опции [{id,_label}] для ссылочных измерений (обобщение
// loadInfoRegRefOpts на произвольный набор полей; используется и для регистров
// накопления, и для регистров сведений). #45.
func (s *Server) loadRefOpts(ctx context.Context, fields []metadata.Field) map[string][]map[string]any {
	opts := make(map[string][]map[string]any)
	for _, f := range fields {
		if f.RefEntity == "" {
			continue
		}
		opts[f.Name] = []map[string]any{}
		refEntity := s.reg.GetEntity(f.RefEntity)
		if refEntity == nil {
			continue
		}
		rows, err := s.store.List(ctx, f.RefEntity, refEntity, storage.ListParams{})
		if err != nil {
			continue
		}
		for _, row := range filterOutFolders(rows) {
			id, _ := row["id"].(string)
			label := firstStringField(row, refEntity)
			opts[f.Name] = append(opts[f.Name], map[string]any{"id": id, "_label": label})
		}
	}
	return opts
}

func (s *Server) resolveRegisterRows(ctx context.Context, rows []map[string]any, reg *metadata.Register) {
	// Резолвим UUID и в измерениях, и в атрибутах: reference-атрибут
	// (например Организация) тоже хранит UUID и должен показываться именем.
	refFields := append(append([]metadata.Field{}, reg.Dimensions...), reg.Attributes...)
	cols := make([]refCol, len(refFields))
	for i, f := range refFields {
		cols[i] = refCol{Key: f.Name, RefEntity: f.RefEntity}
	}
	s.resolveRefColumns(ctx, rows, cols, "")

	// recorder label
	for _, row := range rows {
		recType, _ := row["recorder_type"].(string)
		recIDStr := asString(row["recorder"])
		if recType != "" && recIDStr != "" {
			if recID, err := uuid.Parse(recIDStr); err == nil {
				if entity := s.reg.GetEntityBySlug(recType); entity != nil {
					if docRow, err2 := s.store.GetByID(ctx, entity.Name, recID, entity); err2 == nil {
						num := fmt.Sprintf("%v", docRow["Номер"])
						date := regFmtDate(docRow["Дата"])
						row["recorder_label"] = fmt.Sprintf("%s №%s от %s", entity.Name, num, date)
					}
				}
			}
		}
	}
}

// resolveInfoRegRows подменяет UUID ссылочных измерений и ресурсов регистра
// сведений на наименования (issue #44 — в списке регистра сведений показывались
// id вместо представлений). Зеркало resolveRegisterRows.
func (s *Server) resolveInfoRegRows(ctx context.Context, rows []map[string]any, ir *metadata.InfoRegister) {
	refFields := append(append([]metadata.Field{}, ir.Dimensions...), ir.Resources...)
	var cols []refCol
	for _, f := range refFields {
		if f.RefEntity == "" {
			continue
		}
		cols = append(cols, refCol{Key: f.Name, RefEntity: f.RefEntity})
	}
	if len(cols) == 0 {
		return
	}
	s.resolveRefColumns(ctx, rows, cols, "_label")
}

// resolveAccountRows резолвит reference-субконто (хранятся под ключами субконто<N>)
// в наименования. String/enum-субконто оставляет как есть.
func (s *Server) resolveAccountRows(ctx context.Context, rows []map[string]any, ar *metadata.AccountRegister) {
	var cols []refCol
	for i, f := range ar.Subconto {
		if f.RefEntity == "" {
			continue
		}
		cols = append(cols, refCol{Key: metadata.SubcontoColumn(i + 1), RefEntity: f.RefEntity})
	}
	if len(cols) == 0 {
		return
	}
	s.resolveRefColumns(ctx, rows, cols, "")
}

func (s *Server) getInfoReg(w http.ResponseWriter, r *http.Request) *metadata.InfoRegister {
	name := capitalize(chi.URLParam(r, "name"))
	ir := s.reg.GetInfoRegister(name)
	if ir == nil {
		http.Error(w, "unknown info register: "+name, 404)
	}
	return ir
}

func (s *Server) infoRegList(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "read") {
		return
	}
	flt := parseRegFilter(r, ir.Dimensions, ir.Periodic)
	rows, err := s.store.InfoRegList(r.Context(), ir, flt)
	if err != nil {
		http.Error(w, s.errText(r, err), 500)
		return
	}
	s.resolveInfoRegRows(r.Context(), rows, ir)
	s.render(w, r, "page-inforeg-list", map[string]any{
		"InfoReg":    ir,
		"Rows":       rows,
		"Filter":     filterFormValues(r, ir.Dimensions),
		"RefOpts":    s.loadRefOpts(r.Context(), ir.Dimensions),
		"HasFilters": !flt.IsEmpty(),
	})
}

func (s *Server) loadInfoRegRefOpts(ctx context.Context, ir *metadata.InfoRegister) map[string][]map[string]any {
	return s.loadRefOpts(ctx, ir.Dimensions)
}

func (s *Server) infoRegForm(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "write") {
		return
	}
	now := time.Now().Format("2006-01-02")
	s.render(w, r, "page-inforeg-form", map[string]any{
		"InfoReg": ir,
		"Values":  map[string]string{"period": now},
		"Error":   "",
		"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
	})
}

func (s *Server) infoRegSubmit(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "write") {
		return
	}
	r.ParseForm()

	var periodPtr *time.Time
	if ir.Periodic {
		pStr := r.FormValue("period")
		if pStr == "" {
			s.render(w, r, "page-inforeg-form", map[string]any{
				"InfoReg": ir,
				"Values":  formValuesFromRequest(r, ir),
				"Error":   "Период обязателен для периодического регистра",
				"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
			})
			return
		}
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, pStr, time.Local); err == nil {
				periodPtr = &t
				break
			}
		}
		if periodPtr == nil {
			s.render(w, r, "page-inforeg-form", map[string]any{
				"InfoReg": ir,
				"Values":  formValuesFromRequest(r, ir),
				"Error":   "Неверный формат даты периода",
				"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
			})
			return
		}
	}

	dims := parseInfoRegFields(r, ir.Dimensions)
	resources := parseInfoRegFields(r, ir.Resources)

	if err := s.store.InfoRegSet(r.Context(), ir, dims, resources, periodPtr); err != nil {
		s.render(w, r, "page-inforeg-form", map[string]any{
			"InfoReg": ir,
			"Values":  formValuesFromRequest(r, ir),
			"Error":   err.Error(),
			"RefOpts": s.loadInfoRegRefOpts(r.Context(), ir),
		})
		return
	}
	http.Redirect(w, r, "/ui/inforeg/"+strings.ToLower(ir.Name), http.StatusFound)
}

func (s *Server) infoRegDelete(w http.ResponseWriter, r *http.Request) {
	ir := s.getInfoReg(w, r)
	if ir == nil {
		return
	}
	if !s.requirePerm(w, r, "inforeg", ir.Name, "delete") {
		return
	}
	r.ParseForm()

	var periodPtr *time.Time
	if ir.Periodic {
		// Период берём из машинного ключа period_key (его кладёт InfoRegList в
		// hidden-поле списка). Если ключ не разобран — ОТКАЗЫВАЕМ в удалении:
		// иначе InfoRegDelete с nil-периодом снесёт все периоды комбинации
		// измерений (критическая потеря данных).
		t, ok := storage.ParseRegPeriod(r.FormValue("period"))
		if !ok {
			http.Error(w, s.tr(s.resolveLang(r), "Не удалось определить период записи для удаления"), http.StatusBadRequest)
			return
		}
		periodPtr = &t
	}
	dims := parseInfoRegFields(r, ir.Dimensions)
	if err := s.store.InfoRegDelete(r.Context(), ir, dims, periodPtr); err != nil {
		http.Error(w, s.errText(r, err), 500)
		return
	}
	http.Redirect(w, r, "/ui/inforeg/"+strings.ToLower(ir.Name), http.StatusFound)
}

func parseInfoRegFields(r *http.Request, fields []metadata.Field) map[string]any {
	result := make(map[string]any, len(fields))
	for _, f := range fields {
		val := r.FormValue(f.Name)
		if val == "" {
			result[f.Name] = nil
			continue
		}
		result[f.Name] = parseInfoRegFieldValue(f, val)
	}
	return result
}

func parseInfoRegFieldValue(f metadata.Field, val string) any {
	switch f.Type {
	case metadata.FieldTypeDate:
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, val, time.Local); err == nil {
				return t
			}
		}
		return val
	case metadata.FieldTypeBool:
		return val == "true" || val == "on"
	default:
		return val
	}
}

func (s *Server) constantsList(w http.ResponseWriter, r *http.Request) {
	consts := s.reg.Constants()
	sort.Slice(consts, func(i, j int) bool { return consts[i].Name < consts[j].Name })

	values, _ := s.store.ListConstants(r.Context())
	valStrs := make(map[string]string, len(values))
	for k, v := range values {
		valStrs[k] = fmt.Sprintf("%v", v)
	}

	// ref options for reference-type constants
	refOpts := make(map[string][]map[string]any)
	for _, c := range consts {
		if c.RefEntity == "" {
			continue
		}
		refEntity := s.reg.GetEntity(c.RefEntity)
		if refEntity == nil {
			continue
		}
		rows, err := s.store.List(r.Context(), refEntity.Name, refEntity, storage.ListParams{})
		if err != nil {
			continue
		}
		for _, row := range rows {
			row["_label"] = firstStringField(row, refEntity)
		}
		refOpts[c.Name] = rows
	}

	msg := r.URL.Query().Get("saved")
	s.render(w, r, "page-constants", map[string]any{
		"Constants": consts,
		"Values":    valStrs,
		"RefOpts":   refOpts,
		"Saved":     msg == "1",
	})
}

func (s *Server) constantsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, s.errText(r, err), 400)
		return
	}
	consts := s.reg.Constants()
	for _, c := range consts {
		val := r.FormValue(c.Name)
		var v any
		if val == "" {
			v = nil
		} else {
			v = val
		}
		if err := s.store.SetConstant(r.Context(), c.Name, v); err != nil {
			http.Error(w, s.errText(r, err), 500)
			return
		}
	}
	http.Redirect(w, r, "/ui/constants?saved=1", http.StatusSeeOther)
}

func formValuesFromRequest(r *http.Request, ir *metadata.InfoRegister) map[string]string {
	vals := map[string]string{"period": r.FormValue("period")}
	for _, f := range ir.Dimensions {
		vals[f.Name] = r.FormValue(f.Name)
	}
	for _, f := range ir.Resources {
		vals[f.Name] = r.FormValue(f.Name)
	}
	return vals
}
