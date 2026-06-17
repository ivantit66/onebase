package launcher

import (
	"bytes"
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

// Конфигуратор должен показывать страницу (план 66) и в дереве, и панелью-
// редактором: метаданные + код обработчика + Сохранить/Проверить/Удалить.
// Рендерим tab-tree с одной страницей и проверяем ключевые точки привязки.
func TestConfigurator_RendersPageEditor(t *testing.T) {
	data := &configuratorData{
		Base: &Base{ID: "test-base", Name: "Тест", ConfigSource: "file"},
		Lang: "ru",
		Tab:  "tree",
		Pages: []cfgPage{{
			Name:   "Панель",
			Title:  "Панель руководителя",
			Icon:   "layout-dashboard",
			Roles:  []string{"Руководитель", "Бухгалтер"},
			Params: []string{"период"},
			Source: "Процедура ПриФормировании(Страница, Параметры) Экспорт\nКонецПроцедуры\n",
		}},
	}
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "tab-tree", data); err != nil {
		t.Fatalf("ExecuteTemplate tab-tree: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		// группа дерева + кнопка создания
		`cfgNewObj('page')`,
		// узел дерева ↔ панель (data-id / id совпадают)
		`data-id="page-Панель"`,
		`id="page-Панель"`,
		// формы сохранения и удаления
		`/bases/test-base/configurator/page"`,
		`/bases/test-base/configurator/page-delete"`,
		`name="page_name" value="Панель"`,
		// метаданные в полях редактора
		`value="layout-dashboard"`,
		`value="Руководитель, Бухгалтер"`,
		// исходник обработчика в textarea name=source
		`id="ta-page-Панель" name="source"`,
		"Процедура ПриФормировании(Страница, Параметры) Экспорт",
		// проверка синтаксиса через общий runCheck (kind=dsl)
		`runCheck('dsl','page-Панель','Панель')`,
		`id="check-page-Панель"`,
		// адрес страницы в рантайме
		`/ui/page/Панель`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в рендере конфигуратора нет ожидаемого фрагмента: %q", want)
		}
	}
}

