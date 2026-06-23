package launcher

import (
	"bytes"
	"fmt"
	"html"

	"github.com/ivantit66/onebase/internal/formdoc"
	"github.com/ivantit66/onebase/internal/metadata"
)

// Интерактивный рендер холста визуального конструктора форм (#164, слайс 4).
//
// В отличие от read-only renderManagedFormPreview, холст несёт адресацию: каждый
// элемент помечен data-node-id (путь в дереве yaml.Node), между детьми контейнеров
// вставлены drop-зоны (data-parent + data-index) для приёма реквизита из палитры,
// а выбранный элемент подсвечивается классом fc-selected. Клиент (слайс 6) вешает
// на это drag/drop, клик-выбор и панель свойств; правки уходят на /forms/edit-op.
//
// Возвращается HTML-фрагмент (не iframe): холст встроен в страницу редактора, так
// что ванильный JS напрямую им управляет.

// renderFormCanvas рендерит дерево элементов формы как интерактивный холст.
// selectedID — node-id выбранного элемента (пустой = ничего не выбрано).
func renderFormCanvas(doc *formdoc.Doc, selectedID string) (string, error) {
	els, err := doc.Elements()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<div class="fc-canvas" data-selected="%s">`, html.EscapeString(selectedID))
	renderCanvasChildren(&buf, "", els, selectedID)
	buf.WriteString(`</div>`)
	return buf.String(), nil
}

// renderCanvasChildren рисует последовательность детей контейнера parentID,
// чередуя drop-зоны: перед каждым ребёнком и одну после последнего (N+1 зон).
func renderCanvasChildren(buf *bytes.Buffer, parentID string, els []*formdoc.ElementNode, selectedID string) {
	buf.WriteString(`<div class="fc-children">`)
	for i, en := range els {
		renderDropZone(buf, parentID, i)
		renderCanvasElement(buf, en, selectedID)
	}
	renderDropZone(buf, parentID, len(els))
	buf.WriteString(`</div>`)
}

// renderDropZone — точка приёма реквизита: вставка в контейнер parentID на
// позицию index. Пустой parentID — верхний уровень (elements).
func renderDropZone(buf *bytes.Buffer, parentID string, index int) {
	fmt.Fprintf(buf, `<div class="fc-drop" data-parent="%s" data-index="%d"></div>`,
		html.EscapeString(parentID), index)
}

// elWrapClass собирает классы обёртки элемента, добавляя fc-selected выбранному.
func elWrapClass(base, nodeID, selectedID string) string {
	cls := "fc-el " + base
	if nodeID != "" && nodeID == selectedID {
		cls += " fc-selected"
	}
	return cls
}

func renderCanvasElement(buf *bytes.Buffer, en *formdoc.ElementNode, selectedID string) {
	if en == nil || en.El == nil {
		return
	}
	el := en.El
	id := en.NodeID
	kind := html.EscapeString(string(el.Kind))
	title := html.EscapeString(canvasTitle(el.Name, el.TitleMap, el.Title))

	switch el.Kind {
	case metadata.FormElementGroupBox, metadata.FormElementCommandBar:
		fmt.Fprintf(buf, `<fieldset class="%s" data-node-id="%s" data-kind="%s"><legend class="fc-pick">%s</legend>`,
			elWrapClass("fc-group", id, selectedID), id, kind, title)
		renderCanvasChildren(buf, id, en.Children, selectedID)
		buf.WriteString(`</fieldset>`)

	case metadata.FormElementPages:
		fmt.Fprintf(buf, `<div class="%s" data-node-id="%s" data-kind="%s">`,
			elWrapClass("fc-pages", id, selectedID), id, kind)
		for _, p := range en.Children {
			if p == nil || p.El == nil {
				continue
			}
			ptitle := html.EscapeString(canvasTitle(p.El.Name, p.El.TitleMap, p.El.Title))
			fmt.Fprintf(buf, `<div class="%s" data-node-id="%s" data-kind="%s"><div class="fc-tab fc-pick">%s</div>`,
				elWrapClass("fc-page", p.NodeID, selectedID), p.NodeID, html.EscapeString(string(p.El.Kind)), ptitle)
			renderCanvasChildren(buf, p.NodeID, p.Children, selectedID)
			buf.WriteString(`</div>`)
		}
		buf.WriteString(`</div>`)

	case metadata.FormElementField, metadata.FormElementDatePicker, metadata.FormElementInputList, metadata.FormElementFormField:
		req := ""
		if el.Required {
			req = ` <span class="fc-req">*</span>`
		}
		field := lastSegment(el.DataPath)
		if field == "" {
			field = el.Name
		}
		ro := ""
		if el.ReadOnly {
			ro = " readonly"
		}
		fmt.Fprintf(buf, `<div class="%s fc-pick" data-node-id="%s" data-kind="%s"><label>%s%s</label><input type="text" disabled%s placeholder="%s"></div>`,
			elWrapClass("fc-field", id, selectedID), id, kind, title, req, ro, html.EscapeString(field))

	case metadata.FormElementCheckbox:
		fmt.Fprintf(buf, `<div class="%s fc-pick" data-node-id="%s" data-kind="%s"><input type="checkbox" disabled> <span>%s</span></div>`,
			elWrapClass("fc-check", id, selectedID), id, kind, title)

	case metadata.FormElementLabel:
		fmt.Fprintf(buf, `<div class="%s fc-pick" data-node-id="%s" data-kind="%s">%s</div>`,
			elWrapClass("fc-label", id, selectedID), id, kind, title)

	case metadata.FormElementButton:
		fmt.Fprintf(buf, `<div class="%s fc-pick" data-node-id="%s" data-kind="%s"><button type="button" disabled>%s</button></div>`,
			elWrapClass("fc-btn", id, selectedID), id, kind, title)

	case metadata.FormElementTable, metadata.FormElementTablePart:
		fmt.Fprintf(buf, `<div class="%s fc-pick" data-node-id="%s" data-kind="%s"><div class="fc-tp">▦ %s</div></div>`,
			elWrapClass("fc-table", id, selectedID), id, kind, title)

	default:
		fmt.Fprintf(buf, `<div class="%s fc-pick" data-node-id="%s" data-kind="%s">%s <span class="fc-kind">%s</span></div>`,
			elWrapClass("fc-unknown", id, selectedID), id, kind, title, kind)
	}
}

// canvasElementInfo — редактируемые поля элемента для панели свойств клиента.
// Плоская карта node-id → info отдаётся вместе с холстом, чтобы клик по элементу
// открывал панель без повторного парсинга YAML в браузере.
type canvasElementInfo struct {
	NodeID    string `json:"nodeId"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	TitleRU   string `json:"titleRu"`
	DataPath  string `json:"dataPath"`
	Required  bool   `json:"required"`
	ReadOnly  bool   `json:"readonly"`
	Hint      string `json:"hint"`
	Container bool   `json:"container"`
}

