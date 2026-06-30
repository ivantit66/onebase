package ui

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func TestPageList_TileViewUsesConfiguredFields(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Товар",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Артикул", Type: metadata.FieldTypeString},
			{Name: "Фото", Type: metadata.FieldTypeImage},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
			{Name: "Остаток", Type: metadata.FieldTypeNumber},
			{Name: "Скрыто", Type: metadata.FieldTypeString},
		},
		TileView: &metadata.TileView{
			Image:    "Фото",
			Title:    "Артикул",
			Subtitle: "Наименование",
			Fields:   []string{"Цена", "Остаток"},
		},
	}
	html := renderPageList(t, map[string]any{
		"Entity":           ent,
		"Rows":             []map[string]any{{"id": "1", "Наименование": "Кофе", "Артикул": "A-1", "Фото": "pic-1", "Цена": 0, "Остаток": 12, "Скрыто": "secret"}},
		"Params":           storage.ListParams{},
		"RefFilterOptions": map[string]any{},
		"Lang":             "ru",
		"TilesView":        true,
		"Total":            1, "Page": 1, "TotalPages": 1,
	})

	for _, want := range []string{
		`background-image:url('/ui/_image/pic-1')`,
		`class="tile-title">A-1`,
		`class="tile-subtitle">Кофе`,
		`<span class="tile-label">Цена:</span>`,
		`<span class="tile-val">0</span>`,
		`<span class="tile-label">Остаток:</span>`,
		`<span class="tile-val">12</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("плитка не содержит %q", want)
		}
	}
	for _, unwanted := range []string{"Скрыто:", "secret"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("плитка содержит скрытое поле %q", unwanted)
		}
	}
}

func TestResolveTileViewExplicitEmptyFields(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Товар",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
		},
		TileView: &metadata.TileView{
			Title:     "Наименование",
			FieldsSet: true,
		},
	}
	view := resolveTileView(ent)
	if view.TitleField == nil || view.TitleField.Name != "Наименование" {
		t.Fatalf("TitleField = %+v, ожидалось Наименование", view.TitleField)
	}
	if len(view.Fields) != 0 {
		t.Fatalf("явный пустой fields не должен подставлять автополя, got %+v", view.Fields)
	}
}

// resolveListColumns: без tile_view — все поля; с набором — Заголовок,
// Подзаголовок и выбранные поля (без картинки и невыбранных) (#216).
func TestResolveListColumns(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Товар",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Артикул", Type: metadata.FieldTypeString},
			{Name: "Фото", Type: metadata.FieldTypeImage},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
			{Name: "Остаток", Type: metadata.FieldTypeNumber},
			{Name: "Скрыто", Type: metadata.FieldTypeString},
		},
	}
	if got := resolveListColumns(ent); len(got) != len(ent.Fields) {
		t.Fatalf("без конфигурации ожидались все поля (%d), got %d", len(ent.Fields), len(got))
	}

	ent.TileView = &metadata.TileView{
		Image:    "Фото",
		Title:    "Артикул",
		Subtitle: "Наименование",
		Fields:   []string{"Цена", "Остаток"},
	}
	var names []string
	for _, f := range resolveListColumns(ent) {
		names = append(names, f.Name)
	}
	want := "Артикул,Наименование,Цена,Остаток"
	if strings.Join(names, ",") != want {
		t.Fatalf("колонки списка = %v, ожидалось %q", names, want)
	}
}

// Табличный режим списка (страницы/лента) теперь уважает tile_view.fields:
// выбранные колонки показываются, невыбранные — нет (#216).
func TestPageList_ListViewHonorsTileFields(t *testing.T) {
	ent := &metadata.Entity{
		Name: "Товар",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Артикул", Type: metadata.FieldTypeString},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
			{Name: "Скрыто", Type: metadata.FieldTypeString},
		},
		TileView: &metadata.TileView{Title: "Артикул", Fields: []string{"Цена"}},
	}
	html := renderPageList(t, map[string]any{
		"Entity":           ent,
		"Rows":             []map[string]any{{"id": "1", "Наименование": "Кофе", "Артикул": "A-1", "Цена": 100, "Скрыто": "secret"}},
		"Params":           storage.ListParams{},
		"RefFilterOptions": map[string]any{},
		"Lang":             "ru",
		"Total":            1, "Page": 1, "TotalPages": 1,
	})
	for _, want := range []string{"Артикул", "Цена", "A-1"} {
		if !strings.Contains(html, want) {
			t.Errorf("список не содержит выбранную колонку/значение %q", want)
		}
	}
	for _, unwanted := range []string{"Кофе", "secret"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("список показал значение невыбранной колонки: %q", unwanted)
		}
	}
}

func TestPageList_TreeViewKeepsToggleWhenNameHiddenByTileFields(t *testing.T) {
	ent := &metadata.Entity{
		Name:         "Товар",
		Kind:         metadata.KindCatalog,
		Hierarchical: true,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Артикул", Type: metadata.FieldTypeString},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
			{Name: "Скрыто", Type: metadata.FieldTypeString},
		},
		TileView: &metadata.TileView{Title: "Артикул", Fields: []string{"Цена"}},
	}
	html := renderPageList(t, map[string]any{
		"Entity":           ent,
		"TreeView":         true,
		"TreeRows":         []map[string]any{{"id": "1", "is_folder": true, "_depth": 0, "Наименование": "Кофе", "Артикул": "A-1", "Цена": 100, "Скрыто": "secret"}},
		"Params":           storage.ListParams{},
		"RefFilterOptions": map[string]any{},
		"Lang":             "ru",
		"Total":            1, "Page": 1, "TotalPages": 1,
	})
	for _, want := range []string{`class="tree-toggle"`, "Артикул", "Цена", "A-1", "100"} {
		if !strings.Contains(html, want) {
			t.Errorf("дерево не содержит %q", want)
		}
	}
	for _, unwanted := range []string{"Кофе", "secret"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("дерево показало значение невыбранной колонки: %q", unwanted)
		}
	}
}
