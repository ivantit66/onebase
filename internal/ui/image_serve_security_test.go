package ui

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/storage"
)

// D2/D3: фактическая защита от XSS у image-поля — на стороне отдачи. Закрепляем
// заголовки imageServe (sandbox-CSP, inline) и то, что нерастровый mime (если
// попал в blob через СохранитьКартинку) отдаётся как application/octet-stream,
// а не как, например, text/html.
func TestImageServe_SecurityHeaders(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()
	if err := db.EnsureBlobTable(ctx); err != nil {
		t.Fatalf("EnsureBlobTable: %v", err)
	}

	b, err := db.PutBlob(ctx, "text/html", bytes.NewReader([]byte("<script>alert(1)</script>")), 1<<20, storage.BlobOwner{})
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	s := &Server{store: db}
	req := httptest.NewRequest("GET", "/ui/_image/"+b.ID.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", b.ID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.imageServe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("нерастровый blob должен отдаваться как application/octet-stream, получено %q", ct)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Errorf("ожидался sandbox в CSP, получено %q", csp)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != "inline" {
		t.Errorf("ожидался Content-Disposition: inline, получено %q", cd)
	}
	// Замечание #19: imageServe обязан ставить nosniff сам (не полагаясь на
	// глобальный middleware) — комментарии в коде на него опираются.
	if xcto := rec.Header().Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Errorf("ожидался X-Content-Type-Options: nosniff, получено %q", xcto)
	}
}
