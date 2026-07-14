package interpreter

// DSL-объект ПланыОбмена (план 86). Доступ по имени плана, затем метод:
//
//	Пакет = ПланыОбмена.ФилиалыЦентр.ВыгрузитьИзменения("fil01");
//	Применено = ПланыОбмена.ФилиалыЦентр.ЗагрузитьПакет(Пакет);
//
// Операции сами открывают транзакцию (BuildPackage/ApplyPackage), поэтому их не
// следует оборачивать в DSL-Транзацию(). Правило конфликта hook при загрузке из
// DSL пока откатывается к by_time (интерпретируемый обработчик — этап фазы 2).

import (
	"context"
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// ExchangeRegistry — то, что объекту ПланыОбмена нужно от реестра конфигурации.
// Реализуется *runtime.Registry (GetExchangePlan + GetEntity).
type ExchangeRegistry interface {
	GetExchangePlan(name string) *metadata.ExchangePlan
	GetEntity(name string) *metadata.Entity
}

// ExchangePlansRoot — корневой DSL-объект ПланыОбмена. Член по имени плана
// возвращает менеджер конкретного плана.
type ExchangePlansRoot struct {
	ctx   context.Context
	store *storage.DB
	reg   ExchangeRegistry
}

// NewExchangePlansRoot создаёт объект для инъекции в extraVars как «ПланыОбмена».
func NewExchangePlansRoot(ctx context.Context, store *storage.DB, reg ExchangeRegistry) *ExchangePlansRoot {
	return &ExchangePlansRoot{ctx: ctx, store: store, reg: reg}
}

func (r *ExchangePlansRoot) Get(planName string) any {
	plan := r.reg.GetExchangePlan(planName)
	if plan == nil {
		return nil
	}
	return &exchangePlanProxy{ctx: r.ctx, store: r.store, reg: r.reg, plan: plan}
}

func (r *ExchangePlansRoot) Set(_ string, _ any) {}

// exchangePlanProxy — менеджер одного плана обмена.
type exchangePlanProxy struct {
	ctx   context.Context
	store *storage.DB
	reg   ExchangeRegistry
	plan  *metadata.ExchangePlan
}

func (p *exchangePlanProxy) Get(_ string) any    { return nil }
func (p *exchangePlanProxy) Set(_ string, _ any) {}

func (p *exchangePlanProxy) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "выгрузитьизменения", "exportchanges":
		if len(args) < 1 {
			panic(userError{Msg: "ВыгрузитьИзменения: укажите код узла-получателя"})
		}
		node := exchangeArgString(args[0])
		if p.plan.Node(node) == nil {
			panic(userError{Msg: fmt.Sprintf("ВыгрузитьИзменения: узел %q не описан в плане %q", node, p.plan.Name)})
		}
		data, err := exchange.BuildPackage(p.ctx, p.store, p.reg, p.plan, node)
		if err != nil {
			panic(userError{Msg: "ВыгрузитьИзменения: " + err.Error()})
		}
		return string(data)
	case "загрузитьпакет", "importpackage":
		if len(args) < 1 {
			panic(userError{Msg: "ЗагрузитьПакет: передайте строку пакета"})
		}
		res, err := exchange.ApplyPackage(p.ctx, p.store, p.reg, p.plan, []byte(exchangeArgString(args[0])), exchange.ApplyOptions{})
		if err != nil {
			panic(userError{Msg: "ЗагрузитьПакет: " + err.Error()})
		}
		return float64(res.Applied + res.Deleted)
	}
	panic(userError{Msg: "ПланыОбмена: неизвестный метод " + method})
}

// exchangeArgString извлекает строку из DSL-аргумента (код узла или пакет).
func exchangeArgString(v any) string {
	if v == nil {
		return ""
	}
	if r, ok := v.(*Ref); ok {
		return r.Name
	}
	return fmt.Sprintf("%v", v)
}
