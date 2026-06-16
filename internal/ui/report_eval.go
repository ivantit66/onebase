package ui

import (
	"fmt"
	"sync"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/report/compose"
)

// interpEvaluator вычисляет DSL-условия `when` на интерпретаторе.
// Каждое выражение компилируется в процедуру один раз (кэш) и исполняется
// на строку через RunWithResult с полями строки как переменными.
//
// Не-bool результат when трактуется как false (правило не срабатывает);
// синтаксис when проверяется на этапе onebase check (план 59, Task 11).
type interpEvaluator struct {
	interp *interpreter.Interpreter
	mu     sync.Mutex
	cache  map[string]*ast.ProcedureDecl
}

// Контракт: interpEvaluator реализует compose.Evaluator (используется в Task 10).
var _ compose.Evaluator = (*interpEvaluator)(nil)

func newInterpEvaluator(interp *interpreter.Interpreter) *interpEvaluator {
	return &interpEvaluator{interp: interp, cache: map[string]*ast.ProcedureDecl{}}
}

func (e *interpEvaluator) compile(expr string) (*ast.ProcedureDecl, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.cache[expr]; ok {
		return p, nil
	}
	src := "Функция __cond()\nВозврат (" + expr + ");\nКонецФункции\n"
	prog, err := parser.New(lexer.New(src, "cond.os")).ParseProgram()
	if err != nil {
		return nil, err
	}
	var proc *ast.ProcedureDecl
	for _, d := range prog.Procedures {
		proc = d
		break
	}
	if proc == nil {
		return nil, fmt.Errorf("пустое выражение условия")
	}
	e.cache[expr] = proc
	return proc, nil
}

func (e *interpEvaluator) EvalBool(expr string, row compose.Row) (bool, error) {
	proc, err := e.compile(expr)
	if err != nil {
		return false, err
	}
	var result any
	if err := e.interp.RunWithResult(proc, &interpreter.MapThis{M: row}, &result, map[string]any(row)); err != nil {
		return false, err
	}
	b, _ := result.(bool)
	return b, nil
}
