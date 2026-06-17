package ui

// Запуск DSL-хуков (ОбработкаПроведения/ПриЗаписи) и сборка переменных
// окружения DSL для обработчиков.
// Выделено из handlers.go (план 55, этап 1) — перенос as-is.

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dslvars"
	"github.com/ivantit66/onebase/internal/runtime"
)

func (s *Server) runOnWrite(obj *runtime.Object, mc *runtime.MovementsCollector) string {
	errMsg, _ := s.runOnWriteCtx(context.Background(), obj, mc)
	return errMsg
}

func (s *Server) buildDSLVars(ctx context.Context, mc *runtime.MovementsCollector) map[string]any {
	// Базовый набор (Перечисления, Константы, Запрос, Предопределённые,
	// Движения, HTTP, Email) — общий с scheduler, см. internal/dslvars.
	vars := dslvars.Common{
		Ctx: ctx, Reg: s.reg, Store: s.store, Mailer: s.mailer, Movements: mc,
		NetGuard:  s.netGuard(ctx),
		ExecGuard: s.execGuard(ctx),
	}.Build()

	// TxState несёт «живой» контекст. Транзакционные функции
	// (НачатьТранзакцию и т.д.) и запись справочников из обработки
	// (Справочники.X.Создать().Записать()) используют txState.Ctx(),
	// поэтому запись участвует в открытой DSL-транзакции.
	txState := interpreter.NewTxState(ctx)
	// Caller подключается ДО создания CatalogsRoot.WithManagerCaller —
	// он использует ctx как контекст для вызова процедур менеджера.
	mgrCaller := &managerCaller{s: s, ctx: ctx}
	catalogs := interpreter.NewCatalogsRoot(txState, s.store, s.reg).WithManagerCaller(mgrCaller)
	// Документы.X.Создать()/.Записать()/.Провести() из обработки.
	documents := newDocsRoot(s, txState)
	// РегистрыНакопления.X.Остатки()/.Движения()/.ВыбратьПоРегистратору(Док).
	accumRegs := newAccumRegsRoot(s, txState)
	// #2 managed locks: builtin БлокировкаДанных() возвращает свежий LockObject,
	// привязанный к глобальному менеджеру server'а.
	lockFactory := interpreter.BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
		return runtime.NewLockObject(s.lockMgr), nil
	})

	// API текущего пользователя для персональных настроек.
	// ТекущийПользователь() → объект {ИД, Имя, ПолноеИмя, Админ}.
	// ИмяПользователя()     → строка-логин (или "" для фоновых заданий).
	var curUserID, curUserLogin, curUserFullName string
	var curUserAdmin bool
	if u := auth.UserFromContext(ctx); u != nil {
		curUserID, curUserLogin, curUserFullName, curUserAdmin = u.ID, u.Login, u.FullName, u.IsAdmin
	}
	userObj := &interpreter.MapThis{M: map[string]any{
		"ИД": curUserID, "Имя": curUserLogin, "ПолноеИмя": curUserFullName, "Админ": curUserAdmin,
		"ID": curUserID, "Login": curUserLogin, "FullName": curUserFullName, "IsAdmin": curUserAdmin,
	}}
	currentUserFn := interpreter.BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
		return userObj, nil
	})
	userNameFn := interpreter.BuiltinFunc(func(_ []any, _ string, _ int) (any, error) {
		return curUserLogin, nil
	})

	// ЗначениеРеквизитаОбъекта(Ссылка, "Реквизит") — чтение реквизита по
	// ссылке (ссылка несёт лишь UUID/наименование). Использует txState.Ctx(),
	// поэтому видит данные открытой DSL-транзакции.
	attrValueFn := interpreter.BuiltinFunc(func(args []any, _ string, _ int) (any, error) {
		return s.objectAttributeValue(txState.Ctx(), args)
	})

	vars["Справочники"] = catalogs
	vars["Catalogs"] = catalogs
	vars["Документы"] = documents
	vars["Documents"] = documents
	vars["РегистрыНакопления"] = accumRegs
	vars["AccumulationRegisters"] = accumRegs
	vars["БлокировкаДанных"] = lockFactory
	vars["DataLock"] = lockFactory
	vars["ТекущийПользователь"] = currentUserFn
	vars["CurrentUser"] = currentUserFn
	vars["ИмяПользователя"] = userNameFn
	vars["UserName"] = userNameFn
	vars["ЗначениеРеквизитаОбъекта"] = attrValueFn
	vars["ObjectAttributeValue"] = attrValueFn

	// транзакции из DSL (обработки/проведение). Раньше NewTxFunctions
	// использовался только в тестах — отсюда «unknown function
	// НачатьТранзакцию». Теперь подключаем к реальному рантайму.
	for k, v := range interpreter.NewTxFunctions(txState, s.store) {
		vars[k] = v
	}
	for k, v := range interpreter.NewSpreadsheetFunctions() {
		vars[k] = v
	}
	for k, v := range interpreter.NewChartFunctions() {
		vars[k] = v
	}
	return vars
}

