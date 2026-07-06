package launcher

// Тесты изолированных окон Предприятия (план 78, фаза 3): выбор и очистка
// браузерных профилей, аргументы Chromium и endpoint /bases/{id}/start-isolated.
// Реальный браузер не запускается — isolatedBrowser подменяется фейком.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestChromiumArgs(t *testing.T) {
	args := chromiumArgs(`C:\profiles\p1`, "http://localhost:8080/ui")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		`--user-data-dir=C:\profiles\p1`,
		"--no-first-run",
		"--new-window",
		"--app=http://localhost:8080/ui",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("аргументы не содержат %q: %v", want, args)
		}
	}
}

// makeProfileBusy имитирует занятый браузером профиль платформенным способом
// (Windows — открытый lockfile, POSIX — SingletonLock с живым pid) и
// возвращает функцию освобождения.
func makeProfileBusy(t *testing.T, dir string) func() {
	t.Helper()
	if runtime.GOOS == "windows" {
		lock := filepath.Join(dir, "lockfile")
		if err := os.WriteFile(lock, []byte("x"), 0o644); err != nil {
			t.Fatalf("lockfile: %v", err)
		}
		f, err := os.Open(lock) // открытый handle не даёт os.Remove удалить файл
		if err != nil {
			t.Fatalf("open lockfile: %v", err)
		}
		return func() { f.Close() }
	}
	lock := filepath.Join(dir, "SingletonLock")
	if err := os.Symlink(fmt.Sprintf("host-%d", os.Getpid()), lock); err != nil {
		t.Skipf("symlink недоступен: %v", err)
	}
	return func() { os.Remove(lock) }
}

func TestPickProfileDir_ReusesFreeSkipsBusy(t *testing.T) {
	root := t.TempDir()

	// Первый вызов создаёт profile-1.
	dir1, err := pickProfileDir(root)
	if err != nil {
		t.Fatalf("pickProfileDir: %v", err)
	}
	if filepath.Base(dir1) != "profile-1" {
		t.Fatalf("ожидался profile-1, получен %s", dir1)
	}

	// Свободный profile-1 переиспользуется («рабочее место»).
	dir, err := pickProfileDir(root)
	if err != nil || dir != dir1 {
		t.Fatalf("свободный профиль должен переиспользоваться: %s / %v", dir, err)
	}

	// Занятый profile-1 пропускается — создаётся profile-2.
	release := makeProfileBusy(t, dir1)
	defer release()
	dir2, err := pickProfileDir(root)
	if err != nil {
		t.Fatalf("pickProfileDir (занят 1-й): %v", err)
	}
	if filepath.Base(dir2) != "profile-2" {
		t.Fatalf("ожидался profile-2, получен %s", dir2)
	}
}

func TestCleanIsolatedProfiles(t *testing.T) {
	root := t.TempDir()
	free, _ := pickProfileDir(root)   // profile-1 — свободный
	release := makeProfileBusy(t, free)
	busyDir := free
	free2, _ := pickProfileDir(root)  // profile-2 — свободный
	_ = free2

	removed, err := cleanIsolatedProfiles(root)
	if err != nil {
		t.Fatalf("cleanIsolatedProfiles: %v", err)
	}
	if removed != 1 {
		t.Fatalf("должен удалиться 1 свободный профиль, удалено %d", removed)
	}
	if _, err := os.Stat(busyDir); err != nil {
		t.Fatal("занятый профиль не должен удаляться")
	}
	release()

	// После освобождения удаляется и он.
	removed, err = cleanIsolatedProfiles(root)
	if err != nil || removed != 1 {
		t.Fatalf("после освобождения должен удалиться и занятый: %d / %v", removed, err)
	}

	// Несуществующий корень — не ошибка.
	if _, err := cleanIsolatedProfiles(filepath.Join(root, "nope")); err != nil {
		t.Fatalf("несуществующий корень не должен падать: %v", err)
	}
}

// fakeBrowser записывает вызовы Open вместо запуска реального браузера.
type fakeBrowser struct {
	calls int
	dir   string
	url   string
	err   error
}

func (f *fakeBrowser) Open(dir, url string) error {
	f.calls++
	f.dir, f.url = dir, url
	return f.err
}

