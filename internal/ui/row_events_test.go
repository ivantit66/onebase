package ui

// Направление 3 (Фаза B): события строк табличной части
// ПриДобавленииСтроки/ПриУдаленииСтроки. Бэкенд диспетчеризует их тем же
// generic-маршрутом, что и ПриИзменении/Нажатие; фронтенд (SlickGrid) дёргает их
// после добавления/удаления строки только при объявленном обработчике
// (data-sg-rowadd/data-sg-rowdel).

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// Диспетчер исполняет обработчик ПриДобавленииСтроки, объявленный на элементе ТЧ.
func TestHandleManagedFormEvent_RowAddedFires(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ТоварыПриДобавленииСтроки()
	Сообщить("строка добавлена");
КонецПроцедуры
`, nil,
		[]*metadata.FormElement{
			{
				Kind:     metadata.FormElementTablePart,
				Name:     "Товары",
				DataPath: "Объект.Товары",
				Handlers: map[metadata.FormEventType]string{
					metadata.FormEventOnRowAdded: "ТоварыПриДобавленииСтроки",
				},
			},
		})

	body := url.Values{}
	body.Set("_element", "Товары")
	body.Set("_event", string(metadata.FormEventOnRowAdded))
	body.Set("_kind", "object")

	rec := executeFormEvent(t, srv, ent, body)
	resp := decodeFormEventResponse(t, rec.Body.Bytes())
	if !resp.OK {
		t.Fatalf("ожидался ok=true, error=%q", resp.Error)
	}
	if len(resp.Messages) != 1 || !strings.Contains(resp.Messages[0], "добавлена") {
		t.Errorf("messages=%v, ждали сообщение о добавлении строки", resp.Messages)
	}
}

// ПриИзменении табличной части получает контекст изменённой ячейки: имя ТЧ,
// номер строки, колонку и текущую строку как DSL-объект (#205).
func TestHandleManagedFormEvent_TablePartChangeContext(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ТоварыПриИзменении()
	Сообщить(ИмяТабличнойЧасти);
	Сообщить(ТекущаяКолонка);
	Сообщить(НомерСтроки);
	Сообщить(ТекущаяСтрока.Цена);
КонецПроцедуры
`, nil,
		[]*metadata.FormElement{
			{
				Kind:     metadata.FormElementTablePart,
				Name:     "ЭлементТовары",
				DataPath: "Объект.Товары",
				Handlers: map[metadata.FormEventType]string{
					metadata.FormEventOnChange: "ТоварыПриИзменении",
				},
			},
		})
	ent.TableParts = []metadata.TablePart{{
		Name: "Товары",
		Fields: []metadata.Field{
			{Name: "Количество", Type: metadata.FieldTypeNumber},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
		},
	}}

	body := url.Values{}
	body.Set("_element", "ЭлементТовары")
	body.Set("_event", string(metadata.FormEventOnChange))
	body.Set("_kind", "object")
	body.Set("_tp", "Товары")
	body.Set("_tp_row", "1")
	body.Set("_tp_row_number", "2")
	body.Set("_tp_col", "Цена")
	body.Set("_tp_col_index", "1")
	body.Set("tp_json.Товары", `[{"Количество":1,"Цена":10},{"Количество":2,"Цена":20}]`)

	rec := executeFormEvent(t, srv, ent, body)
	resp := decodeFormEventResponse(t, rec.Body.Bytes())
	if !resp.OK {
		t.Fatalf("ожидался ok=true, error=%q", resp.Error)
	}
	want := []string{"Товары", "Цена", "2", "20"}
	if len(resp.Messages) != len(want) {
		t.Fatalf("messages=%v, ожидалось %v", resp.Messages, want)
	}
	for i := range want {
		if resp.Messages[i] != want[i] {
			t.Errorf("messages[%d]=%q, ожидалось %q (все messages=%v)", i, resp.Messages[i], want[i], resp.Messages)
		}
	}
}

