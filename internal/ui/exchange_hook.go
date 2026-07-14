package ui

// Разрешение конфликтов обмена правилом conflict: hook (план 86, фаза 2).
// Запускает процедуру ПриКонфликтеОбмена(Локальный, Входящий) из общего модуля
// и трактует её результат как «победило ли входящее изменение» (Истина =
// заменить локальный объект входящим, Ложь = оставить локальный).
//
// Подключается на Go-уровневых путях приёма (онлайн push, CLI load/sync). В DSL-
// пути ЗагрузитьПакет правило hook откатывается к by_time (см. exchange_builtins).

import (
	"context"
	"fmt"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dslvars"
	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// ExchangeHookName — имя обработчика конфликта в общем модуле (.module.os).
const ExchangeHookName = "ПриКонфликтеОбмена"

type exchangeHook struct {
	store  *storage.DB
	reg    *runtime.Registry
	interp *interpreter.Interpreter
}

// NewExchangeHook строит exchange.HookResolver поверх интерпретатора конфигурации.
func NewExchangeHook(store *storage.DB, reg *runtime.Registry, interp *interpreter.Interpreter) exchange.HookResolver {
	return exchangeHook{store: store, reg: reg, interp: interp}
}

func (h exchangeHook) ResolveConflict(ctx context.Context, plan *metadata.ExchangePlan, entity *metadata.Entity, local, incoming *exchange.PackageObject) (bool, error) {
	proc := h.reg.GetModuleProc(ExchangeHookName)
	if proc == nil {
		return false, fmt.Errorf("правило конфликта hook: процедура %s не найдена (объявите её Экспорт в общем модуле src/*.module.os)", ExchangeHookName)
	}
	vars := dslvars.Common{Ctx: ctx, Reg: h.reg, Store: h.store}.Build()
	// Локальный/Входящий — Структуры с полями объектов (доступ по имени реквизита
	// регистронезависимый). Имена параметров берём как объявил разработчик.
	if len(proc.Params) >= 1 {
		vars[proc.Params[0].Literal] = interpreter.NewStructFromMap(local.Fields)
	}
	if len(proc.Params) >= 2 {
		vars[proc.Params[1].Literal] = interpreter.NewStructFromMap(incoming.Fields)
	}
	var result any
	if err := h.interp.RunWithResult(proc, nil, &result, vars); err != nil {
		return false, fmt.Errorf("правило конфликта hook (%s): %w", ExchangeHookName, err)
	}
	return asBool(result), nil
}
