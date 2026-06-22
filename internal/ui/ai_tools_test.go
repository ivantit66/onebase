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
	"github.com/ivantit66/onebase/internal/report"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

func aiToolsTestServer(t *testing.T) *Server {
	t.Helper()
	cat := &metadata.Entity{
		Name: "Товар",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Цена", Type: metadata.FieldTypeNumber},
		},
	}
	s, _ := newSubmitTestServer(t, []*metadata.Entity{cat})
	return s
}

func TestAISchemaText(t *testing.T) {
	s := aiToolsTestServer(t)
	txt := s.aiSchemaText(context.Background())
	if !strings.Contains(txt, "Товар") {
		t.Fatalf("в описании нет справочника Товар: %s", txt)
	}
	if !strings.Contains(txt, "Наименование") || !strings.Contains(txt, "Цена") {
		t.Fatalf("в описании нет полей: %s", txt)
	}
}

// TestAISchemaText_NonDataObjects проверяет, что в карту конфигурации попадают не
// только источники данных (справочники/документы/регистры), но и отчёты,
// перечисления и прочие метаданные — иначе ИИ-чат «не видит» готовые отчёты и
// отвечает, что их нет (был такой баг с отчётом ВаловаяПрибыль).
func TestAISchemaText_NonDataObjects(t *testing.T) {
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{
		Reports: []*report.Report{
			{Name: "ВаловаяПрибыль", Title: "Отчёт по валовой прибыли (ФИФО)"},
		},
		Enums: []*metadata.Enum{
			{Name: "СтатусЗаказа", Values: []string{"Новый", "Выполнен"}},
		},
	})
	s := &Server{reg: reg}
	txt := s.aiSchemaText(context.Background())
	if !strings.Contains(txt, "Отчёты") || !strings.Contains(txt, "ВаловаяПрибыль") ||
		!strings.Contains(txt, "Отчёт по валовой прибыли (ФИФО)") {
		t.Fatalf("в карте конфигурации нет отчёта: %s", txt)
	}
	if !strings.Contains(txt, "Перечисления") || !strings.Contains(txt, "СтатусЗаказа") ||
		!strings.Contains(txt, "Выполнен") {
		t.Fatalf("в карте конфигурации нет перечисления: %s", txt)
	}
}

func TestAIRunQueryValid(t *testing.T) {
	s := aiToolsTestServer(t)
	res := s.aiRunQuery(context.Background(), llm.ToolCall{
		ID:    "q1",
		Input: map[string]any{"запрос": "ВЫБРАТЬ Наименование ИЗ Справочник.Товар"},
	})
	if res.IsError {
		t.Fatalf("валидный запрос дал ошибку: %s", res.Content)
	}
	if !strings.Contains(res.Content, "строк") {
		t.Fatalf("в результате нет поля строк: %s", res.Content)
	}
}

func TestAIRunQueryInvalid(t *testing.T) {
	s := aiToolsTestServer(t)
	res := s.aiRunQuery(context.Background(), llm.ToolCall{
		ID:    "q2",
		Input: map[string]any{"запрос": "это не запрос"},
	})
	if !res.IsError {
		t.Fatalf("ожидалась ошибка на некорректный запрос, получено: %s", res.Content)
	}
}

func TestAIRunQueryEmpty(t *testing.T) {
	s := aiToolsTestServer(t)
	res := s.aiRunQuery(context.Background(), llm.ToolCall{ID: "q3", Input: map[string]any{"запрос": "   "}})
	if !res.IsError {
		t.Fatal("ожидалась ошибка на пустой запрос")
	}
}

// Регрессия #14: в режиме rbac запрос наименования связанной сущности через
// ссылочное поле (Контрагент.Наименование) обязан проверять право чтения на
// связанный справочник, а не только на главный документ. Раньше авто-JOIN
// ссылки не регистрировал источник → утечка наименований в обход RBAC.
func TestAIRunQuery_RBACRefDimLeak(t *testing.T) {
	cp := &metadata.Entity{
		Name: "Контрагент", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}},
	}
	order := &metadata.Entity{
		Name: "Заказ", Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Контрагент", Type: "reference:Контрагент", RefEntity: "Контрагент"},
		},
	}
	s, _ := newSubmitTestServer(t, []*metadata.Entity{cp, order})
	ctx := context.Background()
	if err := s.store.SaveAIDataScope(ctx, storage.AIDataScopeRBAC); err != nil {
		t.Fatal(err)
	}

	run := func(user *auth.User) llm.ToolResult {
		uctx := auth.ContextWithUser(ctx, user)
		return s.aiRunQuery(uctx, llm.ToolCall{
			ID:    "ref",
			Input: map[string]any{"запрос": "ВЫБРАТЬ Контрагент.Наименование ИЗ Документ.Заказ"},
		})
	}

	// Право только на документ Заказ — наименования контрагентов читать нельзя.
	docOnly := &auth.User{Login: "u", Roles: []*auth.Role{{
		Permissions: auth.Permission{Documents: map[string][]string{"Заказ": {"read"}}},
	}}}
	if res := run(docOnly); !res.IsError {
		t.Fatalf("без права на Контрагент ожидался отказ (утечка через ссылку), получено: %s", res.Content)
	}

	// С правом и на документ, и на связанный справочник — отказа быть не должно.
	both := &auth.User{Login: "u2", Roles: []*auth.Role{{
		Permissions: auth.Permission{
			Documents: map[string][]string{"Заказ": {"read"}},
			Catalogs:  map[string][]string{"Контрагент": {"read"}},
		},
	}}}
	if res := run(both); res.IsError {
		t.Fatalf("с правом на оба объекта отказа быть не должно: %s", res.Content)
	}
}

