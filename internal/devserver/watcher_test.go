package devserver

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchContextStopsAndSignalsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done, err := WatchContext(ctx, t.TempDir(), func() {})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}

// Watch() должен вызывать onChange при изменении файла.
func TestWatch_TriggersOnFileChange(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.os")
	if err := os.WriteFile(file, []byte("// initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	var changes int32
	if err := Watch(dir, func() { atomic.AddInt32(&changes, 1) }); err != nil {
		t.Fatal(err)
	}

	// debounce — 300ms. Запишем файл и подождём.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(file, []byte("// edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Watcher debounce = 300ms; ждём 500ms.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&changes) > 0 {
			return // OK
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("onChange не вызвался после редактирования файла")
}

// Watch должен ловить правки в подкаталогах (.os-модули лежат в src/),
// а не только в корне проекта — fsnotify не рекурсивен сам по себе.
func TestWatch_TriggersOnSubdirChange(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "поступление.proc.os")
	if err := os.WriteFile(file, []byte("// initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	var changes int32
	if err := Watch(dir, func() { atomic.AddInt32(&changes, 1) }); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(file, []byte("// edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&changes) > 0 {
			return // OK
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("onChange не вызвался после редактирования файла в подкаталоге")
}

// Подкаталог, созданный уже после старта Watch, тоже должен отслеживаться.
func TestWatch_TriggersOnNewSubdir(t *testing.T) {
	dir := t.TempDir()

	var changes int32
	if err := Watch(dir, func() { atomic.AddInt32(&changes, 1) }); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	sub := filepath.Join(dir, "documents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Дать watcher'у время добавить новый каталог в наблюдение.
	time.Sleep(400 * time.Millisecond)
	atomic.StoreInt32(&changes, 0)
	if err := os.WriteFile(filepath.Join(sub, "счёт.yaml"), []byte("name: Счёт"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&changes) > 0 {
			return // OK
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("onChange не вызвался для файла в созданном после старта каталоге")
}

func TestWatch_Debounces(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "y.os")
	if err := os.WriteFile(file, []byte("// initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	var changes int32
	if err := Watch(dir, func() { atomic.AddInt32(&changes, 1) }); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	// Несколько правок подряд должны схлопнуться в один onChange.
	for i := 0; i < 5; i++ {
		os.WriteFile(file, []byte("// edited"+string(rune('A'+i))), 0o644)
		time.Sleep(50 * time.Millisecond)
	}
	// Ждём debounce + запас.
	time.Sleep(700 * time.Millisecond)

	got := atomic.LoadInt32(&changes)
	if got != 1 {
		t.Errorf("ожидался 1 onChange (debounce), получили %d", got)
	}
}
