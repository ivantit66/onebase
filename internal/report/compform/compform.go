// Package compform собирает report.Composition из полей HTML-формы (comp.*).
// Вынесен из internal/launcher, чтобы один и тот же сборщик использовался и
// конфигуратором (сохранение composition в YAML), и рантаймом отчёта
// (пользовательские настройки на форме отчёта, план 70). Зависит только от
// internal/report и stdlib.
package compform

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/ivantit66/onebase/internal/report"
)

// Parse собирает report.Composition из полей формы comp.*.
// (nil, false) — маркера comp.present нет (composition не трогаем).
// (nil, true)  — включён, но пуст (composition очищается).
func Parse(f url.Values) (*report.Composition, bool) {
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

	// Колонки кросс-таблицы (непусто → режим кросс-таблицы)
	for i := 0; ; i++ {
		v := strings.TrimSpace(f.Get("comp.column." + strconv.Itoa(i)))
		if v == "" {
			break
		}
		c.Columns = append(c.Columns, v)
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
	if len(c.Groupings) == 0 && len(c.Columns) == 0 && len(c.Measures) == 0 && len(c.Conditional) == 0 && len(c.Sort) == 0 && c.Chart == nil {
		return nil, true
	}
	return c, true
}
