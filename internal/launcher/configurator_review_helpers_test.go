package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// postCfgRv POST-ит форму в произвольный обработчик конфигуратора с заголовком
// X-Onebase-Ajax (renderCfg отвечает JSON, без полного рендера страницы).
func postCfgRv(t *testing.T, id, path string, form url.Values, fn http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Onebase-Ajax", "1")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func writeCfgFileRv(t *testing.T, dir, sub, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, sub, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertFileContainsRv(t *testing.T, path string, fragments ...string) {
	t.Helper()
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	got := string(out)
	for _, must := range fragments {
		if !strings.Contains(got, must) {
			t.Errorf("в %s нет фрагмента %q\nполучилось:\n%s", filepath.Base(path), must, got)
		}
	}
}
