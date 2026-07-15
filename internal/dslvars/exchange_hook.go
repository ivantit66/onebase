package dslvars

// Разрешение конфликтов обмена правилом conflict: hook (план 86). Запускает
// процедуру ПриКонфликтеОбмена(Локальный, Входящий) из общего модуля и трактует
// её результат как «победило ли входящее изменение» (Истина = заменить локальный
// объект входящим, Ложь = оставить локальный).
//
// Каноническая реализация живёт здесь (а не в ui), чтобы hook подключался на ВСЕХ
// путях приёма: Go-уровневых (онлайн push, CLI load/sync — через ui.NewExchangeHook,
// тонкую обёртку над этой) и DSL-пути ЗагрузитьПакет (ExchangePlansRoot получает
// hook, когда Common.Build видит непустой Interp). Раньше DSL-путь откатывался к
// by_time, потому что реализация была в ui, а interpreter не может импортировать ui.

import (
	"context"
	"fmt"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
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
	// Common без Interp намеренно: если сам обработчик конфликта вызовет
	// ЗагрузитьПакет, его ПланыОбмена уже не потянут hook (откат к by_time) — это
	// защита от бесконечной рекурсии hook → ЗагрузитьПакет → hook.
	vars := Common{Ctx: ctx, Reg: h.reg, Store: h.store}.Build()
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
	return hookResultBool(result), nil
}

// hookResultBool трактует результат ПриКонфликтеОбмена как булево (Истина =
// входящее победило). Числа из DSL приходят как int/int64.
func hookResultBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	}
	return false
}
