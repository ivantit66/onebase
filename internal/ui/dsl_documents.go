package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

// docsCtxSource предоставляет «живой» контекст (с открытой DSL-транзакцией,
// если она есть). Реализуется *interpreter.TxState.
type docsCtxSource interface {
	Ctx() context.Context
}

// refManagerFor строит менеджера для ссылки на сущность по её метаданным:
// CatalogProxy для справочников, docProxy для документов. Используется в
// enrichHeaderRefs/enrichTPRowsWithRefs и dsl_object_attr, чтобы ссылки,
// собранные из значений колонок БД, несли менеджера — иначе у Удалить()
// и ПолучитьОбъект() на них не было бы привязки к типу.
func (s *Server) refManagerFor(entity *metadata.Entity, ctx context.Context) interpreter.RefManager {
	if entity == nil {
		return nil
	}
	ctxSrc := interpreter.NewStaticCtx(ctx)
	switch entity.Kind {
	case metadata.KindCatalog:
		return interpreter.NewCatalogProxy(entity, s.store, ctxSrc)
	case metadata.KindDocument:
		return &docProxy{s: s, ctxSrc: ctxSrc, entity: entity}
	}
	return nil
}

// docsRoot — DSL-глобал Документы / Documents (
// Документы.X.Создать() → пишущий объект документа с табличными частями
// и методами Записать()/Провести().
type docsRoot struct {
	s      *Server
	ctxSrc docsCtxSource
}

func newDocsRoot(s *Server, ctxSrc docsCtxSource) *docsRoot {
	return &docsRoot{s: s, ctxSrc: ctxSrc}
}

func (d *docsRoot) Get(name string) any {
	entity := d.s.reg.GetEntity(name)
	if entity == nil || entity.Kind != metadata.KindDocument {
		return nil
	}
	return &docProxy{s: d.s, ctxSrc: d.ctxSrc, entity: entity}
}

func (d *docsRoot) Set(_ string, _ any) {}

// docProxy — Документы.ПоступлениеТоваров.
type docProxy struct {
	s      *Server
	ctxSrc docsCtxSource
	entity *metadata.Entity
}

func (p *docProxy) Get(_ string) any    { return nil }
func (p *docProxy) Set(_ string, _ any) {}

func (p *docProxy) ctx() context.Context {
	if p.ctxSrc != nil {
		return p.ctxSrc.Ctx()
	}
	return context.Background()
}

func (p *docProxy) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "создать", "create":
		return &docWriter{
			s:      p.s,
			ctxSrc: p.ctxSrc,
			entity: p.entity,
			obj: &runtime.Object{
				ID:            uuid.New(),
				Type:          p.entity.Name,
				Kind:          p.entity.Kind,
				Fields:        map[string]any{},
				TablePartRows: map[string][]map[string]any{},
			},
		}
	case "найтипономеру", "findbynumber":
		if len(args) == 0 {
			return nil
		}
		value := fmt.Sprint(args[0])
		if r, ok := args[0].(*interpreter.Ref); ok {
			value = r.Name
		}
		idStr, display, found, err := p.s.store.FindCatalogByField(p.ctx(), p.entity, "Номер", value)
		if err != nil {
			interpreter.RaiseUserError("НайтиПоНомеру(" + p.entity.Name + "): " + err.Error())
		}
		if !found {
			return nil
		}
		return &interpreter.Ref{UUID: idStr, Name: display, Type: p.entity.Name, Manager: p}
	case "удалить", "delete":
		if len(args) == 0 {
			interpreter.RaiseUserError("Удалить(" + p.entity.Name + "): не передана ссылка")
		}
		ref, ok := args[0].(*interpreter.Ref)
		if !ok {
			interpreter.RaiseUserError(fmt.Sprintf("Удалить(%s): ожидается ссылка, получено %T", p.entity.Name, args[0]))
		}
		if err := p.DeleteRef(ref.UUID); err != nil {
			interpreter.RaiseUserError("Удалить(" + p.entity.Name + "): " + err.Error())
		}
		return nil
	}
	return nil
}

// DeleteRef реализует interpreter.RefManager — удаление документа по UUID.
func (p *docProxy) DeleteRef(uuidStr string) error {
	id, err := uuid.Parse(uuidStr)
	if err != nil {
		return fmt.Errorf("неверный идентификатор ссылки: %q", uuidStr)
	}
	return p.s.store.Delete(p.ctx(), p.entity.Name, id)
}

