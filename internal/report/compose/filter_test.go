package compose

import (
	"testing"

	"github.com/ivantit66/onebase/internal/report"
)

func TestApplyFilters(t *testing.T) {
	rows := []Row{
		{"Товар": "Яблоки", "Сумма": "150"},
		{"Товар": "Груши", "Сумма": "80"},
		{"Товар": "Ананас", "Сумма": "200"},
	}

	// Числовой отбор > 100 (decimal-сравнение).
	got := ApplyFilters(rows, []report.Filter{{Field: "Сумма", Op: "gt", Value: "100"}})
	if len(got) != 2 {
		t.Fatalf("gt 100: ожидали 2, получили %d: %+v", len(got), got)
	}

	// contains без учёта регистра.
	got = ApplyFilters(rows, []report.Filter{{Field: "Товар", Op: "contains", Value: "ЯБЛ"}})
	if len(got) != 1 || got[0]["Товар"] != "Яблоки" {
		t.Fatalf("contains ябл: %+v", got)
	}

	// Пустой список фильтров → все строки.
	if all := ApplyFilters(rows, nil); len(all) != 3 {
		t.Fatalf("пустой фильтр: ожидали 3, получили %d", len(all))
	}
}

func TestApplyFiltersEdgeCases(t *testing.T) {
	rows := []Row{{"Товар": "Яблоки", "Сумма": "150"}}

	// Неизвестное поле + числовой отбор → строка исключается.
	if got := ApplyFilters(rows, []report.Filter{{Field: "Нет", Op: "gt", Value: "1"}}); len(got) != 0 {
		t.Fatalf("неизвестное поле: ожидали 0, получили %+v", got)
	}

	// Неизвестный оператор → фильтр игнорируется (строка проходит).
	if got := ApplyFilters(rows, []report.Filter{{Field: "Сумма", Op: "between", Value: "1"}}); len(got) != 1 {
		t.Fatalf("неизвестный Op: ожидали 1, получили %+v", got)
	}

	// Регистронезависимость: поле фильтра в исходном регистре, ключ строки в
	// нижнем (как колонки из RunQuery).
	lower := []Row{{"сумма": "150"}}
	if got := ApplyFilters(lower, []report.Filter{{Field: "Сумма", Op: "gt", Value: "100"}}); len(got) != 1 {
		t.Fatalf("регистронезависимость: %+v", got)
	}

	// Строковое равенство (нечисловые значения).
	str := []Row{{"Статус": "Проведён"}, {"Статус": "Черновик"}}
	if got := ApplyFilters(str, []report.Filter{{Field: "Статус", Op: "eq", Value: "Проведён"}}); len(got) != 1 {
		t.Fatalf("eq строка: %+v", got)
	}
}