// splitConfigList: запятые/точки с запятой/переводы строк — разделители;
// пробелы и пустые элементы отбрасываются; пустой ввод → nil (omitempty).
func TestSplitConfigList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"Менеджер", []string{"Менеджер"}},
		{"Менеджер, Бухгалтер", []string{"Менеджер", "Бухгалтер"}},
		{" период ; склад \n дата ", []string{"период", "склад", "дата"}},
		{",,Менеджер,,", []string{"Менеджер"}},
	}
	for _, c := range cases {
		got := splitConfigList(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitConfigList(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitConfigList(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// newObjectContent("page") и newPageOSSkeleton дают валидную пару файлов:
// метаданные с name/title и обработчик с ПриФормировании.
func TestNewPageContent(t *testing.T) {
	subdir, content := newObjectContent("page", "Сводка")
	if subdir != "pages" {
		t.Errorf("subdir = %q, ожидался pages", subdir)
	}
	if !strings.Contains(content, "name: Сводка") {
		t.Errorf("в метаданных нет name: %q", content)
	}
	skeleton := newPageOSSkeleton("Сводка")
	if !strings.Contains(skeleton, "Процедура ПриФормировании(Страница, Параметры) Экспорт") {
		t.Errorf("в скелете нет процедуры ПриФормировании: %q", skeleton)
	}
}

// readPageSources читает src/*.page.os и ключует по имени в нижнем регистре —
// так же, как лукапит loadCfgData (pageSources[ToLower(pg.Name)]).
func TestReadPageSources(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "Процедура ПриФормировании(Страница, Параметры) Экспорт\nКонецПроцедуры\n"
	os.WriteFile(filepath.Join(srcDir, "Панель.page.os"), []byte(body), 0o644)
	// Постороннее: модуль и обработка не должны попасть в карту страниц.
	os.WriteFile(filepath.Join(srcDir, "общий.module.os"), []byte("// модуль"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "отчёт.proc.os"), []byte("// обработка"), 0o644)

	got := readPageSources(dir)
	if len(got) != 1 {
		t.Fatalf("ожидалась 1 страница, получено %d: %v", len(got), got)
	}
	if got["панель"] != body {
		t.Errorf("source по ключу 'панель' не найден/не совпал: %q", got["панель"])
	}
}

// pageYAMLRelPath/pageSrcRelPath сохраняют регистр имени (round-trip правки
// существующего файла без дублей на регистрозависимых ФС).
func TestPageRelPaths(t *testing.T) {
	if got := pageYAMLRelPath("Панель"); got != "pages/Панель.yaml" {
		t.Errorf("pageYAMLRelPath = %q", got)
	}
	if got := pageSrcRelPath("Панель"); got != "src/Панель.page.os" {
		t.Errorf("pageSrcRelPath = %q", got)
	}
}

// Полный цикл «страница» в файловом режиме конфигуратора (план 66, доработка 2):
// создание (+ в дереве) → правка метаданных и кода → удаление. Проверяем, что
// на диск ложится валидная пара pages/<Имя>.yaml + src/<Имя>.page.os и что
// удаление убирает оба файла.
func TestConfiguratorPage_NewSaveDelete_FileMode(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	b := &Base{Name: "Тест", ConfigSource: "file", Path: dir}
	if err := s.Add(b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := &handler{store: s, runner: NewRunner()}

	post := func(route string, fn http.HandlerFunc, form url.Values) {
		t.Helper()
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", b.ID)
		req := httptest.NewRequest(http.MethodPost, "/bases/"+b.ID+"/configurator/"+route,
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		fn(httptest.NewRecorder(), req)
	}

	yamlPath := filepath.Join(dir, "pages", "Сводка.yaml")
	osPath := filepath.Join(dir, "src", "Сводка.page.os")

	// 1. Создание через «+» в дереве (kind=page).
	post("new", h.configuratorNewObject, url.Values{"kind": {"page"}, "name": {"Сводка"}})
	if _, err := os.Stat(yamlPath); err != nil {
		t.Fatalf("pages/Сводка.yaml не создан: %v", err)
	}
	skeleton, err := os.ReadFile(osPath)
	if err != nil {
		t.Fatalf("src/Сводка.page.os не создан: %v", err)
	}
	if !strings.Contains(string(skeleton), "Процедура ПриФормировании(Страница, Параметры) Экспорт") {
		t.Errorf("скелет обработчика без ПриФормировании:\n%s", skeleton)
	}

	// 2. Правка метаданных + кода.
	post("page", h.configuratorSavePage, url.Values{
		"page_name": {"Сводка"},
		"title":     {"Сводка за день"},
		"icon":      {"layout-dashboard"},
		"roles":     {"Руководитель, Бухгалтер"},
		"params":    {"период, склад"},
		"source":    {"Процедура ПриФормировании(Страница, Параметры) Экспорт\n    Страница.Заголовок(\"Сводка\");\nКонецПроцедуры\n"},
	})
	yamlBody, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: Сводка",
		"title: Сводка за день",
		"icon: layout-dashboard",
		"- Руководитель",
		"- Бухгалтер",
		"- период",
		"- склад",
	} {
		if !strings.Contains(string(yamlBody), want) {
			t.Errorf("в pages/Сводка.yaml нет %q\nполучилось:\n%s", want, yamlBody)
		}
	}
	srcBody, _ := os.ReadFile(osPath)
	if !strings.Contains(string(srcBody), `Страница.Заголовок("Сводка")`) {
		t.Errorf("обработчик не обновился:\n%s", srcBody)
	}

	// 3. Удаление — оба файла исчезают.
	post("page-delete", h.configuratorDeletePage, url.Values{"page_name": {"Сводка"}})
	if _, err := os.Stat(yamlPath); !os.IsNotExist(err) {
		t.Errorf("pages/Сводка.yaml не удалён (err=%v)", err)
	}
	if _, err := os.Stat(osPath); !os.IsNotExist(err) {
		t.Errorf("src/Сводка.page.os не удалён (err=%v)", err)
	}
}
