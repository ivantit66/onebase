package storage

// Тест переключателя команд ОС (план 67): по умолчанию выключен; флаг
// exec.enabled в _settings включает/выключает.

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExecEnabled_DefaultOff(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "exec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	if db.GetExecEnabled(ctx) {
		t.Fatal("по умолчанию выполнение команд ОС должно быть выключено")
	}
	if err := db.SaveExecEnabled(ctx, true); err != nil {
		t.Fatalf("SaveExecEnabled: %v", err)
	}
	if !db.GetExecEnabled(ctx) {
		t.Fatal("после включения должно быть разрешено")
	}
	if err := db.SaveExecEnabled(ctx, false); err != nil {
		t.Fatalf("SaveExecEnabled: %v", err)
	}
	if db.GetExecEnabled(ctx) {
		t.Fatal("после выключения снова запрещено")
	}
}
