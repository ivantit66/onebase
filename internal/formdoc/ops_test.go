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
