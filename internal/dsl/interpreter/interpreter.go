package interpreter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/dsl/token"
)

// dslStop — системная остановка (Error без Попытки, внутренние ошибки интерпретатора)
type dslStop struct{ err error }

// dslReturn — ранний выход через Возврат
type dslReturn struct{ val any }

// userError — пользовательская ошибка через Error(), перехватывается Попыткой
type userError struct{ Msg string }

// RaiseUserError panics with a DSL user error. Предназначено для
// внешних пакетов (например ui), которым нужно прервать выполнение DSL
// из метода объекта (CallMethod) с осмысленным сообщением — оно
// перехватывается Run/RunWithResult и Попыткой так же, как Error().
func RaiseUserError(msg string) {
	panic(userError{Msg: msg})
}

// loopBreak — выход из цикла через Прервать
type loopBreak struct{}

// loopContinue — переход к следующей итерации через Продолжить
type loopContinue struct{}

// DebugHook is the interface the interpreter calls for debugging.
// When nil on the Interpreter, there is zero overhead.
// Implemented by debugger.ActiveSession.
type DebugHook interface {
	HookCheckBreakpoint(file string, line int) bool
	HookShouldStep(file string, stackDepth int) bool
	HookOnPause(file string, line int, vars map[string]any, evalFn func(string) (any, error), reason string)
	HookPushFrame(procedure string, line int)
	HookPopFrame()
}

type Interpreter struct {
	LookupProc func(name string) *ast.ProcedureDecl
	// LookupSiblingProc resolves a helper procedure defined in the same
	// source file as the currently-executing statement. Used so that
	// `.proc.os` / `.posting.os` / `.rep.os` могут содержать вспомогательные
	// процедуры (см. замечание #13). Optional — может быть nil.
	LookupSiblingProc func(file, name string) *ast.ProcedureDecl
	// LookupModuleProc resolves Module.Proc() namespaced calls, например
	// `Утилиты.ФИФО(...)`. Используется когда object-часть MemberExpr —
	// идентификатор, не разрешённый в env как переменная. См. замечание #5.
	LookupModuleProc func(module, name string) *ast.ProcedureDecl
	DebugHook        DebugHook // nil = no debugging
	curFile          string    // last executed statement location (for error reporting)
	curLine          int
}

func New() *Interpreter { return &Interpreter{} }

// EvalExpr evaluates a parsed AST expression and returns the result.
// Public for the debugger console and debug handlers.
func (i *Interpreter) EvalExpr(expr ast.Expr, this This) any {
	e := newEnv(this)
	return i.evalExpr(expr, e)
}

// RunWithResult executes a function procedure and captures its return value.
func (i *Interpreter) RunWithResult(proc *ast.ProcedureDecl, this This, result *any, extraVars ...map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch s := r.(type) {
			case dslStop:
				err = s.err
			case userError:
				err = &DSLError{File: i.curFile, Line: i.curLine, Msg: s.Msg}
			case dslReturn:
				if result != nil {
					*result = s.val
				}
			default:
				panic(r)
			}
		}
	}()
	e := newEnv(this)
	for _, m := range extraVars {
		for k, v := range m {
			e.set(k, v)
		}
	}
	i.execBlock(proc.Body, e)
	return nil
}

// Run executes a procedure. Optional extra vars (e.g. {"Движения": collector}) are
// injected into the top-level environment.
func (i *Interpreter) Run(proc *ast.ProcedureDecl, this This, extraVars ...map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch s := r.(type) {
			case dslStop:
				err = s.err
			case userError:
				err = &DSLError{File: i.curFile, Line: i.curLine, Msg: s.Msg}
			case dslReturn:
				// early return from procedure — not an error
			default:
				panic(r)
			}
		}
	}()
	e := newEnv(this)
	for _, m := range extraVars {
		for k, v := range m {
			e.set(k, v)
		}
	}
	i.execBlock(proc.Body, e)
	return nil
}

