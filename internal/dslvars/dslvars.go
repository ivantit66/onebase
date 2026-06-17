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

	// СсылкаНаОбъект(Объект) → канонический URL карточки /ui/<вид>/<сущность>/<id>
	// (план 66). То же соглашение, что у виджетов; вид определяется по типу
	// ссылки через реестр. Полезно прежде всего на страницах, но безвредно в
	// обработках/заданиях, поэтому в общем наборе.
	objectRef := interpreter.BuiltinFunc(func(args []any, _ string, _ int) (any, error) {
		if len(args) == 0 || args[0] == nil {
			return "", nil
		}
		ref, ok := args[0].(*interpreter.Ref)
		if !ok || ref.UUID == "" || ref.Type == "" {
			return "", nil
		}
		kind := "catalog"
		if ent := c.Reg.GetEntity(ref.Type); ent != nil {
			kind = strings.ToLower(string(ent.Kind))
		}
		return "/ui/" + kind + "/" + ref.Type + "/" + ref.UUID, nil
	})
	vars["СсылкаНаОбъект"] = objectRef
	vars["ObjectRef"] = objectRef
	return vars
}
