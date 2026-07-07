package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// TestPageList_HasActionsButton — smoke-тест плана 41: страница списка
// рендерится и содержит кнопку «Действия» на панели (id="list-actions-btn"),
// а JS-runtime списка живёт в /static/ui.js, читает JSON-конфиг страницы и
// вызывается через data-ob-* вместо inline handlers.
func TestPageList_HasActionsButton(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Контрагент",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
		},
	}

	rows := []map[string]any{
		{"id": "11111111-1111-1111-1111-111111111111", "Наименование": "ООО Ромашка"},
	}

	data := map[string]any{
		"Entity":           ent,
		"Rows":             rows,
		"Params":           storage.ListParams{},
		"RefFilterOptions": map[string]any{},
		"IsAdmin":          true,
		"CanWrite":         true,
		"CanDelete":        true,
		"CanUnpost":        true,
		"Lang":             "ru",
		"Total":            1,
		"Page":             1,
		"TotalPages":       1,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-list", data); err != nil {
		t.Fatalf("ExecuteTemplate page-list: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `id="list-actions-btn"`) {
		t.Error("на панели списка нет кнопки «Действия» (id=list-actions-btn)")
	}
	for _, want := range []string{
		`data-ob-list-actions`,
		`data-ob-auto-submit="320"`,
		`data-ob-list-row`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("страница списка не содержит delegated marker %q", want)
		}
	}
	for _, old := range []string{
		`onclick="listActionsBtnClick(event)"`,
		`oninput="clearTimeout(window._srch)`,
		`onclick="listRowClick(event,this)"`,
		`ondblclick="listRowDblClick(event,this)"`,
		`oncontextmenu="listCtxMenu(event,this)"`,
	} {
		if strings.Contains(html, old) {
			t.Errorf("страница списка содержит старый inline handler %q", old)
		}
	}

	if !strings.Contains(html, `id="ob-list-config"`) {
		t.Error("список не содержит JSON-конфиг ob-list-config")
	}
	if strings.Contains(html, "function listMenuItems") || strings.Contains(html, "function showListMenu") {
		t.Error("runtime списка должен жить в /static/ui.js, а не в HTML")
	}
	js := string(uiJS)
	for _, want := range []string{"function listMenuItems", "function showListMenu", "function listActionsBtnClick", "function obInitListDelegates"} {
		if !strings.Contains(js, want) {
			t.Errorf("/static/ui.js не содержит %q", want)
		}
	}
}

func TestPageList_EmbeddedOpenUsesShell(t *testing.T) {
	ent := &metadata.Entity{
		Name: "ЗаказПокупателя",
		Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Номер", Type: metadata.FieldTypeString},
		},
	}
	data := map[string]any{
		"Entity":           ent,
		"Rows":             []map[string]any{{"id": "11111111-1111-1111-1111-111111111111", "Номер": "ЗПК-00001"}},
		"Params":           storage.ListParams{},
		"RefFilterOptions": map[string]any{},
		"Lang":             "ru",
		"Total":            1,
		"Page":             1,
		"TotalPages":       1,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-list", data); err != nil {
		t.Fatalf("ExecuteTemplate page-list: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		`id="ob-list-config"`,
		`data-open-url="/ui/document/`,
		`11111111-1111-1111-1111-111111111111"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("список не содержит embedded-open фрагмент %q", want)
		}
	}
	if !strings.Contains(html, `data-folder-url="/ui/document/`) || !strings.Contains(html, `parent=11111111-1111-1111-1111-111111111111`) {
		t.Error("строка списка не содержит data-folder-url для навигации по папкам")
	}
	js := string(uiJS)
	for _, want := range []string{
		`window.obOpenInShell && window.obOpenInShell(url, title || listTitle())`,
		`window.location.href = tr.dataset.folderUrl`,
		`else listOpen(tr.dataset.openUrl);`,
		`fn: function () { listOpen(tr.dataset.openUrl); }`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("/static/ui.js не содержит embedded-open фрагмент %q", want)
		}
	}
	if strings.Contains(js, `window.location.href = tr.dataset.openUrl`) {
		t.Error("открытие записи из списка по-прежнему заменяет текущий iframe вместо новой вкладки")
	}
}

// TestPageList_TilesView — режим «Плитка» (Фаза 1a): при TilesView=true список
// рендерится карточками (.tile-grid/.tile-card) с теми же data-*, что и строки
// таблицы (переиспользование обработчиков), а в панели есть переключатель
// режима отображения (.view-switch).
func TestPageList_TilesView(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Номенклатура",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
		},
	}
	rows := []map[string]any{
		{"id": "11111111-1111-1111-1111-111111111111", "Наименование": "Болт М6", "Цена": "12.5"},
	}
	data := map[string]any{
		"Entity":           ent,
		"Rows":             rows,
		"Params":           storage.ListParams{},
		"RefFilterOptions": map[string]any{},
		"IsAdmin":          true,
		"CanWrite":         true,
		"Lang":             "ru",
		"TilesView":        true,
		"Total":            1,
		"Page":             1,
		"TotalPages":       1,
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "page-list", data); err != nil {
		t.Fatalf("ExecuteTemplate page-list (tiles): %v", err)
	}
	html := buf.String()

	for _, want := range []string{"tile-grid", "tile-card", "Болт М6", "view-switch", "data-open-url=", "data-ob-list-row"} {
		if !strings.Contains(html, want) {
			t.Errorf("плиточный режим: в выводе нет ожидаемого фрагмента %q", want)
		}
	}
}