func (s *Server) buildDSLVarsWithMessages(ctx context.Context, mc *runtime.MovementsCollector, msgs *[]string) map[string]any {
	vars := s.buildDSLVars(ctx, mc)
	userKey := userKeyFromCtx(ctx)
	msgFunc := interpreter.BuiltinFunc(func(args []any, file string, line int) (any, error) {
		if len(args) > 0 {
			text := fmt.Sprintf("%v", args[0])
			if msgs != nil {
				*msgs = append(*msgs, text)
			}
			s.messages.Push(userKey, text)
		}
		return nil, nil
	})
	vars["Сообщить"] = msgFunc
	vars["Message"] = msgFunc
	return vars
}

func (s *Server) runOnWriteCtx(ctx context.Context, obj *runtime.Object, mc *runtime.MovementsCollector) (string, []string) {
	proc := s.reg.GetProcedure(obj.Type, "OnWrite")
	if proc == nil {
		return "", nil
	}
	// Симметрично runOnPostCtx: ссылки в полях шапки из формы приходят
	// сырыми UUID — обогащаем до *Ref{UUID,Name}, чтобы ЗначениеРеквизитаОбъекта
	// и Строка(ref) работали в ПриЗаписи так же, как при проведении.
	if entity := s.reg.GetEntity(obj.Type); entity != nil {
		s.enrichHeaderRefs(ctx, entity, obj)
	}
	var msgs []string
	vars := s.buildDSLVarsWithMessages(ctx, mc, &msgs)
	if err := s.interp.Run(proc, obj, vars); err != nil {
		if dslErr, ok := err.(*interpreter.DSLError); ok {
			return dslErr.Error(), msgs
		}
		return err.Error(), msgs
	}
	return "", msgs
}

// callManagerProc вызывает процедуру модуля менеджера (X.manager.os) для
// сущности entityName. found=true если процедура объявлена — независимо от
// успеха/ошибки. Используется CatalogProxy/docProxy в качестве fallback после
// встроенных методов (Создать, НайтиПо…, Удалить).
//
// MovementsCollector создаётся пустой (UUID.Nil): методы менеджера не привязаны
// к экземпляру и не пишут движения; если пользователю нужны движения — он
// должен делать Документы.X.Создать().Записать() явно.
func (s *Server) callManagerProc(ctx context.Context, entityName, method string, args []any) (any, bool, error) {
	proc := s.reg.GetManagerProc(entityName, method)
	if proc == nil {
		return nil, false, nil
	}
	mc := runtime.NewMovementsCollector(entityName, uuid.Nil)
	vars := s.buildDSLVars(ctx, mc)
	result, err := s.interp.Call(proc, nil, args, vars)
	return result, true, err
}

// managerCaller адаптер для interpreter.ManagerCaller. Используется в
// buildDSLVars для подключения fallback к CatalogsRoot.
type managerCaller struct {
	s   *Server
	ctx context.Context
}

func (m *managerCaller) CallManager(entityName, method string, args []any) (any, bool, error) {
	return m.s.callManagerProc(m.ctx, entityName, method, args)
}

func (s *Server) runOnPostCtx(ctx context.Context, obj *runtime.Object, mc *runtime.MovementsCollector) (string, []string) {
	proc := s.reg.GetProcedure(obj.Type, "OnPost")
	if proc == nil {
		return "", nil
	}
	// Симметрично табличным частям: ссылки в полях шапки из формы приходят
	// сырыми UUID — обогащаем до *Ref{UUID,Name}, чтобы string-измерения
	// (Склад, Касса, Контрагент) фильтровались по имени, как при проведении
	// из обработки. См. П.37.
	if entity := s.reg.GetEntity(obj.Type); entity != nil {
		s.enrichHeaderRefs(ctx, entity, obj)
	}
	var msgs []string
	vars := s.buildDSLVarsWithMessages(ctx, mc, &msgs)
	if err := s.interp.Run(proc, obj, vars); err != nil {
		if dslErr, ok := err.(*interpreter.DSLError); ok {
			return dslErr.Error(), msgs
		}
		return err.Error(), msgs
	}
	return "", msgs
}
