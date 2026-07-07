package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/llm"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/storage"
)

// runAIQuery выполняет запрос к справочнику Товар от имени user (nil = открытый
// деплой без пользователей).
func runAIQuery(s *Server, user *auth.User) llm.ToolResult {
	ctx := context.Background()
	if user != nil {
		ctx = auth.ContextWithUser(ctx, user)
	}
	return s.aiRunQuery(ctx, llm.ToolCall{
		ID:    "q",
		Input: map[string]any{"запрос": "ВЫБРАТЬ Наименование ИЗ Справочник.Товар"},
	})
}

// TestAIRunQuery_RBACScope: в режиме rbac запрос фильтруется по правам чтения на
// объекты-источники — без права на Товар ассистент получает отказ, QueryAll не
// выполняется (план 54, объектный RBAC для ИИ).
func TestAIRunQuery_RBACScope(t *testing.T) {
	s := aiToolsTestServer(t)
	if err := s.store.SaveAIDataScope(context.Background(), storage.AIDataScopeRBAC); err != nil {
		t.Fatal(err)
	}

	if res := runAIQuery(s, catalogUser("Другое", "read")); !res.IsError {
		t.Fatalf("без прав на Товар ожидался отказ, получено: %s", res.Content)
	}
	if res := runAIQuery(s, catalogUser("Товар", "read")); res.IsError {
		t.Fatalf("с правом read на Товар отказа быть не должно: %s", res.Content)
	}
	if res := runAIQuery(s, nil); res.IsError {
		t.Fatalf("в открытом деплое (без пользователей) отказа быть не должно: %s", res.Content)
	}
}

// TestAIRunQuery_DefaultScope_NoFilter: в дефолтном режиме admin_only объектная
// фильтрация источников не применяется (инструменты и так только у админов).
func TestAIRunQuery_DefaultScope_NoFilter(t *testing.T) {
	s := aiToolsTestServer(t)
	if res := runAIQuery(s, catalogUser("Другое", "read")); res.IsError {
		t.Fatalf("в режиме admin_only фильтрации быть не должно: %s", res.Content)
	}
}

func TestAIRunQuery_RowAccessFailClosed(t *testing.T) {
	cat := &metadata.Entity{
		Name: "Товар", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Owner", Type: metadata.FieldTypeString},
		},
	}
	s, _ := newSubmitTestServer(t, []*metadata.Entity{cat})

	user := &auth.User{Login: "u", Roles: []*auth.Role{{
		Permissions: auth.Permission{
			Catalogs: map[string][]string{"Товар": {"read"}},
			RowAccess: auth.RowAccess{Catalogs: map[string]auth.RowPolicies{
				"Товар": {"read": {Field: "Owner", Op: "eq", Value: auth.RowValue{User: "login"}}},
			}},
		},
	}}}
	res := runAIQuery(s, user)
	if !res.IsError || !strings.Contains(res.Content, "строковые ограничения") {
		t.Fatalf("ожидался fail-closed для row_access в произвольном запросе, получено: err=%v content=%s", res.IsError, res.Content)
	}
}

// flaggedUserAndServer поднимает сервер с authRepo и одним не-админом, имеющим
// флаг AIDataAccess, и запрос, несущий этого пользователя.
func flaggedUserAndServer(t *testing.T) (*Server, *http.Request) {
	t.Helper()
	ctx := context.Background()
	authDB, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { authDB.Close() })
	repo := auth.NewRepo(authDB)
	if err := repo.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := repo.Create(ctx, "flagged", "password1", "Flagged User", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, u.ID, "Flagged User", false, false, false, true); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newSubmitTestServer(t, nil)
	s.authRepo = repo
	r := httptest.NewRequest(http.MethodPost, "/ui/ai/chat", nil)
	r = r.WithContext(auth.ContextWithUser(r.Context(), got))
	return s, r
}

// TestAITools_FlaggedUser_DefaultScope_NoTools: безопасный дефолт — в режиме
// admin_only флаг AIDataAccess не даёт не-админу инструментов данных.
func TestAITools_FlaggedUser_DefaultScope_NoTools(t *testing.T) {
	s, r := flaggedUserAndServer(t)
	if tools, exec := s.aiTools(r); tools != nil || exec != nil {
		t.Fatalf("в режиме admin_only флаг не должен давать инструменты: tools=%v exec!=nil=%v", tools, exec != nil)
	}
}
