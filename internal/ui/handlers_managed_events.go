package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

// ── Рантайм событий управляемых форм (план 37, этап 8) ───────────────────
//
// POST /ui/{kind}/{entity}/form-event
//
// Form-data:
//   _id       — uuid документа (пусто = новый)
//   _element  — имя элемента формы (пусто = form-level event)
//   _event    — "Нажатие" | "ПриИзменении" | "ПриОткрытии" | ...
//   _form     — имя формы (опционально; берётся первая managed object form)
//   <field>   — текущие значения формы (как при сохранении)
//   tp.X.Y.Z  — значения табличных частей
//
// Response (JSON):
//   { ok: true, values: {field: val}, tableparts: {tp: [rows]}, messages: [str], error: "" }
//
// Логика: строим *runtime.Object из form-data → находим *ast.ProcedureDecl
// в form.ProgramAST по имени из form.Handlers[event] или element.Handlers[event]
// → запускаем через s.interp.Run(proc, obj, vars) с buildDSLVarsWithMessages
// для сбора Сообщить() → сериализуем обратно obj.Fields / obj.TablePartRows.

// formEventResponse — структура ответа JSON.
type formEventResponse struct {
	OK         bool                       `json:"ok"`
	Values     map[string]any             `json:"values,omitempty"`
	TableParts map[string][]map[string]any `json:"tableparts,omitempty"`
	Messages   []string                   `json:"messages,omitempty"`
	Error      string                     `json:"error,omitempty"`
}

// handleManagedFormEvent — единая точка обработки событий managed-форм.
func (s *Server) handleManagedFormEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)

	if err := r.ParseForm(); err != nil {
		enc.Encode(formEventResponse{Error: "bad form: " + err.Error()})
		return
	}

	entityName := chi.URLParam(r, "entity")
	if entityName == "" {
		enc.Encode(formEventResponse{Error: "entity required"})
		return
	}
	entity := s.reg.GetEntity(entityName)
	if entity == nil {
		enc.Encode(formEventResponse{Error: "entity not found: " + entityName})
		return
	}

	formKind := strings.ToLower(strings.TrimSpace(r.FormValue("_kind")))
	if formKind == "" {
		formKind = "object"
	}
	form := pickManagedForm(entity, formKind)
	if form == nil {
		enc.Encode(formEventResponse{Error: "managed form not found for " + entityName})
		return
	}
	progAny := form.ProgramAST
	if progAny == nil {
		// .form.os не загружен или не распарсен — обработчики недоступны.
		enc.Encode(formEventResponse{OK: true})
		return
	}
	program, ok := progAny.(*ast.Program)
	if !ok || program == nil {
		enc.Encode(formEventResponse{Error: "form AST type mismatch"})
		return
	}

	elementName := strings.TrimSpace(r.FormValue("_element"))
	eventName := strings.TrimSpace(r.FormValue("_event"))
	if eventName == "" {
		enc.Encode(formEventResponse{Error: "_event required"})
		return
	}

	// Найти имя процедуры, которая привязана к событию.
	procName := resolveHandlerProc(form, elementName, eventName)
	if procName == "" {
		// Нет привязки — это не ошибка, просто событие декларативное.
		enc.Encode(formEventResponse{OK: true})
		return
	}

	// Найти AST процедуры.
	var decl *ast.ProcedureDecl
	for _, p := range program.Procedures {
		if strings.EqualFold(p.Name.Literal, procName) {
			decl = p
			break
		}
	}
	if decl == nil {
		enc.Encode(formEventResponse{OK: true, Messages: []string{
			"⚠ Процедура «" + procName + "» не найдена в .form.os",
		}})
		return
	}

	// Построить объект из текущих form-values.
	obj := buildObjectFromForm(r, entity)

	// Подмешать ссылки → *runtime.Ref, как при сохранении (нужно для
	// Объект.Покупатель.Наименование и проч.).
	s.enrichHeaderRefs(r.Context(), entity, obj)
	for _, tp := range entity.TableParts {
		if rows, ok := obj.TablePartRows[tp.Name]; ok {
			s.enrichTPRowsWithRefs(r.Context(), tp, rows)
		}
	}

	// Сборка vars c builtin Сообщить, копящим сообщения.
	mc := runtime.NewMovementsCollector(entity.Name, obj.ID)
	var msgs []string
	vars := s.buildDSLVarsWithMessages(r.Context(), mc, &msgs)

	// Выполнение процедуры. Ошибка DSL отдаётся в JSON, не как 500 —
	// клиент покажет красный баннер и не закроет форму.
	if runErr := s.interp.Run(decl, obj, vars); runErr != nil {
		enc.Encode(formEventResponse{
			OK:         false,
			Values:     serializeFields(obj.Fields),
			TableParts: serializeTablePartRows(obj.TablePartRows),
			Messages:   msgs,
			Error:      runErr.Error(),
		})
		return
	}

	enc.Encode(formEventResponse{
		OK:         true,
		Values:     serializeFields(obj.Fields),
		TableParts: serializeTablePartRows(obj.TablePartRows),
		Messages:   msgs,
	})
}

