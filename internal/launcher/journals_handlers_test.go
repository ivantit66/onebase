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
	"gopkg.in/yaml.v3"
)

// Конфигуратор показывает журнал документов и в дереве (группа + «+»), и
// панелью-редактором сырого YAML с формами Сохранить/Удалить.
func TestConfigurator_RendersJournalEditor(t *testing.T) {
	data := &configuratorData{
		Base: &Base{ID: "test-base", Name: "Тест", ConfigSource: "file"},
		Lang: "ru",
		Tab:  "tree",
		Journals: []cfgJournal{{
			Name:    "РасписаниеДокладов",
			Title:   "Расписание докладов",
			YAML:    "name: РасписаниеДокладов\ntitle: Расписание докладов\ndocuments: [Доклад]\n",
			RelPath: "journals/РасписаниеДокладов.yaml",
		}},
	}
	var buf bytes.Buffer
	if err := cfgTmpl.ExecuteTemplate(&buf, "tab-tree", data); err != nil {
		t.Fatalf("ExecuteTemplate tab-tree: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		`cfgNewObj('journal')`,
		`data-id="journal-РасписаниеДокладов"`,
		`id="journal-РасписаниеДокладов"`,
		`/bases/test-base/configurator/journal"`,
		`/bases/test-base/configurator/journal-delete"`,
		`name="journal_name" value="РасписаниеДокладов"`,
		`name="journal_path" value="journals/РасписаниеДокладов.yaml"`,
		`<code>journals/РасписаниеДокладов.yaml</code>`,
		`id="ta-journal-РасписаниеДокладов" name="source"`,
		"documents: [Доклад]", // сырой YAML в редакторе
		`/ui/journal/РасписаниеДокладов`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в рендере конфигуратора нет фрагмента журнала: %q", want)
		}
	}
}

// newObjectContent("journal") даёт проходящий парсинг каркас в journals/.
func TestNewJournalContent(t *testing.T) {
	subdir, content := newObjectContent("journal", "РасписаниеДокладов")
	if subdir != "journals" {
		t.Errorf("subdir = %q, ожидался journals", subdir)
	}
	if !strings.Contains(content, "name: РасписаниеДокладов") {
		t.Errorf("в каркасе нет name: %q", content)
	}
	var probe map[string]any
	if err := yaml.Unmarshal([]byte(content), &probe); err != nil {
		t.Errorf("каркас журнала не парсится как YAML: %v", err)
	}
}

// journalYAMLRelPath приводит имя к нижнему регистру — как ИИ-генератор, чтобы
// правка перезаписывала тот же файл, а не плодила дубль.
func TestJournalRelPath(t *testing.T) {
	if got := journalYAMLRelPath("РасписаниеДокладов"); got != "journals/расписаниедокладов.yaml" {
		t.Errorf("journalYAMLRelPath = %q", got)
	}
	if got, ok := journalYAMLRelPathFromForm("РасписаниеДокладов", "journals/РасписаниеДокладов.yaml"); !ok || got != "journals/РасписаниеДокладов.yaml" {
		t.Errorf("mixed-case path = %q, %v", got, ok)
	}
	for _, bad := range []string{
		"../journals/РасписаниеДокладов.yaml",
		"journals/nested/РасписаниеДокладов.yaml",
		"documents/РасписаниеДокладов.yaml",
		"journals/РасписаниеДокладов.yml",
	} {
		if got, ok := journalYAMLRelPathFromForm("РасписаниеДокладов", bad); ok {
			t.Errorf("небезопасный путь %q принят как %q", bad, got)
		}
	}
}

// readJournalSources читает journals/*.yaml и ключует по «name:».
func TestReadJournalSources(t *testing.T) {
	dir := t.TempDir()
	jDir := filepath.Join(dir, "journals")
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: РасписаниеДокладов\ntitle: Расписание\ndocuments: [Доклад]\n"
	os.WriteFile(filepath.Join(jDir, "РасписаниеДокладов.yaml"), []byte(body), 0o644)
	os.WriteFile(filepath.Join(jDir, "readme.txt"), []byte("не yaml"), 0o644)

	got := readJournalSources(dir)
	if len(got) != 1 {
		t.Fatalf("ожидался 1 журнал, получено %d: %v", len(got), got)
	}
	source := got["РасписаниеДокладов"]
	if source.YAML != body {
		t.Errorf("source по ключу 'РасписаниеДокладов' не найден/не совпал: %q", source.YAML)
	}
	if source.RelPath != "journals/РасписаниеДокладов.yaml" {
		t.Errorf("исходный регистр пути потерян: %q", source.RelPath)
	}
}