func (i *Interpreter) execBlock(stmts []ast.Stmt, e *env) {
	for _, s := range stmts {
		if loc := getLocation(s); loc != nil {
			i.curFile = loc.File
			i.curLine = loc.Line
		}
		if i.DebugHook != nil {
			i.beforeStmt(s, e)
		}
		i.execStmt(s, e)
	}
}

// execLoopBody runs a loop body and returns true if the loop should continue,
// false if Прервать was encountered. Продолжить causes early return to next iteration.
func (i *Interpreter) execLoopBody(body []ast.Stmt, e *env) (cont bool) {
	cont = true
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case loopBreak:
				cont = false
			case loopContinue:
				// cont stays true, body was interrupted but loop continues
			default:
				panic(r)
			}
		}
	}()
	i.execBlock(body, e)
	return
}

func (i *Interpreter) beforeStmt(s ast.Stmt, e *env) {
	loc := getLocation(s)
	if loc == nil {
		return
	}

	hitBP := i.DebugHook.HookCheckBreakpoint(loc.File, loc.Line)
	shouldStep := i.DebugHook.HookShouldStep(loc.File, stackDepth(e))
	if !hitBP && !shouldStep {
		return
	}

	reason := "step"
	if hitBP {
		reason = "breakpoint"
	}
	vars := e.GetAllVariables()
	evalFn := func(expr string) (any, error) {
		return i.evaluateExprString(expr, e)
	}
	i.DebugHook.HookOnPause(loc.File, loc.Line, vars, evalFn, reason)
}

func stackDepth(e *env) int {
	d := 0
	for e != nil {
		d++
		e = e.parent
	}
	return d
}

func (i *Interpreter) evaluateExprString(expr string, e *env) (any, error) {
	l := lexer.New(expr, "<console>")
	p := parser.New(l)
	parsed, err := p.ParseExpr()
	if err != nil {
		return nil, err
	}
	return i.evalExpr(parsed, e), nil
}

func (i *Interpreter) execStmt(s ast.Stmt, e *env) {
	switch v := s.(type) {
	case *ast.IfStmt:
		cond := i.evalExpr(v.Cond, e)
		if truthy(cond) {
			i.execBlock(v.Then, e.child())
		} else {
			matched := false
			for _, elif := range v.ElseIfs {
				if truthy(i.evalExpr(elif.Cond, e)) {
					i.execBlock(elif.Body, e.child())
					matched = true
					break
				}
			}
			if !matched && len(v.Else) > 0 {
				i.execBlock(v.Else, e.child())
			}
		}
	case *ast.ForEachStmt:
		coll := i.evalExpr(v.Collection, e)
		switch items := coll.(type) {
		case []map[string]any:
			for _, row := range items {
				child := e.child()
				child.set(v.Var.Literal, &MapThis{M: row})
				if !i.execLoopBody(v.Body, child) {
					break
				}
			}
		case []any:
			for _, item := range items {
				child := e.child()
				child.set(v.Var.Literal, item)
				if !i.execLoopBody(v.Body, child) {
					break
				}
			}
		case *Array:
			for _, item := range items.Iterate() {
				child := e.child()
				child.set(v.Var.Literal, item)
				if !i.execLoopBody(v.Body, child) {
					break
				}
			}
		case *Map:
			for idx, key := range items.keys {
				child := e.child()
				child.set(v.Var.Literal, &KeyValue{Key: key, Value: items.vals[idx]})
				if !i.execLoopBody(v.Body, child) {
					break
				}
			}
		}
	case *ast.AssignStmt:
		val := i.evalExpr(v.Value, e)
		if v.Op != token.ASSIGN && v.Op != 0 {
			old := i.evalExpr(v.Target, e)
			val = applyCompoundOp(v.Op, old, val)
		}
		i.assign(v.Target, val, e)
	case *ast.ExprStmt:
		i.evalExpr(v.X, e)
	case *ast.VarDecl:
		e.set(v.Name.Literal, nil)
	case *ast.NumericForStmt:
		start := toFloatOr0(i.evalExpr(v.Start, e))
		end := toFloatOr0(i.evalExpr(v.End, e))
		for counter := start; counter <= end; counter++ {
			child := e.child()
			child.set(v.Var.Literal, counter)
			if !i.execLoopBody(v.Body, child) {
				break
			}
		}
	case *ast.ReturnStmt:
		var val any
		if v.Value != nil {
			val = i.evalExpr(v.Value, e)
		}
		panic(dslReturn{val: val})
	case *ast.TryStmt:
		i.execTry(v, e)
	case *ast.BreakStmt:
		panic(loopBreak{})
	case *ast.ContinueStmt:
		panic(loopContinue{})
	}
}

