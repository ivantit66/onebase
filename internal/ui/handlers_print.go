package ui

// HTTP-обработчики печатных форм (HTML/PDF/DSL-печать).
// Выделено из handlers.go (план 55, этап 1) — перенос as-is.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/printform"
	"github.com/ivantit66/onebase/internal/runtime"
)

// printDocument renders a print form for a specific document/catalog record.
func (s *Server) printDocument(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	formName := chi.URLParam(r, "form")
	if dec, err2 := url.PathUnescape(formName); err2 == nil {
		formName = dec
	}

	forms := s.reg.GetPrintForms(entity.Name)
	var form *printform.PrintForm
	for _, f := range forms {
		if strings.EqualFold(f.Name, formName) {
			form = f
			break
		}
	}
	if form == nil {
		http.Error(w, "print form not found: "+formName, 404)
		return
	}

	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, s.errText(r, err), 404)
		return
	}

	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, _ := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		tpRows[tp.Name] = rows
	}

	refs := s.buildPrintRefs(r.Context(), row, entity, tpRows)

	constants, _ := s.store.ListConstants(r.Context())

	ctx := &printform.RenderContext{
		Document:   row,
		TableParts: tpRows,
		Constants:  constants,
		Refs:       refs,
	}
	pdfURL := r.URL.Path + "/pdf"
	html, err := printform.RenderWithPDFURL(form, ctx, pdfURL)
	if err != nil {
		http.Error(w, s.errText(r, err), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// buildPrintRefs returns a map of UUID → {fields...} for all reference fields in the entity and table parts.
func (s *Server) buildPrintRefs(ctx context.Context, row map[string]any, entity *metadata.Entity, tpRows map[string][]map[string]any) map[string]map[string]any {
	refs := make(map[string]map[string]any)
	resolveRef := func(refEntityName, idStr string) {
		if idStr == "" {
			return
		}
		if _, dup := refs[idStr]; dup {
			return
		}
		refEntity := s.reg.GetEntity(refEntityName)
		if refEntity == nil {
			return
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return
		}
		refRow, err := s.store.GetByID(ctx, refEntity.Name, id, refEntity)
		if err != nil {
			return
		}
		refs[idStr] = refRow
	}
	for _, f := range entity.Fields {
		if f.RefEntity == "" {
			continue
		}
		idStr, _ := row[f.Name].(string)
		resolveRef(f.RefEntity, idStr)
	}
	for _, tp := range entity.TableParts {
		rows := tpRows[tp.Name]
		for _, f := range tp.Fields {
			if f.RefEntity == "" {
				continue
			}
			for _, r := range rows {
				idStr, _ := r[f.Name].(string)
				resolveRef(f.RefEntity, idStr)
			}
		}
	}
	return refs
}

// resolveDSLRefs replaces reference UUID strings in row with MapThis objects
// so that DSL dot-notation like Документ.Организация.Наименование works.
func (s *Server) resolveDSLRefs(row map[string]any, fields []metadata.Field, refs map[string]map[string]any) {
	for _, f := range fields {
		if f.RefEntity == "" {
			continue
		}
		v, ok := row[f.Name]
		if !ok {
			continue
		}
		idStr, ok := v.(string)
		if !ok || idStr == "" {
			continue
		}
		refData, ok := refs[idStr]
		if !ok {
			continue
		}
		// Wrap ref data as MapThis for DSL dot-notation access
		row[f.Name] = &interpreter.MapThis{M: refData}
	}
}

// printDocumentPDF renders a print form as PDF and sends it as a download.
func (s *Server) printDocumentPDF(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	formName := chi.URLParam(r, "form")
	if dec, err2 := url.PathUnescape(formName); err2 == nil {
		formName = dec
	}

	forms := s.reg.GetPrintForms(entity.Name)
	var form *printform.PrintForm
	for _, f := range forms {
		if strings.EqualFold(f.Name, formName) {
			form = f
			break
		}
	}
	if form == nil {
		http.Error(w, "print form not found: "+formName, 404)
		return
	}

	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, s.errText(r, err), 404)
		return
	}

	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, _ := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		tpRows[tp.Name] = rows
	}

	refs := s.buildPrintRefs(r.Context(), row, entity, tpRows)
	constants, _ := s.store.ListConstants(r.Context())

	ctx := &printform.RenderContext{
		Document:   row,
		TableParts: tpRows,
		Constants:  constants,
		Refs:       refs,
	}
	pdfBytes, err := printform.RenderPDF(form, ctx)
	if err != nil {
		http.Error(w, "PDF error: "+s.errText(r, err), 500)
		return
	}

	origName := form.Name + ".pdf"
	if num, ok := row["Номер"].(string); ok && num != "" {
		origName = form.Name + "_" + num + ".pdf"
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", contentDisposition(origName))
	w.Write(pdfBytes)
}

