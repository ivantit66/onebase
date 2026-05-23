package storage_test

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		prefix string
		length int
		number int
		want   string
	}{
		{"ПОС-", 8, 1, "ПОС-00000001"},
		{"ПОС-", 8, 42, "ПОС-00000042"},
		{"РТ-", 6, 999, "РТ-000999"},
		{"", 5, 1, "00001"},
		{"", 3, 1000, "1000"}, // число длиннее length — не обрезаем
	}
	for _, c := range cases {
		got := storage.FormatNumber(c.prefix, c.length, c.number)
		if got != c.want {
			t.Errorf("FormatNumber(%q, %d, %d) = %q, want %q", c.prefix, c.length, c.number, got, c.want)
		}
	}
}

func TestComputePeriodKey_Year(t *testing.T) {
	num := &metadata.Numerator{Period: "year"}
	fields := map[string]any{
		"Дата": time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
	}
	got := storage.ComputePeriodKey(num, fields)
	if got != "2026" {
		t.Errorf("expected '2026', got %q", got)
	}
}

func TestComputePeriodKey_Month(t *testing.T) {
	num := &metadata.Numerator{Period: "month"}
	fields := map[string]any{
		"Дата": time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
	}
	got := storage.ComputePeriodKey(num, fields)
	if got != "2026-05" {
		t.Errorf("expected '2026-05', got %q", got)
	}
}

func TestComputePeriodKey_None(t *testing.T) {
	num := &metadata.Numerator{Period: "none"}
	fields := map[string]any{
		"Дата": time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
	}
	got := storage.ComputePeriodKey(num, fields)
	if got != "" {
		t.Errorf("expected '', got %q", got)
	}
}

func TestComputePeriodKey_NoDateField(t *testing.T) {
	num := &metadata.Numerator{Period: "year"}
	fields := map[string]any{"Название": "тест"}
	// Нет поля date — берётся текущий год, не паникует
	got := storage.ComputePeriodKey(num, fields)
	if len(got) != 4 {
		t.Errorf("expected 4-digit year, got %q", got)
	}
}

// scope: Организация — отдельный счётчик у каждой организации.
func TestComputePeriodKey_ScopeWithYear(t *testing.T) {
	num := &metadata.Numerator{Period: "year", Scope: "Организация"}
	fields := map[string]any{
		"Дата":        time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		"Организация": "uuid-org-A",
	}
	got := storage.ComputePeriodKey(num, fields)
	if got != "2026|uuid-org-A" {
		t.Errorf("expected '2026|uuid-org-A', got %q", got)
	}
}

// Разные организации дают разные ключи → отдельные счётчики.
func TestComputePeriodKey_ScopeDistinguishesOrgs(t *testing.T) {
	num := &metadata.Numerator{Period: "year", Scope: "Организация"}
	keyA := storage.ComputePeriodKey(num, map[string]any{
		"Дата":        time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		"Организация": "A",
	})
	keyB := storage.ComputePeriodKey(num, map[string]any{
		"Дата":        time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		"Организация": "B",
	})
	if keyA == keyB {
		t.Errorf("разные организации дали одинаковый ключ: %q", keyA)
	}
}

// Без периода, только scope.
func TestComputePeriodKey_ScopeOnly(t *testing.T) {
	num := &metadata.Numerator{Period: "none", Scope: "Касса"}
	fields := map[string]any{"Касса": "касса-1"}
	got := storage.ComputePeriodKey(num, fields)
	if got != "касса-1" {
		t.Errorf("expected 'касса-1', got %q", got)
	}
}

// Scope с отсутствующим полем — пустая часть, но не паникует.
func TestComputePeriodKey_ScopeMissingField(t *testing.T) {
	num := &metadata.Numerator{Period: "year", Scope: "Организация"}
	fields := map[string]any{
		"Дата": time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
	}
	got := storage.ComputePeriodKey(num, fields)
	if got != "2026|" {
		t.Errorf("expected '2026|', got %q", got)
	}
}

// TestNextNumber_Concurrency — конкурентная гонка на NextNumber.
// Запускаем 50 горутин × 20 вызовов = 1000 номеров на одном (entity, period).
// Проверки:
//  1. Все вызовы успешны (ошибок нет).
//  2. Полученные номера — ровно множество {1..1000}: нет дубликатов и нет пропусков
//     (счётчик стартует с 1, транзакций, которые могли бы откатиться, в этом тесте нет).
//
// Гарантия атомарности опирается на INSERT ... ON CONFLICT DO UPDATE ... RETURNING
// в storage/numerator.go:26. SQLite сериализует пишущие транзакции; PostgreSQL
// держит row-level lock на конфликтной строке — оба корректны.
func TestNextNumber_Concurrency(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "concurrency.db")

	db, err := storage.ConnectSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()

	if err := db.EnsureNumeratorSchema(ctx); err != nil {
		t.Fatalf("EnsureNumeratorSchema: %v", err)
	}

	const (
		goroutines   = 50
		perGoroutine = 20
		total        = goroutines * perGoroutine
		entity       = "ТестДок"
		periodKey    = "2026"
	)

	nums := make(chan int, total)
	errs := make(chan error, total)

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1) // одинаковый старт всех горутин — максимум гонок

	for g := 0; g < goroutines; g++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			for i := 0; i < perGoroutine; i++ {
				n, err := db.NextNumber(ctx, entity, periodKey)
				if err != nil {
					errs <- err
					return
				}
				nums <- n
			}
		}()
	}
	start.Done()
	done.Wait()
	close(nums)
	close(errs)

	for e := range errs {
		t.Fatalf("NextNumber error: %v", e)
	}

	seen := make(map[int]bool, total)
	for n := range nums {
		if seen[n] {
			t.Fatalf("дубликат номера: %d", n)
		}
		seen[n] = true
	}
	if len(seen) != total {
		t.Fatalf("получено %d уникальных номеров, ожидалось %d", len(seen), total)
	}

	got := make([]int, 0, total)
	for n := range seen {
		got = append(got, n)
	}
	sort.Ints(got)
	for i, n := range got {
		if n != i+1 {
			t.Fatalf("неплотная последовательность: позиция %d = %d (ожидалось %d)", i, n, i+1)
		}
	}
}

// TestNextNumber_IsolatedPeriodKeys — независимые ключи периода считаются
// независимыми счётчиками даже при параллельной записи.
func TestNextNumber_IsolatedPeriodKeys(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "isolated.db")

	db, err := storage.ConnectSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()

	if err := db.EnsureNumeratorSchema(ctx); err != nil {
		t.Fatalf("EnsureNumeratorSchema: %v", err)
	}

	const perKey = 100
	keys := []string{"2025", "2026", "2027"}

	var wg sync.WaitGroup
	results := make(map[string][]int, len(keys))
	var mu sync.Mutex

	for _, k := range keys {
		k := k
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]int, 0, perKey)
			for i := 0; i < perKey; i++ {
				n, err := db.NextNumber(ctx, "ТестДок", k)
				if err != nil {
					t.Errorf("NextNumber(%s): %v", k, err)
					return
				}
				local = append(local, n)
			}
			mu.Lock()
			results[k] = local
			mu.Unlock()
		}()
	}
	wg.Wait()

	for _, k := range keys {
		got := results[k]
		sort.Ints(got)
		if len(got) != perKey {
			t.Fatalf("key %s: получено %d номеров, ожидалось %d", k, len(got), perKey)
		}
		for i, n := range got {
			if n != i+1 {
				t.Fatalf("key %s: неплотная последовательность %v", k, got)
			}
		}
	}
}