// LoadObject реализует interpreter.RefManager — загружает существующий документ
// (шапка + табличные части) по UUID и возвращает docWriter, через который
// Ссылка.ПолучитьОбъект().Поле = … → Записать()/Провести() обновят документ.
func (p *docProxy) LoadObject(uuidStr string) (any, error) {
	id, err := uuid.Parse(uuidStr)
	if err != nil {
		return nil, fmt.Errorf("неверный идентификатор ссылки: %q", uuidStr)
	}
	row, err := p.s.store.GetByID(p.ctx(), p.entity.Name, id, p.entity)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]any, len(row))
	for _, f := range p.entity.Fields {
		if v, ok := row[f.Name]; ok && v != nil {
			fields[strings.ToLower(f.Name)] = v
		}
	}
	tpRows := make(map[string][]map[string]any, len(p.entity.TableParts))
	for _, tp := range p.entity.TableParts {
		rows, err := p.s.store.GetTablePartRows(p.ctx(), p.entity.Name, tp.Name, id, tp)
		if err != nil {
			return nil, fmt.Errorf("табличная часть %s: %w", tp.Name, err)
		}
		tpRows[tp.Name] = rows
	}
	obj := &runtime.Object{
		ID:            id,
		Type:          p.entity.Name,
		Kind:          p.entity.Kind,
		Fields:        fields,
		TablePartRows: tpRows,
	}
	// Обогащаем UUID-строки в ссылочных полях шапки и ТЧ до *Ref{…,Manager},
	// чтобы DSL мог писать Док.СсылочноеПоле.ПолучитьОбъект()/.Наименование.
	// Без этого Док.Покупатель — голая строка UUID, у которой нет методов.
	p.s.enrichHeaderRefs(p.ctx(), p.entity, obj)
	for _, tp := range p.entity.TableParts {
		p.s.enrichTPRowsWithRefs(p.ctx(), tp, tpRows[tp.Name])
	}
	return &docWriter{
		s:      p.s,
		ctxSrc: p.ctxSrc,
		entity: p.entity,
		obj:    obj,
	}, nil
}

// docWriter — записываемый/проводимый документ.
//
//	Док = Документы.ПоступлениеТоваров.Создать();
//	Док.Дата = ТекущаяДата();
//	Стр = Док.Товары.Добавить();
//	Стр.Номенклатура = Ном; Стр.Количество = 100; Стр.Цена = 500;
//	Док.Записать();
//	Док.Провести();
type docWriter struct {
	s      *Server
	ctxSrc docsCtxSource
	entity *metadata.Entity
	obj    *runtime.Object
}

func (w *docWriter) ctx() context.Context {
	if w.ctxSrc != nil {
		return w.ctxSrc.Ctx()
	}
	return context.Background()
}

// Get: имя табличной части → tpProxy, иначе значение поля шапки.
func (w *docWriter) Get(name string) any {
	for _, tp := range w.entity.TableParts {
		if strings.EqualFold(tp.Name, name) {
			return &tpProxy{w: w, tpName: tp.Name}
		}
	}
	return w.obj.Get(name)
}

func (w *docWriter) Set(name string, v any) {
	w.obj.Set(name, v)
}

func (w *docWriter) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "записать", "write":
		if err := w.write(); err != nil {
			interpreter.RaiseUserError("Записать(" + w.entity.Name + "): " + err.Error())
		}
		return w.ref()
	case "провести", "post":
		if err := w.write(); err != nil {
			interpreter.RaiseUserError("Провести/Записать(" + w.entity.Name + "): " + err.Error())
		}
		if err := w.post(); err != nil {
			interpreter.RaiseUserError("Провести(" + w.entity.Name + "): " + err.Error())
		}
		return w.ref()
	case "установитьзначение", "setvalue":
		if len(args) >= 2 {
			if n, ok := args[0].(string); ok {
				w.Set(n, args[1])
			}
		}
	}
	return nil
}

