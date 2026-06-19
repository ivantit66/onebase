package compose

import (
	"fmt"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ivantit66/onebase/internal/report"
)

// errEval — Evaluator, который всегда падает с runtime-ошибкой (имитация
// ошибки исполнения выражения, не пойманной на этапе onebase check).
type errEval struct{}

func (errEval) EvalBool(string, Row) (bool, error) {
	return false, fmt.Errorf("boom-bool")
}
func (errEval) EvalNum(string, Row) (decimal.Decimal, bool, error) {
	return decimal.Zero, false, fmt.Errorf("boom-num")
}

// depEval разрешает цепочку вычисляемых показателей по уже агрегированным
// значениям: "Сумма*2" и "Двойная*2" (последний читает результат первого).
type depEval struct{}

func (depEval) EvalBool(string, Row) (bool, error) { return false, nil }
func (depEval) EvalNum(expr string, row Row) (decimal.Decimal, bool, error) {
	switch expr {
	case "Сумма*2":
		if d, ok := toDecimal(row["Сумма"]); ok {
			return d.Mul(decimal.NewFromInt(2)), true, nil
		}
	case "Двойная*2":
		if d, ok := toDecimal(row["Двойная"]); ok {
			return d.Mul(decimal.NewFromInt(2)), true, nil
		}
	}
	return decimal.Zero, false, nil
}

// cycleEval разрешает взаимно-зависимые выражения "A+1"/"B+1" (для проверки
// защиты от цикла — не должно зависать).
type cycleEval struct{}

func (cycleEval) EvalBool(string, Row) (bool, error) { return false, nil }
func (cycleEval) EvalNum(string, Row) (decimal.Decimal, bool, error) {
	return decimal.Zero, false, nil
}

// TestComposeWarning_MeasureError: runtime-ошибка вычисляемого показателя больше
// не глотается — попадает в Result.Warnings, а ячейка остаётся пустой.
func TestComposeWarning_MeasureError(t *testing.T) {
	rows := []Row{{"М": "A", "Сумма": "10"}}
	spec := report.Composition{
		Groupings: []string{"М"},
		Measures: []report.Measure{
			{Field: "Сумма", Agg: "sum"},
			{Field: "Вычисл", Expr: "что-то"},
		},
	}
	res, _ := Compose(rows, spec, errEval{})
	if len(res.Warnings) != 1 {
		t.Fatalf("ожидалось ровно одно (дедуплицированное) предупреждение, got %v", res.Warnings)
	}
	if res.Grand["Вычисл"] != nil {
		t.Fatalf("ячейка с ошибкой должна быть пустой (nil), got %v", res.Grand["Вычисл"])
	}
}

// TestComposeWarning_ConditionError: runtime-ошибка условия оформления попадает
// в Warnings (а не молча гасит стиль).
func TestComposeWarning_ConditionError(t *testing.T) {
	rows := []Row{{"М": "A", "Сумма": "10"}}
	spec := report.Composition{
		Groupings:   []string{"М"},
		Measures:    []report.Measure{{Field: "Сумма", Agg: "sum"}},
		Conditional: []report.CondRule{{When: "плохое", Field: "", Style: report.CellStyle{Bold: true}}},
	}
	res, _ := Compose(rows, spec, errEval{})
	if len(res.Warnings) == 0 {
		t.Fatal("ожидалось предупреждение об ошибке условия оформления")
	}
}

// TestExprMeasureOrderIndependent: вычисляемый показатель, объявленный РАНЬШЕ
// своей зависимости (тоже вычисляемой), всё равно разрешается — порядок
// объявления больше не критичен.
func TestExprMeasureOrderIndependent(t *testing.T) {
	rows := []Row{{"М": "A", "Сумма": "10"}}
	spec := report.Composition{
		Groupings: []string{"М"},
		Measures: []report.Measure{
			{Field: "Сумма", Agg: "sum"},
			{Field: "Четверная", Expr: "Двойная*2"}, // объявлена ПЕРЕД зависимостью
			{Field: "Двойная", Expr: "Сумма*2"},
		},
	}
	res, _ := Compose(rows, spec, depEval{})
	decEq(t, res.Grand["Двойная"], "20")
	decEq(t, res.Grand["Четверная"], "40")
}

// TestExprMeasureCycle: взаимная зависимость не зависает и даёт предупреждение.
func TestExprMeasureCycle(t *testing.T) {
	rows := []Row{{"М": "A", "Сумма": "10"}}
	spec := report.Composition{
		Groupings: []string{"М"},
		Measures: []report.Measure{
			{Field: "A", Expr: "B+1"},
			{Field: "B", Expr: "A+1"},
		},
	}
	res, _ := Compose(rows, spec, cycleEval{})
	if len(res.Warnings) == 0 {
		t.Fatal("ожидалось предупреждение о циклической зависимости")
	}
}
