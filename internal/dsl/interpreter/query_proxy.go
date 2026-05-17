package interpreter

import (
	"context"
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
	"github.com/ivantit66/onebase/internal/storage"
)

// QueryDB is the minimal storage interface needed by queryProxy.
type QueryDB interface {
	QueryAll(ctx context.Context, sql string, args ...any) ([]map[string]any, error)
	Dialect() storage.Dialect
}

// QueryRegistry is the minimal registry interface needed by queryProxy.
type QueryRegistry interface {
	Registers() []*metadata.Register
	InfoRegisters() []*metadata.InfoRegister
	AccountRegisters() []*metadata.AccountRegister
}

// queryProxy реализует DSL-объект Новый Запрос.
// Поддерживает свойство Текст (Get/Set) и методы УстановитьПараметр / Выполнить.
type queryProxy struct {
	text   string
	params map[string]any
	db     QueryDB
	reg    QueryRegistry
	ctx    context.Context
}

// NewQueryProxy создаёт фабрику для инъекции через extraVars.
// Использование: extraVars["__factory_Запрос"] = interpreter.NewQueryFactory(ctx, db, reg)
func NewQueryFactory(ctx context.Context, db QueryDB, reg QueryRegistry) func(args []any) any {
	return func(args []any) any {
		return &queryProxy{
			params: make(map[string]any),
			db:     db,
			reg:    reg,
			ctx:    ctx,
		}
	}
}

// ─── This interface ───────────────────────────────────────────────────────────

func (q *queryProxy) Get(field string) any {
	switch field {
	case "текст", "text":
		return q.text
	}
	return nil
}

func (q *queryProxy) Set(field string, val any) {
	switch field {
	case "текст", "text":
		q.text = fmt.Sprintf("%v", val)
	}
}

// ─── MethodCallable interface ─────────────────────────────────────────────────

func (q *queryProxy) CallMethod(name string, args []any) any {
	switch name {
	case "установитьпараметр", "setparameter":
		if len(args) >= 2 {
			key := fmt.Sprintf("%v", args[0])
			q.params[key] = args[1]
		}
		return nil
	case "выполнить", "execute":
		return q.execute()
	}
	panic(userError{Msg: "Объект Запрос не имеет метода " + name})
}

func (q *queryProxy) execute() *Array {
	if strings.TrimSpace(q.text) == "" {
		panic(userError{Msg: "Запрос.Текст не задан"})
	}
	res, err := query.Compile(q.text, query.CompileOpts{
		Params:      q.params,
		Registers:   q.reg.Registers(),
		InfoRegs:    q.reg.InfoRegisters(),
		AccountRegs: q.reg.AccountRegisters(),
		Dialect:     q.db.Dialect(),
	})
	if err != nil {
		panic(userError{Msg: "Ошибка запроса: " + err.Error()})
	}
	rows, err := q.db.QueryAll(q.ctx, res.SQL, res.Args...)
	if err != nil {
		panic(userError{Msg: "Ошибка выполнения SQL: " + err.Error()})
	}
	arr := &Array{}
	for _, row := range rows {
		s := &Struct{vals: make(map[string]any)}
		for k, v := range row {
			k = strings.ToLower(k)
			s.keys = append(s.keys, k)
			s.vals[k] = v
		}
		arr.items = append(arr.items, s)
	}
	return arr
}
