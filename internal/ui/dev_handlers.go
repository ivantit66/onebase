package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/runtime"
)

// ─── Query Console ──────────────────────────────────────────────────────────

func (s *Server) queryConsolePage(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}
	sources := s.buildQuerySources()
	schemaJSON, _ := json.Marshal(sources)
	s.render(w, r, "page-query-console", map[string]any{
		"Schema": template.JS(schemaJSON),
	})
}

func (s *Server) queryConsoleExec(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}

	var req struct {
		Query  string         `json:"query"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResp(w, 400, map[string]any{"error": "Некорректный запрос"})
		return
	}
	req.Query = stripQueryQuotes(req.Query)

	coerceParams(req.Params)
	res, err := query.Compile(req.Query, query.CompileOpts{
		Params:      req.Params,
		Entities:    s.reg.Entities(),
		Registers:   s.reg.Registers(),
		InfoRegs:    s.reg.InfoRegisters(),
		AccountRegs: s.reg.AccountRegisters(),
		Dialect:     s.store.Dialect(),
	})
	if err != nil {
		jsonResp(w, 200, map[string]any{"error": "Ошибка запроса: " + err.Error()})
		return
	}

	start := time.Now()
	rows, err := s.store.QueryAll(r.Context(), res.SQL, res.Args...)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		jsonResp(w, 200, map[string]any{"error": "Ошибка выполнения: " + err.Error()})
		return
	}

	columns := []string{}
	if len(rows) > 0 {
		for k := range rows[0] {
			columns = append(columns, k)
		}
	}

	dataRows := make([][]any, len(rows))
	for i, row := range rows {
		vals := make([]any, len(columns))
		for j, col := range columns {
			v := row[col]
			if t, ok := v.(time.Time); ok {
				v = t.Format("02.01.2006 15:04:05")
			}
			vals[j] = v
		}
		dataRows[i] = vals
	}

	jsonResp(w, 200, map[string]any{
		"columns": columns,
		"rows":    dataRows,
		"count":   len(rows),
		"time":    elapsed.String(),
	})
}

// ─── Query Analyze (param type detection) ──────────────────────────────────

func (s *Server) queryConsoleAnalyze(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResp(w, 400, map[string]any{"error": "Некорректный запрос"})
		return
	}
	req.Query = stripQueryQuotes(req.Query)

	paramRe := regexp.MustCompile(`&([A-Za-zА-Яа-яёЁ_][A-Za-zА-Яа-яёЁ_0-9]*)`)
	matches := paramRe.FindAllStringSubmatch(req.Query, -1)
	paramSet := map[string]bool{}
	for _, m := range matches {
		paramSet[m[1]] = true
	}

	// Build column type map from metadata
	colTypeMap := map[string]string{}
	for _, reg := range s.reg.Registers() {
		for _, d := range reg.Dimensions {
			n := strings.ToLower(d.Name)
			if metadata.IsReference(d.Type) {
				colTypeMap[n+"_id"] = "uuid"
				colTypeMap[n] = "uuid"
			}
			if d.Type == metadata.FieldTypeNumber {
				colTypeMap[n] = "number"
			}
		}
		for _, f := range reg.Resources {
			n := strings.ToLower(f.Name)
			if f.Type == metadata.FieldTypeNumber {
				colTypeMap[n] = "number"
			}
		}
	}
	for _, ir := range s.reg.InfoRegisters() {
		for _, d := range ir.Dimensions {
			n := strings.ToLower(d.Name)
			if metadata.IsReference(d.Type) {
				colTypeMap[n+"_id"] = "uuid"
				colTypeMap[n] = "uuid"
			}
		}
	}
	for _, e := range s.reg.Entities() {
		for _, f := range e.Fields {
			n := strings.ToLower(f.Name)
			if metadata.IsReference(f.Type) {
				colTypeMap[n+"_id"] = "uuid"
				colTypeMap[n] = "uuid"
			}
		}
	}

	// Build entity name index for reference detection
	entityRefMap := map[string]string{} // lowercase name → Entity.Name
	for _, e := range s.reg.Entities() {
		entityRefMap[strings.ToLower(e.Name)] = e.Name
	}

	// For each param: compile with sentinel value, find its $N placeholder,
	// then analyse what column precedes $N in that same SQL to infer the type.
	// We use the sentinel SQL (not a separate null-params compile) because nil
	// params emit "NULL" instead of "$N", making split-based analysis impossible.
	type paramDebug struct {
		CompileErr string `json:"compileErr,omitempty"`
		PhIdx      int    `json:"phIdx"`
		SQL        string `json:"sql,omitempty"`
		Col        string `json:"col,omitempty"`
		Type       string `json:"type"`
	}
	debugInfo := map[string]paramDebug{}

	paramTypes := map[string]string{}
	for name := range paramSet {
		dbg := paramDebug{}
		singleParam := map[string]any{}
		for n := range paramSet {
			singleParam[n] = nil
		}
		singleParam[name] = "__DETECT__"
		sr, err2 := query.Compile(req.Query, query.CompileOpts{
			Params:      singleParam,
		Entities:    s.reg.Entities(),
			Registers:   s.reg.Registers(),
			InfoRegs:    s.reg.InfoRegisters(),
			AccountRegs: s.reg.AccountRegisters(),
			Dialect:     s.store.Dialect(),
		})
		if err2 != nil {
			dbg.CompileErr = err2.Error()
			dbg.Type = "compile_error→fallback"
			debugInfo[name] = dbg
			continue // will be handled by name-based fallback below
		}
		dbg.SQL = sr.SQL
		phIdx := -1
		for i, a := range sr.Args {
			if fmt.Sprintf("%v", a) == "__DETECT__" {
				phIdx = i + 1
				break
			}
		}
		dbg.PhIdx = phIdx
		if phIdx < 0 {
			dbg.Type = "no_placeholder→fallback"
			debugInfo[name] = dbg
			continue
		}
		sqlLower := strings.ToLower(sr.SQL)
		// Плейсхолдер зависит от диалекта: PostgreSQL — именованный «$N»,
		// SQLite — позиционный «?». Для именованного целевое вхождение
		// единственное (occ=1); для «?» плейсхолдеры неотличимы, поэтому
		// берём phIdx-е по счёту вхождение.
		ph := strings.ToLower(s.store.Dialect().Placeholder(phIdx))
		occ := 1
		if !strings.ContainsAny(ph, "0123456789") {
			occ = phIdx
		}
		parts := strings.Split(sqlLower, ph)
		if len(parts) <= occ {
			dbg.Type = "ph_not_in_sql→fallback"
			debugInfo[name] = dbg
			continue
		}
		before := strings.TrimSpace(parts[occ-1])
		tokens := strings.Fields(before)
		if len(tokens) < 2 {
			dbg.Type = "too_few_tokens→fallback"
			debugInfo[name] = dbg
			continue
		}
		col := strings.TrimRight(tokens[len(tokens)-2], "=><!")
		if dotIdx := strings.LastIndex(col, "."); dotIdx >= 0 {
			col = col[dotIdx+1:]
		}
		dbg.Col = col
		colNoID := strings.TrimSuffix(col, "_id")
		switch {
		case strings.HasSuffix(col, "_id") || colTypeMap[col] == "uuid":
			if eName, ok := entityRefMap[colNoID]; ok {
				paramTypes[name] = "reference:" + eName
			} else {
				paramTypes[name] = "uuid"
			}
		case colTypeMap[col] == "number":
			paramTypes[name] = "number"
		case col == "period":
			paramTypes[name] = "date"
		default:
			if eName, ok := entityRefMap[col]; ok {
				paramTypes[name] = "reference:" + eName
			}
			// leave unset → name-based fallback below
		}
		dbg.Type = paramTypes[name]
		debugInfo[name] = dbg
	}
	// Name-based fallback: if param name matches an entity, treat as reference
	for name := range paramSet {
		if _, ok := paramTypes[name]; !ok {
			if eName, ok := entityRefMap[strings.ToLower(name)]; ok {
				paramTypes[name] = "reference:" + eName
			} else {
				paramTypes[name] = "string"
			}
			// Update type in existing debug entry (preserve compile/sql info)
			if d, ok := debugInfo[name]; ok {
				d.Type += " → name_fallback→" + paramTypes[name]
				debugInfo[name] = d
			} else {
				debugInfo[name] = paramDebug{Type: "name_fallback→" + paramTypes[name]}
			}
		}
	}
	jsonResp(w, 200, map[string]any{"paramTypes": paramTypes, "_debug": debugInfo})
}

// ─── Entity Search (reference param picker) ─────────────────────────────────

func (s *Server) devEntitySearch(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}
	entityType := r.URL.Query().Get("type")
	q := r.URL.Query().Get("q")

	var found *metadata.Entity
	for _, e := range s.reg.Entities() {
		if strings.EqualFold(e.Name, entityType) {
			found = e
			break
		}
	}
	if found == nil {
		jsonResp(w, 404, map[string]any{"error": "Сущность не найдена: " + entityType})
		return
	}

	nameCol := ""
	codeCol := ""
	for _, f := range found.Fields {
		if f.Type == metadata.FieldTypeString {
			if nameCol == "" {
				nameCol = strings.ToLower(f.Name)
			}
		}
		if strings.EqualFold(f.Name, "код") || strings.EqualFold(f.Name, "code") {
			codeCol = strings.ToLower(f.Name)
		}
	}
	if nameCol == "" {
		jsonResp(w, 200, map[string]any{"items": []any{}})
		return
	}

	tableName := metadata.TableName(found.Name)

	selectCols := fmt.Sprintf("id, %s AS name", nameCol)
	if codeCol != "" {
		selectCols = fmt.Sprintf("id, %s AS name, %s AS code", nameCol, codeCol)
	}

	var rows []map[string]any
	var err error
	if strings.TrimSpace(q) == "" {
		rows, err = s.store.QueryAll(r.Context(),
			fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT 50", selectCols, tableName, nameCol))
	} else {
		rows, err = s.store.QueryAll(r.Context(),
			fmt.Sprintf("SELECT %s FROM %s WHERE LOWER(%s) LIKE LOWER($1) ORDER BY %s LIMIT 50",
				selectCols, tableName, nameCol, nameCol),
			"%"+q+"%")
	}
	if err != nil {
		jsonResp(w, 200, map[string]any{"error": err.Error()})
		return
	}

	items := make([]map[string]any, len(rows))
	for i, row := range rows {
		item := map[string]any{"id": row["id"], "name": row["name"]}
		if codeCol != "" {
			item["code"] = row["code"]
		}
		items[i] = item
	}
	jsonResp(w, 200, map[string]any{"items": items})
}

// ─── Code Console ───────────────────────────────────────────────────────────

func (s *Server) codeConsolePage(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}
	s.render(w, r, "page-code-console", nil)
}

func (s *Server) codeConsoleExec(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		s.renderForbidden(w, r)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResp(w, 400, map[string]any{"error": "Некорректный запрос"})
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		jsonResp(w, 200, map[string]any{"error": "Пустой код"})
		return
	}

	// Wrap in procedure if user didn't provide one
	lower := strings.ToLower(code)
	if !strings.HasPrefix(lower, "процедура") && !strings.HasPrefix(lower, "procedure") &&
		!strings.HasPrefix(lower, "функция") && !strings.HasPrefix(lower, "function") {
		code = "Процедура __Console()\n" + code + "\nКонецПроцедуры"
	}

	l := lexer.New(code, "<console>")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		jsonResp(w, 200, map[string]any{"error": "Ошибка синтаксиса: " + err.Error()})
		return
	}
	if len(prog.Procedures) == 0 {
		jsonResp(w, 200, map[string]any{"error": "Нет процедур для выполнения"})
		return
	}

	mc := runtime.NewMovementsCollector("console", uuid.Nil)
	var msgs []string
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	vars := s.buildDSLVarsWithMessages(ctx, mc, &msgs)
	proc := prog.Procedures[0]

	runErr := ""
	if err := s.interp.Run(proc, nil, vars); err != nil {
		runErr = err.Error()
	}

	resp := map[string]any{
		"output": msgs,
	}
	if runErr != "" {
		resp["error"] = runErr
	}

	jsonResp(w, 200, resp)
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// stripQueryQuotes removes surrounding single or double quotes if the entire
// query is wrapped in them (e.g. user pastes 'ВЫБРАТЬ...' into the editor).
func stripQueryQuotes(q string) string {
	q = strings.TrimSpace(q)
	if len(q) >= 2 {
		if (q[0] == '\'' && q[len(q)-1] == '\'') ||
			(q[0] == '"' && q[len(q)-1] == '"') {
			return strings.TrimSpace(q[1 : len(q)-1])
		}
	}
	return q
}

func jsonResp(w http.ResponseWriter, status int, data map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
// coerceParams converts string values to appropriate types for query parameters:
//   - "DD.MM.YYYY" or "DD.MM.YYYY HH:MM" → time.Time
//   - numeric strings → float64
// This is needed because JSON params arrive as strings from the query console.
func coerceParams(params map[string]any) {
	for k, v := range params {
		s, ok := v.(string)
		if !ok {
			continue
		}
		// UUID-значения (ссылочные параметры) оставляем строкой — иначе
		// UUID вида "123e4567-..." был бы ошибочно принят за число и в SQL
		// получилось бы "uuid = numeric".
		if _, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
			continue
		}
		// Try date formats
		for _, layout := range []string{
			"02.01.2006 15:04",
			"02.01.2006 15:04:05",
			"02.01.2006",
			"2006-01-02",
		} {
			if t, err := time.Parse(layout, s); err == nil {
				params[k] = t
				break
			}
		}
		if _, ok := params[k].(time.Time); ok {
			continue
		}
		// Try numeric
		if f, err := parseFloat(s); err == nil {
			params[k] = f
		}
	}
}

// parseFloat принимает строку как число только если она ЦЕЛИКОМ является
// числом. fmt.Sscanf("%f") разбирал префикс ("123e4..." из UUID → число),
// поэтому здесь строгий strconv.ParseFloat.
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

