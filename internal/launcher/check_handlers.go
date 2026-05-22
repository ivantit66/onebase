package launcher

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/configdb"
	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/query"
)

// checkIssue is a single error/warning reported by the syntax checker. Line
// and column are 1-based; zero means the parser could not pinpoint a position.
type checkIssue struct {
	File    string `json:"file,omitempty"`
	Object  string `json:"object,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

// checkResponse is what AJAX-callers expect: ok=true means clean, otherwise
// issues holds the list. Total is convenient for "Найдено N ошибок" banners.
type checkResponse struct {
	OK     bool         `json:"ok"`
	Total  int          `json:"total"`
	Issues []checkIssue `json:"issues,omitempty"`
}

// configuratorCheck is the per-fragment endpoint. It accepts ad-hoc text from
// the module/widget/home-page editors and validates it without touching the
// stored configuration. The "kind" field selects the validator.
func (h *handler) configuratorCheck(w http.ResponseWriter, r *http.Request) {
	if _, err := h.store.Get(chi.URLParam(r, "id")); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	kind := r.FormValue("kind")
	source := r.FormValue("source")
	name := strings.TrimSpace(r.FormValue("name"))

	var issues []checkIssue
	switch kind {
	case "dsl":
		issues = checkDSLSource(source, name)
	case "widget":
		issues = checkWidgetYAML(source, name)
	case "home_page":
		issues = checkHomePageYAML(source)
	case "entity":
		issues = checkEntityYAML(source, name)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "неизвестный kind: " + kind})
		return
	}
	writeJSON(w, http.StatusOK, checkResponse{
		OK:     len(issues) == 0,
		Total:  len(issues),
		Issues: issues,
	})
}

// configuratorCheckAll walks the whole configuration and reports every
// detectable error: YAML parse failures, DSL syntax errors, and compile errors
// for queries declared in widgets/reports/scheduled jobs.
func (h *handler) configuratorCheckAll(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Materialise the project into a temp dir (or use the file path directly).
	dir, cleanup, err := materializeProject(r.Context(), h, b)
	if err != nil {
		writeJSON(w, http.StatusOK, checkResponse{
			OK:     false,
			Total:  1,
			Issues: []checkIssue{{Message: "не удалось получить конфигурацию: " + err.Error()}},
		})
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	issues := checkProjectDir(dir)

	// If file-level checks already failed, project.Load() will likely fail too;
	// run it anyway to surface cross-reference errors (validate.go) and to get
	// a Project we can use for query compilation.
	if proj, err := project.Load(dir); err == nil {
		defer proj.Close()
		issues = append(issues, checkQueries(proj)...)
	} else if !alreadyReported(issues, err.Error()) {
		issues = append(issues, checkIssue{Message: "Project.Load: " + err.Error()})
	}

	writeJSON(w, http.StatusOK, checkResponse{
		OK:     len(issues) == 0,
		Total:  len(issues),
		Issues: issues,
	})
}

// materializeProject ensures the configuration is available as a directory tree
// so the existing project.Load + file walkers can be reused unchanged. For
// file-backed bases that's just b.Path; for db-backed bases we export into a
// temp directory and the cleanup callback removes it on the way out.
func materializeProject(ctx context.Context, h *handler, b *Base) (dir string, cleanup func(), err error) {
	if b.ConfigSource != "database" {
		return b.Path, nil, nil
	}
	db, dberr := OpenDB(ctx, b)
	if dberr != nil {
		return "", nil, dberr
	}
	tmp, terr := os.MkdirTemp("", "onebase-check-")
	if terr != nil {
		db.Close()
		return "", nil, terr
	}
	repo := configdb.New(db)
	if exporr := repo.ExportToDir(ctx, tmp); exporr != nil {
		db.Close()
		os.RemoveAll(tmp)
		return "", nil, exporr
	}
	return tmp, func() { db.Close(); os.RemoveAll(tmp) }, nil
}

// checkProjectDir does a manual walk over the configuration sources so each
// broken file is reported individually instead of aborting on the first
// error (which is what project.Load would do).
func checkProjectDir(dir string) []checkIssue {
	var issues []checkIssue

	type yamlGroup struct {
		subdir string
		kind   string
		check  func(path string) error
	}
	groups := []yamlGroup{
		{"catalogs", "Справочник", func(p string) error { _, err := metadata.LoadFile(p, metadata.KindCatalog); return err }},
		{"documents", "Документ", func(p string) error { _, err := metadata.LoadFile(p, metadata.KindDocument); return err }},
		{"registers", "Регистр", func(p string) error { _, err := metadata.LoadRegisterFile(p); return err }},
		{"inforegs", "Регистр сведений", func(p string) error { _, err := metadata.LoadInfoRegisterFile(p); return err }},
		{"enums", "Перечисление", func(p string) error { _, err := metadata.LoadEnumFile(p); return err }},
		{"constants", "Константы", func(p string) error { _, err := metadata.LoadConstantsFile(p); return err }},
		{"widgets", "Виджет", func(p string) error { _, err := metadata.LoadWidgetFile(p); return err }},
	}
	for _, g := range groups {
		gdir := filepath.Join(dir, g.subdir)
		entries, _ := os.ReadDir(gdir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".yaml") {
				continue
			}
			path := filepath.Join(gdir, e.Name())
			if err := g.check(path); err != nil {
				issues = append(issues, checkIssue{
					File:    g.subdir + "/" + e.Name(),
					Object:  strings.TrimSuffix(e.Name(), ".yaml"),
					Kind:    g.kind,
					Message: err.Error(),
				})
			}
		}
	}

	// home_page.yaml — single file
	hpPath := filepath.Join(dir, "config", "home_page.yaml")
	if _, err := metadata.LoadHomePage(hpPath); err != nil {
		issues = append(issues, checkIssue{
			File:    "config/home_page.yaml",
			Object:  "home_page",
			Kind:    "Главная страница",
			Message: err.Error(),
		})
	}

	// .os DSL files
	srcDir := filepath.Join(dir, "src")
	entries, _ := os.ReadDir(srcDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".os") {
			continue
		}
		path := filepath.Join(srcDir, e.Name())
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			issues = append(issues, checkIssue{File: "src/" + e.Name(), Message: rerr.Error()})
			continue
		}
		issues = append(issues, parseDSL(string(raw), "src/"+e.Name())...)
	}

	return issues
}

// checkQueries compiles every query in widgets, reports, and scheduled jobs.
// Compilation needs metadata about registers, so this runs after the project
// has loaded successfully.
func checkQueries(proj *project.Project) []checkIssue {
	var issues []checkIssue
	opts := query.CompileOpts{
		Registers:   proj.Registers,
		InfoRegs:    proj.InfoRegisters,
		AccountRegs: proj.AccountRegisters,
		Entities:    proj.Entities,
	}
	for _, w := range proj.Widgets {
		if w.Query == "" {
			continue
		}
		params := map[string]any{}
		for k := range w.Params {
			params[k] = nil // placeholder so &Param doesn't fail name resolution
		}
		o := opts
		o.Params = params
		if _, err := query.Compile(w.Query, o); err != nil {
			issues = append(issues, checkIssue{
				File:    "widgets/" + w.Name + ".yaml",
				Object:  w.Name,
				Kind:    "Виджет (запрос)",
				Message: err.Error(),
			})
		}
	}
	for _, rep := range proj.Reports {
		if rep.Query == "" {
			continue
		}
		params := map[string]any{}
		for _, p := range rep.Params {
			params[p.Name] = nil
		}
		o := opts
		o.Params = params
		if _, err := query.Compile(rep.Query, o); err != nil {
			issues = append(issues, checkIssue{
				File:    "reports/" + rep.Name + ".yaml",
				Object:  rep.Name,
				Kind:    "Отчёт (запрос)",
				Message: err.Error(),
			})
		}
	}
	return issues
}

// parseDSL runs the lexer and parser on a snippet and then validates all
// function calls against known builtins + procedures defined in the module.
func parseDSL(source, label string) []checkIssue {
	l := lexer.New(source, label)
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		return []checkIssue{{File: label, Message: err.Error()}}
	}
	return checkDSLCalls(prog, label)
}

// checkDSLCalls walks the AST and reports calls to undefined functions.
// Known = builtins (interpreter package) + procedures declared in the same module.
func checkDSLCalls(prog *ast.Program, label string) []checkIssue {
	known := interpreter.KnownBuiltinNames()
	// collect names defined in this module
	for _, pr := range prog.Procedures {
		known[strings.ToLower(pr.Name.Literal)] = struct{}{}
	}
	var issues []checkIssue
	for _, pr := range prog.Procedures {
		walkStmts(pr.Body, known, label, &issues)
	}
	return issues
}

func walkStmts(stmts []ast.Stmt, known map[string]struct{}, label string, issues *[]checkIssue) {
	for _, s := range stmts {
		walkStmt(s, known, label, issues)
	}
}

func walkStmt(s ast.Stmt, known map[string]struct{}, label string, issues *[]checkIssue) {
	switch v := s.(type) {
	case *ast.ExprStmt:
		if ident, ok := v.X.(*ast.Ident); ok {
			*issues = append(*issues, checkIssue{
				File:    label,
				Line:    ident.Tok.Line,
				Column:  ident.Tok.Col,
				Message: fmt.Sprintf("выражение без эффекта: «%s» (возможно, пропущены скобки вызова?)", ident.Tok.Literal),
			})
		}
		walkExpr(v.X, known, label, issues)
	case *ast.AssignStmt:
		walkExpr(v.Value, known, label, issues)
	case *ast.ReturnStmt:
		if v.Value != nil {
			walkExpr(v.Value, known, label, issues)
		}
	case *ast.IfStmt:
		walkExpr(v.Cond, known, label, issues)
		walkStmts(v.Then, known, label, issues)
		for _, ei := range v.ElseIfs {
			walkExpr(ei.Cond, known, label, issues)
			walkStmts(ei.Body, known, label, issues)
		}
		walkStmts(v.Else, known, label, issues)
	case *ast.ForEachStmt:
		walkExpr(v.Collection, known, label, issues)
		walkStmts(v.Body, known, label, issues)
	case *ast.NumericForStmt:
		walkExpr(v.Start, known, label, issues)
		walkExpr(v.End, known, label, issues)
		walkStmts(v.Body, known, label, issues)
	case *ast.TryStmt:
		walkStmts(v.Try, known, label, issues)
		walkStmts(v.Except, known, label, issues)
	}
}

func walkExpr(e ast.Expr, known map[string]struct{}, label string, issues *[]checkIssue) {
	if e == nil {
		return
	}
	switch v := e.(type) {
	case *ast.CallExpr:
		if ident, ok := v.Callee.(*ast.Ident); ok {
			name := strings.ToLower(ident.Tok.Literal)
			if _, found := known[name]; !found {
				*issues = append(*issues, checkIssue{
					File:    label,
					Line:    ident.Tok.Line,
					Column:  ident.Tok.Col,
					Message: fmt.Sprintf("неизвестная функция %q", ident.Tok.Literal),
				})
			}
		}
		// always walk callee and args
		walkExpr(v.Callee, known, label, issues)
		for _, arg := range v.Args {
			walkExpr(arg, known, label, issues)
		}
	case *ast.BinaryExpr:
		walkExpr(v.Left, known, label, issues)
		walkExpr(v.Right, known, label, issues)
	case *ast.UnaryExpr:
		walkExpr(v.Operand, known, label, issues)
	case *ast.MemberExpr:
		walkExpr(v.Object, known, label, issues)
	case *ast.IndexExpr:
		walkExpr(v.Object, known, label, issues)
		walkExpr(v.Index, known, label, issues)
	case *ast.TernaryExpr:
		walkExpr(v.Cond, known, label, issues)
		walkExpr(v.True, known, label, issues)
		walkExpr(v.False, known, label, issues)
	case *ast.NewExpr:
		for _, arg := range v.Args {
			walkExpr(arg, known, label, issues)
		}
	}
}

func checkDSLSource(source, name string) []checkIssue {
	issues := parseDSL(source, name)
	for i := range issues {
		issues[i].Kind = "DSL модуль"
		issues[i].Object = name
	}
	return issues
}

func checkWidgetYAML(source, name string) []checkIssue {
	tmp, err := os.CreateTemp("", "widget-check-*.yaml")
	if err != nil {
		return []checkIssue{{Message: err.Error()}}
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(source)
	tmp.Close()
	if _, err := metadata.LoadWidgetFile(tmp.Name()); err != nil {
		return []checkIssue{{Kind: "Виджет", Object: name, Message: err.Error()}}
	}
	return nil
}

func checkHomePageYAML(source string) []checkIssue {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	tmp, err := os.CreateTemp("", "homepage-check-*.yaml")
	if err != nil {
		return []checkIssue{{Message: err.Error()}}
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(source)
	tmp.Close()
	if _, err := metadata.LoadHomePage(tmp.Name()); err != nil {
		return []checkIssue{{Kind: "Главная страница", Message: err.Error()}}
	}
	return nil
}

// checkEntityYAML validates a free-form catalog/document YAML. We don't know
// the kind from the form, so we try both — passing if either parses cleanly.
func checkEntityYAML(source, name string) []checkIssue {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	tmp, err := os.CreateTemp("", "entity-check-*.yaml")
	if err != nil {
		return []checkIssue{{Message: err.Error()}}
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(source)
	tmp.Close()
	if _, err1 := metadata.LoadFile(tmp.Name(), metadata.KindCatalog); err1 == nil {
		return nil
	}
	if _, err2 := metadata.LoadFile(tmp.Name(), metadata.KindDocument); err2 == nil {
		return nil
	}
	// Re-run to surface a real error message rather than the silent second try.
	_, e := metadata.LoadFile(tmp.Name(), metadata.KindCatalog)
	return []checkIssue{{Kind: "Объект метаданных", Object: name, Message: e.Error()}}
}

func alreadyReported(issues []checkIssue, msg string) bool {
	for _, i := range issues {
		if strings.Contains(msg, i.Message) || strings.Contains(i.Message, msg) {
			return true
		}
	}
	return false
}

