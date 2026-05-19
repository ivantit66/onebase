package devserver

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Замечание #16: Watch() должен вызывать onChange при изменении файла.
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
