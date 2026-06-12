package printform

import (
	"bytes"
	"testing"
)

// TestRenderPDFEmbedsCyrillicFont проверяет, что legacy YAML-форма рендерится
// в PDF со встроенным кириллическим PT-шрифтом и без транслитерации (план 64,
// этап 2 — смерть latinize).
func TestRenderPDFEmbedsCyrillicFont(t *testing.T) {
	form := &PrintForm{
		Name:   "ТестоваяФорма",
		Title:  "Накладная № 123",
		Header: "## Поставщик: ООО «Ромашка»",
		Table: &TableSection{
			Source: "Товары",
			Columns: []Column{
				{Field: "@row", Label: "№", Width: "10mm", Align: "center"},
				{Field: "Наименование", Label: "Наименование", Width: "60%"},
				{Field: "Сумма", Label: "Сумма", Align: "right", Format: "number:2"},
			},
			Totals: []TotalSpec{{Field: "Сумма", Sum: true, Label: "Итого"}},
		},
		Footer: "Подпись: ___________",
	}
	ctx := &RenderContext{
		Document: map[string]any{"Номер": "123"},
		TableParts: map[string][]map[string]any{
			"Товары": {
				{"Наименование": "Гвозди строительные", "Сумма": 100.5},
				{"Наименование": "Шурупы", "Сумма": 250.0},
			},
		},
	}

	b, err := RenderPDF(form, ctx)
	if err != nil {
		t.Fatalf("RenderPDF error: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("PDF не начинается с %%PDF")
	}
	// Встроенный PT-шрифт виден в байтах (fpdf: utf8ptserif).
	if !bytes.Contains(bytes.ToLower(b), []byte("ptserif")) {
		t.Fatal("в PDF не найден встроенный PT-шрифт (ptserif) — шрифт не встроен")
	}
	// Транслитерация умерла: кириллический заголовок не должен латинизироваться.
	if bytes.Contains(b, []byte("Nakladnaja")) || bytes.Contains(b, []byte("Postavshchik")) {
		t.Fatal("в PDF найдена латинизация — транслитерация не убита")
	}
}