// canvasModel разворачивает дерево формы в плоскую карту node-id → редактируемые
// поля — источник данных для панели свойств визуального конструктора (#164).
func canvasModel(doc *formdoc.Doc) (map[string]canvasElementInfo, error) {
	els, err := doc.Elements()
	if err != nil {
		return nil, err
	}
	m := make(map[string]canvasElementInfo)
	var walk func(ens []*formdoc.ElementNode)
	walk = func(ens []*formdoc.ElementNode) {
		for _, en := range ens {
			el := en.El
			info := canvasElementInfo{
				NodeID:    en.NodeID,
				Kind:      string(el.Kind),
				Name:      el.Name,
				DataPath:  el.DataPath,
				Required:  el.Required,
				ReadOnly:  el.ReadOnly,
				Hint:      el.Hint,
				Container: el.IsContainer(),
			}
			if el.TitleMap != nil {
				info.TitleRU = el.TitleMap["ru"]
			}
			m[en.NodeID] = info
			walk(en.Children)
		}
	}
	walk(els)
	return m, nil
}

// canvasTitle выбирает отображаемый заголовок элемента: ru-локаль → legacy
// Title → имя. Совпадает с приоритетом read-only предпросмотра.
func canvasTitle(name string, titleMap map[string]string, legacy string) string {
	if titleMap != nil && titleMap["ru"] != "" {
		return titleMap["ru"]
	}
	if legacy != "" {
		return legacy
	}
	return name
}
