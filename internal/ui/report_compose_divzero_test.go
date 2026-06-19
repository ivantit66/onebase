package ui

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/report/compose"
)

// TestComposeDivByZero_SilentEmptyCell: деление на ноль в вычисляемом показателе
// даёт пустую ячейку (как в 1С) БЕЗ предупреждения — это «неопределённое
// значение», а не runtime-ошибка. Проверяем на РЕАЛЬНОМ интерпретаторе (он
// возбуждает «Деление на ноль» исключением), а не на синтетическом моке.
func TestComposeDivByZero_SilentEmptyCell(t *testing.T) {
	rows := []compose.Row{{"М": "A", "Выручка": "0", "Прибыль": "10"}}
	spec := report.Composition{
		Groupings: []string{"М"},
		Measures: []report.Measure{
			{Field: "Прибыль", Agg: "sum"},
			{Field: "Выручка", Agg: "sum"},
			{Field: "Маржа", Expr: "Прибыль / Выручка"},
		},
	}
	res, _ := compose.Compose(rows, spec, newInterpEvaluator(interpreter.New()))
	if len(res.Warnings) != 0 {
		t.Fatalf("деление на ноль не должно давать предупреждение, got %v", res.Warnings)
	}
	if res.Grand["Маржа"] != nil {
		t.Fatalf("ячейка деления на ноль должна быть пустой (nil), got %v", res.Grand["Маржа"])
	}
}

// TestComposeExprMeasure_CaseInsensitiveOrder: ссылка одного вычисляемого
// показателя на другой в ДРУГОМ регистре (интерпретатор регистронезависим)
// корректно определяет зависимость и порядок вычисления, даже если зависимый
// объявлен раньше.
func TestComposeExprMeasure_CaseInsensitiveOrder(t *testing.T) {
	rows := []compose.Row{{"М": "A", "Сумма": "10"}}
	spec := report.Composition{
		Groupings: []string{"М"},
		Measures: []report.Measure{
			{Field: "Сумма", Agg: "sum"},
			{Field: "Четверная", Expr: "двойная * 2"}, // ссылка в нижнем регистре, объявлена раньше
			{Field: "Двойная", Expr: "Сумма * 2"},
		},
	}
	res, _ := compose.Compose(rows, spec, newInterpEvaluator(interpreter.New()))
	if len(res.Warnings) != 0 {
		t.Fatalf("неожиданные предупреждения: %v", res.Warnings)
	}
	d, ok := compose.ExportToDecimal(res.Grand["Четверная"])
	if !ok || !d.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("Четверная должна разрешиться в 40 (зависимость по регистронезависимому имени), got %v", res.Grand["Четверная"])
	}
}