func (i *Interpreter) assign(target ast.Expr, val any, e *env) {
	switch t := target.(type) {
	case *ast.Ident:
		e.set(t.Tok.Literal, val)
	case *ast.MemberExpr:
		obj := i.evalExpr(t.Object, e)
		field := strings.ToLower(t.Field.Literal)
		switch o := obj.(type) {
		case This:
			o.Set(field, val)
		case *Struct:
			o.Set(field, val)
		}
	case *ast.IndexExpr:
		obj := i.evalExpr(t.Object, e)
		idx := i.evalExpr(t.Index, e)
		switch o := obj.(type) {
		case *Array:
			o.SetIndex(int(toFloatOr0(idx)), val)
		case *Map:
			o.CallMethod("вставить", []any{idx, val})
		}
	}
}

func (i *Interpreter) evalExpr(expr ast.Expr, e *env) any {
	switch v := expr.(type) {
	case *ast.StringLit:
		return v.Value
	case *ast.NumberLit:
		f, _ := strconv.ParseFloat(v.Value, 64)
		return f
	case *ast.BoolLit:
		return v.Value
	case *ast.Ident:
		val, _ := e.get(v.Tok.Literal)
		return val
	case *ast.MemberExpr:
		obj := i.evalExpr(v.Object, e)
		field := strings.ToLower(v.Field.Literal)
		switch o := obj.(type) {
		case This:
			return o.Get(field)
		case *Struct:
			return o.Get(field)
		case *KeyValue:
			return o.Get(field)
		}
		return nil
	case *ast.IndexExpr:
		obj := i.evalExpr(v.Object, e)
		idx := i.evalExpr(v.Index, e)
		switch o := obj.(type) {
		case *Array:
			return o.Index(int(toFloatOr0(idx)))
		case *Map:
			return o.CallMethod("получить", []any{idx})
		}
		return nil
	case *ast.NewExpr:
		return i.evalNew(v, e)
	case *ast.UnaryExpr:
		return i.evalUnary(v, e)
	case *ast.TernaryExpr:
		if truthy(i.evalExpr(v.Cond, e)) {
			return i.evalExpr(v.True, e)
		}
		return i.evalExpr(v.False, e)
	case *ast.BinaryExpr:
		return i.evalBinary(v, e)
	case *ast.CallExpr:
		return i.evalCall(v, e)
	}
	return nil
}

func (i *Interpreter) evalNew(n *ast.NewExpr, e *env) any {
	args := i.evalArgs(n.Args, e)
	typeName := strings.ToLower(n.TypeName.Literal)
	switch typeName {
	case "массив", "array":
		return &Array{}
	case "соответствие", "map":
		return &Map{}
	case "структура", "structure":
		return newStruct(args)
	}
	// Расширяемые типы через env: "__factory_<ИмяТипа>"
	if factory, ok := e.get("__factory_" + typeName); ok {
		if fn, ok := factory.(func([]any) any); ok {
			return fn(args)
		}
	}
	panic(userError{Msg: "Новый: неизвестный тип " + n.TypeName.Literal})
}

