package ui

import (
	"bytes"
	"strings"
	"testing"
)

func renderAbout(t *testing.T, cfg Config, isAdmin bool) string {
	t.Helper()
	var buf bytes.Buffer
	data := map[string]any{
		"Cfg":       prepareAboutConfig(cfg),
		"Lang":      "ru",
		"IsAdmin":   isAdmin,
		"Catalogs":  0,
		"Documents": 0,
		"Registers": 0,
		"Reports":   0,
	}
	if err := tmpl.ExecuteTemplate(&buf, "page-about", data); err != nil {
		t.Fatalf("render page-about: %v", err)
	}
	return buf.String()
}

func TestAbout_AdminSeesFileConfigAndSQLiteLocations(t *testing.T) {
	html := renderAbout(t, Config{
		ConfigSource:     "file",
		ConfigLocation:   `/srv/configs/<trade>`,
		DatabaseType:     "sqlite",
		DatabaseLocation: `/srv/data/<trade>.db`,
	}, true)

	for _, want := range []string{
		"Хранение конфигурации",
		"Файлы",
		"Расположение конфигурации",
		`/srv/configs/&lt;trade&gt;`,
		"SQLite · /srv/data/&lt;trade&gt;.db",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в пользовательском «О программе» нет %q", want)
		}
	}
	if strings.Contains(html, "<trade>") {
		t.Error("пути должны быть HTML-экранированы")
	}
}

func TestAbout_DatabaseConfigUsesMaskedDatabaseLocation(t *testing.T) {
	html := renderAbout(t, Config{
		ConfigSource:     "database",
		DatabaseType:     "postgres",
		DatabaseLocation: "postgres://onebase:very-secret@db.example/production",
	}, true)

	if strings.Count(html, "postgres://onebase:***@db.example/production") != 2 {
		t.Errorf("маскированный DSN должен обозначать расположение конфигурации и базы: %s", html)
	}
	if strings.Contains(html, "very-secret") {
		t.Error("страница «О программе» раскрыла пароль PostgreSQL")
	}
}

func TestAbout_RegularUserDoesNotSeeServerPaths(t *testing.T) {
	html := renderAbout(t, Config{
		ConfigSource:     "file",
		ConfigLocation:   "/srv/private/config",
		DatabaseType:     "sqlite",
		DatabaseLocation: "/srv/private/data.db",
	}, false)

	if !strings.Contains(html, "Файлы") || !strings.Contains(html, "SQLite") {
		t.Error("обычному пользователю должны быть видны типы хранения")
	}
	for _, hidden := range []string{"/srv/private/config", "/srv/private/data.db", "Расположение конфигурации"} {
		if strings.Contains(html, hidden) {
			t.Errorf("обычному пользователю раскрыто %q", hidden)
		}
	}
}
