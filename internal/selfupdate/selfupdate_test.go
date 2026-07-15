package selfupdate

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStageBinary_FromZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "onebase-v0.9.1.zip")
	// В архиве кладём бинарь в подкаталог — StageBinary ищет по базовому имени.
	writeZip(t, zipPath, map[string]string{
		"README.txt":           "release notes",
		"dist/" + BinaryName(): "NEW-BINARY",
	})
	stage := filepath.Join(dir, "stage")
	got, err := StageBinary(zipPath, stage)
	if err != nil {
		t.Fatalf("StageBinary: %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "NEW-BINARY" {
		t.Fatalf("извлекли %q, ждали NEW-BINARY", data)
	}
}

func TestStageBinary_PlainFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, BinaryName())
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := StageBinary(bin, filepath.Join(dir, "stage"))
	if err != nil {
		t.Fatalf("StageBinary: %v", err)
	}
	if got != bin {
		t.Fatalf("для прямого файла вернули %q, ждали %q", got, bin)
	}
}

func TestStageBinary_RejectsOversizedPlainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), BinaryName())
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxBinaryBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := StageBinary(path, t.TempDir()); err == nil {
		t.Fatal("ожидался отказ от слишком большого бинаря")
	}
}

func TestStageBinary_MissingInZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bad.zip")
	writeZip(t, zipPath, map[string]string{"README.txt": "no binary here"})
	if _, err := StageBinary(zipPath, filepath.Join(dir, "stage")); err == nil {
		t.Fatal("ожидали ошибку: бинаря в архиве нет")
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "f")
	if err := os.WriteFile(bin, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	const sum = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" // sha256("hello")
	if err := VerifySHA256(bin, sum); err != nil {
		t.Fatalf("верная сумма отвергнута: %v", err)
	}
	if err := VerifySHA256(bin, "DEADBEEF"); err == nil {
		t.Fatal("ожидали ошибку на неверной сумме")
	}
}

func TestSwapAndRollback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, BinaryName())
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := filepath.Join(dir, "new")
	if err := os.WriteFile(newBin, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	backup, err := SwapBinary(target, newBin)
	if err != nil {
		t.Fatalf("SwapBinary: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "NEW" {
		t.Fatalf("после подмены target=%q, ждали NEW", data)
	}
	if data, _ := os.ReadFile(backup); string(data) != "OLD" {
		t.Fatalf("резервная копия=%q, ждали OLD", data)
	}

	if err := Rollback(target, backup); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "OLD" {
		t.Fatalf("после отката target=%q, ждали OLD", data)
	}
}

func TestPollHealthz_BecomesReady(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Первые два запроса — 503, затем 200 (эмуляция поднимающегося сервиса).
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := PollHealthz(context.Background(), srv.URL, 3*time.Second, 20*time.Millisecond); err != nil {
		t.Fatalf("ожидали успех, получили: %v", err)
	}
}

func TestPollHealthz_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := PollHealthz(context.Background(), srv.URL, 100*time.Millisecond, 20*time.Millisecond); err == nil {
		t.Fatal("ожидали таймаут, получили nil")
	}
}

func TestPollHealthzVersion_RequiresExpectedBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OneBase-Version", "old")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := PollHealthzVersion(context.Background(), srv.URL, "new", 100*time.Millisecond, 20*time.Millisecond); err == nil {
		t.Fatal("HTTP 200 от старой версии не должен подтверждать обновление")
	}
	if err := PollHealthzVersion(context.Background(), srv.URL, "old", time.Second, 20*time.Millisecond); err != nil {
		t.Fatalf("ожидался успех для правильной версии: %v", err)
	}
}
