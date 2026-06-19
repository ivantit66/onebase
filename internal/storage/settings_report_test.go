package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReportUserSettings(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.EnsureSettingsSchema(ctx); err != nil {
		t.Fatalf("EnsureSettingsSchema: %v", err)
	}

	// Пусто до сохранения.
	got, err := db.GetReportUserSettings(ctx, "Продажи", "alice")
	if err != nil {
		t.Fatalf("Get(пусто): %v", err)
	}
	if got != "" {
		t.Fatalf("ожидали пусто, получили %q", got)
	}

	// Сохранение и чтение.
	raw := `{"variant":"X"}`
	if err := db.SaveReportUserSettings(ctx, "Продажи", "alice", raw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err = db.GetReportUserSettings(ctx, "Продажи", "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != raw {
		t.Fatalf("Get: хотели %q, получили %q", raw, got)
	}

	// Настройки разных пользователей не пересекаются (ключи …alice vs …bob).
	if err := db.SaveReportUserSettings(ctx, "Продажи", "bob", `{"variant":"Y"}`); err != nil {
		t.Fatalf("Save(bob): %v", err)
	}
	if alice, _ := db.GetReportUserSettings(ctx, "Продажи", "alice"); alice != raw {
		t.Fatalf("alice затёрта bob: %q", alice)
	}
	if bob, _ := db.GetReportUserSettings(ctx, "Продажи", "bob"); bob != `{"variant":"Y"}` {
		t.Fatalf("bob: %q", bob)
	}

	// Сброс (Delete) затрагивает только alice.
	if err := db.DeleteReportUserSettings(ctx, "Продажи", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := db.GetReportUserSettings(ctx, "Продажи", "alice"); got != "" {
		t.Fatalf("после Delete ожидали пусто, получили %q", got)
	}
	if bob, _ := db.GetReportUserSettings(ctx, "Продажи", "bob"); bob == "" {
		t.Fatal("Delete(alice) не должен затрагивать bob")
	}
}
