package page

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("панель.yaml", "name: Панель\ntitle: Панель руководителя\ntitles:\n  en: Manager\nroles: [Руководитель]\nparams: [период]\n")
	write("без_имени.yaml", "title: Без имени\n")
	write("ignore.txt", "не yaml")

	pages, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("ожидалось 2 страницы, получено %d", len(pages))
	}

	var panel, noname *Page
	for _, p := range pages {
		switch p.Name {
		case "Панель":
			panel = p
		case "без_имени":
			noname = p
		}
	}
	if panel == nil {
		t.Fatal("страница «Панель» не загружена")
	}
	if got := panel.DisplayName("en"); got != "Manager" {
		t.Errorf("DisplayName(en) = %q", got)
	}
	if got := panel.DisplayName("ru"); got != "Панель руководителя" {
		t.Errorf("DisplayName(ru) = %q", got)
	}
	if len(panel.Roles) != 1 || panel.Roles[0] != "Руководитель" {
		t.Errorf("Roles = %v", panel.Roles)
	}
	if len(panel.Params) != 1 || panel.Params[0] != "период" {
		t.Errorf("Params = %v", panel.Params)
	}
	// Имя берётся из имени файла, заголовок — из YAML.
	if noname == nil {
		t.Fatal("страница без name не получила имя из файла")
	}
	if noname.Title != "Без имени" {
		t.Errorf("noname.Title = %q", noname.Title)
	}
}

func TestLoadDir_MissingDir(t *testing.T) {
	pages, err := LoadDir(filepath.Join(t.TempDir(), "нет-такой"))
	if err != nil {
		t.Fatalf("отсутствующая папка не должна быть ошибкой: %v", err)
	}
	if pages != nil {
		t.Errorf("ожидался nil, получено %v", pages)
	}
}
