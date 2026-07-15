package launcher

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func renderCfgAdminAbout(t *testing.T, b *Base) string {
	t.Helper()
	store := &Store{path: filepath.Join(t.TempDir(), "ibases.yaml")}
	if err := store.save([]*Base{b}); err != nil {
		t.Fatalf("store.save: %v", err)
	}

	req := httptest.NewRequest("GET", "/bases/"+b.ID+"/configurator/admin/about", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", b.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	(&handler{store: store}).cfgAdminAbout(rec, req)
	if rec.Code != 200 {
		t.Fatalf("cfgAdminAbout status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestCfgAdminAbout_ShowsSQLiteAndFileConfigLocations(t *testing.T) {
	b := &Base{
		ID:           "about-file",
		Name:         "File base",
		ConfigSource: "file",
		Path:         `/srv/configs/<trade>`,
		DBType:       "sqlite",
		DBPath:       `/srv/data/<trade>.db`,
		Port:         8080,
	}
	html := renderCfgAdminAbout(t, b)

	for _, want := range []string{
		"Расположение конфигурации",
		"Файлы",
		`/srv/configs/&lt;trade&gt;`,
		"База данных",
		`/srv/data/&lt;trade&gt;.db`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в окне «О программе» нет %q", want)
		}
	}
	if strings.Contains(html, "<trade>") {
		t.Error("пути в окне «О программе» должны быть HTML-экранированы")
	}
}

func TestCfgAdminAbout_DatabaseConfigShowsItsSQLiteFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "application.db")
	b := &Base{
		ID:           "about-database",
		Name:         "Database base",
		ConfigSource: "database",
		DBType:       "sqlite",
		DBPath:       dbPath,
		Port:         8081,
	}
	html := renderCfgAdminAbout(t, b)

	if !strings.Contains(html, "В базе данных") {
		t.Error("не показан режим хранения конфигурации в базе данных")
	}
	if strings.Count(html, dbPath) != 2 {
		t.Errorf("путь SQLite должен быть указан и для конфигурации, и для данных; HTML: %s", html)
	}
}

func TestCfgAdminAbout_MasksPostgresPassword(t *testing.T) {
	b := &Base{
		ID:           "about-postgres",
		Name:         "Postgres base",
		ConfigSource: "file",
		Path:         "/srv/onebase/config",
		DBType:       "postgres",
		DB:           "postgres://onebase:very-secret@db.example/production",
		Port:         8082,
	}
	html := renderCfgAdminAbout(t, b)

	if !strings.Contains(html, "postgres://onebase:***@db.example/production") {
		t.Error("в окне «О программе» нет маскированного PostgreSQL DSN")
	}
	if strings.Contains(html, "very-secret") {
		t.Error("окно «О программе» раскрыло пароль PostgreSQL")
	}
}
