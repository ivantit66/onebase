package interpreter

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/printform"
)

// updateGolden управляет перегенерацией golden-файлов: `go test -run Golden -update`.
// По умолчанию тест СРАВНИВАЕТ текущий HTML с зафиксированным эталоном (бит-в-бит) —
// это страховка при выносе модели в internal/sheet (план 64, этап 1).
var updateGolden = flag.Bool("update", false, "перегенерировать golden-файлы HTML табличного документа")

// goldenCase описывает один сценарий: имя файла + построитель документа через
// ПУБЛИЧНЫЙ DSL-API (CallMethod/Get/Set) — как это делает интерпретатор из .os.
type goldenCase struct {
	name  string
	build func() *SpreadsheetDocument
}

// goldenCases — представительный набор форм для проверки HTML-рендера:
//
//	(а) simple   — текст + стили (bold/italic/fontSize/цвета/выравнивания) через Ячейка;
//	(б) spans    — объединения (Объединить документа + Объединить области с covered);
//	(в) layout   — области из Макета (LayoutTemplate) с параметрами + Вывести/Присоединить;
//	(г) sizing   — ШиринаКолонки/ВысотаСтроки + смешанные стили.
func goldenCases() []goldenCase {
	return []goldenCase{
		{"simple", buildSimpleDoc},
		{"spans", buildSpansDoc},
		{"layout", buildLayoutDoc},
		{"sizing", buildSizingDoc},
	}
}

// setCell заполняет ячейку (1-based) через DSL-обёртку, как из .os.
func setCell(d *SpreadsheetDocument, row, col int, props map[string]any) {
	obj := d.CallMethod("ячейка", []any{float64(row), float64(col)})
	w, ok := obj.(*SpreadsheetDocumentCellWrapper)
	if !ok {
		return
	}
	for k, v := range props {
		w.Set(k, v)
	}
}

func buildSimpleDoc() *SpreadsheetDocument {
	d := NewSpreadsheetDocument()
	setCell(d, 1, 1, map[string]any{"текст": "Накладная № 123"})
	setCell(d, 1, 1, map[string]any{"жирный": true, "размершрифта": 14.0, "выравнивание": "center"})
	setCell(d, 2, 1, map[string]any{"текст": "Поставщик", "курсив": true})
	setCell(d, 2, 2, map[string]any{"текст": "ООО Ромашка", "цветтекста": "#003399"})
	setCell(d, 3, 1, map[string]any{"текст": "Сумма", "выравнивание": "right", "цветфона": "#eef"})
	setCell(d, 3, 2, map[string]any{"текст": "1 000.00", "выравнивание": "right", "вервыравнивание": "bottom"})
	return d
}

func buildSpansDoc() *SpreadsheetDocument {
	d := NewSpreadsheetDocument()
	// Заголовок объединён по 3 колонкам через метод документа Объединить.
	setCell(d, 1, 1, map[string]any{"текст": "Отчёт за период", "жирный": true})
	d.CallMethod("объединить", []any{1.0, 1.0, 1.0, 3.0})
	// Тело таблицы.
	setCell(d, 2, 1, map[string]any{"текст": "Товар"})
	setCell(d, 2, 2, map[string]any{"текст": "Кол-во"})
	setCell(d, 2, 3, map[string]any{"текст": "Цена"})
	setCell(d, 3, 1, map[string]any{"текст": "Гвозди"})
	setCell(d, 3, 2, map[string]any{"текст": "10"})
	setCell(d, 3, 3, map[string]any{"текст": "5.50"})
	// Объединение через область (rowspan): колонка 1 строки 4-5.
	a := d.CallMethod("область", []any{4.0, 1.0, 5.0, 1.0}).(*SpreadsheetDocumentArea)
	a.Set("текст", "Итого")
	a.CallMethod("объединить", nil)
	d.CallMethod("вывести", []any{a})
	return d
}

func buildLayoutDoc() *SpreadsheetDocument {
	lt := &printform.LayoutTemplate{
		Name: "Накладная",
		Areas: map[string]*printform.LayoutArea{
			"шапка": {
				Rows: []printform.LayoutRow{
					{Cells: []printform.LayoutCell{
						{Text: "ТОВАРНАЯ НАКЛАДНАЯ", Bold: true, Align: "Center", FontSize: 12, ColSpan: 2},
					}},
					{Cells: []printform.LayoutCell{
						{Text: "Номер:"},
						{Parameter: "Номер"},
					}},
				},
			},
			"строка": {
				Rows: []printform.LayoutRow{
					{Cells: []printform.LayoutCell{
						{Parameter: "Наименование", Border: "all"},
						{Parameter: "Сумма", Align: "Right"},
					}},
				},
			},
		},
	}
	m := NewMaket(lt)
	d := NewSpreadsheetDocument()

	head := m.getArea("шапка")
	head.CallMethod("установитьпараметр", []any{"Номер", "УПД-0001"})
	d.CallMethod("вывести", []any{head})

	for _, it := range []struct{ name, sum string }{{"Гайка", "120.00"}, {"Болт", "80.00"}} {
		row := m.getArea("строка")
		ap := row.Get("параметры").(*AreaParameters)
		ap.Set("Наименование", it.name)
		ap.Set("Сумма", it.sum)
		d.CallMethod("вывести", []any{row})
	}
	return d
}

func buildSizingDoc() *SpreadsheetDocument {
	d := NewSpreadsheetDocument()
	setCell(d, 1, 1, map[string]any{"текст": "Левая"})
	setCell(d, 1, 2, map[string]any{"текст": "Правая"})
	setCell(d, 2, 1, map[string]any{"текст": "низ"})
	d.CallMethod("ширинаколонки", []any{1.0, 150.0})
	d.CallMethod("ширинаколонки", []any{2.0, 80.0})
	d.CallMethod("высотастроки", []any{1.0, 40.0})
	// Присоединить область справа.
	a := d.CallMethod("область", []any{1.0, 1.0, 1.0, 1.0}).(*SpreadsheetDocumentArea)
	a.Set("текст", "Доп")
	a.Set("жирный", true)
	d.CallMethod("присоединить", []any{a})
	return d
}

func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name+".html")
}

// TestSpreadsheetGoldenHTML фиксирует HTML-вывод табличного документа бит-в-бит.
// При -update перезаписывает эталоны; иначе сравнивает с ними.
func TestSpreadsheetGoldenHTML(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.build().HTMLString()
			path := goldenPath(tc.name)
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir golden: %v", err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("обновлён эталон %s (%d байт)", path, len(got))
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("чтение эталона %s: %v (запустите с -update)", path, err)
			}
			if got != string(want) {
				t.Errorf("HTML отличается от эталона %s\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
			}
		})
	}
}