// При объявленных ПриДобавленииСтроки/ПриУдаленииСтроки рендер грида проставляет
// флаги data-sg-rowadd/data-sg-rowdel — без них фронтенд не дёргает событие.
func TestManagedFormGridRowEventAttrs(t *testing.T) {
	form := &metadata.FormModule{
		Name:       "ФормаОбъекта",
		Kind:       "object",
		EntityName: "Заказ",
		LayoutKind: metadata.FormLayoutManaged,
		Title:      map[string]string{"ru": "Заказ"},
		Elements: []*metadata.FormElement{
			{
				Kind:     metadata.FormElementTablePart,
				Name:     "ЭлементТовары",
				TitleMap: map[string]string{"ru": "Товары"},
				DataPath: "Объект.Товары",
				Handlers: map[metadata.FormEventType]string{
					metadata.FormEventOnRowAdded:   "ТоварыПриДобавленииСтроки",
					metadata.FormEventOnRowDeleted: "ТоварыПриУдаленииСтроки",
				},
			},
		},
	}
	ent := &metadata.Entity{
		Name: "Заказ",
		Kind: metadata.KindDocument,
		TableParts: []metadata.TablePart{{
			Name:   "Товары",
			Fields: []metadata.Field{{Name: "Количество", Type: "number"}},
		}},
		Forms: []*metadata.FormModule{form},
	}
	data := map[string]any{
		"Entity":        ent,
		"Form":          form,
		"IsNew":         true,
		"Values":        map[string]string{},
		"RefOptions":    map[string]any{},
		"EnumOptions":   map[string]any{},
		"ChoiceOptions": map[string]any{},
		"TPRefOptions":  map[string]any{},
		"TPEnumLabels":  map[string]map[string]map[string]string{},
		"TPEnumOrder":   map[string]map[string][]string{},
		"TPRefMeta":     map[string]any{},
		"TablePartRows": map[string][]map[string]any{"Товары": {}},
		"User":          nil,
		"Lang":          "ru",
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-sg-rowadd="1"`) {
		t.Error("нет data-sg-rowadd при объявленном ПриДобавленииСтроки")
	}
	if !strings.Contains(html, `data-sg-rowdel="1"`) {
		t.Error("нет data-sg-rowdel при объявленном ПриУдаленииСтроки")
	}
	if strings.Contains(html, "gridCellEventParams") {
		t.Error("runtime грида должен жить в /static/managed.js, а не в HTML")
	}
	js := string(managedJS)
	if !strings.Contains(js, "gridCellEventParams") || !strings.Contains(js, "_tp_col") || !strings.Contains(js, "_tp_row_number") {
		t.Error("/static/managed.js не содержит передачу контекста изменённой ячейки")
	}
}

// AutoSum на элементе ТЧ → грид получает data-sg-autosum; без флага — нет.
// Иначе обычная ТЧ с колонками Цена/Количество/Сумма связывалась бы сама (#215.1).
func TestManagedFormGridAutoSumAttr(t *testing.T) {
	render := func(autoSum bool) string {
		form := &metadata.FormModule{
			Name:       "ФормаОбъекта",
			Kind:       "object",
			EntityName: "Заказ",
			LayoutKind: metadata.FormLayoutManaged,
			Title:      map[string]string{"ru": "Заказ"},
			Elements: []*metadata.FormElement{{
				Kind:     metadata.FormElementTablePart,
				Name:     "ЭлементТовары",
				TitleMap: map[string]string{"ru": "Товары"},
				DataPath: "Объект.Товары",
				AutoSum:  autoSum,
			}},
		}
		ent := &metadata.Entity{
			Name: "Заказ",
			Kind: metadata.KindDocument,
			TableParts: []metadata.TablePart{{
				Name: "Товары",
				Fields: []metadata.Field{
					{Name: "Количество", Type: "number"},
					{Name: "Цена", Type: "number"},
					{Name: "Сумма", Type: "number"},
				},
			}},
			Forms: []*metadata.FormModule{form},
		}
		data := map[string]any{
			"Entity":        ent,
			"Form":          form,
			"IsNew":         true,
			"Values":        map[string]string{},
			"RefOptions":    map[string]any{},
			"EnumOptions":   map[string]any{},
			"ChoiceOptions": map[string]any{},
			"TPRefOptions":  map[string]any{},
			"TPEnumLabels":  map[string]map[string]map[string]string{},
			"TPEnumOrder":   map[string]map[string][]string{},
			"TPRefMeta":     map[string]any{},
			"TablePartRows": map[string][]map[string]any{"Товары": {}},
			"User":          nil,
			"Lang":          "ru",
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
			t.Fatalf("ExecuteTemplate: %v", err)
		}
		return buf.String()
	}

	if html := render(true); !strings.Contains(html, `data-sg-autosum="1"`) {
		t.Error("нет data-sg-autosum при auto_sum: true")
	}
	if html := render(false); strings.Contains(html, `data-sg-autosum="1"`) {
		t.Error("data-sg-autosum появился без auto_sum")
	}
}
