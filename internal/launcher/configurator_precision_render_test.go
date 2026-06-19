package launcher

import (
	"bytes"
	"strings"
	"testing"
)

func renderCfgTree(t *testing.T, data *configuratorData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "tab-tree", data); err != nil {
		t.Fatalf("ExecuteTemplate tab-tree: %v", err)
	}
	return buf.String()
}

// Регистр накопления: у числовых полей должны быть инпуты Длина/Точность, а у
// каждой секции — кнопка «+ Добавить» (раньше их не было вовсе, нельзя было
// добавить поле через UI).
func TestRegisterDetail_PrecisionAndAddButtons(t *testing.T) {
	data := &configuratorData{
		Base: &Base{ID: "b", Name: "X", ConfigSource: "file"}, Lang: "ru", Tab: "tree",
		Registers: []cfgRegister{{
			Name:       "Остатки",
			Dimensions: []cfgField{{Name: "Товар", Type: "string"}},
			Resources:  []cfgField{{Name: "Сумма", Type: "number", Length: 15, Scale: 2}},
		}},
	}
	html := renderCfgTree(t, data)
	for _, want := range []string{
		`name="res.0.length"`,
		`name="res.0.scale"`,
		`cfgAddField('rg-dim-Остатки','new_dim','')`,
		`cfgAddField('rg-res-Остатки','new_res','')`,
		`cfgAddField('rg-attr-Остатки','new_attr','')`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("register-detail: нет фрагмента %q", want)
		}
	}
}

// Регистр сведений: у числовых измерений/ресурсов — инпуты Длина/Точность.
func TestInfoRegDetail_Precision(t *testing.T) {
	data := &configuratorData{
		Base: &Base{ID: "b", Name: "X", ConfigSource: "file"}, Lang: "ru", Tab: "tree",
		InfoRegisters: []cfgInfoRegister{{
			Name:       "Курсы",
			Dimensions: []cfgField{{Name: "Валюта", Type: "string"}},
			Resources:  []cfgField{{Name: "Курс", Type: "number", Length: 10, Scale: 4}},
		}},
	}
	html := renderCfgTree(t, data)
	for _, want := range []string{`name="res.0.length"`, `name="res.0.scale"`} {
		if !strings.Contains(html, want) {
			t.Errorf("inforeg-detail: нет фрагмента %q", want)
		}
	}
}

// План счетов: у числового ресурса — инпуты Длина/Точность.
func TestAccountRegDetail_Precision(t *testing.T) {
	data := &configuratorData{
		Base: &Base{ID: "b", Name: "X", ConfigSource: "file"}, Lang: "ru", Tab: "tree",
		AccountRegisters: []cfgAccountRegister{{
			Name:      "Бухучёт",
			Resources: []cfgField{{Name: "Сумма", Type: "number", Length: 18, Scale: 2}},
		}},
	}
	html := renderCfgTree(t, data)
	for _, want := range []string{`name="res.0.length"`, `name="res.0.scale"`} {
		if !strings.Contains(html, want) {
			t.Errorf("accountreg-detail: нет фрагмента %q", want)
		}
	}
}

// Константа: у типа «число» — инпуты Длина/Точность (имена length/scale).
func TestConstantDetail_Precision(t *testing.T) {
	data := &configuratorData{
		Base: &Base{ID: "b", Name: "X", ConfigSource: "file"}, Lang: "ru", Tab: "tree",
		Constants: []cfgConstant{{Name: "СтавкаНДС", Type: "number", Length: 5, Scale: 2, Label: "НДС"}},
	}
	html := renderCfgTree(t, data)
	for _, want := range []string{`name="length"`, `name="scale"`} {
		if !strings.Contains(html, want) {
			t.Errorf("constant-detail: нет фрагмента %q", want)
		}
	}
}
