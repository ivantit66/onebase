package compose

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Регрессия: числовая колонка, пришедшая из драйвера как []byte (SQLite), должна
// агрегироваться, а не выпадать как «не число».
func TestExportToDecimal_Bytes(t *testing.T) {
	d, ok := ExportToDecimal([]byte("12.5"))
	if !ok {
		t.Fatalf("[]byte не распознан как число")
	}
	if !d.Equal(decimal.RequireFromString("12.5")) {
		t.Fatalf("ожидалось 12.5, получено %s", d)
	}
	if _, ok := ExportToDecimal([]byte("не число")); ok {
		t.Errorf("нечисловой []byte не должен парситься")
	}
}