// TestAITools_NonAdminGetsNoTools проверяет, что не-администратор не получает
// инструменты ИИ-чата. Для этого создаётся реальный authRepo со схемой и одним
// пользователем (HasUsers()==true), а запрос не несёт пользователя в контексте
// → UserFromContext==nil → isAdmin==false → aiTools возвращает (nil, nil).
func TestAITools_NonAdminGetsNoTools(t *testing.T) {
	ctx := context.Background()

	// Поднимаем отдельную БД для auth-репо.
	authDB, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { authDB.Close() })

	repo := auth.NewRepo(authDB)
	if err := repo.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	// Создаём пользователя: теперь HasUsers()==true.
	if _, err := repo.Create(ctx, "testuser", "password1", "Test User", false); err != nil {
		t.Fatal(err)
	}

	// Строим сервер аналогично aiToolsTestServer, но с authRepo.
	s, _ := newSubmitTestServer(t, nil)
	s.authRepo = repo

	// Запрос без пользователя в контексте → isAdmin==false.
	r := httptest.NewRequest(http.MethodPost, "/ui/ai/chat", nil)
	tools, exec := s.aiTools(r)
	if tools != nil || exec != nil {
		t.Fatalf("не-админ не должен получать инструменты ИИ: tools=%v exec!=nil=%v", tools, exec != nil)
	}
}

// TestAITools_AdminGetsTools — положительный контраст: сервер без authRepo
// (эквивалент открытого доступа/администратора) возвращает непустой набор инструментов.
func TestAITools_AdminGetsTools(t *testing.T) {
	s := aiToolsTestServer(t) // authRepo == nil → isAdmin всегда true
	r := httptest.NewRequest(http.MethodPost, "/ui/ai/chat", nil)
	tools, exec := s.aiTools(r)
	if tools == nil {
		t.Fatal("администратор должен получать инструменты ИИ, получено nil")
	}
	if exec == nil {
		t.Fatal("администратор должен получать исполнитель инструментов, получено nil")
	}
}

// Делегирование в aicontext: ui-путь теперь отдаёт ТЧ и пометку проведения.
func TestAISchemaText_TablePartsAndPosting(t *testing.T) {
	doc := &metadata.Entity{
		Name: "Заказ", Kind: metadata.KindDocument, Posting: true,
		Fields: []metadata.Field{{Name: "Дата", Type: metadata.FieldTypeDate}},
		TableParts: []metadata.TablePart{
			{Name: "Товары", Fields: []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}}},
		},
	}
	s, _ := newSubmitTestServer(t, []*metadata.Entity{doc})
	txt := s.aiSchemaText(context.Background())
	for _, sub := range []string{"Заказ", "(проводится)", "ТЧ Товары", "Количество"} {
		if !strings.Contains(txt, sub) {
			t.Fatalf("в срезе нет %q: %s", sub, txt)
		}
	}
}

// TestAITools_FlaggedUserGetsTools проверяет, что не-администратор с флагом
// AIDataAccess получает инструменты ИИ-чата в режиме rbac. Прогоняет весь путь
// флага: схема (ALTER) → Update (сохранение) → GetByID (чтение) →
// ContextWithUser (гейт). В дефолтном admin_only флаг не действует — см.
// TestAITools_FlaggedUser_DefaultScope_NoTools.
func TestAITools_FlaggedUserGetsTools(t *testing.T) {
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
	// Выдаём доступ ИИ-чата к данным, оставаясь не-администратором.
	if err := repo.Update(ctx, u.ID, "Flagged User", false, false, false, true); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsAdmin || !got.AIDataAccess {
		t.Fatalf("ожидался не-админ с AIDataAccess=true, получено IsAdmin=%v AIDataAccess=%v", got.IsAdmin, got.AIDataAccess)
	}

	s, _ := newSubmitTestServer(t, nil)
	s.authRepo = repo
	// Режим rbac: флаг AIDataAccess действует (с фильтрацией источников по правам).
	if err := s.store.SaveAIDataScope(ctx, storage.AIDataScopeRBAC); err != nil {
		t.Fatal(err)
	}

	// Запрос несёт пользователя с флагом → aiDataAllowed==true.
	r := httptest.NewRequest(http.MethodPost, "/ui/ai/chat", nil)
	r = r.WithContext(auth.ContextWithUser(r.Context(), got))
	tools, exec := s.aiTools(r)
	if tools == nil || exec == nil {
		t.Fatalf("пользователь с AIDataAccess должен получать инструменты ИИ: tools=%v exec!=nil=%v", tools, exec != nil)
	}
}

func TestAISchemaText_RBACFiltered(t *testing.T) {
	ctx := context.Background()
	pub := &metadata.Entity{Name: "Товар", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Наименование", Type: metadata.FieldTypeString}}}
	secret := &metadata.Entity{Name: "Секрет", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{{Name: "Код", Type: metadata.FieldTypeString}}}
	s, _ := newSubmitTestServer(t, []*metadata.Entity{pub, secret})
	if err := s.store.SaveAIDataScope(ctx, storage.AIDataScopeRBAC); err != nil {
		t.Fatal(err)
	}
	// Не-админ с правом read только на «Товар».
	user := &auth.User{Login: "u", Roles: []*auth.Role{{
		Permissions: auth.Permission{Catalogs: map[string][]string{"Товар": {"read"}}},
	}}}
	uctx := auth.ContextWithUser(ctx, user)
	txt := s.aiSchemaText(uctx)
	if !strings.Contains(txt, "Товар") {
		t.Fatalf("разрешённый объект отсутствует: %s", txt)
	}
	if strings.Contains(txt, "Секрет") {
		t.Fatalf("запрещённый объект просочился в схему: %s", txt)
	}
}