// Полный цикл «журнал» в файловом режиме: создание через «+» → правка сырого
// YAML → удаление. Файл journals/<имя>.yaml (нижний регистр) появляется и
// исчезает; правка перезаписывает тот же файл.
func TestConfiguratorJournal_NewSaveDelete_FileMode(t *testing.T) {
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

	yamlPath := filepath.Join(dir, "journals", "расписаниедокладов.yaml")

	// 1. Создание через «+» в дереве (kind=journal).
	post("new", h.configuratorNewObject, url.Values{"kind": {"journal"}, "name": {"РасписаниеДокладов"}})
	if _, err := os.Stat(yamlPath); err != nil {
		t.Fatalf("journals/расписаниедокладов.yaml не создан: %v", err)
	}

	// 2. Правка сырого YAML.
	edited := "name: РасписаниеДокладов\ntitle: Расписание докладов\ndocuments: [Доклад]\ncolumns:\n  - {field: Дата, label: Дата, format: date}\n"
	post("journal", h.configuratorSaveJournal, url.Values{
		"journal_name": {"РасписаниеДокладов"},
		"journal_path": {"journals/расписаниедокладов.yaml"},
		"source":       {edited},
	})
	saved, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != edited {
		t.Errorf("журнал сохранён не verbatim:\n%s", saved)
	}

	// 2b. Битый YAML не должен затирать файл.
	post("journal", h.configuratorSaveJournal, url.Values{
		"journal_name": {"РасписаниеДокладов"},
		"journal_path": {"journals/расписаниедокладов.yaml"},
		"source":       {"name: [не закрытый список\n"},
	})
	after, _ := os.ReadFile(yamlPath)
	if string(after) != edited {
		t.Errorf("битый YAML перезаписал файл журнала:\n%s", after)
	}

	// 3. Удаление — файл исчезает.
	post("journal-delete", h.configuratorDeleteJournal, url.Values{
		"journal_name": {"РасписаниеДокладов"},
		"journal_path": {"journals/расписаниедокладов.yaml"},
	})
	if _, err := os.Stat(yamlPath); !os.IsNotExist(err) {
		t.Errorf("journals/расписаниедокладов.yaml не удалён (err=%v)", err)
	}
}

// Редактор сохраняет и удаляет существующий mixed-case файл по его точному
// пути, даже если YAML name не совпадает с именем файла. Регресс: раньше оба
// действия вычисляли lower(name), создавая дубликат и оставляя оригинал.
func TestConfiguratorJournal_PreservesOriginalMixedCasePath(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	b := &Base{Name: "Тест", ConfigSource: "file", Path: dir}
	if err := s.Add(b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := &handler{store: s, runner: NewRunner()}

	jDir := filepath.Join(dir, "journals")
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(jDir, "Кассовые.yaml")
	if err := os.WriteFile(originalPath, []byte("name: КассовыеДокументы\ntitle: Кассовые\ndocuments: []\ncolumns: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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

	edited := "name: КассовыеДокументы\ntitle: Кассовые документы\ndocuments: []\ncolumns: []\n"
	form := url.Values{
		"journal_name": {"КассовыеДокументы"},
		"journal_path": {"journals/Кассовые.yaml"},
		"source":       {edited},
	}
	post("journal", h.configuratorSaveJournal, form)

	saved, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != edited {
		t.Errorf("исходный mixed-case файл не обновлён:\n%s", saved)
	}
	duplicatePath := filepath.Join(jDir, "кассовыедокументы.yaml")
	if _, err := os.Stat(duplicatePath); !os.IsNotExist(err) {
		t.Errorf("создан lower-дубликат %s (err=%v)", duplicatePath, err)
	}

	post("journal-delete", h.configuratorDeleteJournal, form)
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Errorf("исходный mixed-case файл не удалён (err=%v)", err)
	}
}

// Сохранение подсистемы через конфигуратор пишет contents.journals из формы и
// НЕ теряет contents.pages (у которых нет галочек в форме) — раньше pages
// молча вытирались при каждом сохранении подсистемы.
func TestConfiguratorSubsystem_PersistsJournalsKeepsPages(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	b := &Base{Name: "Тест", ConfigSource: "file", Path: dir}
	if err := s.Add(b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := &handler{store: s, runner: NewRunner()}

	subDir := filepath.Join(dir, "subsystems")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subFile := filepath.Join(subDir, nameToFilename("Конференция")+".yaml")
	os.WriteFile(subFile, []byte("name: Конференция\ntitle: Конференция\ncontents:\n  pages: [ПанельОрганизатора]\n"), 0o644)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", b.ID)
	form := url.Values{
		"subsystem_name": {"Конференция"},
		"title":          {"Конференция"},
		"journals":       {"РасписаниеДокладов"},
	}
	req := httptest.NewRequest(http.MethodPost, "/bases/"+b.ID+"/configurator/subsystem",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	h.configuratorSaveSubsystem(httptest.NewRecorder(), req)

	saved, err := os.ReadFile(subFile)
	if err != nil {
		t.Fatal(err)
	}
	body := string(saved)
	for _, want := range []string{"journals:", "РасписаниеДокладов", "pages:", "ПанельОрганизатора"} {
		if !strings.Contains(body, want) {
			t.Errorf("в сохранённой подсистеме нет %q\nполучилось:\n%s", want, body)
		}
	}
}
