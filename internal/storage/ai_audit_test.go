package storage

// Тесты журнала ИИ-обращений (план 54, этап 2) и агрегата токенов для
// суточного потолка (этап 3).

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newAIAuditDB(t *testing.T) (*DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "ai.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.EnsureAIAuditSchema(ctx); err != nil {
		t.Fatalf("EnsureAIAuditSchema: %v", err)
	}
	return db, ctx
}

func TestAIAudit_StoresAndReadsResponse(t *testing.T) {
	db, ctx := newAIAuditDB(t)
	db.LogAIQuery(ctx, AIAuditEntry{
		Task: "конфигуратор-генерация", Model: "glm-4.6",
		Query: "справочник Клиенты", Response: "создан catalogs/клиенты.yaml",
		InputTokens: 12, OutputTokens: 34,
	})
	got, err := db.ListAIAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAIAudit: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ожидалась 1 запись, получено %d", len(got))
	}
	if got[0].Response != "создан catalogs/клиенты.yaml" {
		t.Errorf("Response не сохранён/прочитан: %q", got[0].Response)
	}
}

func TestAIAudit_LogAndList(t *testing.T) {
	db, ctx := newAIAuditDB(t)

	db.LogAIQuery(ctx, AIAuditEntry{
		UserID: "u1", UserLogin: "ivan", Task: "чат", Model: "claude-x",
		Query: "ВЫБРАТЬ * ИЗ Товары", Rows: 7, InputTokens: 120, OutputTokens: 40,
	})

	entries, err := db.ListAIAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAIAudit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ожидалась 1 запись, получено %d", len(entries))
	}
	e := entries[0]
	if e.UserLogin != "ivan" || e.Model != "claude-x" || e.Query != "ВЫБРАТЬ * ИЗ Товары" ||
		e.Rows != 7 || e.InputTokens != 120 || e.OutputTokens != 40 {
		t.Fatalf("поля записи не совпали: %+v", e)
	}
	if e.At.IsZero() {
		t.Fatal("время записи не заполнено")
	}
}

func TestAIAudit_TokensUsedSince(t *testing.T) {
	db, ctx := newAIAuditDB(t)

	db.LogAIQuery(ctx, AIAuditEntry{UserLogin: "a", Task: "чат", InputTokens: 100, OutputTokens: 50})
	db.LogAIQuery(ctx, AIAuditEntry{UserLogin: "b", Task: "чат", InputTokens: 10, OutputTokens: 5})

	used, err := db.AITokensUsedSince(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("AITokensUsedSince: %v", err)
	}
	if used != 165 {
		t.Fatalf("ожидалось 165 токенов, получено %d", used)
	}

	used, err = db.AITokensUsedSince(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("AITokensUsedSince(будущее): %v", err)
	}
	if used != 0 {
		t.Fatalf("за будущий период ожидалось 0, получено %d", used)
	}
}

func TestAISettings_DailyTokenCap(t *testing.T) {
	db, ctx := newAIAuditDB(t)

	if cap := db.GetAIDailyTokenCap(ctx); cap != 0 {
		t.Fatalf("без настройки ожидался 0 (без лимита), получено %d", cap)
	}
	if err := db.SaveAIDailyTokenCap(ctx, 50000); err != nil {
		t.Fatalf("SaveAIDailyTokenCap: %v", err)
	}
	if cap := db.GetAIDailyTokenCap(ctx); cap != 50000 {
		t.Fatalf("ожидалось 50000, получено %d", cap)
	}
}

func TestAISettings_DataScope(t *testing.T) {
	db, ctx := newAIAuditDB(t)

	if sc := db.GetAIDataScope(ctx); sc != AIDataScopeAdminOnly {
		t.Fatalf("без настройки ожидался admin_only, получено %q", sc)
	}
	if err := db.SaveAIDataScope(ctx, AIDataScopeRBAC); err != nil {
		t.Fatalf("SaveAIDataScope: %v", err)
	}
	if sc := db.GetAIDataScope(ctx); sc != AIDataScopeRBAC {
		t.Fatalf("ожидалось rbac, получено %q", sc)
	}
	// Неизвестное значение нормализуется в безопасный дефолт admin_only.
	if err := db.SaveAIDataScope(ctx, "чтотопопало"); err != nil {
		t.Fatalf("SaveAIDataScope(invalid): %v", err)
	}
	if sc := db.GetAIDataScope(ctx); sc != AIDataScopeAdminOnly {
		t.Fatalf("некорректный режим должен стать admin_only, получено %q", sc)
	}
}
