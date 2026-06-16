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
// а вспомогательные JS-функции listMenuItems/showListMenu определены ровно
// один раз (контекстное меню и кнопка делят один источник пунктов).
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

	// listMenuItems — единый источник пунктов; должен быть объявлен ровно один раз.
	if n := strings.Count(html, "function listMenuItems"); n != 1 {
		t.Errorf("function listMenuItems объявлена %d раз(а), ожидалось 1", n)
	}
	if !strings.Contains(html, "function showListMenu") {
		t.Error("не найдена функция showListMenu — рендер меню не вынесен")
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

	for _, want := range []string{"tile-grid", "tile-card", "Болт М6", "view-switch", "data-open-url="} {
		if !strings.Contains(html, want) {
			t.Errorf("плиточный режим: в выводе нет ожидаемого фрагмента %q", want)
		}
	}
}