func (i *Interpreter) evalUnary(u *ast.UnaryExpr, e *env) any {
	val := i.evalExpr(u.Operand, e)
	switch u.Op.Type {
	case token.NOT:
		return !truthy(val)
	case token.MINUS:
		f, _ := toFloat(val)
		return -f
	}
	return nil
}

func (i *Interpreter) evalBinary(b *ast.BinaryExpr, e *env) any {
	// short-circuit для AND/OR
	if b.Op.Type == token.AND {
		l := i.evalExpr(b.Left, e)
		if !truthy(l) {
			return false
		}
		return truthy(i.evalExpr(b.Right, e))
	}
	if b.Op.Type == token.OR {
		l := i.evalExpr(b.Left, e)
		if truthy(l) {
			return true
		}
		return truthy(i.evalExpr(b.Right, e))
	}
	l := i.evalExpr(b.Left, e)
	r := i.evalExpr(b.Right, e)
	switch b.Op.Type {
	case token.ASSIGN: // equality in conditions
		return equal(l, r)
	case token.NEQ:
		return !equal(l, r)
	case token.LT:
		return compare(l, r) < 0
	case token.GT:
		return compare(l, r) > 0
	case token.LTE:
		return compare(l, r) <= 0
	case token.GTE:
		return compare(l, r) >= 0
	case token.PLUS:
		lf, lok := toFloat(l)
		rf, rok := toFloat(r)
		if lok && rok {
			return lf + rf
		}
		return fmt.Sprintf("%v", l) + fmt.Sprintf("%v", r)
	case token.MINUS:
		lf, lok := toFloat(l)
		rf, rok := toFloat(r)
		if lok && rok {
			return lf - rf
		}
	case token.STAR:
		lf, lok := toFloat(l)
		rf, rok := toFloat(r)
		if lok && rok {
			return lf * rf
		}
	case token.SLASH:
		lf, lok := toFloat(l)
		rf, rok := toFloat(r)
		if lok && rok && rf != 0 {
			return lf / rf
		}
	}
	return nil
}

func (i *Interpreter) evalCall(c *ast.CallExpr, e *env) any {
	args := i.evalArgs(c.Args, e)
	switch callee := c.Callee.(type) {
	case *ast.Ident:
		fnName := callee.Tok.Literal
		if val, ok := e.get(fnName); ok {
			if bf, ok2 := val.(BuiltinFunc); ok2 {
				result, err := bf(args, callee.Tok.File, callee.Tok.Line)
				if err != nil {
					panic(dslStop{err: err})
				}
				return result
			}
		}
		if i.LookupProc != nil {
			if proc := i.LookupProc(fnName); proc != nil {
				return i.callUserProc(proc, e, args)
			}
		}
		// Помощник из того же файла (.proc.os / .posting.os / .rep.os),
		// см. замечание #13.
		if i.LookupSiblingProc != nil && i.curFile != "" {
			if proc := i.LookupSiblingProc(i.curFile, fnName); proc != nil {
				return i.callUserProc(proc, e, args)
			}
		}
		fn, ok := builtins[strings.ToLower(fnName)]
		if !ok {
			panic(dslStop{err: fmt.Errorf("%s:%d: unknown function %q", callee.Tok.File, callee.Tok.Line, fnName)})
		}
		result, err := fn(args, callee.Tok.File, callee.Tok.Line)
		if err != nil {
			panic(dslStop{err: err})
		}
		return result
	case *ast.MemberExpr:
		recv := i.evalExpr(callee.Object, e)
		method := strings.ToLower(callee.Field.Literal)
		switch o := recv.(type) {
		case MethodCallable:
			return o.CallMethod(method, args)
		case *Struct:
			return o.CallMethod(method, args)
		}
		// Если object — идентификатор, не разрешившийся в значение,
		// и это известный модуль — резолвим Module.Proc() (замечание #5).
		if recv == nil && i.LookupModuleProc != nil {
			if objIdent, ok := callee.Object.(*ast.Ident); ok {
				if proc := i.LookupModuleProc(objIdent.Tok.Literal, callee.Field.Literal); proc != nil {
					return i.callUserProc(proc, e, args)
				}
			}
		}
		return nil
	}
	return nil
}

