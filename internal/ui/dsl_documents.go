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

// docsRoot — DSL-глобал Документы / Documents (замечание #26).
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

func (p *docProxy) Get(_ string) any  { return nil }
func (p *docProxy) Set(_ string, _ any) {}

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
	}
	return nil
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
		return &interpreter.Ref{UUID: w.obj.ID.String(), Name: w.displayName()}
	case "провести", "post":
		if err := w.write(); err != nil {
			interpreter.RaiseUserError("Провести/Записать(" + w.entity.Name + "): " + err.Error())
		}
		if err := w.post(); err != nil {
			interpreter.RaiseUserError("Провести(" + w.entity.Name + "): " + err.Error())
		}
		return &interpreter.Ref{UUID: w.obj.ID.String(), Name: w.displayName()}
	case "установитьзначение", "setvalue":
		if len(args) >= 2 {
			if n, ok := args[0].(string); ok {
				w.Set(n, args[1])
			}
		}
	}
	return nil
}

// write сохраняет шапку + табличные части. Использует живой ctx, поэтому
// при открытой DSL-транзакции запись участвует в ней; иначе автокоммит.
func (w *docWriter) write() error {
	ctx := w.ctx()
	if err := w.s.store.Upsert(ctx, w.entity.Name, w.obj.ID, w.obj.Fields, w.entity); err != nil {
		return err
	}
	return w.s.saveTablePartsDirect(ctx, w.entity, w.obj.ID, w.obj.TablePartRows)
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

func (t *tpProxy) Get(_ string) any  { return nil }
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
