package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// pickManagedForm должен возвращать nil, если у сущности только legacy формы
// или Forms вообще пуст.
func TestPickManagedForm_OnlyAutogen(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Контрагент",
		Forms: []*metadata.FormModule{
			{Name: "ФормаОбъекта", Kind: "object", LayoutKind: metadata.FormLayoutAutogen},
		},
	}
	if got := pickManagedForm(ent, "object"); got != nil {
		t.Errorf("ожидался nil для autogen-only, получили %+v", got)
	}
}

// Если есть managed-форма нужного Kind — она возвращается.
func TestPickManagedForm_MatchByKind(t *testing.T) {
	managedObj := &metadata.FormModule{Name: "ФормаОбъекта", Kind: "object", LayoutKind: metadata.FormLayoutManaged}
	managedList := &metadata.FormModule{Name: "ФормаСписка", Kind: "list", LayoutKind: metadata.FormLayoutManaged}
	ent := &metadata.Entity{
		Forms: []*metadata.FormModule{managedList, managedObj},
	}
	if got := pickManagedForm(ent, "object"); got != managedObj {
		t.Errorf("по 'object' должна быть managedObj, получили %+v", got)
	}
	if got := pickManagedForm(ent, "list"); got != managedList {
		t.Errorf("по 'list' должна быть managedList, получили %+v", got)
	}
	if got := pickManagedForm(ent, "choice"); got != nil {
		t.Errorf("по 'choice' должно быть nil, получили %+v", got)
	}
}

// Smoke-тест: page-managed-form реально компилируется и рендерит элементы
// дерева. Проверяем что в выводе есть заголовки группы, оба поля и
// маркер ◇ managed.
func TestPageManagedForm_Renders(t *testing.T) {
	form := &metadata.FormModule{
		Name:       "ФормаОбъекта",
		Kind:       "object",
		EntityName: "Контрагент",
		LayoutKind: metadata.FormLayoutManaged,
		Title:      map[string]string{"ru": "Контрагент"},
		Elements: []*metadata.FormElement{
			{
				Kind:     metadata.FormElementGroupBox,
				Name:     "Реквизиты",
				TitleMap: map[string]string{"ru": "Реквизиты"},
				Children: []*metadata.FormElement{
					{
						Kind:     metadata.FormElementField,
						Name:     "ПолеНаименование",
						TitleMap: map[string]string{"ru": "Наименование"},
						DataPath: "Объект.Наименование",
						Required: true,
					},
					{
						Kind:     metadata.FormElementCheckbox,
						Name:     "ПолеАктивен",
						TitleMap: map[string]string{"ru": "Активен"},
						DataPath: "Объект.Активен",
					},
				},
			},
		},
	}

	ent := &metadata.Entity{
		Name: "Контрагент",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Активен", Type: metadata.FieldTypeBool},
		},
		Forms: []*metadata.FormModule{form},
	}

	data := map[string]any{
		"Entity":       ent,
		"Form":         form,
		"IsNew":        true,
		"Values":       map[string]string{"Наименование": "", "Активен": "false"},
		"RefOptions":   map[string]any{},
		"EnumOptions":  map[string]any{},
		"TPRefOptions": map[string]any{},
		"User":         nil,
		"Lang":         "ru",
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	html := buf.String()

	expects := []string{
		"◇ managed",
		"Контрагент",   // DisplayName
		"Реквизиты",    // legend группы
		"Наименование", // label поля
		"Активен",      // label чекбокса
		"type=\"checkbox\"",
		"name=\"Наименование\"",
		"name=\"Активен\"",
		`id="ob-managed-config"`,
		`id="ob-managed-tp-ref-opts"`,
		`src="/static/managed.js"`,
	}
	for _, e := range expects {
		if !strings.Contains(html, e) {
			t.Errorf("в HTML не найдено %q", e)
		}
	}
	for _, old := range []string{
		"window._tpRefOpts =",
		"window.obFire = async function",
		"function addVtRow",
		"function gridCellEventParams",
	} {
		if strings.Contains(html, old) {
			t.Errorf("managed runtime должен жить в /static/managed.js, но HTML содержит %q", old)
		}
	}
	for _, old := range []string{
		"onclick=",
		"onchange=",
		"oninput=",
		"onfocus=",
		"onsubmit=",
	} {
		if strings.Contains(tplManagedForm, old) {
			t.Errorf("templates_managed.go не должен содержать inline handler %q", old)
		}
	}
}

