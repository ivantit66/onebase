package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/storage"
)

// TestHealthzHandler проверяет readiness-семантику: 200 при доступной БД и 503,
// когда БД недоступна (здесь — после Close). Именно это отличает /healthz от
// liveness-/health, который всегда 200.
func TestHealthzHandler(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	healthzHandler(db)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("живая БД: ожидали 200, получили %d", rec.Code)
	}
	if got := rec.Header().Get("X-OneBase-Version"); got == "" {
		t.Fatal("readiness-проба должна сообщать версию обслуживающего бинаря")
	}

	db.Close()
	rec = httptest.NewRecorder()
	healthzHandler(db)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("закрытая БД: ожидали 503, получили %d", rec.Code)
	}
}
