package launcher

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ivantit66/onebase/internal/formdoc"
)

// Эндпоинт визуальной правки формы (#164, слайс 5).
//
// Холст серверо-центричен: правка на клиенте превращается в команду
// {op, target, value}, уходит сюда; сервер хирургически меняет дерево
// yaml.Node, пере-сериализует YAML и пере-рендерит холст, возвращая
// {yaml, canvasHtml, selectedId, errors}. Источник истины — текст YAML из
// редактора: его присылает клиент, мы его правим и отдаём обратно — Monaco и
// холст синхронизируются от одного результата. Состояние на сервере не копится.

// editOpRequest — разобранная команда правки формы.
type editOpRequest struct {
	Op       string // setProp | insert | move
	Node     string // node-id цели (setProp, move)
	Key      string // setProp: имя свойства (может быть вложенным, "title.ru")
	Value    string // setProp: значение (сырое; bool-свойства приводятся)
	Parent   string // insert/move: node-id контейнера ("" = верхний уровень)
	Index    int    // insert/move: позиция в контейнере
	Kind     string // insert: вид нового элемента
	Name     string // insert: имя нового элемента
	DataPath string // insert: data_path нового элемента
	TitleRU  string // insert: ru-заголовок нового элемента
}

// editOpResult — результат применения команды к YAML.
type editOpResult struct {
	YAML       string
	CanvasHTML string
	SelectedID string
	Model      map[string]canvasElementInfo
}

// boolProps — свойства элемента, значение которых интерпретируется как bool
// (чекбоксы панели свойств шлют "true"/"" — пишем в YAML булев скаляр, не строку).
var boolProps = map[string]bool{
	"required": true, "readonly": true, "choice": true,
	"visible": true, "enabled": true, "no_grid": true,
}

// coercePropValue приводит сырое строковое значение свойства к типу: bool для
// чекбокс-свойств, иначе — строка как есть.
func coercePropValue(key, raw string) any {
	leaf := key
	if i := strings.LastIndex(key, "."); i >= 0 {
		leaf = key[i+1:]
	}
	if boolProps[leaf] {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "true", "1", "on", "yes", "да":
			return true
		default:
			return false
		}
	}
	return raw
}

// applyEditOp применяет команду к YAML-исходнику формы и возвращает обновлённые
// YAML + HTML холста + node-id выделения. Чистая функция (без HTTP) — ядро
// эндпоинта, покрытое юнит-тестами.
func applyEditOp(yamlSrc []byte, req editOpRequest) (editOpResult, error) {
	doc, err := formdoc.Load(yamlSrc)
	if err != nil {
		return editOpResult{}, fmt.Errorf("YAML формы не разобран: %w", err)
	}

	var selected string
	switch req.Op {
	case "render":
		// Перезагрузка холста из текущего YAML без мутаций (направление
		// YAML→холст). Выделение сохраняется по присланному node-id.
		selected = req.Node

	case "setProp":
		if req.Key == "" {
			return editOpResult{}, fmt.Errorf("setProp: пустой key")
		}
		if err := doc.SetProp(req.Node, req.Key, coercePropValue(req.Key, req.Value)); err != nil {
			return editOpResult{}, err
		}
		selected = req.Node

	case "insert":
		if strings.TrimSpace(req.Kind) == "" {
			return editOpResult{}, fmt.Errorf("insert: не указан kind")
		}
		fields := map[string]any{"kind": req.Kind}
		if req.Name != "" {
			fields["name"] = req.Name
		}
		if req.DataPath != "" {
			fields["data_path"] = req.DataPath
		}
		if req.TitleRU != "" {
			fields["title"] = map[string]string{"ru": req.TitleRU}
		}
		newID, err := doc.InsertElement(req.Parent, req.Index, fields)
		if err != nil {
			return editOpResult{}, err
		}
		selected = newID

	case "move":
		if err := doc.Move(req.Node, req.Parent, req.Index); err != nil {
			return editOpResult{}, err
		}
		// Индексы после переноса смещаются — клиент перезапрашивает выделение.
		selected = ""

	default:
		return editOpResult{}, fmt.Errorf("неизвестная операция %q", req.Op)
	}

	out, err := doc.Bytes()
	if err != nil {
		return editOpResult{}, err
	}
	canvas, err := renderFormCanvas(doc, selected)
	if err != nil {
		return editOpResult{}, err
	}
	model, err := canvasModel(doc)
	if err != nil {
		return editOpResult{}, err
	}
	return editOpResult{YAML: string(out), CanvasHTML: canvas, SelectedID: selected, Model: model}, nil
}

// editOpResponse — JSON-ответ эндпоинта.
type editOpResponse struct {
	OK         bool                         `json:"ok"`
	YAML       string                       `json:"yaml,omitempty"`
	CanvasHTML string                       `json:"canvasHtml,omitempty"`
	SelectedID string                       `json:"selectedId,omitempty"`
	Model      map[string]canvasElementInfo `json:"model,omitempty"`
	Errors     []string                     `json:"errors,omitempty"`
}

// configuratorFormsEditOp — POST: применяет визуальную команду к YAML формы и
// возвращает синхронизированные YAML + холст. Состояние не пишется на диск —
// сохранение по-прежнему через /forms/save.
func (h *handler) configuratorFormsEditOp(w http.ResponseWriter, r *http.Request) {
	if _, err := h.store.Get(chi.URLParam(r, "id")); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeFormsJSON(w, editOpResponse{OK: false, Errors: []string{err.Error()}})
		return
	}
	index, _ := strconv.Atoi(r.FormValue("index"))
	req := editOpRequest{
		Op:       strings.TrimSpace(r.FormValue("op")),
		Node:     r.FormValue("node"),
		Key:      r.FormValue("key"),
		Value:    r.FormValue("value"),
		Parent:   r.FormValue("parent"),
		Index:    index,
		Kind:     r.FormValue("kind"),
		Name:     r.FormValue("name"),
		DataPath: r.FormValue("data_path"),
		TitleRU:  r.FormValue("title_ru"),
	}
	res, err := applyEditOp([]byte(r.FormValue("yaml")), req)
	if err != nil {
		writeFormsJSON(w, editOpResponse{OK: false, Errors: []string{err.Error()}})
		return
	}
	writeFormsJSON(w, editOpResponse{
		OK:         true,
		YAML:       res.YAML,
		CanvasHTML: res.CanvasHTML,
		SelectedID: res.SelectedID,
		Model:      res.Model,
	})
}