// serializeTablePartRows прогоняет каждое значение каждой строки через
// serializeValue. Без этого Ref-объекты после enrichTPRowsWithRefs
// сериализуются как {Name:…, UUID:…} → JS-applyTableParts получает объект
// без GetRefUUID-метода, не может сопоставить с option.value и select
// показывает «— выбрать —» вместо реального товара.
func serializeTablePartRows(tps map[string][]map[string]any) map[string][]map[string]any {
	if tps == nil {
		return nil
	}
	out := make(map[string][]map[string]any, len(tps))
	for tpName, rows := range tps {
		outRows := make([]map[string]any, len(rows))
		for i, row := range rows {
			outRow := make(map[string]any, len(row))
			for fk, fv := range row {
				outRow[fk] = serializeValue(fv)
			}
			outRows[i] = outRow
		}
		out[tpName] = outRows
	}
	return out
}

// resolveHandlerProc возвращает имя процедуры-обработчика по уровню события.
// Если elementName пуст — ищет form.Handlers[event] (form-level).
// Иначе — element.Handlers[event] у указанного элемента дерева.
func resolveHandlerProc(form *metadata.FormModule, elementName, eventName string) string {
	evt := metadata.FormEventType(eventName)
	if elementName == "" {
		if form.Handlers != nil {
			if proc, ok := form.Handlers[evt]; ok {
				return proc
			}
		}
		return ""
	}
	el := form.GetElementByName(elementName)
	if el == nil || el.Handlers == nil {
		return ""
	}
	return el.Handlers[evt]
}

// buildObjectFromForm восстанавливает *runtime.Object из POST-формы.
// Использует те же helper'ы что и сохранение документа (formToFields,
// parseTablePartRows), чтобы поведение было идентично.
func buildObjectFromForm(r *http.Request, entity *metadata.Entity) *runtime.Object {
	fields := formToFields(r, entity)
	tpRows := parseTablePartRows(r, entity)
	idStr := strings.TrimSpace(r.FormValue("_id"))
	var id uuid.UUID
	if idStr != "" {
		if parsed, err := uuid.Parse(idStr); err == nil {
			id = parsed
		}
	}
	if id == uuid.Nil {
		id = uuid.New()
	}
	return &runtime.Object{
		Type:          entity.Name,
		Kind:          entity.Kind,
		ID:            id,
		Fields:        fields,
		TablePartRows: tpRows,
	}
}

// serializeFields подготавливает Fields к JSON-ответу. UUID и refs
// превращаем в строку, time.Time — в RFC3339, остальное оставляем как есть.
// Это нужно, чтобы клиент мог напрямую подставить значение в input.
func serializeFields(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = serializeValue(v)
	}
	return out
}

func serializeValue(v any) any {
	if v == nil {
		return ""
	}
	type refLike interface{ GetRefUUID() string }
	if r, ok := v.(refLike); ok {
		return r.GetRefUUID()
	}
	switch t := v.(type) {
	case uuid.UUID:
		return t.String()
	case fmt.Stringer:
		return t.String()
	}
	return v
}
