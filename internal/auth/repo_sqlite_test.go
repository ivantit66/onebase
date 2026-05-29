package auth_test

// Тесты критичных путей аутентификации/авторизации на реальной SQLite-базе
// через t.TempDir() — без внешних зависимостей и build-тегов, поэтому
// выполняются в обычном `go test ./...` и в CI. Это покрывает многопользовательские
// сценарии (несколько пользователей, роли, сессии), которые в integration_test.go
// заперты за тегом integration + TEST_DATABASE_URL и в CI не запускаются.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/storage"
)

// newTestRepo поднимает чистую SQLite-базу со схемой auth.
func newTestRepo(t *testing.T) (*auth.Repo, context.Context) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "auth.db")
	db, err := storage.ConnectSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	t.Cleanup(db.Close)
	repo := auth.NewRepo(db)
	if err := repo.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return repo, ctx
}

func TestCreateAndAuthenticate(t *testing.T) {
	repo, ctx := newTestRepo(t)

	has, err := repo.HasUsers(ctx)
	if err != nil {
		t.Fatalf("HasUsers: %v", err)
	}
	if has {
		t.Fatal("свежая база не должна содержать пользователей")
	}

	user, err := repo.Create(ctx, "ivan", "secret123", "Иван Петров", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if user.ID == "" {
		t.Fatal("Create должен присвоить ID")
	}

	if has, _ := repo.HasUsers(ctx); !has {
		t.Fatal("после Create HasUsers должен вернуть true")
	}

	// Верный пароль.
	got, err := repo.Authenticate(ctx, "ivan", "secret123")
	if err != nil {
		t.Fatalf("Authenticate (верный пароль): %v", err)
	}
	if got.Login != "ivan" || got.FullName != "Иван Петров" {
		t.Fatalf("неожиданный пользователь: %+v", got)
	}

	// Неверный пароль — отказ.
	if _, err := repo.Authenticate(ctx, "ivan", "wrong"); err == nil {
		t.Fatal("Authenticate с неверным паролем должен вернуть ошибку")
	}

	// Несуществующий пользователь — отказ.
	if _, err := repo.Authenticate(ctx, "nobody", "secret123"); err == nil {
		t.Fatal("Authenticate с неизвестным логином должен вернуть ошибку")
	}
}

func TestUpdatePasswordInvalidatesOldPassword(t *testing.T) {
	repo, ctx := newTestRepo(t)
	user, _ := repo.Create(ctx, "petr", "oldpass", "", false)

	if err := repo.UpdatePassword(ctx, user.ID, "newpass"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}

	if _, err := repo.Authenticate(ctx, "petr", "oldpass"); err == nil {
		t.Fatal("старый пароль не должен работать после смены")
	}
	if _, err := repo.Authenticate(ctx, "petr", "newpass"); err != nil {
		t.Fatalf("новый пароль должен работать: %v", err)
	}
}

func TestSessionsLifecycle(t *testing.T) {
	repo, ctx := newTestRepo(t)
	user, _ := repo.Create(ctx, "sess", "pass", "", false)

	token, err := repo.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if token == "" {
		t.Fatal("CreateSession должен вернуть непустой токен")
	}

	looked, err := repo.LookupSession(ctx, token)
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if looked.ID != user.ID {
		t.Fatalf("сессия вернула чужого пользователя: %s != %s", looked.ID, user.ID)
	}

	if err := repo.DeleteSession(ctx, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := repo.LookupSession(ctx, token); err == nil {
		t.Fatal("LookupSession после удаления должен вернуть ошибку")
	}
}

// CreateSession удаляет прежние сессии пользователя (одна активная сессия).
func TestCreateSessionReplacesOld(t *testing.T) {
	repo, ctx := newTestRepo(t)
	user, _ := repo.Create(ctx, "single", "pass", "", false)

	first, _ := repo.CreateSession(ctx, user.ID)
	second, _ := repo.CreateSession(ctx, user.ID)

	if _, err := repo.LookupSession(ctx, first); err == nil {
		t.Fatal("первая сессия должна быть аннулирована после создания второй")
	}
	if _, err := repo.LookupSession(ctx, second); err != nil {
		t.Fatalf("вторая сессия должна быть валидной: %v", err)
	}
}

func TestKickUserDropsSession(t *testing.T) {
	repo, ctx := newTestRepo(t)
	user, _ := repo.Create(ctx, "kickme", "pass", "", false)
	token, _ := repo.CreateSession(ctx, user.ID)

	if err := repo.KickUser(ctx, "kickme"); err != nil {
		t.Fatalf("KickUser: %v", err)
	}
	if _, err := repo.LookupSession(ctx, token); err == nil {
		t.Fatal("после KickUser сессия должна быть недействительна")
	}
}

func TestRolesAssignAndPermissions(t *testing.T) {
	repo, ctx := newTestRepo(t)
	user, _ := repo.Create(ctx, "manager", "pass", "Менеджер", false)

	roles := []*auth.Role{{
		Name:        "Продажник",
		Description: "Работа с продажами",
		Permissions: auth.Permission{
			Catalogs:  map[string][]string{"Контрагенты": {"read", "write"}},
			Documents: map[string][]string{"Реализация": {"read", "post"}},
		},
	}}
	if err := repo.SyncRoles(ctx, roles); err != nil {
		t.Fatalf("SyncRoles: %v", err)
	}
	if roles[0].ID == "" {
		t.Fatal("SyncRoles должен заполнить ID роли")
	}

	if err := repo.AssignRole(ctx, user.ID, roles[0].ID); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	// Повторное назначение идемпотентно.
	if err := repo.AssignRole(ctx, user.ID, roles[0].ID); err != nil {
		t.Fatalf("повторный AssignRole должен быть идемпотентным: %v", err)
	}

	loaded, err := repo.GetRolesForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetRolesForUser: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Продажник" {
		t.Fatalf("ожидалась 1 роль 'Продажник', получено %+v", loaded)
	}

	// Проверка прав через загруженные из БД роли (round-trip permissions JSON).
	user.Roles = loaded
	if !user.Has("catalog", "Контрагенты", "write") {
		t.Error("право catalog/Контрагенты/write должно сохраниться через БД")
	}
	if user.Has("document", "Реализация", "write") {
		t.Error("право document/Реализация/write не выдавалось")
	}

	ids, err := repo.GetUserRoleIDs(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserRoleIDs: %v", err)
	}
	if !ids[roles[0].ID] {
		t.Fatal("GetUserRoleIDs должен содержать назначенную роль")
	}

	// Снятие роли.
	if err := repo.UnassignRole(ctx, user.ID, roles[0].ID); err != nil {
		t.Fatalf("UnassignRole: %v", err)
	}
	if after, _ := repo.GetRolesForUser(ctx, user.ID); len(after) != 0 {
		t.Fatalf("после UnassignRole ролей быть не должно, получено %+v", after)
	}
}

// SyncRoles вызывается повторно (upsert по имени) — роль не дублируется.
func TestSyncRolesUpsert(t *testing.T) {
	repo, ctx := newTestRepo(t)

	r1 := []*auth.Role{{Name: "Кладовщик", Description: "v1"}}
	if err := repo.SyncRoles(ctx, r1); err != nil {
		t.Fatalf("SyncRoles v1: %v", err)
	}
	r2 := []*auth.Role{{Name: "Кладовщик", Description: "v2"}}
	if err := repo.SyncRoles(ctx, r2); err != nil {
		t.Fatalf("SyncRoles v2: %v", err)
	}

	all, err := repo.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ожидалась 1 роль после upsert, получено %d", len(all))
	}
	if all[0].Description != "v2" {
		t.Fatalf("описание должно обновиться до v2, получено %q", all[0].Description)
	}

	if err := repo.DeleteRoleByName(ctx, "Кладовщик"); err != nil {
		t.Fatalf("DeleteRoleByName: %v", err)
	}
	if all, _ := repo.ListRoles(ctx); len(all) != 0 {
		t.Fatalf("после удаления ролей быть не должно, получено %d", len(all))
	}
}

