package ui

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// choiceElement — элемент ПолеСписка с тремя пунктами: с полным переводом,
// только с ru-подписью (проверка отката en→ru) и без подписи (откат на value).
func choiceElement() *metadata.FormElement {
	return &metadata.FormElement{
		Kind:     metadata.FormElementInputList,
		Name:     "ПолеПриоритет",
		TitleMap: map[string]string{"ru": "Приоритет"},
		DataPath: "Объект.Приоритет",
		Choices: []metadata.FormChoice{
			{Value: "high", Title: map[string]string{"ru": "Высокий", "en": "High"}},
			{Value: "low", Title: map[string]string{"ru": "Низкий"}},
			{Value: "raw"},
		},
		Handlers: map[metadata.FormEventType]string{
			metadata.FormEventOnChange: "ПриоритетПриИзменении",
		},
	}
}

// loadChoiceOptions обходит дерево рекурсивно (Walk) и переводит подписи с
// откатом en→ru→value, сохраняя порядок объявления.
func TestLoadChoiceOptions(t *testing.T) {
	form := &metadata.FormModule{
		Elements: []*metadata.FormElement{
			{
				Kind:     metadata.FormElementGroupBox,
				Name:     "Группа",
				Children: []*metadata.FormElement{choiceElement()},
			},
		},
	}
	opts := loadChoiceOptions(form, "en")
	got := opts["ПолеПриоритет"]
	if len(got) != 3 {
		t.Fatalf("options = %d, want 3", len(got))
	}
	if got[0].Value != "high" || got[0].Label != "High" {
		t.Errorf("opt0 = %+v, want {high High}", got[0])
	}
	if got[1].Value != "low" || got[1].Label != "Низкий" { // нет en → откат на ru
		t.Errorf("opt1 = %+v, want {low Низкий}", got[1])
	}
	if got[2].Value != "raw" || got[2].Label != "raw" { // нет title → откат на value
		t.Errorf("opt2 = %+v, want {raw raw}", got[2])
	}
}

// При рендере managed-формы элемент ПолеСписка отрисовывается как <select> с
// опциями из ChoiceOptions, текущее значение помечается selected, а при наличии
// обработчика навешивается data-ob-fire-change для delegated runtime.
func TestManagedFormChoiceListRenders(t *testing.T) {
	form := &metadata.FormModule{
		Name:       "ФормаОбъекта",
		Kind:       "object",
		EntityName: "Задача",
		LayoutKind: metadata.FormLayoutManaged,
		Title:      map[string]string{"ru": "Задача"},
		Elements:   []*metadata.FormElement{choiceElement()},
	}
	ent := &metadata.Entity{
		Name:   "Задача",
		Kind:   metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Приоритет", Type: metadata.FieldTypeString}},
		Forms:  []*metadata.FormModule{form},
	}
	data := map[string]any{
		"Entity":        ent,
		"Form":          form,
		"IsNew":         true,
		"Values":        map[string]string{"Приоритет": "high"},
		"RefOptions":    map[string]any{},
		"EnumOptions":   map[string]any{},
		"ChoiceOptions": loadChoiceOptions(form, "ru"),
		"TPRefOptions":  map[string]any{},
		"TPEnumLabels":  map[string]map[string]map[string]string{},
		"TPEnumOrder":   map[string]map[string][]string{},
		"TPRefMeta":     map[string]any{},
		"TablePartRows": map[string][]map[string]any{},
		"User":          nil,
		"Lang":          "ru",
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "<select") || !strings.Contains(html, `name="Приоритет"`) {
		t.Error("нет <select name=\"Приоритет\"> для ПолеСписка")
	}
	if !strings.Contains(html, `value="high"`) || !strings.Contains(html, "Высокий") {
		t.Error("опции списка значений не отрендерились")
	}
	if !strings.Contains(html, `value="high" selected`) {
		t.Error("текущее значение high не помечено selected")
	}
	if !strings.Contains(html, `data-ob-fire-change="ПолеПриоритет"`) {
		t.Error("нет data-ob-fire-change у поля со списком значений")
	}
}

// Выбор значения из списка дёргает ПриИзменении тем же путём, что enum/ссылка:
// обработчик читает выбранное значение, заполняет реквизит и возвращает его в
// values + копит сообщение.
func TestHandleManagedFormEvent_ChoiceListFiresOnChange(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ПриоритетПриИзменении()
	Если Объект.Наименование = "high" Тогда
		Сообщить("выбран high");
		Объект.Наименование = "Высокий приоритет";
	КонецЕсли;
КонецПроцедуры
`, nil,
		[]*metadata.FormElement{
			{
				Kind:     metadata.FormElementInputList,
				Name:     "ПолеНаименование",
				DataPath: "Объект.Наименование",
				Choices: []metadata.FormChoice{
					{Value: "high", Title: map[string]string{"ru": "Высокий"}},
					{Value: "low", Title: map[string]string{"ru": "Низкий"}},
				},
				Handlers: map[metadata.FormEventType]string{
					metadata.FormEventOnChange: "ПриоритетПриИзменении",
				},
			},
		})

	body := url.Values{}
	body.Set("_element", "ПолеНаименование")
	body.Set("_event", string(metadata.FormEventOnChange))
	body.Set("_kind", "object")
	body.Set("Наименование", "high")

	rec := executeFormEvent(t, srv, ent, body)
	resp := decodeFormEventResponse(t, rec.Body.Bytes())

	if !resp.OK {
		t.Fatalf("ожидался ok=true, error=%q", resp.Error)
	}
	if len(resp.Messages) != 1 || !strings.Contains(resp.Messages[0], "high") {
		t.Errorf("messages=%v, ждали сообщение с 'high'", resp.Messages)
	}
	if got, _ := resp.Values["Наименование"].(string); got != "Высокий приоритет" {
		t.Errorf("values[Наименование]=%v, ждали 'Высокий приоритет'", resp.Values["Наименование"])
	}
}
