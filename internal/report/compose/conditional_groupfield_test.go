package compose

import (
	"testing"

	"github.com/ivantit66/onebase/internal/report"
	"github.com/shopspring/decimal"
)

// groupFieldEval — Evaluator, чьё условие ссылается на ПОЛЕ ГРУППИРОВКИ (не меру).
type groupFieldEval struct{}

func (groupFieldEval) EvalBool(_ string, row Row) (bool, error) {
	return row["М"] == "Убыток", nil
}
func (groupFieldEval) EvalNum(string, Row) (decimal.Decimal, bool, error) {
	return decimal.Zero, false, nil
}

// C3: условное оформление итоговой строки группы должно видеть поле группировки
// (раньше итогу была доступна только карта мер, поэтому правило вида
// «Регион = "Юг"» никогда не срабатывало на подытоге).
func TestConditionalOnGroups_GroupingFieldVisible(t *testing.T) {
	rows := []Row{
		{"М": "Убыток", "Сумма": "-100"},
		{"М": "Прибыль", "Сумма": "50"},
	}
	spec := report.Composition{
		Groupings:   []string{"М"},
		Measures:    []report.Measure{{Field: "Сумма", Agg: "sum"}},
		Conditional: []report.CondRule{{When: `М = "Убыток"`, Field: "", Style: report.CellStyle{Color: "#c00"}}},
	}
	res, _ := Compose(rows, spec, groupFieldEval{})
	var loss *Group
	for _, g := range res.Groups {
		if g.Key == "Убыток" {
			loss = g
		}
	}
	if loss == nil || loss.Styles[""].Color != "#c00" {
		t.Fatalf("правило по полю группировки должно стилизовать итог группы, получено: %+v", loss)
	}
}
