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
