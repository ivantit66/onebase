package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

func newRegistryForConstantsTest(t *testing.T) *runtime.Registry {
	t.Helper()
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{
		Enums: []*metadata.Enum{
			{Name: "ТипыКооперативов", Values: []string{"СТ", "ГК"}},
		},
		Constants: []*metadata.Constant{
			{Name: "ТипКооператива", Type: "enum:ТипыКооперативов", EnumName: "ТипыКооперативов", Required: true, Label: "Тип кооператива"},
			{Name: "Комментарий", Type: metadata.FieldTypeString, Label: "Комментарий"},
			{Name: "Организация", Type: metadata.FieldTypeString, Required: true, Label: "Организация"},
		},
	})
	return reg
}

func TestValidateConstant(t *testing.T) {
	s := &Server{reg: newRegistryForConstantsTest(t)}
	byName := map[string]*metadata.Constant{}
	for _, c := range s.reg.Constants() {
		byName[c.Name] = c
	}

	cases := []struct {
		name    string
		c       *metadata.Constant
		val     string
		wantErr bool
	}{
		{"enum: пусто и required → ошибка", byName["ТипКооператива"], "", true},
		{"enum: значение вне домена → ошибка", byName["ТипКооператива"], "МУСОР", true},
		{"enum: валидное значение → ок", byName["ТипКооператива"], "СТ", false},
		{"string required: пусто → ошибка", byName["Организация"], "", true},
		{"string required: заполнено → ок", byName["Организация"], "СТ ГК", false},
		{"string необязательная: пусто → ок", byName["Комментарий"], "", false},
		{"enum: описание не загружено → ошибка", &metadata.Constant{Name: "НДС", EnumName: "Нет"}, "20", true},
		{"reference: описание не загружено → ошибка", &metadata.Constant{Name: "Орг", RefEntity: "Нет"}, uuid.NewString(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := s.validateConstant(context.Background(), tc.c, "ru", tc.val)
			if (got != "") != tc.wantErr {
				t.Fatalf("validateConstant(%q) = %q, wantErr=%v", tc.val, got, tc.wantErr)
			}
		})
	}
}

// TestConstantsPageRendersEnumSelect проверяет, что enum-константа рендерится
// как <select> с опциями перечисления (issue #320, симптом 2), а не как
// текстовое поле, и что выбранное значение отмечено selected.
func TestConstantsPageRendersEnumSelect(t *testing.T) {
	consts := []*metadata.Constant{
		{Name: "ТипКооператива", Type: "enum:ТипыКооперативов", EnumName: "ТипыКооперативов", Required: true, Label: "Тип кооператива"},
	}
	data := map[string]any{
		"Constants": consts,
		"Values":    map[string]string{"ТипКооператива": "СТ"},
		"RefOpts":   map[string][]map[string]any{},
		"EnumOpts": map[string][]EnumOption{
			"ТипКооператива": {{Value: "СТ", Label: "Садовое товарищество"}, {Value: "ГК", Label: "Гаражный кооператив"}},
		},
		"Saved": false,
		"Error": "",
		"Lang":  "ru",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-constants", data); err != nil {
		t.Fatalf("ExecuteTemplate page-constants: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `<select name="ТипКооператива" required>`) {
		t.Error("enum-константа не отрендерилась как <select required>")
	}
	if strings.Contains(html, `<input type="text" name="ТипКооператива"`) {
		t.Error("enum-константа всё ещё рендерится как текстовое поле")
	}
	for _, want := range []string{
		`<option value="СТ" selected>Садовое товарищество</option>`,
		`<option value="ГК" >Гаражный кооператив</option>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML не содержит опцию %q", want)
		}
	}
}

// TestConstantsPageRendersErrorBanner проверяет, что сообщение об ошибке
// валидации показывается баннером на форме констант.
func TestConstantsPageRendersErrorBanner(t *testing.T) {
	data := map[string]any{
		"Constants": []*metadata.Constant{},
		"Values":    map[string]string{},
		"RefOpts":   map[string][]map[string]any{},
		"EnumOpts":  map[string][]EnumOption{},
		"Saved":     false,
		"Error":     "Константа «Тип кооператива» обязательна для заполнения",
		"Lang":      "ru",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-constants", data); err != nil {
		t.Fatalf("ExecuteTemplate page-constants: %v", err)
	}
	if !strings.Contains(buf.String(), "Константа «Тип кооператива» обязательна для заполнения") {
		t.Error("баннер с ошибкой валидации не отрендерился")
	}
}
