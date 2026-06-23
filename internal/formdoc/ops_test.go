package formdoc

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
)

// SetProp меняет существующий скаляр in-place: значение обновляется, а
// inline-комментарий соседнего поля и порядок ключей не страдают — это требование
// неразрушающей правки свойств на холсте (#164).
func TestSetProp_ReplacesScalarKeepsComments(t *testing.T) {
	doc, err := Load([]byte(elemSample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := doc.SetProp("elements.0.children.1", "data_path", "Объект.Новое"); err != nil {
		t.Fatalf("SetProp: %v", err)
	}
	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "data_path: Объект.Новое") {
		t.Errorf("новое значение не записано:\n%s", got)
	}
	if strings.Contains(got, "Объект.Дата") {
		t.Errorf("старое значение осталось:\n%s", got)
	}
	if !strings.Contains(got, "# дата звонка") {
		t.Errorf("потерян inline-комментарий соседнего поля:\n%s", got)
	}
}

// SetProp умеет добавлять отсутствующий ключ (bool) и вложенный (title.ru).
func TestSetProp_AddsBoolAndNestedTitle(t *testing.T) {
	doc, err := Load([]byte(elemSample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := doc.SetProp("elements.0.children.0", "required", true); err != nil {
		t.Fatalf("SetProp required: %v", err)
	}
	if err := doc.SetProp("elements.0.children.0", "title.ru", "Номер звонка"); err != nil {
		t.Fatalf("SetProp title.ru: %v", err)
	}
	out, _ := doc.Bytes()
	got := string(out)
	if !strings.Contains(got, "required: true") {
		t.Errorf("bool-проп не записан:\n%s", got)
	}
	if !strings.Contains(got, "Номер звонка") {
		t.Errorf("вложенный title.ru не записан:\n%s", got)
	}
	// Декодируется обратно корректно.
	els, _ := doc.Elements()
	f := els[0].Children[0]
	if !f.El.Required || f.El.TitleMap["ru"] != "Номер звонка" {
		t.Errorf("обратное декодирование: required=%v title=%v", f.El.Required, f.El.TitleMap)
	}
}

// InsertElement вставляет новый структурный узел в children по индексу;
// соседи и их комментарии сдвигаются, но сохраняются. Возвращённый node-id
// разрешается и указывает на вставленный элемент.
func TestInsertElement_IntoChildren(t *testing.T) {
	doc, err := Load([]byte(elemSample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	newID, err := doc.InsertElement("elements.0", 1, map[string]any{
		"kind":      string(metadata.FormElementField),
		"name":      "ПолеНовое",
		"data_path": "Объект.Новое",
	})
	if err != nil {
		t.Fatalf("InsertElement: %v", err)
	}
	if newID != "elements.0.children.1" {
		t.Errorf("newID = %q, ожидался elements.0.children.1", newID)
	}
	els, _ := doc.Elements()
	g := els[0]
	if len(g.Children) != 3 {
		t.Fatalf("ожидалось 3 ребёнка, получено %d", len(g.Children))
	}
	if g.Children[1].El.Name != "ПолеНовое" || g.Children[1].El.DataPath != "Объект.Новое" {
		t.Errorf("вставленный элемент неверен: %+v", g.Children[1].El)
	}
	// Сдвинутый сосед сохранил inline-комментарий.
	out, _ := doc.Bytes()
	if !strings.Contains(string(out), "# дата звонка") {
		t.Errorf("потерян комментарий сдвинутого соседа:\n%s", out)
	}
	// node-id вставленного разрешается.
	if _, err := doc.NodeByID(newID); err != nil {
		t.Errorf("NodeByID(%q): %v", newID, err)
	}
}

// InsertElement с пустым parentID добавляет элемент верхнего уровня (создаёт
// elements при необходимости — здесь он уже есть).
func TestInsertElement_TopLevel(t *testing.T) {
	doc, _ := Load([]byte(elemSample))
	newID, err := doc.InsertElement("", 1, map[string]any{
		"kind": string(metadata.FormElementGroupBox),
		"name": "Группа2",
	})
	if err != nil {
		t.Fatalf("InsertElement top: %v", err)
	}
	if newID != "elements.1" {
		t.Errorf("newID = %q, ожидался elements.1", newID)
	}
	els, _ := doc.Elements()
	if len(els) != 2 || els[1].El.Name != "Группа2" {
		t.Errorf("верхний уровень: %d элементов, последний %+v", len(els), els[1].El)
	}
}

// Move переносит элемент между контейнерами; round-trip остаётся валидным,
// дерево отражает новое расположение.
func TestMove_ChildToTopLevel(t *testing.T) {
	doc, _ := Load([]byte(elemSample))
	// Перенос первого поля группы на верхний уровень в конец.
	if err := doc.Move("elements.0.children.0", "", 1); err != nil {
		t.Fatalf("Move: %v", err)
	}
	els, _ := doc.Elements()
	if len(els) != 2 {
		t.Fatalf("ожидалось 2 верхних элемента после переноса, получено %d", len(els))
	}
	if els[1].El.Kind != metadata.FormElementField || els[1].El.DataPath != "Объект.Номер" {
		t.Errorf("перенесённый элемент неверен: %+v", els[1].El)
	}
	if len(els[0].Children) != 1 {
		t.Errorf("в группе должен остаться 1 ребёнок, осталось %d", len(els[0].Children))
	}
	// round-trip валиден.
	if _, err := doc.Bytes(); err != nil {
		t.Errorf("Bytes после Move: %v", err)
	}
}

// DeleteElement вырезает узел из родительского контейнера; сосед и его
// inline-комментарий сохраняются, round-trip остаётся валидным (follow-up #164).
func TestDeleteElement_RemovesNodeKeepsSiblings(t *testing.T) {
	doc, err := Load([]byte(elemSample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := doc.DeleteElement("elements.0.children.0"); err != nil {
		t.Fatalf("DeleteElement: %v", err)
	}
	els, _ := doc.Elements()
	if len(els[0].Children) != 1 {
		t.Fatalf("ожидался 1 ребёнок после удаления, получено %d", len(els[0].Children))
	}
	if els[0].Children[0].El.DataPath != "Объект.Дата" {
		t.Errorf("остался не тот ребёнок: %+v", els[0].Children[0].El)
	}
	out, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "Объект.Номер") {
		t.Errorf("удалённый элемент остался в YAML:\n%s", got)
	}
	if !strings.Contains(got, "# дата звонка") {
		t.Errorf("потерян inline-комментарий соседа:\n%s", got)
	}
}

// DeleteElement контейнера удаляет его целиком вместе с детьми.
func TestDeleteElement_ContainerWithChildren(t *testing.T) {
	doc, _ := Load([]byte(elemSample))
	if err := doc.DeleteElement("elements.0"); err != nil {
		t.Fatalf("DeleteElement: %v", err)
	}
	els, _ := doc.Elements()
	if len(els) != 0 {
		t.Fatalf("ожидалось 0 верхних элементов, получено %d", len(els))
	}
	out, _ := doc.Bytes()
	if strings.Contains(string(out), "ПолеНомер") || strings.Contains(string(out), "Группа1") {
		t.Errorf("дети удалённого контейнера остались в YAML:\n%s", out)
	}
}

// DeleteElement по индексу вне диапазона и по node-id без родителя — ошибка.
func TestDeleteElement_Errors(t *testing.T) {
	doc, _ := Load([]byte(elemSample))
	if err := doc.DeleteElement("elements.5"); err == nil {
		t.Error("ожидалась ошибка для индекса вне диапазона")
	}
	if err := doc.DeleteElement("elements"); err == nil {
		t.Error("ожидалась ошибка для node-id без родителя")
	}
}

// DeleteProp удаляет вложенный ключ (events.Нажатие), не трогая соседние ключи
// и комментарии (привязка событий конструктора, follow-up #164 batch B1).
func TestDeleteProp_NestedEventKeepsSiblings(t *testing.T) {
	src := `schema: onebase.form/v1
form:
  name: ФормаОбъекта
  kind: object
  entity: Звонок
elements:
  - kind: Кнопка
    name: КнопкаОК   # основная кнопка
    events:
      Нажатие: Обработать
      ПриИзменении: Другое
`
	doc, err := Load([]byte(src))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := doc.DeleteProp("elements.0", "events.Нажатие"); err != nil {
		t.Fatalf("DeleteProp: %v", err)
	}
	out, _ := doc.Bytes()
	got := string(out)
	if strings.Contains(got, "Нажатие: Обработать") {
		t.Errorf("ключ Нажатие не удалён:\n%s", got)
	}
	if !strings.Contains(got, "ПриИзменении: Другое") {
		t.Errorf("потерян соседний ключ события:\n%s", got)
	}
	if !strings.Contains(got, "# основная кнопка") {
		t.Errorf("потерян inline-комментарий:\n%s", got)
	}
	// Обратное декодирование: Нажатие исчезло, ПриИзменении осталось.
	els, _ := doc.Elements()
	h := els[0].El.Handlers
	if _, ok := h["Нажатие"]; ok {
		t.Errorf("Нажатие осталось в Handlers: %v", h)
	}
	if h["ПриИзменении"] != "Другое" {
		t.Errorf("ПриИзменении потеряно: %v", h)
	}
}

// DeleteProp отсутствующего ключа (и через отсутствующий промежуточный узел) —
// no-op, не ошибка.
func TestDeleteProp_MissingIsNoop(t *testing.T) {
	doc, _ := Load([]byte(elemSample))
	if err := doc.DeleteProp("elements.0.children.0", "events.Нажатие"); err != nil {
		t.Errorf("удаление отсутствующего ключа должно быть no-op, got %v", err)
	}
	if err := doc.DeleteProp("elements.0.children.0", "hint"); err != nil {
		t.Errorf("удаление отсутствующего листа должно быть no-op, got %v", err)
	}
}

// SetTopProp/DeleteTopProp правят верхний уровень документа (события формы лежат
// рядом с form/elements, batch B2).
func TestTopProp_FormEvents(t *testing.T) {
	doc, _ := Load([]byte(elemSample))
	if err := doc.SetTopProp("events.ПриОткрытии", "ПриОткрытииФормы"); err != nil {
		t.Fatalf("SetTopProp: %v", err)
	}
	out, _ := doc.Bytes()
	if !strings.Contains(string(out), "ПриОткрытии: ПриОткрытииФормы") {
		t.Errorf("событие формы не записано на верхний уровень:\n%s", out)
	}
	meta, err := doc.FormMeta()
	if err != nil {
		t.Fatalf("FormMeta: %v", err)
	}
	if meta.Events["ПриОткрытии"] != "ПриОткрытииФормы" {
		t.Errorf("FormMeta.Events = %v", meta.Events)
	}
	if err := doc.DeleteTopProp("events.ПриОткрытии"); err != nil {
		t.Fatalf("DeleteTopProp: %v", err)
	}
	out, _ = doc.Bytes()
	if strings.Contains(string(out), "ПриОткрытииФормы") {
		t.Errorf("событие формы не удалено:\n%s", out)
	}
}

// SetOptions пишет набор значений целиком; число остаётся числом, строка —
// строкой; повторный вызов заменяет; пустой список удаляет ключ (batch C1).
func TestSetOptions_RoundTrip(t *testing.T) {
	src := `schema: onebase.form/v1
form:
  name: ФормаОбъекта
  kind: object
  entity: Заказ
elements:
  - kind: Переключатель
    name: Приоритет
    data_path: Объект.Приоритет
`
	doc, err := Load([]byte(src))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	opts := []Option{
		{Value: 1, Label: map[string]string{"ru": "Низкий"}},
		{Value: 2, Label: map[string]string{"ru": "Высокий"}},
	}
	if err := doc.SetOptions("elements.0", opts); err != nil {
		t.Fatalf("SetOptions: %v", err)
	}
	out, _ := doc.Bytes()
	got := string(out)
	if !strings.Contains(got, "value: 1") || strings.Contains(got, `value: "1"`) {
		t.Errorf("числовое значение опции должно быть числом:\n%s", got)
	}
	if !strings.Contains(got, "Высокий") {
		t.Errorf("представление опции не записано:\n%s", got)
	}
	// Декодируется в FormElement.Options.
	els, _ := doc.Elements()
	if n := len(els[0].El.Options); n != 2 {
		t.Fatalf("ожидалось 2 опции, получено %d", n)
	}
	if els[0].El.Options[1].Label() != "Высокий" || els[0].El.Options[1].ValueStr() != "2" {
		t.Errorf("опция декодирована неверно: %+v", els[0].El.Options[1])
	}
	// Повторный вызов заменяет, а не дублирует.
	if err := doc.SetOptions("elements.0", []Option{{Value: "X"}}); err != nil {
		t.Fatalf("SetOptions replace: %v", err)
	}
	els, _ = doc.Elements()
	if len(els[0].El.Options) != 1 {
		t.Errorf("повторный SetOptions не заменил набор: %+v", els[0].El.Options)
	}
	// Пустой список удаляет ключ options.
	if err := doc.SetOptions("elements.0", nil); err != nil {
		t.Fatalf("SetOptions empty: %v", err)
	}
	out, _ = doc.Bytes()
	if strings.Contains(string(out), "options:") {
		t.Errorf("пустой набор должен удалять ключ options:\n%s", out)
	}
}

// Move не должен позволять переносить узел внутрь собственного поддерева:
// yaml.Node хранит указатели, и такой перенос создал бы цикл, который нельзя
// безопасно сериализовать обратно в YAML.
func TestMove_RejectsMoveIntoOwnSubtree(t *testing.T) {
	doc, _ := Load([]byte(elemSample))

	err := doc.Move("elements.0", "elements.0.children.0", 0)
	if err == nil {
		t.Fatalf("ожидалась ошибка при переносе родителя внутрь потомка")
	}
	if !strings.Contains(err.Error(), "собственного поддерева") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}