// newIsolatedFixture поднимает handler с одной sqlite-базой, помеченной как
// запущенная, и httptest-сервером, отвечающим на /health (для WaitReady).
func newIsolatedFixture(t *testing.T, fb *fakeBrowser) (*handler, *Base) {
	t.Helper()

	profilesRootOverride = t.TempDir()
	t.Cleanup(func() { profilesRootOverride = "" })

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(health.Close)
	u, _ := url.Parse(health.URL)
	port, _ := strconv.Atoi(u.Port())

	b := &Base{ID: "iso-test", Name: "iso", ConfigSource: "file", Path: t.TempDir(),
		DBType: "sqlite", DBPath: filepath.Join(t.TempDir(), "iso.db"), Port: port}
	st := &Store{path: filepath.Join(t.TempDir(), "ibases.yaml")}
	if err := st.Add(b); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	rn := NewRunner()
	rn.procs[b.ID] = &managedProc{port: port} // база «запущена» — Start не зовётся

	return &handler{store: st, runner: rn, isoBrowser: fb}, b
}

func startIsolatedReq(t *testing.T, h *handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/bases/"+id+"/start-isolated", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.startIsolated(rec, req)
	return rec
}

func TestStartIsolated_OpensBrowserWithProfile(t *testing.T) {
	fb := &fakeBrowser{}
	h, b := newIsolatedFixture(t, fb)

	rec := startIsolatedReq(t, h, b.ID)
	if rec.Code != 200 {
		t.Fatalf("код ответа %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp.OK {
		t.Fatalf("ожидался ok=true: %s", rec.Body.String())
	}
	if fb.calls != 1 {
		t.Fatalf("браузер должен быть запущен ровно один раз, вызовов: %d", fb.calls)
	}
	wantURL := fmt.Sprintf("http://localhost:%d/ui", b.Port)
	if fb.url != wantURL {
		t.Errorf("URL окна: %q, ожидался %q", fb.url, wantURL)
	}
	if !strings.Contains(fb.dir, filepath.Join(b.ID, "profile-1")) {
		t.Errorf("профиль должен лежать в каталоге базы: %q", fb.dir)
	}
	// Сессионный токен в URL не передаётся — свежий профиль попадёт на /login.
	if strings.Contains(fb.url, "token") || strings.Contains(fb.url, "code=") {
		t.Errorf("в URL не должно быть секретов: %q", fb.url)
	}
}

func TestStartIsolated_BrowserNotFound(t *testing.T) {
	fb := &fakeBrowser{err: fmt.Errorf("не найден Chromium-совместимый браузер")}
	h, b := newIsolatedFixture(t, fb)

	rec := startIsolatedReq(t, h, b.ID)
	if rec.Code != 500 {
		t.Fatalf("ожидался код 500, получен %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "браузер") {
		t.Errorf("в ответе должна быть понятная ошибка: %s", rec.Body.String())
	}
}

// Лаунчер усыновляет базу, запущенную не им (прежний экземпляр после
// пересборки exe): порт занят, /health отвечает — это не «порт занят», а
// живая база.
func TestStartIsolated_AdoptsUntrackedRunningBase(t *testing.T) {
	fb := &fakeBrowser{}
	h, b := newIsolatedFixture(t, fb)
	delete(h.runner.procs, b.ID) // этим лаунчером база не запускалась

	rec := startIsolatedReq(t, h, b.ID)
	if rec.Code != 200 {
		t.Fatalf("живую базу надо усыновлять, а не отвечать «порт занят»: %d %s", rec.Code, rec.Body.String())
	}
	if fb.calls != 1 {
		t.Fatalf("окно должно открыться, вызовов браузера: %d", fb.calls)
	}
}

func TestStart_AdoptsUntrackedRunningBase(t *testing.T) {
	h, b := newIsolatedFixture(t, &fakeBrowser{})
	delete(h.runner.procs, b.ID)

	req := httptest.NewRequest("POST", "/bases/"+b.ID+"/start", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", b.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.start(rec, req)

	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"url"`) {
		t.Fatalf("ожидался url усыновлённой базы: %d %s", rec.Code, rec.Body.String())
	}
}

func TestStartIsolated_UnknownBase(t *testing.T) {
	fb := &fakeBrowser{}
	h, _ := newIsolatedFixture(t, fb)
	rec := startIsolatedReq(t, h, "no-such-base")
	if rec.Code != 404 {
		t.Fatalf("ожидался 404, получен %d", rec.Code)
	}
	if fb.calls != 0 {
		t.Fatal("браузер не должен запускаться для неизвестной базы")
	}
}