// ГруппаФормы с orientation: horizontal раскладывает дочерние реквизиты в ряд
// в runtime managed-форме (#205).
func TestPageManagedForm_GroupHorizontalOrientation(t *testing.T) {
	form := &metadata.FormModule{
		Name: "ФормаОбъекта", Kind: "object", EntityName: "Контрагент",
		LayoutKind: metadata.FormLayoutManaged,
		Elements: []*metadata.FormElement{
			{
				Kind:        metadata.FormElementGroupBox,
				Name:        "Реквизиты",
				Orientation: "horizontal",
				TitleMap:    map[string]string{"ru": "Реквизиты"},
				Children: []*metadata.FormElement{
					{Kind: metadata.FormElementField, Name: "ПолеНаименование", DataPath: "Объект.Наименование"},
					{Kind: metadata.FormElementField, Name: "ПолеКод", DataPath: "Объект.Код"},
				},
			},
		},
	}
	ent := &metadata.Entity{
		Name: "Контрагент", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Код", Type: metadata.FieldTypeString},
		},
		Forms: []*metadata.FormModule{form},
	}
	data := map[string]any{
		"Entity": ent, "Form": form, "IsNew": true,
		"Values":     map[string]string{"Наименование": "", "Код": ""},
		"RefOptions": map[string]any{}, "EnumOptions": map[string]any{},
		"TPRefOptions": map[string]any{}, "User": nil, "Lang": "ru",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"managed-group-horizontal",
		"managed-group-body",
		"name=\"Наименование\"",
		"name=\"Код\"",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в HTML нет %q:\n%s", want, html)
		}
	}
}

// Отдельная Страница (вне набора СтраницыФормы) рендерится в рантайме как блок
// со своими детьми, а не «рендеринг не реализован» (#164, фикс после batch B/C).
func TestPageManagedForm_StandalonePage(t *testing.T) {
	form := &metadata.FormModule{
		Name: "ФормаОбъекта", Kind: "object", EntityName: "Заказ",
		LayoutKind: metadata.FormLayoutManaged,
		Title:      map[string]string{"ru": "Заказ"},
		Elements: []*metadata.FormElement{
			{
				Kind: metadata.FormElementPage, Name: "Товары",
				TitleMap: map[string]string{"ru": "Товары"},
				Children: []*metadata.FormElement{
					{Kind: metadata.FormElementField, Name: "ПолеКомментарий",
						TitleMap: map[string]string{"ru": "Комментарий"}, DataPath: "Объект.Комментарий"},
				},
			},
		},
	}
	ent := &metadata.Entity{
		Name: "Заказ", Kind: metadata.KindDocument,
		Fields: []metadata.Field{{Name: "Комментарий", Type: metadata.FieldTypeString}},
		Forms:  []*metadata.FormModule{form},
	}
	data := map[string]any{
		"Entity": ent, "Form": form, "IsNew": true,
		"Values": map[string]string{"Комментарий": ""}, "RefOptions": map[string]any{},
		"EnumOptions": map[string]any{}, "TPRefOptions": map[string]any{}, "User": nil, "Lang": "ru",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-managed-form", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, "рендеринг не реализован") {
		t.Errorf("отдельная страница даёт «рендеринг не реализован»:\n%s", html)
	}
	for _, s := range []string{"Товары", "Комментарий", `name="Комментарий"`} {
		if !strings.Contains(html, s) {
			t.Errorf("рантайм отдельной страницы не содержит %q", s)
		}
	}
}

// Тест что pickManagedForm с пустой строкой kind возвращает первую managed-форму
// (используется на путях где kind не известен).
func TestPickManagedForm_AnyKind(t *testing.T) {
	listForm := &metadata.FormModule{Kind: "list", LayoutKind: metadata.FormLayoutManaged}
	ent := &metadata.Entity{
		Forms: []*metadata.FormModule{listForm},
	}
	if got := pickManagedForm(ent, ""); got != listForm {
		t.Errorf("по '' должна быть любая managed, получили %+v", got)
	}
}
