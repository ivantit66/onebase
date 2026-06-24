package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFile_TileView(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "товар.yaml")
	src := `name: Товар
fields:
  - name: Наименование
    type: string
  - name: Артикул
    type: string
  - name: Фото
    type: image
  - name: Цена
    type: number
tile_view:
  image: Фото
  title: Артикул
  subtitle: Наименование
  fields: [Цена]
`
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := LoadFile(p, KindCatalog)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if e.TileView == nil {
		t.Fatal("TileView не разобран")
	}
	if e.TileView.Image != "Фото" || e.TileView.Title != "Артикул" || e.TileView.Subtitle != "Наименование" {
		t.Fatalf("TileView разобран неверно: %+v", e.TileView)
	}
	if !e.TileView.FieldsSet || len(e.TileView.Fields) != 1 || e.TileView.Fields[0] != "Цена" {
		t.Fatalf("TileView.Fields = %#v, ожидалось [Цена]", e.TileView.Fields)
	}
	if err := Validate([]*Entity{e}, nil); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadFile_TileViewEmptyFieldsIsExplicit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "товар.yaml")
	src := `name: Товар
fields:
  - name: Наименование
    type: string
tile_view:
  title: Наименование
  fields: []
`
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := LoadFile(p, KindCatalog)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if e.TileView == nil || !e.TileView.FieldsSet || len(e.TileView.Fields) != 0 {
		t.Fatalf("fields: [] должен сохраняться как явный пустой список, got %+v", e.TileView)
	}
}

func TestValidateTileViewRejectsUnknownField(t *testing.T) {
	ent := &Entity{
		Name: "Товар",
		Kind: KindCatalog,
		Fields: []Field{
			{Name: "Наименование", Type: FieldTypeString},
		},
		TileView: &TileView{Title: "Артикул"},
	}
	err := Validate([]*Entity{ent}, nil)
	if err == nil || !strings.Contains(err.Error(), "tile_view.title") {
		t.Fatalf("ожидалась ошибка tile_view.title, получено %v", err)
	}
}

func TestValidateTileViewRequiresImageFieldType(t *testing.T) {
	ent := &Entity{
		Name: "Товар",
		Kind: KindCatalog,
		Fields: []Field{
			{Name: "Наименование", Type: FieldTypeString},
		},
		TileView: &TileView{Image: "Наименование"},
	}
	err := Validate([]*Entity{ent}, nil)
	if err == nil || !strings.Contains(err.Error(), "must have type image") {
		t.Fatalf("ожидалась ошибка типа image, получено %v", err)
	}
}
