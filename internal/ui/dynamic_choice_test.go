package ui

// Направление 3 (Фаза B): динамический список значений. Обработчик события
// НачалоВыбора элемента ПолеСписка формирует список кодом через билтин
// ДобавитьЗначениеСписка; пункты возвращаются клиенту в choiceList.

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// При объявленном НачалоВыбора у ПолеСписка рендер навешивает data-el и
// data-ob-list-choice — крючок для динамического списка значений.
func TestManagedFormChoiceListDynamicAttrs(t *testing.T) {
	form := &metadata.FormModule{
		Name:       "ФормаОбъекта",
		Kind:       "object",
		EntityName: "Сделка",
		LayoutKind: metadata.FormLayoutManaged,
		Title:      map[string]string{"ru": "Сделка"},
		Elements: []*metadata.FormElement{
			{
				Kind:     metadata.FormElementInputList,
				Name:     "ПолеВалюта",
				TitleMap: map[string]string{"ru": "Валюта"},
				DataPath: "Объект.Валюта",
				Handlers: map[metadata.FormEventType]string{
					metadata.FormEventStartChoice: "ВалютаНачалоВыбора",
				},
			},
		},
	}
	ent := &metadata.Entity{
		Name:   "Сделка",
		Kind:   metadata.KindDocument,
		Fields: []metadata.Field{{Name: "Валюта", Type: metadata.FieldTypeString}},
		Forms:  []*metadata.FormModule{form},
	}
	data := map[string]any{
		"Entity": ent, "Form": form, "IsNew": true,
		"Values":        map[string]string{},
		"RefOptions":    map[string]any{},
		"EnumOptions":   map[string]any{},
		"ChoiceOptions": loadChoiceOptions(form, "ru"),
		"TPRefOptions":  map[string]any{},
		"TPEnumLabels":  map[string]map[string]map[string]string{},
		"TPEnumOrder":   map[string]map[string][]string{},
		"TPRefMeta":     map[string]any{},
		"TablePartRows": map[string][]map[string]any{},
		"User":          nil, "Lang": "ru",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-el="ПолеВалюта"`) {
		t.Error("нет data-el у ПолеСписка с НачалоВыбора")
	}
	if !strings.Contains(html, `data-ob-list-choice="ПолеВалюта"`) {
		t.Error("нет data-ob-list-choice у ПолеСписка с НачалоВыбора")
	}
}

func TestHandleManagedFormEvent_StartListChoiceBuildsList(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ВалютаНачалоВыбора()
	ДобавитьЗначениеСписка("USD", "Доллар");
	ДобавитьЗначениеСписка("EUR", "Евро");
	ДобавитьЗначениеСписка("RUB");
КонецПроцедуры
`, nil,
		[]*metadata.FormElement{
			{
				Kind:     metadata.FormElementInputList,
				Name:     "ПолеВалюта",
				DataPath: "Объект.Наименование",
				Handlers: map[metadata.FormEventType]string{
					metadata.FormEventStartChoice: "ВалютаНачалоВыбора",
				},
			},
		})

	body := url.Values{}
	body.Set("_element", "ПолеВалюта")
	body.Set("_event", string(metadata.FormEventStartChoice))
	body.Set("_kind", "object")

	rec := executeFormEvent(t, srv, ent, body)
	resp := decodeFormEventResponse(t, rec.Body.Bytes())
	if !resp.OK {
		t.Fatalf("ожидался ok=true, error=%q", resp.Error)
	}
	if len(resp.ChoiceList) != 3 {
		t.Fatalf("choiceList=%d, ждали 3: %+v", len(resp.ChoiceList), resp.ChoiceList)
	}
	if resp.ChoiceList[0].Value != "USD" || resp.ChoiceList[0].Label != "Доллар" {
		t.Errorf("item0 = %+v, ждали {USD Доллар}", resp.ChoiceList[0])
	}
	if resp.ChoiceList[2].Value != "RUB" || resp.ChoiceList[2].Label != "RUB" {
		t.Errorf("item2 = %+v (представление по умолчанию = значение)", resp.ChoiceList[2])
	}
}
