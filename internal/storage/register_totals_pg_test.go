//go:build integration

package storage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

func TestRegisterTotals_PostgresLifecycleAndUTCBucket(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	name := "TotalsPG" + strings.ReplaceAll(uuid.NewString(), "-", "")
	reg := &metadata.Register{
		Name:       name,
		Dimensions: []metadata.Field{{Name: "Товар", Type: metadata.FieldTypeString}},
		Resources:  []metadata.Field{{Name: "Количество", Type: metadata.FieldTypeNumber}},
		Totals:     metadata.RegisterTotals{Enabled: true},
	}
	defer func() {
		_, _ = db.Exec(context.Background(), "DROP TABLE IF EXISTS "+metadata.RegisterTotalsTableName(name))
		_, _ = db.Exec(context.Background(), "DROP TABLE IF EXISTS "+metadata.RegisterTableName(name))
		_, _ = db.Exec(context.Background(), "DELETE FROM "+totalsMetaTable+" WHERE register_name = $1", strings.ToLower(name))
	}()
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatal(err)
	}
	moscow := time.FixedZone("UTC+3", 3*60*60)
	period := time.Date(2026, 3, 1, 0, 30, 0, 0, moscow) // still February in UTC
	if err := db.WriteMovements(ctx, name, "Док", uuid.New(), []map[string]any{
		{"ВидДвижения": "Приход", "Товар": "A", "Количество": 5},
	}, reg, &period); err != nil {
		t.Fatal(err)
	}
	var month string
	if err := db.QueryRow(ctx, "SELECT "+totalsMonthCol+" FROM "+metadata.RegisterTotalsTableName(name)).Scan(&month); err != nil {
		t.Fatal(err)
	}
	if month != "2026-02" {
		t.Fatalf("UTC month bucket=%q, want 2026-02", month)
	}

	disabled := *reg
	disabled.Totals.Enabled = false
	if err := db.MigrateRegisters(ctx, []*metadata.Register{&disabled}); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteMovements(ctx, name, "Док", uuid.New(), []map[string]any{
		{"ВидДвижения": "Приход", "Товар": "A", "Количество": 3},
	}, &disabled, &period); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateRegisters(ctx, []*metadata.Register{reg}); err != nil {
		t.Fatal(err)
	}
	var total float64
	if err := db.QueryRow(ctx, "SELECT SUM(количество) FROM "+metadata.RegisterTotalsTableName(name)).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 8 {
		t.Fatalf("re-enabled total=%v, want 8", total)
	}

	changed := *reg
	changed.Dimensions = append(append([]metadata.Field{}, reg.Dimensions...), metadata.Field{Name: "Партия", Type: metadata.FieldTypeString})
	changed.Resources = append(append([]metadata.Field{}, reg.Resources...), metadata.Field{Name: "Сумма", Type: metadata.FieldTypeNumber})
	if err := db.MigrateRegisters(ctx, []*metadata.Register{&changed}); err != nil {
		t.Fatalf("schema rebuild: %v", err)
	}
	var nullRows int
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE партия IS NULL", metadata.RegisterTotalsTableName(name))
	if err := db.QueryRow(ctx, q).Scan(&nullRows); err != nil || nullRows == 0 {
		t.Fatalf("nullable added dimension was not rebuilt: rows=%d err=%v", nullRows, err)
	}
}
