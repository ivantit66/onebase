// Package dslvars содержит общую базу DSL-переменных, разделяемую UI-handlers
// (internal/ui) и фоновыми scheduled jobs (internal/scheduler). Раньше каждый
// из этих контекстов строил карту инжекций сам — две почти одинаковые
// реализации с тонкими отличиями, из-за чего добавление новой builtin требовало
// правок в обоих местах (и регулярно одно из них забывали).
//
// Сейчас обе стороны зовут Common.Build() для пересечения функционала
// (Перечисления, Константы, Запрос, ПредопределённыеЗначения, Движения,
// HTTP/Email), а сверху добавляют свои специфичные переменные (для UI это
// блокировки, текущий пользователь, Документы.Создать, транзакции из DSL,
// табличные документы, диаграммы; для scheduler — Параметры и Сообщить с
// привязкой к runtime-логу задания).
package dslvars

import (
	"context"
	"errors"
	"strings"

	"github.com/ivantit66/onebase/internal/aiassist"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// Common — параметры базовой DSL-карты. Заполните поля и вызовите Build().
type Common struct {
	Ctx       context.Context
	Reg       *runtime.Registry
	Store     *storage.DB
	Mailer    interpreter.EmailSender     // nil допустим — email-функции запаникуют при вызове, не при сборке
	Movements *runtime.MovementsCollector // nil допустим — попадёт в карту как Движения=nil (совместимо с прежним поведением)
	// NetGuard вызывается перед каждой сетевой операцией DSL (HTTP-клиент,
	// email). Возвращает ошибку, если сеть заблокирована предохранителем
	// (план 62). nil → сеть не ограничивается (для тестов/совместимости).
	NetGuard func() error
	// ExecGuard вызывается перед запуском команды ОС (ВыполнитьКоманду, план 67).
	// nil → команды ЗАПРЕЩЕНЫ (secure by default, в отличие от NetGuard): exec —
	// исполнение кода на сервере, разрешается только явной настройкой базы.
	ExecGuard func() error
}

// Build возвращает map с пересечением DSL-переменных, общих для UI и scheduler.
// Caller добавляет свои специфичные ключи сверху (например, "Сообщить",
// "Документы", транзакционные builtins) напрямую в возвращённую карту.
func (c Common) Build() map[string]any {
	enumsMap := make(map[string]any)
	for _, e := range c.Reg.Enums() {
		inner := make(map[string]any, len(e.Values))
		for _, v := range e.Values {
			inner[v] = v
		}
		enumsMap[e.Name] = &interpreter.MapThis{M: inner}
	}
	constsMap := make(map[string]any)
	if vals, err := c.Store.ListConstants(c.Ctx); err == nil {
		constsMap = vals
	}
	queryFactory := interpreter.NewQueryFactory(c.Ctx, c.Store, c.Reg)
	predefined := interpreter.NewPredefinedRoot(c.Ctx, c.Store)

	vars := map[string]any{
		"Движения":                 c.Movements,
		"Перечисления":             &interpreter.MapThis{M: enumsMap},
		"Константы":                &interpreter.MapThis{M: constsMap},
		"__factory_Запрос":         queryFactory,
		"__factory_Query":          queryFactory,
		"ПредопределённыеЗначения": predefined,
		"PredefinedValues":         predefined,
	}
	for k, v := range interpreter.NewHTTPFunctions(c.NetGuard) {
		vars[k] = v
	}
	for k, v := range interpreter.NewEmailFunctions(c.Mailer, c.NetGuard) {
		vars[k] = v
	}
	for k, v := range interpreter.NewFileFunctions(nil) {
		vars[k] = v
	}
	for k, v := range interpreter.NewLLMFunctions(aiassist.New(c.Ctx, c.Store, nil)) {
		vars[k] = v
	}
	// Объекты HTTP-сервисов (HTTPСервисОтвет, ОтветJSON/ОтветТекст) — нужны
	// прежде всего обработчикам сервисов, но безвредны и в обработках/заданиях,
	// поэтому держим их в общем наборе (как HTTP-клиент).
	for k, v := range interpreter.NewServiceFunctions() {
		vars[k] = v
	}

	// Команды ОС (план 67). Выключены по умолчанию: если caller не задал
	// ExecGuard (фоновые задания/тесты), подставляем deny — secure by default
	// (в отличие от сети, где nil-guard разрешает для совместимости). Каждый
	// запуск пишется в журнал (defense-in-depth, поверх настроек аудита).
	execGuard := c.ExecGuard
	if execGuard == nil {
		execGuard = func() error { return errors.New("выполнение команд ОС отключено") }
	}
	var execAudit interpreter.ExecAudit
	if c.Store != nil {
		execAudit = func(command string, cmdArgs []string, code int) {
			detail := strings.Join(cmdArgs, " ")
			if len(detail) > 1000 {
				detail = detail[:1000]
			}
			_ = c.Store.Log(c.Ctx, &storage.AuditEntry{
				UserLogin:  storage.AuditUserLogin(c.Ctx),
				Action:     "exec",
				EntityName: command,
				Field:      detail,
				NewValue:   code,
			})
		}
	}
	for k, v := range interpreter.NewExecFunctions(execGuard, execAudit) {
		vars[k] = v
	}
	return vars
}
