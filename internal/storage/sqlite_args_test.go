package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// normalizeSQLiteArgs должен приводить time.Time к UTC-строке,
// которую понимают strftime/date SQLite, и не трогать прочие значения.
func TestNormalizeSQLiteArgs(t *testing.T) {
	loc := time.FixedZone("MSK", 3*60*60)
	tm := time.Date(2026, 1, 3, 0, 0, 0, 0, loc)

	got := normalizeSQLiteArgs([]any{tm, &tm, "строка", 42, nil, (*time.Time)(nil)})
	if got[0] != "2026-01-02 21:00:00+00:00" {
		t.Errorf("time.Time: got %v, want 2026-01-02 21:00:00+00:00", got[0])
	}
	if got[1] != "2026-01-02 21:00:00+00:00" {
		t.Errorf("*time.Time: got %v, want 2026-01-02 21:00:00+00:00", got[1])
	}
	if got[2] != "строка" || got[3] != 42 || got[4] != nil {
		t.Errorf("прочие значения изменены: %v", got)
	}
	if got[5] != nil {
		t.Errorf("nil *time.Time должен дать nil, got %v", got[5])
	}
}

// Регресс на баг period: time.Time, записанный через Exec, должен храниться в
// формате, который понимают strftime/date SQLite. До фикса драйвер modernc
// сохранял Go-строку `… +0300 MSK`, и strftime возвращал NULL → группировка по
// месяцам молча ломалась (.Обороты(,,Месяц), НАЧАЛОПЕРИОДА, Год/Месяц/День).
func TestSQLiteTimeArg_StrftimeParseable(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(ctx, "CREATE TABLE t(period TEXT)"); err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("MSK", 3*60*60)
	tm := time.Date(2026, 1, 3, 0, 0, 0, 0, loc)
	if _, err := db.Exec(ctx, "INSERT INTO t(period) VALUES(?)", tm); err != nil {
		t.Fatal(err)
	}

	var month string
	if err := db.QueryRow(ctx, "SELECT strftime('%Y-%m', period) FROM t").Scan(&month); err != nil {
		t.Fatal(err)
	}
	if month != "2026-01" {
		t.Errorf("strftime('%%Y-%%m', period) = %q, want \"2026-01\" — period записан в нераспарсиваемом формате", month)
	}
}
