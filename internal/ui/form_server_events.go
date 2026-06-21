package ui

// Серверные события управляемых форм, исполняемые ДО рендера HTML (issue #148).
//
// Раньше единственным «событием открытия» был ПриОткрытии — но он исполняется
// на КЛИЕНТЕ (obFire по DOMContentLoaded), уже после того как сервер отдал форму
// со всеми полями. Это не позволяло реализовать RLS на чтение: пользователь
// видел данные чужой записи, даже если обработчик потом бросал исключение.
//
// ПриЧтенииНаСервере (по аналогии с 1С «ПриЧтенииНаСервере») исполняется
// синхронно на сервере при GET формы объекта, до формирования HTML. Если
// обработчик бросает ВызватьИсключение — formEdit отдаёт 403 и не раскрывает
// данные.

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

// loadRuntimeObject загружает существующую запись (шапка + табличные части +
// обогащённые ссылки) в *runtime.Object — пригодный для исполнения обработчиков
// формы как «Объект». Та же логика, что в docProxy.LoadObject.
func (s *Server) loadRuntimeObject(ctx context.Context, entity *metadata.Entity, id uuid.UUID) (*runtime.Object, error) {
	row, err := s.store.GetByID(ctx, entity.Name, id, entity)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]any, len(row))
	for _, f := range entity.Fields {
		if v, ok := row[f.Name]; ok && v != nil {
			fields[strings.ToLower(f.Name)] = v
		}
	}
	tpRows := make(map[string][]map[string]any, len(entity.TableParts))
	for _, tp := range entity.TableParts {
		rows, err := s.store.GetTablePartRows(ctx, entity.Name, tp.Name, id, tp)
		if err != nil {
			return nil, fmt.Errorf("табличная часть %s: %w", tp.Name, err)
		}
		tpRows[tp.Name] = rows
	}
	obj := &runtime.Object{
		ID:            id,
		Type:          entity.Name,
		Kind:          entity.Kind,
		Fields:        fields,
		TablePartRows: tpRows,
	}
	s.enrichHeaderRefs(ctx, entity, obj)
	for _, tp := range entity.TableParts {
		s.enrichTPRowsWithRefs(ctx, tp, tpRows[tp.Name])
	}
	return obj, nil
}

// runFormReadHook исполняет серверный обработчик ПриЧтенииНаСервере формы объекта
// (если он объявлен) с «Объект», загруженным из БД. Возвращает ошибку обработчика
// (если он бросил исключение) — тогда вызывающий код обязан отказать в рендере.
//
// Если формы/обработчика/AST нет — возвращает nil (no-op). Ошибку загрузки
// объекта тоже глотает в nil: пусть обычный путь formEdit отрисует ошибку 404.
func (s *Server) runFormReadHook(ctx context.Context, entity *metadata.Entity, form *metadata.FormModule, id uuid.UUID) error {
	if form == nil || s.interp == nil {
		return nil
	}
	procName := resolveHandlerProc(form, "", string(metadata.FormEventOnReadAtServer))
	if procName == "" {
		return nil
	}
	program, ok := form.ProgramAST.(*ast.Program)
	if !ok || program == nil {
		return nil
	}
	var decl *ast.ProcedureDecl
	for _, p := range program.Procedures {
		if strings.EqualFold(p.Name.Literal, procName) {
			decl = p
			break
		}
	}
	if decl == nil {
		return nil
	}

	obj, err := s.loadRuntimeObject(ctx, entity, id)
	if err != nil {
		return nil
	}

	mc := runtime.NewMovementsCollector(entity.Name, obj.ID)
	var msgs []string
	vars := s.buildDSLVarsWithMessages(ctx, mc, &msgs)
	thisObj := &formObjectThis{obj: obj, entity: entity, form: form}
	vars["Объект"] = thisObj
	vars["ЭтотОбъект"] = thisObj

	formProcs := make(map[string]*ast.ProcedureDecl, len(program.Procedures))
	for _, p := range program.Procedures {
		formProcs[strings.ToLower(p.Name.Literal)] = p
	}
	vars["__form_procs__"] = formProcs

	return s.interp.Run(decl, thisObj, vars)
}