func TestListForSelectionFiltersByShowInList(t *testing.T) {
	repo, ctx := newTestRepo(t)
	visible, _ := repo.Create(ctx, "visible", "p", "", false)
	repo.Create(ctx, "hidden", "p", "", false)

	if err := repo.SetShowInList(ctx, visible.ID, true); err != nil {
		t.Fatalf("SetShowInList: %v", err)
	}

	sel, err := repo.ListForSelection(ctx)
	if err != nil {
		t.Fatalf("ListForSelection: %v", err)
	}
	if len(sel) != 1 || sel[0].Login != "visible" {
		t.Fatalf("в выборку должен попасть только 'visible', получено %+v", sel)
	}

	all, _ := repo.List(ctx)
	if len(all) != 2 {
		t.Fatalf("List должен вернуть всех (2), получено %d", len(all))
	}
}

func TestMiddlewareRequiresSessionWhenUsersExist(t *testing.T) {
	repo, ctx := newTestRepo(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := repo.Middleware(next)

	// Пока пользователей нет — middleware пропускает (первый запуск).
	rr0 := httptest.NewRecorder()
	handler.ServeHTTP(rr0, httptest.NewRequest(http.MethodGet, "/ui", nil))
	if rr0.Code != http.StatusOK {
		t.Fatalf("без пользователей должен быть проход, получено %d", rr0.Code)
	}

	// Появился пользователь — теперь нужна сессия.
	user, _ := repo.Create(ctx, "guard", "pass", "", false)

	reqNoCookie := httptest.NewRequest(http.MethodGet, "/ui", nil)
	reqNoCookie.Header.Set("Accept", "text/html")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, reqNoCookie)
	if rr1.Code != http.StatusFound {
		t.Fatalf("без cookie ожидался редирект 302, получено %d", rr1.Code)
	}

	// Валидная сессия — проход.
	token, _ := repo.CreateSession(ctx, user.ID)
	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	req.AddCookie(&http.Cookie{Name: "onebase_session", Value: token})
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("с валидной сессией ожидался 200, получено %d", rr2.Code)
	}
}

func TestLoadRolesYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: Бухгалтер
description: Ведение учёта
permissions:
  catalogs:
    Контрагенты: [read, write]
  documents:
    Платёж: [read, post, unpost]
`
	if err := os.WriteFile(filepath.Join(dir, "buh.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Файл не-yaml должен игнорироваться.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("WriteFile txt: %v", err)
	}

	roles, err := auth.LoadRolesYAML(dir)
	if err != nil {
		t.Fatalf("LoadRolesYAML: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("ожидалась 1 роль, получено %d", len(roles))
	}
	r := roles[0]
	if r.Name != "Бухгалтер" {
		t.Fatalf("имя роли: %q", r.Name)
	}
	if got := r.Permissions.Documents["Платёж"]; len(got) != 3 {
		t.Fatalf("ожидалось 3 операции для Платёж, получено %v", got)
	}

	// Несуществующая папка — не ошибка, пустой результат.
	none, err := auth.LoadRolesYAML(filepath.Join(dir, "nope"))
	if err != nil {
		t.Fatalf("LoadRolesYAML несуществующей папки не должен падать: %v", err)
	}
	if none != nil {
		t.Fatalf("ожидался nil для несуществующей папки, получено %+v", none)
	}
}
