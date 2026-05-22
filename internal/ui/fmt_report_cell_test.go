package ui

import (
	"testing"
	"time"
)

func TestFmtReportCell(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil → пусто", nil, ""},
		{"дата без времени", time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC), "22.05.2026"},
		{"дата со временем", time.Date(2026, 5, 22, 13, 5, 9, 0, time.UTC), "22.05.2026 13:05:09"},
		{"целое float64 → разделители", float64(1234567), "1 234 567"},
		{"дробное float64", float64(1234.5), "1234.50"},
		{"int", 1000000, "1 000 000"},
		{"int64", int64(2500), "2 500"},
		{"строка-дата ISO", "2026-05-22", "22.05.2026"},
		{"строка-дата с временем", "2026-05-22 13:05:09", "22.05.2026 13:05:09"},
		{"обычная строка", "Тумбочка", "Тумбочка"},
		{"короткая строка не парсится как дата", "ООО", "ООО"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fmtReportCell(c.in); got != c.want {
				t.Errorf("fmtReportCell(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
