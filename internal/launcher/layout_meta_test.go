package launcher

import (
	"encoding/json"
	"strings"
	"testing"
)

// План 64, этап 5b, блок B: метаданные панели «Данные» и её рендер.

// buildLayoutMeta собирает реквизиты/ТЧ сущности, константы и карту макет→документ.
func TestBuildLayoutMeta(t *testing.T) {
	d := &configuratorData{
		Entities: []cfgEntity{{
			Name: "Реализация",
			Kind: "Документ",
			Fields: []cfgField{
				{Name: "Номер", Type: "string"},
				{Name: "Контрагент", Type: "ref", RefEntity: "Контрагенты"},
			},
			TableParts: []cfgTablePart{{
				Name: "Товары",
				Fields: []cfgField{
					{Name: "Номенклатура", Type: "ref", RefEntity: "Номенклатура"},
					{Name: "Сумма", Type: "number"},
				},
			}},
		}},
		Constants: []cfgConstant{{Name: "НаименованиеОрганизации"}, {Name: "ИНН"}},
		DSLPrintForms: []cfgDSLPrintForm{
			{Name: "Накладная", Document: "Реализация", HasLayout: true},
		},
	}

	raw := string(buildLayoutMeta(d))
	var meta ldMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatalf("buildLayoutMeta produced invalid JSON: %v\n%s", err, raw)
	}

	ent, ok := meta.Entities["Реализация"]
	if !ok {
		t.Fatalf("нет сущности Реализация в meta: %+v", meta.Entities)
	}
	if len(ent.Fields) != 2 || ent.Fields[1].Ref != "Контрагенты" {
		t.Errorf("реквизиты сущности неверны: %+v", ent.Fields)
	}
	if len(ent.TableParts) != 1 || ent.TableParts[0].Name != "Товары" || len(ent.TableParts[0].Fields) != 2 {
		t.Errorf("ТЧ сущности неверны: %+v", ent.TableParts)
	}
	if ent.TableParts[0].Fields[0].Ref != "Номенклатура" {
		t.Errorf("ref колонки ТЧ потерян: %+v", ent.TableParts[0].Fields[0])
	}
	if len(meta.Constants) != 2 {
		t.Errorf("констант %d, want 2", len(meta.Constants))
	}
	// карта макет → документ, ключ в нижнем регистре.
	if meta.FormDoc["накладная"] != "Реализация" {
		t.Errorf("formDoc[накладная] = %q, want Реализация (%+v)", meta.FormDoc["накладная"], meta.FormDoc)
	}
}

// Пустые данные дают валидный JSON-объект.
func TestBuildLayoutMeta_Empty(t *testing.T) {
	raw := string(buildLayoutMeta(&configuratorData{}))
	var meta ldMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatalf("пустой meta невалиден: %v\n%s", err, raw)
	}
}

// HTML панели редактора содержит контейнер дерева данных и select форматтера.
func TestLayoutEditor_DataPanelControls(t *testing.T) {
	html := renderLayoutPanelTree(t)
	for _, sub := range []string{
		`id="vdata-Накладная"`,    // контейнер дерева данных
		`id="vp-fmt-Накладная"`,   // select форматтера
		`ldSetFormat('Накладная'`, // обработчик форматтера
	} {
		if !strings.Contains(html, sub) {
			t.Errorf("в HTML панели редактора нет фрагмента: %q", sub)
		}
	}
}

// JS-редактор содержит функции привязки данных, форматов и повтора областей.
func TestLayoutEditor_DataBindingJS(t *testing.T) {
	js := configuratorJS(t)
	for _, sub := range []string{
		"function renderDataPanel",      // дерево данных
		"function onDataFieldClick",     // клик по полю → параметр
		"function _ldBindParameter",     // автопривязка по имени
		"function ldSetFormat",          // форматтер
		"function ldSetAreaRepeat",      // повтор по ТЧ
		"function ldSetRepeatHeader",    // повтор на каждой странице
		"function _ldAreaBindingRow",    // строка привязки области
		"var _ldMeta = window.__ldMeta", // метаданные из bootstrap (план 55 фаза 2b-1)
	} {
		if !strings.Contains(js, sub) {
			t.Errorf("в JS редактора нет: %q", sub)
		}
	}
}
