package ui

// Разрешение конфликтов обмена правилом conflict: hook (план 86). Каноническая
// реализация живёт в internal/dslvars (чтобы hook подключался и на DSL-пути
// ЗагрузитьПакет, недоступном ui из-за направления импортов); здесь — тонкие
// ре-экспорты для Go-путей приёма (онлайн push, CLI load/sync).

import (
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dslvars"
	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// ExchangeHookName — имя обработчика конфликта в общем модуле (.module.os).
const ExchangeHookName = dslvars.ExchangeHookName

// NewExchangeHook строит exchange.HookResolver поверх интерпретатора конфигурации.
func NewExchangeHook(store *storage.DB, reg *runtime.Registry, interp *interpreter.Interpreter) exchange.HookResolver {
	return dslvars.NewExchangeHook(store, reg, interp)
}
