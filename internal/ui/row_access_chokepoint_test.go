package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// План 79F (defense-in-depth): у query compiler есть fail-closed сеть
// assertRowFiltersApplied; у списочных/объектных путей (store.List/GetByID) её
// нет — строковый фильтр применяется по соглашению в хендлерах. Этот тест не даёт
// молча появиться новому HTTP-хендлеру, читающему данные в обход строковой
// политики: каждый метод-хендлер (сигнатура (http.ResponseWriter, *http.Request)),
// вызывающий store.List/GetByID напрямую, обязан ЛИБО сам вызвать одну из функций
// строкового доступа (chokepointGateFuncs), ЛИБО стоять в chokepointExempt с
// обоснованием. Хелперы (иные сигнатуры) считаются вызванными из гейтнутых
// хендлеров и здесь не проверяются — их поверхность закрывают вызывающие.
//
// Добавляя новый хендлер с прямым store.List/GetByID: примените applyRowFilter /
// rowAllowed* (см. row_access.go) или, если данные не подлежат строковому
// ограничению, внесите его в chokepointExempt с причиной.

// isHTTPHandler — метод с сигнатурой (http.ResponseWriter, *http.Request).
func isHTTPHandler(fd *ast.FuncDecl) bool {
	if fd.Type.Params == nil || len(fd.Type.Params.List) != 2 {
		return false
	}
	return chokepointExprString(fd.Type.Params.List[0].Type) == "http.ResponseWriter" &&
		chokepointExprString(fd.Type.Params.List[1].Type) == "*http.Request"
}

func chokepointExprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
	case *ast.StarExpr:
		return "*" + chokepointExprString(t.X)
	}
	return ""
}

var chokepointStoreReads = map[string]bool{"List": true, "GetByID": true}

var chokepointGateFuncs = map[string]bool{
	"applyRowFilter": true, "rowAllowed": true, "rowAllowedFor": true,
	"rowAllowedID": true, "rowAllowedUpdate": true, "rowAllowsID": true,
	"rowAllowsSelected": true, "rowAllowsOwnerID": true, "requireOwnerRow": true,
	"rowFilterFor": true, "applyRegRowFilter": true, "rowDecision": true,
	"rowDecisionFor": true, "renderRowDecision": true, "rowAccessRestricted": true,
}

// chokepointExempt — функции, читающие store.List/GetByID напрямую и осознанно
// без строкового фильтра. Ключ — имя функции/метода, значение — обоснование.
var chokepointExempt = map[string]string{}

func TestRowAccessChokepoint_NoUngatedStoreReads(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	type finding struct {
		fn     string
		file   string
		gated  bool
		exempt bool
	}
	var findings []finding
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range af.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if !isHTTPHandler(fd) {
				continue // хелперы вне области — их закрывают вызывающие хендлеры
			}
			readsStore, gated := false, false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if chokepointGateFuncs[sel.Sel.Name] {
					gated = true
				}
				// s.store.List / s.store.GetByID: внешний селектор .store
				if chokepointStoreReads[sel.Sel.Name] {
					if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "store" {
						readsStore = true
					}
				}
				return true
			})
			if !readsStore {
				continue
			}
			name := fd.Name.Name
			_, isExempt := chokepointExempt[name]
			findings = append(findings, finding{fn: name, file: f, gated: gated, exempt: isExempt})
		}
	}
	// Страж от вакуумного прохождения: детектор обязан находить хотя бы один
	// хендлер, читающий store (сейчас их несколько, напр. listExcel гейтит через
	// applyRowFilter). Ноль означал бы, что детектор сломался.
	if len(findings) == 0 {
		t.Fatal("детектор не нашёл ни одного хендлера с store.List/GetByID — сломан матчинг?")
	}
	var violations []string
	for _, fnd := range findings {
		if !fnd.gated && !fnd.exempt {
			violations = append(violations, fnd.fn+" ("+fnd.file+")")
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("функции читают store.List/GetByID без строкового гейта и не в chokepointExempt (%d):\n  %s\n\n"+
			"Примените applyRowFilter/rowAllowed* (row_access.go) или внесите в chokepointExempt с обоснованием.",
			len(violations), strings.Join(violations, "\n  "))
	}
}
