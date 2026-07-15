package auth

import (
	"testing"
	"time"
)

// parseSessionTime нормализует expires_at независимо от диалекта:
// PostgreSQL отдаёт time.Time, SQLite — TEXT (строку/[]byte), возможно
// в Go-формате (с зоной/MST), который драйвер не парсит сам.
func TestParseSessionTime(t *testing.T) {
	t.Run("time.Time как есть", func(t *testing.T) {
		want := time.Date(2026, 5, 22, 3, 16, 13, 0, time.UTC)
		if got := parseSessionTime(want); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("RFC3339 строка", func(t *testing.T) {
		if got := parseSessionTime("2026-05-22T03:16:13Z"); got.IsZero() {
			t.Error("RFC3339 не распарсился")
		}
	})

	t.Run("Go-формат с зоной MST", func(t *testing.T) {
		// именно этот формат SQLite хранил для expires_at и не мог
		// сконвертировать при Scan → пустая страница активных сессий
		got := parseSessionTime("2026-05-23 03:16:13 +0300 MSK")
		if got.IsZero() {
			t.Fatal("Go-формат времени не распарсился")
		}
		if got.Year() != 2026 || got.Day() != 23 || got.Hour() != 3 {
			t.Errorf("неверный разбор: %v", got)
		}
	})

	t.Run("SQLite datetime", func(t *testing.T) {
		if got := parseSessionTime("2026-05-22 03:16:13"); got.IsZero() {
			t.Error("SQLite datetime не распарсился")
		}
	})

	t.Run("SQLite UTC with offset", func(t *testing.T) {
		if got := parseSessionTime("2026-05-22 03:16:13+00:00"); got.IsZero() {
			t.Error("SQLite UTC datetime не распарсился")
		}
	})

	t.Run("[]byte", func(t *testing.T) {
		if got := parseSessionTime([]byte("2026-05-22T03:16:13Z")); got.IsZero() {
			t.Error("[]byte не распарсился")
		}
	})

	t.Run("мусор → нулевое время", func(t *testing.T) {
		if got := parseSessionTime("не дата"); !got.IsZero() {
			t.Errorf("мусор → %v, ожидалось нулевое", got)
		}
	})
}
