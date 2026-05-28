// Package configcheck validates a OneBase configuration: YAML metadata schema,
// DSL syntax, undefined function calls, and query compilation. It reports each
// problem with a file:line:column location.
//
// The logic was originally inside internal/launcher (configurator endpoints);
// it lives here so both the web configurator and the `onebase check` CLI gate
// share one implementation.
package configcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/query"
)

// Issue is a single error/warning. Line and column are 1-based; zero means the
// parser could not pinpoint a position.
type Issue struct {
	File    string `json:"file,omitempty"`
	Object  string `json:"object,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}

// Result is the machine-readable check outcome: ok=true means clean.
type Result struct {
	OK     bool    `json:"ok"`
	Total  int     `json:"total"`
	Issues []Issue `json:"issues,omitempty"`
}

// NewResult wraps a slice of issues into a Result (sets OK/Total).
func NewResult(issues []Issue) Result {
	return Result{OK: len(issues) == 0, Total: len(issues), Issues: issues}
}

// CheckDir walks the configuration sources under dir so each broken file is
// reported individually instead of aborting on the first error (which is what
// project.Load would do). Covers YAML metadata + .os DSL files.
func CheckDir(dir string) []Issue {
	var issues []Issue

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
				issues = append(issues, Issue{
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
		issues = append(issues, Issue{
			File:    "config/home_page.yaml",
			Object:  "home_page",
			Kind:    "Главная страница",
			Message: err.Error(),
		})
	}

	// .os DSL files — два прохода, чтобы вызовы процедур из ДРУГИХ модулей
	// (общие модули, утилиты) не считались «неизвестными функциями».
	srcDir := filepath.Join(dir, "src")
	entries, _ := os.ReadDir(srcDir)
	type parsedModule struct {
		label string
		prog  *ast.Program
	}
	var modules []parsedModule
	projProcs := map[string]struct{}{} // имена всех процедур по всей конфигурации
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".os") {
			continue
		}
		label := "src/" + e.Name()
		raw, rerr := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if rerr != nil {
			issues = append(issues, Issue{File: label, Message: rerr.Error()})
			continue
		}
		l := lexer.New(string(raw), label)
		prog, perr := parser.New(l).ParseProgram()
		if perr != nil {
			issues = append(issues, Issue{File: label, Message: perr.Error()})
			continue
		}
		for _, pr := range prog.Procedures {
			projProcs[strings.ToLower(pr.Name.Literal)] = struct{}{}
		}
		modules = append(modules, parsedModule{label, prog})
	}
	for _, m := range modules {
		issues = append(issues, CheckDSLCallsKnown(m.prog, m.label, projProcs)...)
	}

	return issues
}

// CheckQueries compiles every query in widgets and reports. Compilation needs
// metadata about registers, so this runs after the project has loaded.
func CheckQueries(proj *project.Project) []Issue {
	var issues []Issue
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
			issues = append(issues, Issue{
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
			issues = append(issues, Issue{
				File:    "reports/" + rep.Name + ".yaml",
				Object:  rep.Name,
				Kind:    "Отчёт (запрос)",
				Message: err.Error(),
			})
		}
	}
	return issues
}

// ParseDSL runs the lexer and parser on a snippet, then validates function
// calls against known builtins + procedures defined in the same module.
func ParseDSL(source, label string) []Issue {
	l := lexer.New(source, label)
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		return []Issue{{File: label, Message: err.Error()}}
	}
	return CheckDSLCalls(prog, label)
}

// CheckDSLCalls walks the AST and reports calls to undefined functions.
// Known = builtins + procedures declared in the same module.
func CheckDSLCalls(prog *ast.Program, label string) []Issue {
	return CheckDSLCallsKnown(prog, label, nil)
}

// CheckDSLCallsKnown is like CheckDSLCalls but also treats the names in extra
// as known — used by CheckDir to recognise procedures declared in OTHER modules
// (common modules, utilities), avoiding false "unknown function" reports.
func CheckDSLCallsKnown(prog *ast.Program, label string, extra map[string]struct{}) []Issue {
	known := interpreter.KnownBuiltinNames()
	for _, pr := range prog.Procedures {
		known[strings.ToLower(pr.Name.Literal)] = struct{}{}
	}
	for k := range extra {
		known[k] = struct{}{}
	}
	var issues []Issue
	for _, pr := range prog.Procedures {
		walkStmts(pr.Body, known, label, &issues)
	}
	return issues
}

// CheckDSLSource validates ad-hoc module text (configurator editor).
func CheckDSLSource(source, name string) []Issue {
	issues := ParseDSL(source, name)
	for i := range issues {
		issues[i].Kind = "DSL модуль"
		issues[i].Object = name
	}
	return issues
}

// CheckWidgetYAML validates ad-hoc widget YAML text.
func CheckWidgetYAML(source, name string) []Issue {
	tmp, err := os.CreateTemp("", "widget-check-*.yaml")
	if err != nil {
		return []Issue{{Message: err.Error()}}
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(source)
	tmp.Close()
	if _, err := metadata.LoadWidgetFile(tmp.Name()); err != nil {
		return []Issue{{Kind: "Виджет", Object: name, Message: err.Error()}}
	}
	return nil
}

// CheckHomePageYAML validates ad-hoc home-page YAML text.
func CheckHomePageYAML(source string) []Issue {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	tmp, err := os.CreateTemp("", "homepage-check-*.yaml")
	if err != nil {
		return []Issue{{Message: err.Error()}}
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(source)
	tmp.Close()
	if _, err := metadata.LoadHomePage(tmp.Name()); err != nil {
		return []Issue{{Kind: "Главная страница", Message: err.Error()}}
	}
	return nil
}

// CheckEntityYAML validates a free-form catalog/document YAML. The kind is
// unknown, so both are tried — passing if either parses cleanly.
func CheckEntityYAML(source, name string) []Issue {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	tmp, err := os.CreateTemp("", "entity-check-*.yaml")
	if err != nil {
		return []Issue{{Message: err.Error()}}
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
	_, e := metadata.LoadFile(tmp.Name(), metadata.KindCatalog)
	return []Issue{{Kind: "Объект метаданных", Object: name, Message: e.Error()}}
}

// AlreadyReported reports whether msg overlaps an existing issue message.
func AlreadyReported(issues []Issue, msg string) bool {
	for _, i := range issues {
		if strings.Contains(msg, i.Message) || strings.Contains(i.Message, msg) {
			return true
		}
	}
	return false
}

func walkStmts(stmts []ast.Stmt, known map[string]struct{}, label string, issues *[]Issue) {
	for _, s := range stmts {
		walkStmt(s, known, label, issues)
	}
}

func walkStmt(s ast.Stmt, known map[string]struct{}, label string, issues *[]Issue) {
	switch v := s.(type) {
	case *ast.ExprStmt:
		if ident, ok := v.X.(*ast.Ident); ok {
			*issues = append(*issues, Issue{
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

func walkExpr(e ast.Expr, known map[string]struct{}, label string, issues *[]Issue) {
	if e == nil {
		return
	}
	switch v := e.(type) {
	case *ast.CallExpr:
		if ident, ok := v.Callee.(*ast.Ident); ok {
			name := strings.ToLower(ident.Tok.Literal)
			if _, found := known[name]; !found {
				*issues = append(*issues, Issue{
					File:    label,
					Line:    ident.Tok.Line,
					Column:  ident.Tok.Col,
					Message: fmt.Sprintf("неизвестная функция %q", ident.Tok.Literal),
				})
			}
		}
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
