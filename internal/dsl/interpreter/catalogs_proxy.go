package interpreter

import (
	"context"
	"fmt"
	"strings"

	"github.com/ivantit66/onebase/internal/metadata"
)

// CatalogsDB extends PredefinedDB with field-based lookups and writes.
// Returns ("", "", false, nil) on not-found so the DSL can compare against nil.
type CatalogsDB interface {
	PredefinedDB
	FindCatalogByField(ctx context.Context, entity *metadata.Entity, fieldName, value string) (idStr, display string, ok bool, err error)
	// WriteCatalogRecord upserts a record (замечание #25). idStr пустой →
	// генерируется новый UUID. Возвращает UUID записанной записи.
	WriteCatalogRecord(ctx context.Context, entity *metadata.Entity, idStr string, fields map[string]any) (string, error)
}

// EntityLookup resolves an entity name (case-insensitive) to its metadata.
type EntityLookup interface {
	GetEntity(name string) *metadata.Entity
}

// ctxSource предоставляет «живой» контекст. Для обычного запуска это
// статический контекст; при активной DSL-транзакции — *TxState, чей
// Ctx() несёт открытую транзакцию (замечание #25: запись справочника
// из обработки должна участвовать в транзакции).
type ctxSource interface {
	Ctx() context.Context
}

// staticCtx — ctxSource c фиксированным контекстом (нет транзакции).
type staticCtx struct{ ctx context.Context }

func (s staticCtx) Ctx() context.Context { return s.ctx }

// NewStaticCtx wraps a plain context as a ctxSource.
func NewStaticCtx(ctx context.Context) ctxSource { return staticCtx{ctx: ctx} }

// CatalogsRoot is the DSL global Справочники / Catalogs.
type CatalogsRoot struct {
	db     CatalogsDB
	lookup EntityLookup
	ctxSrc ctxSource
}

// NewCatalogsRoot creates the root object for injection as DSL extraVar.
// ctxSrc — источник живого контекста (staticCtx или *TxState).
func NewCatalogsRoot(ctxSrc ctxSource, db CatalogsDB, lookup EntityLookup) *CatalogsRoot {
	return &CatalogsRoot{db: db, lookup: lookup, ctxSrc: ctxSrc}
}

func (r *CatalogsRoot) Get(entityName string) any {
	entity := r.lookup.GetEntity(entityName)
	if entity == nil {
		return nil
	}
	return &CatalogProxy{entity: entity, db: r.db, ctxSrc: r.ctxSrc}
}

func (r *CatalogsRoot) Set(_ string, _ any) {}

// CatalogProxy resolves predefined items, runtime lookups, and record creation.
//
//	Справочники.ТипЦен.Закупочная                  → *Ref to predefined item
//	Справочники.ТипЦен.НайтиПоНаименованию("X")     → *Ref or nil
//	Справочники.Контрагент.Создать()                → *CatalogRecordWriter
type CatalogProxy struct {
	entity *metadata.Entity
	db     CatalogsDB
	ctxSrc ctxSource
}

func (p *CatalogProxy) ctx() context.Context {
	if p.ctxSrc != nil {
		return p.ctxSrc.Ctx()
	}
	return context.Background()
}

// Get is called for foo.Bar attribute access — predefined item lookup.
func (p *CatalogProxy) Get(itemName string) any {
	for _, item := range p.entity.Predefined {
		if strings.EqualFold(item.Name, itemName) {
			id, err := p.db.GetPredefinedIDStr(p.ctx(), p.entity.Name, item.Name)
			if err != nil || id == "" {
				return nil
			}
			return &Ref{UUID: id, Name: item.Name}
		}
	}
	return nil
}

func (p *CatalogProxy) Set(_ string, _ any) {}

// CallMethod implements MethodCallable for method-style invocation.
func (p *CatalogProxy) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "найтипонаименованию", "findbyname":
		return p.findByField("Наименование", args)
	case "найтипокоду", "findbycode":
		return p.findByField("Код", args)
	case "создать", "create":
		return &CatalogRecordWriter{
			entity: p.entity,
			db:     p.db,
			ctxSrc: p.ctxSrc,
			fields: map[string]any{},
		}
	}
	return nil
}

func (p *CatalogProxy) findByField(field string, args []any) any {
	if len(args) == 0 {
		return nil
	}
	value, ok := args[0].(string)
	if !ok {
		if r, ok := args[0].(*Ref); ok {
			value = r.Name
		} else {
			return nil
		}
	}
	idStr, display, found, err := p.db.FindCatalogByField(p.ctx(), p.entity, field, value)
	if err != nil || !found {
		return nil
	}
	return &Ref{UUID: idStr, Name: display}
}

// CatalogRecordWriter — записываемый объект справочника/документа,
// созданный через Справочники.X.Создать() (замечание #25).
//
//	Зап = Справочники.Контрагент.Создать();
//	Зап.Наименование = "ООО Ромашка";
//	Зап.ИНН = "7701234567";
//	Ссыл = Зап.Записать();   // → *Ref на записанную запись
type CatalogRecordWriter struct {
	entity *metadata.Entity
	db     CatalogsDB
	ctxSrc ctxSource
	idStr  string
	fields map[string]any
}

func (w *CatalogRecordWriter) ctx() context.Context {
	if w.ctxSrc != nil {
		return w.ctxSrc.Ctx()
	}
	return context.Background()
}

// Get — чтение установленного значения поля (case-insensitive).
func (w *CatalogRecordWriter) Get(name string) any {
	low := strings.ToLower(name)
	for k, v := range w.fields {
		if strings.ToLower(k) == low {
			return v
		}
	}
	return nil
}

// Set — установка значения поля (Зап.Поле = значение).
func (w *CatalogRecordWriter) Set(name string, v any) {
	w.fields[strings.ToLower(name)] = v
}

// CallMethod — Записать() / УстановитьЗначение().
func (w *CatalogRecordWriter) CallMethod(method string, args []any) any {
	switch strings.ToLower(method) {
	case "записать", "write":
		id, err := w.db.WriteCatalogRecord(w.ctx(), w.entity, w.idStr, w.fields)
		if err != nil {
			panic(userError{Msg: "Записать(" + w.entity.Name + "): " + err.Error()})
		}
		w.idStr = id
		name := ""
		if v := w.Get("Наименование"); v != nil {
			name = fmt.Sprintf("%v", v)
		}
		return &Ref{UUID: id, Name: name}
	case "установитьзначение", "setvalue":
		if len(args) >= 2 {
			if n, ok := args[0].(string); ok {
				w.Set(n, args[1])
			}
		}
	}
	return nil
}