func (i *Interpreter) callUserProc(proc *ast.ProcedureDecl, callEnv *env, args []any) (retVal any) {
	if i.DebugHook != nil {
		i.DebugHook.HookPushFrame(proc.Name.Literal, 0)
		defer i.DebugHook.HookPopFrame()
	}
	defer func() {
		if r := recover(); r != nil {
			switch s := r.(type) {
			case dslReturn:
				retVal = s.val
			default:
				panic(r)
			}
		}
	}()
	child := &env{vars: make(map[string]any), parent: callEnv, this: callEnv.this}
	for idx, param := range proc.Params {
		if idx < len(args) {
			child.set(param.Literal, args[idx])
			continue
		}
		// Параметр без переданного значения — пробуем дефолт (замечание #12).
		// Дефолт вычисляется в callEnv, чтобы видеть глобальные/модульные
		// идентификаторы. child ещё не имеет других параметров — это
		// сознательно: не даём дефолтам ссылаться на «соседей» (1С-семантика).
		if idx < len(proc.Defaults) && proc.Defaults[idx] != nil {
			child.set(param.Literal, i.evalExpr(proc.Defaults[idx], callEnv))
		} else {
			child.set(param.Literal, nil)
		}
	}
	i.execBlock(proc.Body, child)
	return nil
}

func (i *Interpreter) evalArgs(exprs []ast.Expr, e *env) []any {
	args := make([]any, len(exprs))
	for idx, a := range exprs {
		args[idx] = i.evalExpr(a, e)
	}
	return args
}

func truthy(v any) bool {
	if v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t != ""
	}
	return true
}

func equal(a, b any) bool {
	return refKey(a) == refKey(b)
}

func compare(a, b any) int {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	as := fmt.Sprintf("%v", a)
	bs := fmt.Sprintf("%v", b)
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func toFloatOr0(v any) float64 {
	f, _ := toFloat(v)
	return f
}

// execTry выполняет Попытка/Исключение.
// Только userError перехватывается; системные паники и dslReturn пробрасываются дальше.
func (i *Interpreter) execTry(t *ast.TryStmt, e *env) {
	var caught *userError
	func() {
		defer func() {
			if r := recover(); r != nil {
				if ue, ok := r.(userError); ok {
					caught = &ue
					return
				}
				panic(r) // dslReturn, dslStop, loopBreak, loopContinue — пробрасываем
			}
		}()
		i.execBlock(t.Try, e)
	}()
	if caught != nil {
		if len(t.Except) == 0 {
			// Нет блока Исключение — пробрасываем ошибку дальше
			panic(*caught)
		}
		msg := caught.Msg
		exceptEnv := e.child()
		descFn := BuiltinFunc(func(args []any, file string, line int) (any, error) {
			return msg, nil
		})
		exceptEnv.set("ОписаниеОшибки", descFn)
		exceptEnv.set("ErrorDescription", descFn)
		i.execBlock(t.Except, exceptEnv)
	}
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// applyCompoundOp computes the result of a compound assignment operator.
func applyCompoundOp(op token.Type, old, val any) any {
	lf, lok := toFloat(old)
	rf, rok := toFloat(val)
	if lok && rok {
		switch op {
		case token.PLUS_ASSIGN:
			return lf + rf
		case token.MINUS_ASSIGN:
			return lf - rf
		case token.STAR_ASSIGN:
			return lf * rf
		case token.SLASH_ASSIGN:
			if rf != 0 {
				return lf / rf
			}
			return float64(0)
		}
	}
	if op == token.PLUS_ASSIGN {
		return fmt.Sprintf("%v", old) + fmt.Sprintf("%v", val)
	}
	return val
}
