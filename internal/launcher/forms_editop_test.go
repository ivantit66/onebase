package launcher

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// HTTP-уровень: маршрут /forms/edit-op доходит до handler'а, парсит форму,
// применяет команду и отдаёт JSON — закрывает интеграционный разрыв поверх
// юнит-тестов applyEditOp.
func TestConfiguratorFormsEditOp_HTTP(t *testing.T) {
	s := &Store{path: filepath.Join(t.TempDir(), "ibases.yaml")}
	b := &Base{Path: t.TempDir(), ConfigSource: "file"}
	if err := s.Add(b); err != nil {
		t.Fatalf("Add base: %v", err)
	}
	h := &handler{store: s}

	form := url.Values{}
	form.Set("op", "insert")
	form.Set("parent", "elements.0")
	form.Set("index", "0")
	form.Set("kind", "ПолеВвода")
	form.Set("name", "ПолеТест")
	form.Set("data_path", "Объект.Тест")
	form.Set("yaml", canvasSample)

	req := httptest.NewRequest("POST", "/bases/"+b.ID+"/configurator/forms/edit-op", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", b.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	h.configuratorFormsEditOp(rr, req)

	if rr.Code != 200 {
		t.Fatalf("статус %d: %s", rr.Code, rr.Body.String())
	}
	var resp editOpResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("разбор JSON: %v; тело=%s", err, rr.Body.String())
	}
	if !resp.OK {
		t.Fatalf("ok=false, errors=%v", resp.Errors)
	}
	if !strings.Contains(resp.YAML, "ПолеТест") {
		t.Errorf("в YAML нет нового поля:\n%s", resp.YAML)
	}
	if resp.SelectedID != "elements.0.children.0" {
		t.Errorf("selectedId = %q", resp.SelectedID)
	}
	if _, ok := resp.Model["elements.0.children.0"]; !ok {
		t.Errorf("в модели нет нового узла: %v", resp.Model)
	}
}

// setProp через эндпоинт: значение пишется в YAML, выделение остаётся на узле,
// холст пере-рендерен с тем же node-id.
func TestApplyEditOp_SetProp(t *testing.T) {
	res, err := applyEditOp([]byte(canvasSample), editOpRequest{
		Op:    "setProp",
		Node:  "elements.0.children.1",
		Key:   "data_path",
		Value: "Объект.Изменено",
	})
	if err != nil {
		t.Fatalf("applyEditOp: %v", err)
	}
	if !strings.Contains(res.YAML, "data_path: Объект.Изменено") {
		t.Errorf("YAML не обновлён:\n%s", res.YAML)
	}
	if res.SelectedID != "elements.0.children.1" {
		t.Errorf("selectedId = %q", res.SelectedID)
	}
	if !strings.Contains(res.CanvasHTML, `data-node-id="elements.0.children.1"`) {
		t.Errorf("холст не содержит выбранный узел")
	}
}

// Чекбокс-свойство приходит строкой "true" → в YAML булев скаляр, не строка.
func TestApplyEditOp_BoolProp(t *testing.T) {
	res, err := applyEditOp([]byte(canvasSample), editOpRequest{
		Op:    "setProp",
		Node:  "elements.0.children.1",
		Key:   "readonly",
		Value: "true",
	})
	if err != nil {
		t.Fatalf("applyEditOp: %v", err)
	}
	if !strings.Contains(res.YAML, "readonly: true") {
		t.Errorf("bool-проп не булев:\n%s", res.YAML)
	}
}

// insert через эндпоинт: новый элемент попадает в YAML с title.ru и data_path,
// становится выбранным, и виден на холсте.
func TestApplyEditOp_Insert(t *testing.T) {
	res, err := applyEditOp([]byte(canvasSample), editOpRequest{
		Op:       "insert",
		Parent:   "elements.0",
		Index:    2,
		Kind:     "ПолеВвода",
		Name:     "ПолеНовое",
		DataPath: "Объект.Новое",
		TitleRU:  "Новое поле",
	})
	if err != nil {
		t.Fatalf("applyEditOp: %v", err)
	}
	if res.SelectedID != "elements.0.children.2" {
		t.Errorf("selectedId = %q, ожидался elements.0.children.2", res.SelectedID)
	}
	for _, want := range []string{"ПолеНовое", "Объект.Новое", "Новое поле"} {
		if !strings.Contains(res.YAML, want) {
			t.Errorf("YAML без %q:\n%s", want, res.YAML)
		}
	}
	if !strings.Contains(res.CanvasHTML, `data-node-id="elements.0.children.2"`) {
		t.Errorf("холст без нового узла:\n%s", res.CanvasHTML)
	}
}

// Невалидный YAML, неизвестная операция и устаревший node-id — штатные ошибки,
// без паники (план 71: баннер/конфликт на клиенте).
func TestApplyEditOp_Errors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		req  editOpRequest
	}{
		{"битый YAML", "form:\n\tbad: 1\n", editOpRequest{Op: "setProp", Node: "elements.0", Key: "name", Value: "x"}},
		{"неизвестная операция", canvasSample, editOpRequest{Op: "frobnicate"}},
		{"устаревший узел", canvasSample, editOpRequest{Op: "setProp", Node: "elements.7", Key: "name", Value: "x"}},
		{"insert без kind", canvasSample, editOpRequest{Op: "insert", Parent: "elements.0", Index: 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := applyEditOp([]byte(tc.yaml), tc.req); err == nil {
				t.Errorf("ожидалась ошибка для %q, получено nil", tc.name)
			}
		})
	}
}

// render — перезагрузка холста из YAML без мутаций: YAML не меняется, холст и
// модель свойств отдаются, выделение сохраняется по присланному node-id.
func TestApplyEditOp_Render(t *testing.T) {
	res, err := applyEditOp([]byte(canvasSample), editOpRequest{Op: "render", Node: "elements.0.children.0"})
	if err != nil {
		t.Fatalf("applyEditOp render: %v", err)
	}
	if res.SelectedID != "elements.0.children.0" {
		t.Errorf("render не сохранил выделение: %q", res.SelectedID)
	}
	info, ok := res.Model["elements.0.children.0"]
	if !ok {
		t.Fatalf("в модели нет элемента: %v", res.Model)
	}
	if info.DataPath != "Объект.Номер" || !info.Required {
		t.Errorf("модель свойств неверна: %+v", info)
	}
	if g := res.Model["elements.0"]; !g.Container {
		t.Errorf("группа должна быть container: %+v", g)
	}
}

// Round-trip эндпоинта сохраняет ручной комментарий пользователя — ключевое
// требование #164 (правка свойства не затирает аннотации).
func TestApplyEditOp_PreservesComments(t *testing.T) {
	src := `schema: onebase.form/v1
form:
  name: ФормаОбъекта
  kind: object
  entity: Звонок
elements:
  - kind: ГруппаФормы
    name: Группа1   # основная группа
    children:
      - kind: ПолеВвода
        name: Поле1
        data_path: Объект.Дата
`
	res, err := applyEditOp([]byte(src), editOpRequest{
		Op: "setProp", Node: "elements.0.children.0", Key: "required", Value: "true",
	})
	if err != nil {
		t.Fatalf("applyEditOp: %v", err)
	}
	if !strings.Contains(res.YAML, "# основная группа") {
		t.Errorf("потерян ручной комментарий:\n%s", res.YAML)
	}
}
