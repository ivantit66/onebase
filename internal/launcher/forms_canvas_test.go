package launcher

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/formdoc"
)

const canvasSample = `schema: onebase.form/v1
form:
  name: ФормаОбъекта
  kind: object
  entity: Звонок
elements:
  - kind: ГруппаФормы
    name: Группа1
    children:
      - kind: ПолеВвода
        name: ПолеНомер
        data_path: Объект.Номер
        required: true
      - kind: ПолеВвода
        name: ПолеДата
        data_path: Объект.Дата
`

func loadCanvasDoc(t *testing.T) *formdoc.Doc {
	t.Helper()
	doc, err := formdoc.Load([]byte(canvasSample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return doc
}

// Холст помечает каждый элемент его node-id — это адресация, по которой клиент
// наводит выбор и правки на конкретный узел дерева (#164).
func TestRenderFormCanvas_NodeIDs(t *testing.T) {
	html, err := renderFormCanvas(loadCanvasDoc(t), "")
	if err != nil {
		t.Fatalf("renderFormCanvas: %v", err)
	}
	for _, want := range []string{
		`data-node-id="elements.0"`,
		`data-node-id="elements.0.children.0"`,
		`data-node-id="elements.0.children.1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в холсте нет %s:\n%s", want, html)
		}
	}
}

// В контейнере на N детей должно быть N+1 drop-зон с правильными parent/index —
// иначе реквизит из палитры некуда уронить между полями.
func TestRenderFormCanvas_DropZones(t *testing.T) {
	out, err := renderFormCanvas(loadCanvasDoc(t), "")
	if err != nil {
		t.Fatalf("renderFormCanvas: %v", err)
	}
	// Верхний уровень: 1 элемент → 2 зоны (index 0 и 1).
	for _, want := range []string{
		`data-parent="" data-index="0"`,
		`data-parent="" data-index="1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("нет верхней drop-зоны %s", want)
		}
	}
	// Внутри группы: 2 ребёнка → 3 зоны (0,1,2).
	for _, want := range []string{
		`data-parent="elements.0" data-index="0"`,
		`data-parent="elements.0" data-index="1"`,
		`data-parent="elements.0" data-index="2"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("нет drop-зоны группы %s", want)
		}
	}
}

// Выбранный элемент получает класс fc-selected; невыбранный — нет.
func TestRenderFormCanvas_Selection(t *testing.T) {
	out, err := renderFormCanvas(loadCanvasDoc(t), "elements.0.children.1")
	if err != nil {
		t.Fatalf("renderFormCanvas: %v", err)
	}
	if !strings.Contains(out, `data-selected="elements.0.children.1"`) {
		t.Errorf("не проставлен data-selected:\n%s", out)
	}
	// Должна быть ровно одна обёртка с fc-selected, и это поле даты.
	if n := strings.Count(out, "fc-selected"); n != 1 {
		t.Errorf("ожидалась 1 пометка fc-selected, найдено %d:\n%s", n, out)
	}
	// Грубая проверка, что выбран именно children.1: класс рядом с его node-id.
	i := strings.Index(out, `data-node-id="elements.0.children.1"`)
	if i < 0 {
		t.Fatalf("нет узла children.1")
	}
	// fc-selected должен идти в той же открывающей теге (до '>').
	tagStart := strings.LastIndex(out[:i], "<")
	tagEnd := strings.Index(out[tagStart:], ">") + tagStart
	if !strings.Contains(out[tagStart:tagEnd], "fc-selected") {
		t.Errorf("fc-selected не на узле children.1:\n%s", out[tagStart:tagEnd])
	}
}

// Страница редактора должна разворачивать клиент конструктора: вкладку
// «Конструктор», холст, панель свойств и проводку к /forms/edit-op (#164).
func TestFormsEditor_RendersDesignerScaffold(t *testing.T) {
	data := &configuratorData{
		Base: &Base{ID: "test-base"},
		EditingForm: &cfgManagedForm{
			Entity: "Контрагент",
			Name:   "ФормаОбъекта",
			Kind:   "object",
			YAML:   "schema: onebase.form/v1\nform:\n  name: ФормаОбъекта\n  kind: object\n  entity: Контрагент\n",
		},
	}
	var buf bytes.Buffer
	if err := formsTmpl.ExecuteTemplate(&buf, "forms-editor", data); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	page := buf.String()
	for _, want := range []string{
		`id="canvas-host"`,
		`id="prop-panel"`,
		`data-rp="design"`,
		`switchRightPane(`,
		`/configurator/forms/edit-op`,
		`function reloadCanvas`,
		`op: 'insert'`,
		`var _entity = "Контрагент"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("в странице редактора нет %q", want)
		}
	}
}

// Поле с required:true несёт визуальную звёздочку; data_path определяет
// плейсхолдер последнего сегмента.
func TestRenderFormCanvas_FieldDetails(t *testing.T) {
	out, err := renderFormCanvas(loadCanvasDoc(t), "")
	if err != nil {
		t.Fatalf("renderFormCanvas: %v", err)
	}
	if !strings.Contains(out, "fc-req") {
		t.Errorf("нет отметки обязательности у поля required:true")
	}
	if !strings.Contains(out, `placeholder="Номер"`) {
		t.Errorf("плейсхолдер не из последнего сегмента data_path:\n%s", out)
	}
}
