package printform

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/sheet"
)

// TestMigratedExamples_RenderSmoke — спот-чек реальных мигрированных макетов из
// examples/*: загружаем .layout.yaml, рендерим с синтетическими данными и
// проверяем смысловые маркеры (метки колонок, итоги). Итог теперь РАБОТАЕТ —
// в legacy footer был пуст.
func TestMigratedExamples_RenderSmoke(t *testing.T) {
	cases := []struct {
		path      string
		source    string   // имя ТЧ
		wantTexts []string // ожидаемые подстроки в HTML
	}{
		{
			path:   filepath.Join("..", "..", "examples", "trade", "printforms", "ПриходнаяНакладная.layout.yaml"),
			source: "Товары",
			wantTexts: []string{
				"Приходная накладная № 7",
				"Наименование",
				"Кол-во",
				"Стол",
				"700.00", // ИТОГ по Сумме (300+400)
			},
		},
		{
			path:   filepath.Join("..", "..", "examples", "accounting", "printforms", "счёт_фактура.layout.yaml"),
			source: "Товары",
			wantTexts: []string{
				"Счёт-фактура № 99",
				"Стоимость без НДС",
				"Сумма НДС",
			},
		},
	}

	for _, tc := range cases {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			lt, err := LoadLayout(tc.path)
			if err != nil {
				t.Fatalf("LoadLayout %s: %v", tc.path, err)
			}
			ctx := &RenderContext{
				Document: map[string]any{
					"Номер":          numForPath(tc.path),
					"Поставщик":      "ref-p",
					"Склад":          "ref-w",
					"Организация":    "ref-o",
					"Покупатель":     "ref-b",
					"Сумма":          700.0,
					"СуммаНДС":       0.0,
					"СуммаДокумента": 700.0,
					"НДС":            0.0,
				},
				Refs: map[string]map[string]any{
					"ref-p":  {"Наименование": "ООО Поставщик", "наименование": "ООО Поставщик"},
					"ref-w":  {"Наименование": "Главный склад", "наименование": "Главный склад"},
					"ref-o":  {"Наименование": "ООО Орг", "наименование": "ООО Орг"},
					"ref-b":  {"Наименование": "ИП Иванов", "наименование": "ИП Иванов"},
					"ref-g1": {"Наименование": "Стол", "наименование": "Стол"},
					"ref-g2": {"Наименование": "Стул", "наименование": "Стул"},
				},
				TableParts: map[string][]map[string]any{
					tc.source: {
						{"Номенклатура": "ref-g1", "Количество": 1.0, "Цена": 300.0, "Сумма": 300.0, "СтавкаНДС": "20%", "НДС": 0.0},
						{"Номенклатура": "ref-g2", "Количество": 1.0, "Цена": 400.0, "Сумма": 400.0, "СтавкаНДС": "20%", "НДС": 0.0},
					},
				},
			}
			doc, err := BuildSheet(lt, ctx)
			if err != nil {
				t.Fatalf("BuildSheet: %v", err)
			}
			html := doc.HTML(sheet.HTMLOptions{})
			for _, w := range tc.wantTexts {
				if !strings.Contains(html, w) {
					t.Errorf("в HTML %s нет ожидаемого текста %q", filepath.Base(tc.path), w)
				}
			}
		})
	}
}

func TestArchiveCoverExampleRenderSmoke(t *testing.T) {
	lt, err := LoadLayout(filepath.Join("..", "..", "examples", "archive", "printforms", "ОбложкаДела.layout.yaml"))
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if lt.Page == nil || lt.Page.Format != "229x162mm" {
		t.Fatalf("page format = %#v, want 229x162mm", lt.Page)
	}

	ctx := &RenderContext{
		Document: map[string]any{
			"Номер":         "Д-000042",
			"Дата":          "14.07.2026",
			"ИндексДела":    "01-04",
			"ЗаголовокДела": "Договоры поставки и акты сверки",
			"СрокХранения":  "5 лет",
			"Ответственный": "Секретарь архива",
		},
		TableParts: map[string][]map[string]any{
			"Документы": {
				{"Индекс": "01-04/1", "ДатаДокумента": "10.01.2026", "Заголовок": "Договор поставки", "Листы": "1-8"},
				{"Индекс": "01-04/2", "ДатаДокумента": "15.01.2026", "Заголовок": "Акт сверки", "Листы": "9-12"},
			},
		},
	}
	doc, err := BuildSheet(lt, ctx)
	if err != nil {
		t.Fatalf("BuildSheet: %v", err)
	}
	html := doc.HTML(sheet.HTMLOptions{})
	for _, want := range []string{"ONEBASE", "ДЕЛО № 01-04", "229 x 162 мм", "Договоры поставки"} {
		if !strings.Contains(html, want) {
			t.Fatalf("в HTML нет %q", want)
		}
	}
	if _, err := doc.PDF(sheet.PDFOptions{Title: "Обложка дела"}); err != nil {
		t.Fatalf("PDF: %v", err)
	}
}

func numForPath(p string) string {
	if strings.Contains(p, "Приходная") {
		return "7"
	}
	return "99"
}
