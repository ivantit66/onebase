package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/entityservice"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

// catFactory реализует interpreter.CatalogObjectFactory: объекты справочников,
// создаваемые из DSL (Справочники.X.Создать() / Ссылка.ПолучитьОбъект()),
// получают табличные части и DSL-хук ПриЗаписи — симметрично документам
// (docWriter). Сохранение идёт через entityservice.Save, то есть тем же путём,
// что и запись из веб-формы: хук, ТЧ, движения, веб-хуки, планы обмена.
type catFactory struct {
	s      *Server
	ctxSrc docsCtxSource
}

func (s *Server) catObjectFactory(ctxSrc docsCtxSource) interpreter.CatalogObjectFactory {
	return &catFactory{s: s, ctxSrc: ctxSrc}
}

func (f *catFactory) NewCatalogObject(entity *metadata.Entity) any {
	return &catWriter{
		s:      f.s,
		ctxSrc: f.ctxSrc,
		entity: entity,
		obj:    runtime.NewObject(entity.Name, entity.Kind),
	}
}

func (f *catFactory) LoadCatalogObject(entity *metadata.Entity, uuidStr string) (any, error) {
	id, err := uuid.Parse(uuidStr)
	if err != nil {
		return nil, fmt.Errorf("неверный идентификатор ссылки: %q", uuidStr)
	}
	ctx := f.ctx()
	if err := f.s.checkDSLRowAccess(ctx, entity, "read", id, nil); err != nil {
		return nil, err
	}
	obj, err := f.s.loadRuntimeObject(ctx, entity, id)
	if err != nil {
		return nil, err
	}
	return &catWriter{s: f.s, ctxSrc: f.ctxSrc, entity: entity, obj: obj, loaded: true}, nil
}

func (f *catFactory) ctx() context.Context {
	if f.ctxSrc != nil {
		return f.ctxSrc.Ctx()
	}
	return context.Background()
}

// catWriter — записываемый объект справочника с табличными частями.
//
//	Кл = Справочники.Клиенты.Создать();
//	Кл.Наименование = "ООО Ромашка";
//	Стр = Кл.Контакты.Добавить();
//	Стр.Вид = "Email"; Стр.Значение = "info@romashka.ru";
//	Ссылка = Кл.Записать();
type catWriter struct {
	s      *Server
	ctxSrc docsCtxSource
	entity *metadata.Entity
	obj    *runtime.Object
	// loaded — объект получен из БД (Ссылка.ПолучитьОбъект), а не создан.
	// saved — объект уже записан в этой сессии. Оба используются ЭтоНовый().
	loaded bool
	saved  bool
}

func (w *catWriter) ctx() context.Context {
	if w.ctxSrc != nil {
		return w.ctxSrc.Ctx()
	}
	return context.Background()
}

// Get: имя табличной части → tpProxy, иначе значение поля шапки.
func (w *catWriter) Get(name string) any {
	for _, tp := range w.entity.TableParts {
		if strings.EqualFold(tp.Name, name) {
			return &tpProxy{obj: w.obj, tpName: tp.Name}
		}
	}
	return w.obj.Get(name)
}

func (w *catWriter) Set(name string, v any) {
	w.obj.Set(name, v)
}

// Fields — имена заполненных полей объекта: позволяет использовать объект как
// источник в ЗаполнитьЗначенияСвойств (совместимо с CatalogRecordWriter).
func (w *catWriter) Fields() []string {
	names := make([]string, 0, len(w.obj.Fields))
	for k := range w.obj.Fields {
		names = append(names, k)
	}
	return names
}

func (w *catWriter) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "записать", "write":
		if err := w.write(); err != nil {
			interpreter.RaiseUserError("Записать(" + w.entity.Name + "): " + err.Error())
		}
		return w.ref()
	case "установитьзначение", "setvalue":
		if len(args) >= 2 {
			if n, ok := args[0].(string); ok {
				w.Set(n, args[1])
			}
		}
	case "этоновый", "isnew":
		return !w.loaded && !w.saved
	case "прочитать", "read":
		if err := w.read(); err != nil {
			interpreter.RaiseUserError("Прочитать(" + w.entity.Name + "): " + err.Error())
		}
		return nil
	}
	return nil
}

// write сохраняет объект через entityservice.Save — с запуском ПриЗаписи,
// записью табличных частей и веб-хуками, как при записи из веб-формы.
func (w *catWriter) write() error {
	ctx := w.ctx()
	isNew := !w.loaded && !w.saved
	if w.accessID() == uuid.Nil {
		if err := w.s.autoFillRowAccessFields(ctx, w.entity, "write", w.obj.Fields); err != nil {
			return err
		}
	}
	if err := w.s.checkDSLRowAccess(ctx, w.entity, "write", w.accessID(), w.obj.Fields); err != nil {
		return err
	}
	result, err := w.s.entitySvc.Save(ctx, entityservice.SaveRequest{
		Entity:        w.entity,
		ID:            w.obj.ID,
		IsNew:         isNew,
		Fields:        w.obj.Fields,
		TablePartRows: w.obj.TablePartRows,
	})
	if err != nil {
		return err
	}
	if result.DSLError != "" {
		return fmt.Errorf("%s", result.DSLError)
	}
	w.saved = true
	return nil
}

func (w *catWriter) accessID() uuid.UUID {
	if w.loaded || w.saved {
		return w.obj.ID
	}
	return uuid.Nil
}

// read перечитывает шапку и табличные части из БД (Объект.Прочитать()).
func (w *catWriter) read() error {
	if !w.loaded && !w.saved {
		return fmt.Errorf("объект ещё не записан")
	}
	if err := w.s.checkDSLRowAccess(w.ctx(), w.entity, "read", w.obj.ID, nil); err != nil {
		return err
	}
	obj, err := w.s.loadRuntimeObject(w.ctx(), w.entity, w.obj.ID)
	if err != nil {
		return err
	}
	w.obj = obj
	w.loaded = true
	return nil
}

// ref строит ссылку на записанный объект с менеджером-прокси, чтобы
// Ссылка.ПолучитьОбъект()/Удалить() работали и возвращали catWriter.
func (w *catWriter) ref() *interpreter.Ref {
	return &interpreter.Ref{
		UUID:    w.obj.ID.String(),
		Name:    w.displayName(),
		Type:    w.entity.Name,
		Manager: w.s.refManagerFor(w.entity, w.ctx()),
	}
}

func (w *catWriter) displayName() string {
	for _, k := range []string{"наименование", "name"} {
		if v, ok := w.obj.Fields[k]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				return s
			}
		}
	}
	id := w.obj.ID.String()
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}