// autoNumber заполняет реквизит Номер очередным номером нумератора, если
// у документа есть строковый реквизит Номер и он ещё не задан. Повторяет
// поведение веб-хендлера: документ, записанный из обработки, нумеруется
// так же, как созданный через форму. Явно заданный Док.Номер сохраняется.
func (w *docWriter) autoNumber() {
	if w.entity.Kind != metadata.KindDocument {
		return
	}
	for _, f := range w.entity.Fields {
		if !strings.EqualFold(f.Name, "Номер") || f.Type != metadata.FieldTypeString {
			continue
		}
		if cur := w.obj.Get("Номер"); cur == nil || fmt.Sprint(cur) == "" {
			w.obj.Set("Номер", w.s.generateNumber(w.ctx(), w.entity, w.obj.Fields))
		}
		return
	}
}

// write проставляет номер документа, вызывает ПриЗаписи (OnWrite), затем
// сохраняет шапку + табличные части. Автонумерация и вызов ПриЗаписи
// повторяют поведение веб-хендлера при обычной записи: без них номер и
// расчётные реквизиты (СуммаНДС, итоги) остались бы незаполненными при
// записи документа из обработки.
// Использует живой ctx, поэтому при открытой DSL-транзакции запись
// участвует в ней; иначе автокоммит.
func (w *docWriter) write() error {
	ctx := w.ctx()
	w.autoNumber()
	mc := runtime.NewMovementsCollector(w.entity.Name, w.obj.ID)
	setPeriodFromFields(mc, w.entity, w.obj.Fields)
	if errMsg, _ := w.s.runOnWriteCtx(ctx, w.obj, mc); errMsg != "" {
		return fmt.Errorf("%s", errMsg)
	}
	if err := w.s.store.Upsert(ctx, w.entity.Name, w.obj.ID, w.obj.Fields, w.entity); err != nil {
		return err
	}
	if err := w.s.saveTablePartsDirect(ctx, w.entity, w.obj.ID, w.obj.TablePartRows); err != nil {
		return err
	}
	// Для непроводимых документов движения, записанные в ПриЗаписи, фиксируем.
	// У проводимых документов движения формирует проведение (post).
	if !w.entity.Posting {
		return w.s.saveMovements(ctx, w.entity.Name, w.obj.ID, mc)
	}
	return nil
}

// post запускает OnPost, собирает движения и фиксирует проведение —
// та же логика, что в postDocument (UI-проведение).
func (w *docWriter) post() error {
	ctx := w.ctx()
	mc := runtime.NewMovementsCollector(w.entity.Name, w.obj.ID)
	setPeriodFromFields(mc, w.entity, w.obj.Fields)
	if errMsg, _ := w.s.runOnPostCtx(ctx, w.obj, mc); errMsg != "" {
		return fmt.Errorf("%s", errMsg)
	}
	if err := w.s.saveMovements(ctx, w.entity.Name, w.obj.ID, mc); err != nil {
		return err
	}
	return w.s.store.SetPosted(ctx, w.entity.Name, w.obj.ID, true)
}

// ref строит ссылку на записанный документ с привязкой к менеджеру,
// чтобы Ссылка.Удалить() работала.
func (w *docWriter) ref() *interpreter.Ref {
	return &interpreter.Ref{
		UUID:    w.obj.ID.String(),
		Name:    w.displayName(),
		Type:    w.entity.Name,
		Manager: &docProxy{s: w.s, ctxSrc: w.ctxSrc, entity: w.entity},
	}
}

func (w *docWriter) displayName() string {
	for _, k := range []string{"номер", "number"} {
		if v, ok := w.obj.Fields[k]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				return s
			}
		}
	}
	id := w.obj.ID.String()
	if len(id) >= 8 {
		return w.entity.Name + ":" + id[:8]
	}
	return w.entity.Name
}

// tpProxy — табличная часть документа (Док.Товары).
type tpProxy struct {
	w      *docWriter
	tpName string
}

func (t *tpProxy) Get(_ string) any    { return nil }
func (t *tpProxy) Set(_ string, _ any) {}

func (t *tpProxy) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "добавить", "add":
		row := map[string]any{}
		t.w.obj.TablePartRows[t.tpName] = append(t.w.obj.TablePartRows[t.tpName], row)
		return &interpreter.MapThis{M: row}
	case "очистить", "clear":
		t.w.obj.TablePartRows[t.tpName] = nil
	}
	return nil
}
