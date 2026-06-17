package launcher

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ivantit66/onebase/internal/report"
	"gopkg.in/yaml.v3"
)

// parseCompositionForm собирает report.Composition из полей формы comp.*.
// (nil, false) — маркера comp.present нет (composition не трогаем).
// (nil, true)  — включён, но пуст (composition очищается).
func parseCompositionForm(f url.Values) (*report.Composition, bool) {
	if f.Get("comp.present") == "" {
		return nil, false
	}
	c := &report.Composition{}

	// Группировки
	for i := 0; ; i++ {
		v := strings.TrimSpace(f.Get("comp.grouping." + strconv.Itoa(i)))
		if v == "" {
			break
		}
		c.Groupings = append(c.Groupings, v)
	}

	// Показатели
	for i := 0; ; i++ {
		p := "comp.measure." + strconv.Itoa(i)
		fld := strings.TrimSpace(f.Get(p + ".field"))
		if fld == "" {
			break
		}
		c.Measures = append(c.Measures, report.Measure{
			Field:  fld,
			Agg:    f.Get(p + ".agg"),
			Title:  strings.TrimSpace(f.Get(p + ".title")),
			Align:  f.Get(p + ".align"),
			Format: strings.TrimSpace(f.Get(p + ".format")),
			Expr:   strings.TrimSpace(f.Get(p + ".expr")),
		})
	}

	// Итоги и детальные записи
	c.Totals.Grand = f.Get("comp.totals.grand") != ""
	c.Totals.Subtotals = f.Get("comp.totals.subtotals") != ""
	c.Detail = f.Get("comp.detail") != ""
	c.DetailLink = strings.TrimSpace(f.Get("comp.detail_link"))
	c.DetailEntity = strings.TrimSpace(f.Get("comp.detail_entity"))

	// Сортировка
	for i := 0; ; i++ {
		p := "comp.sort." + strconv.Itoa(i)
		fld := strings.TrimSpace(f.Get(p + ".field"))
		if fld == "" {
			break
		}
		c.Sort = append(c.Sort, report.SortKey{Field: fld, Dir: f.Get(p + ".dir")})
	}

	// Условное оформление
	for i := 0; ; i++ {
		p := "comp.cond." + strconv.Itoa(i)
		when := strings.TrimSpace(f.Get(p + ".when"))
		if when == "" {
			break
		}
		color := strings.TrimSpace(f.Get(p + ".color"))
		if color == "#000000" {
			color = ""
		}
		background := strings.TrimSpace(f.Get(p + ".background"))
		if background == "#ffffff" {
			background = ""
		}
		c.Conditional = append(c.Conditional, report.CondRule{
			When:  when,
			Field: strings.TrimSpace(f.Get(p + ".field")),
			Style: report.CellStyle{
				Color:      color,
				Background: background,
				Bold:       f.Get(p+".bold") != "",
				Italic:     f.Get(p+".italic") != "",
			},
		})
	}

	// Диаграмма (пустой type → нет диаграммы)
	if ct := strings.TrimSpace(f.Get("comp.chart.type")); ct != "" {
		var series []string
		for _, s := range strings.Split(f.Get("comp.chart.series"), ",") {
			if s = strings.TrimSpace(s); s != "" {
				series = append(series, s)
			}
		}
		c.Chart = &report.ChartSpec{
			Type:     ct,
			Category: strings.TrimSpace(f.Get("comp.chart.category")),
			Series:   series,
		}
	}

	// Включён, но пуст → сигнал очистки (nil, true). Очищаем только когда пусто
	// ВСЁ — иначе правила оформления / сортировка / график без группировок молча
	// терялись бы при сохранении (work-in-progress конструктора).
	if len(c.Groupings) == 0 && len(c.Measures) == 0 && len(c.Conditional) == 0 && len(c.Sort) == 0 && c.Chart == nil {
		return nil, true
	}
	return c, true
}

// applyReportComposition обновляет блок composition в сыром YAML отчёта по форме.
// Если в форме нет comp.present — YAML возвращается без изменений. Иначе блок
// composition точечно перезаписывается (или удаляется, если форма пуста) прямо
// в дереве YAML, чтобы остальные поля отчёта — мультиязычные titles, params и
// любые будущие — сохранялись как есть. Раньше функция round-trip'ила YAML через
// частичную структуру без поля Titles и молча теряла titles (issue #86).
func applyReportComposition(raw []byte, f url.Values) ([]byte, error) {
	c, present := parseCompositionForm(f)
	if !present {
		return raw, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("applyReportComposition: ожидалось YAML-отображение в корне отчёта")
	}
	var val any
	if c != nil {
		val = c // c==nil (пустая форма) → ключ composition удаляется
	}
	if err := setYAMLMapField(root.Content[0], "composition", val); err != nil {
		return nil, err
	}
	return yaml.Marshal(&root)
}

// setYAMLMapField устанавливает значение ключа в mapping-узле YAML, сохраняя
// порядок и форматирование прочих ключей. val==nil удаляет ключ. Позволяет
// точечно править одно поле документа без round-trip через типизированную
// структуру (которая молча теряет неперечисленные в ней поля).
func setYAMLMapField(m *yaml.Node, key string, val any) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if val == nil {
				m.Content = append(m.Content[:i], m.Content[i+2:]...)
				return nil
			}
			var vn yaml.Node
			if err := vn.Encode(val); err != nil {
				return err
			}
			m.Content[i+1] = &vn
			return nil
		}
	}
	if val == nil {
		return nil
	}
	var vn yaml.Node
	if err := vn.Encode(val); err != nil {
		return err
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&vn)
	return nil
}
