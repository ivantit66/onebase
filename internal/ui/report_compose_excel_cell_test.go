package ui

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/report"
)

// C1/C4: ячейка показателя в Excel должна совпадать с HTML — применять Format
// (иначе процент 0.123 уходил бы в XLSX как 0.123, а не «12,3%»), а нечисловой
// показатель детали показывать текстом, а не нулём.
func TestMeasureCellForExcel(t *testing.T) {
	// nil → пустая ячейка
	if got := measureCellForExcel(nil, report.Measure{Field: "x"}); got != nil {
		t.Errorf("nil → ожидался nil, получено %v", got)
	}
	// формат процента применён (строка, содержит 12 и %)
	got := measureCellForExcel(0.123, report.Measure{Field: "x", Format: "0.0%"})
	s, ok := got.(string)
	if !ok || !strings.Contains(s, "%") || !strings.Contains(s, "12") {
		t.Errorf("формат 0.0%% не применён к 0.123: %v", got)
	}
	// без формата → float64 (число)
	if got := measureCellForExcel(5, report.Measure{Field: "x"}); got != float64(5) {
		t.Errorf("без формата ожидался float64(5), получено %T %v", got, got)
	}
	// нечисловое значение → текст, не 0
	if got := measureCellForExcel("н/д", report.Measure{Field: "x"}); got != "н/д" {
		t.Errorf("нечисловой показатель → ожидался текст «н/д», получено %v", got)
	}
}