// printDocumentDSLPF renders a DSL (.os) print form for a document/catalog record.
func (s *Server) printDocumentDSLPF(w http.ResponseWriter, r *http.Request) {
	entity := s.getEntity(w, r)
	if entity == nil {
		return
	}
	if !s.requirePerm(w, r, string(entity.Kind), entity.Name, "read") {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}
	pfName := chi.URLParam(r, "pfName")
	if dec, err2 := url.PathUnescape(pfName); err2 == nil {
		pfName = dec
	}

	// 1. Find DSL print form in registry
	dslForm := s.reg.GetDSLPrintForm(entity.Name, pfName)

	// 2. Also check entity module for a "Печать" procedure
	var procDecl *ast.ProcedureDecl
	var source string

	if dslForm != nil {
		source = dslForm.Source
	} else {
		// Try module procedure: entity module → "Печать"
		procDecl = s.reg.GetProcedure(entity.Name, "Печать")
		if procDecl == nil {
			procDecl = s.reg.GetProcedure(entity.Name, "Print")
		}
		if procDecl == nil {
			http.Error(w, "DSL print form not found: "+pfName, 404)
			return
		}
	}

	// 3. Parse .os source if needed (for standalone print form files)
	if procDecl == nil && source != "" {
		l := lexer.New(source, "printforms/"+pfName+".os")
		p := parser.New(l)
		prog, parseErr := p.ParseProgram()
		if parseErr != nil {
			http.Error(w, "parse error: "+s.errText(r, parseErr), 500)
			return
		}
		for _, proc := range prog.Procedures {
			lower := strings.ToLower(proc.Name.Literal)
			if lower == "сформировать" || lower == "сформироватьпечатнуюформу" || lower == "form" {
				procDecl = proc
				break
			}
		}
		if procDecl == nil {
			http.Error(w, fmt.Sprintf(s.tr(s.resolveLang(r), "Функция Сформировать() не найдена в %s.os"), pfName), 404)
			return
		}
	}

	// 4. Load record data
	row, err := s.store.GetByID(r.Context(), entity.Name, id, entity)
	if err != nil {
		http.Error(w, s.errText(r, err), 404)
		return
	}

	tpRows := make(map[string][]map[string]any)
	for _, tp := range entity.TableParts {
		rows, _ := s.store.GetTablePartRows(r.Context(), entity.Name, tp.Name, id, tp)
		tpRows[tp.Name] = rows
	}

	// 5. Resolve references so DSL can access Документ.Организация.Наименование etc.
	refs := s.buildPrintRefs(r.Context(), row, entity, tpRows)
	s.resolveDSLRefs(row, entity.Fields, refs)
	for _, tp := range entity.TableParts {
		for _, tpRow := range tpRows[tp.Name] {
			s.resolveDSLRefs(tpRow, tp.Fields, refs)
		}
	}

	// 6. Build DSL environment
	mc := runtime.NewMovementsCollector(entity.Name, id)
	dslVars := s.buildDSLVars(r.Context(), mc)

	// Embed table parts into document row for Документ.Товары access
	for tpName, rows := range tpRows {
		row[tpName] = rows
	}

	// Convert row + table parts into a DSL object
	docData := &interpreter.MapThis{M: row}
	dslVars["Документ"] = docData
	dslVars["Document"] = docData

	// Pass макет layout as DSL variable (if available)
	if dslForm != nil && dslForm.Layout != nil {
		dslVars["Макет"] = interpreter.NewMaket(dslForm.Layout)
	}

	// 7. Execute the DSL function
	var result any
	err = s.interp.RunWithResult(procDecl, docData, &result, dslVars)
	if err != nil {
		http.Error(w, "DSL error: "+s.errText(r, err), 500)
		return
	}

	// 8. Render result
	sd, ok := result.(*interpreter.SpreadsheetDocument)
	if !ok {
		http.Error(w, s.tr(s.resolveLang(r), "Процедура должна возвращать ТабличныйДокумент"), 500)
		return
	}

	// Set back URL for the Назад button
	backPath := fmt.Sprintf("/ui/%s/%s/%s", strings.ToLower(string(entity.Kind)), strings.ToLower(entity.Name), id.String())
	sd.SetBackURL(backPath)

	html := sd.HTMLString()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
