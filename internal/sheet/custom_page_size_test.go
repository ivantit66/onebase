package sheet

import (
	"bytes"
	"math"
	"testing"
)

func TestCustomFormatMM(t *testing.T) {
	cases := []struct {
		in     string
		w, h   float64
		wantOk bool
	}{
		{"229x162mm", 229, 162, true},
		{"229x162", 229, 162, true},
		{"229 × 162 мм", 229, 162, true},
		{"229Х162", 229, 162, true},     // кириллическая Х
		{"210,5x297", 210.5, 297, true}, // запятая-десятичный
		{"100*50", 100, 50, true},
		{"A4", 0, 0, false},
		{"A5", 0, 0, false},
		{"Letter", 0, 0, false},
		{"", 0, 0, false},
		{"229x", 0, 0, false},
		{"x162", 0, 0, false},
		{"0x100", 0, 0, false},  // нулевая ширина
		{"100x-5", 0, 0, false}, // отрицательная (не матчится регекспом)
	}
	for _, c := range cases {
		w, h, ok := customFormatMM(c.in)
		if ok != c.wantOk {
			t.Errorf("customFormatMM(%q) ok=%v, ожидалось %v", c.in, ok, c.wantOk)
			continue
		}
		if ok && (math.Abs(w-c.w) > 1e-9 || math.Abs(h-c.h) > 1e-9) {
			t.Errorf("customFormatMM(%q) = (%g, %g), ожидалось (%g, %g)", c.in, w, h, c.w, c.h)
		}
	}
}

func TestPageSizeMM(t *testing.T) {
	cases := []struct {
		name string
		page PageSetup
		w, h float64
	}{
		{"кастом литерально", PageSetup{Format: "229x162mm"}, 229, 162},
		{"кастом игнорирует ориентацию", PageSetup{Format: "229x162mm", Orientation: "landscape"}, 229, 162},
		{"A4 портрет", PageSetup{Format: "A4"}, 210, 297},
		{"A4 ландшафт свопается", PageSetup{Format: "A4", Orientation: "landscape"}, 297, 210},
	}
	for _, c := range cases {
		w, h := pageSizeMM(c.page)
		if math.Abs(w-c.w) > 1e-6 || math.Abs(h-c.h) > 1e-6 {
			t.Errorf("%s: pageSizeMM = (%g, %g), ожидалось (%g, %g)", c.name, w, h, c.w, c.h)
		}
	}
}

// TestPDFCustomPageSizeMediaBox — кастомный размер попадает в MediaBox PDF
// (fpdf пишет в пунктах: мм·72/25.4). 229×162мм → 649.13×459.21pt.
func TestPDFCustomPageSizeMediaBox(t *testing.T) {
	d := NewDocument()
	d.Page.Format = "229x162mm"
	d.GetOrCreateCell(0, 0).Text = "Конверт"

	b, err := d.PDF(PDFOptions{})
	if err != nil {
		t.Fatalf("PDF(): %v", err)
	}
	want := "/MediaBox [0 0 649.13 459.21]"
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("PDF не содержит %q (кастомный размер не применился)", want)
	}
}

// TestPDFCustomPageSizeAffectsPagination — маленький кастомный лист даёт больше
// страниц, чем A4, на одном и том же содержимом (размер прошёл в пагинацию).
func TestPDFCustomPageSizeAffectsPagination(t *testing.T) {
	build := func(format string) int {
		d := NewDocument()
		d.Page.Format = format
		for r := 0; r < 40; r++ {
			d.GetOrCreateCell(r, 0).Text = "строка описи"
		}
		b, err := d.PDF(PDFOptions{})
		if err != nil {
			t.Fatalf("PDF(%q): %v", format, err)
		}
		return countPages(b)
	}
	a4 := build("A4")
	small := build("80x80mm")
	if small <= a4 {
		t.Fatalf("ожидалось больше страниц на 80x80mm (%d), чем на A4 (%d)", small, a4)
	}
}
