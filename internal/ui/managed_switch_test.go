package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// renderManagedSwitch — хелпер: рендерит одну управляемую форму с заданным
// элементом-переключателем поверх сущности и текущих значений.
func renderManagedSwitch(t *testing.T, el *metadata.FormElement, ent *metadata.Entity, values map[string]string, enumOpts map[string][]EnumOption) string {
	t.Helper()
	form := &metadata.FormModule{
		Name: "ФормаОбъекта", Kind: "object", EntityName: ent.Name,
		LayoutKind: metadata.FormLayoutManaged,
		Title:      map[string]string{"ru": ent.Name},
		Elements:   []*metadata.FormElement{el},
	}
	ent.Forms = []*metadata.FormModule{form}
	data := map[string]any{
		"Entity": ent, "Form": form, "IsNew": true,
		"Values": values, "RefOptions": map[string]any{},
		"EnumOptions": enumOpts, "TPRefOptions": map[string]any{},
		"User": nil, "Lang": "ru",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	return buf.String()
}

// Переключатель с произвольным (числовым) набором значений рендерится радио-
// кнопками name=поле; текущее значение отмечено checked (план 71b C1).
func TestManagedSwitch_RadioCustomOptions(t *testing.T) {
	el := &metadata.FormElement{
		Kind: metadata.FormElementSwitch, Name: "Приоритет",
		DataPath: "Объект.Приоритет",
		Options: []metadata.FormOption{
			{Value: 1, Labels: map[string]string{"ru": "Низкий"}},
			{Value: 2, Labels: map[string]string{"ru": "Высокий"}},
		},
	}
	ent := &metadata.Entity{Name: "Заказ", Kind: metadata.KindDocument, Fields: []metadata.Field{
		{Name: "Приоритет", Type: metadata.FieldTypeNumber},
	}}
	html := renderManagedSwitch(t, el, ent, map[string]string{"Приоритет": "2"}, nil)
	for _, want := range []string{`type="radio"`, `name="Приоритет"`, `value="1"`, `value="2"`, "Низкий", "Высокий"} {
		if !strings.Contains(html, want) {
			t.Errorf("в HTML нет %q:\n%s", want, html)
		}
	}
	if !strings.Contains(html, `value="2" checked`) {
		t.Errorf("текущее значение 2 не отмечено checked:\n%s", html)
	}
}

// view: select рисует Переключатель выпадающим списком (C2).
func TestManagedSwitch_SelectView(t *testing.T) {
	el := &metadata.FormElement{
		Kind: metadata.FormElementSwitch, Name: "Приоритет",
		DataPath: "Объект.Приоритет", View: "select",
		Options: []metadata.FormOption{
			{Value: 1, Labels: map[string]string{"ru": "Низкий"}},
			{Value: 2, Labels: map[string]string{"ru": "Высокий"}},
		},
	}
	ent := &metadata.Entity{Name: "Заказ", Kind: metadata.KindDocument, Fields: []metadata.Field{
		{Name: "Приоритет", Type: metadata.FieldTypeNumber},
	}}
	html := renderManagedSwitch(t, el, ent, map[string]string{"Приоритет": "2"}, nil)
	if !strings.Contains(html, `<select name="Приоритет"`) {
		t.Errorf("view=select не дал <select>:\n%s", html)
	}
	if !strings.Contains(html, `value="2" selected`) {
		t.Errorf("текущее значение 2 не selected:\n%s", html)
	}
	if strings.Contains(html, `type="radio" name="Приоритет"`) {
		t.Errorf("view=select не должен рисовать radio для поля:\n%s", html)
	}
}

// Для enum-поля Переключатель берёт значения из перечисления автоматически
// (Options не заданы) — радио по EnumOptions (C1).
func TestManagedSwitch_EnumAutoOptions(t *testing.T) {
	el := &metadata.FormElement{
		Kind: metadata.FormElementSwitch, Name: "Статус",
		DataPath: "Объект.Статус",
	}
	ent := &metadata.Entity{Name: "Заказ", Kind: metadata.KindDocument, Fields: []metadata.Field{
		{Name: "Статус", Type: metadata.FieldType("enum:Статусы")},
	}}
	enumOpts := map[string][]EnumOption{"Статус": {
		{Value: "new", Label: "Новый"},
		{Value: "done", Label: "Готов"},
	}}
	html := renderManagedSwitch(t, el, ent, map[string]string{"Статус": "done"}, enumOpts)
	for _, want := range []string{`type="radio"`, `name="Статус"`, `value="new"`, `value="done"`, "Новый", "Готов"} {
		if !strings.Contains(html, want) {
			t.Errorf("в HTML нет %q:\n%s", want, html)
		}
	}
	if !strings.Contains(html, `value="done" checked`) {
		t.Errorf("текущий enum-статус не checked:\n%s", html)
	}
}
