package widget

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

func TestFormatKPI_Money(t *testing.T) {
	got := formatKPI(1234567.5, "money")
	// 1 234 567,50 ₽ — NBSP is replaced with regular space in groupDigits
	want := "1 234 567,50 ₽"
	if got != want {
		t.Errorf("money: got %q, want %q", got, want)
	}
}

func TestFormatKPI_Number(t *testing.T) {
	if got := formatKPI(42.0, "number"); got != "42" {
		t.Errorf("number 42: got %q", got)
	}
	if got := formatKPI(1000000.0, "number"); got != "1 000 000" {
		t.Errorf("number 1m: got %q", got)
	}
}

func TestFormatKPI_Percent(t *testing.T) {
	if got := formatKPI(12.345, "percent"); got != "12.3%" {
		t.Errorf("percent: got %q", got)
	}
}

func TestFormatKPI_DefaultInteger(t *testing.T) {
	if got := formatKPI(5.0, ""); got != "5" {
		t.Errorf("default int: got %q", got)
	}
}

func TestFormatKPI_DefaultFloat(t *testing.T) {
	got := formatKPI(3.14, "")
	if got != "3.14" {
		t.Errorf("default float: got %q", got)
	}
}

func TestGroupDigits(t *testing.T) {
	cases := map[int64]string{
		0:        "0",
		7:        "7",
		1234:     "1 234",
		12345:    "12 345",
		1000000:  "1 000 000",
		12345678: "12 345 678",
	}
	for in, want := range cases {
		if got := groupDigits(in); got != want {
			t.Errorf("groupDigits(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstScalar(t *testing.T) {
	row := map[string]any{"x": 42}
	if got := firstScalar(row); got != 42 {
		t.Errorf("firstScalar = %v", got)
	}
	if got := firstScalar(map[string]any{}); got != nil {
		t.Errorf("empty row should give nil, got %v", got)
	}
}

func TestToFloat(t *testing.T) {
	if toFloat(int64(42)) != 42 {
		t.Error("int64")
	}
	if toFloat("3.14") != 3.14 {
		t.Error("string")
	}
	if toFloat(nil) != 0 {
		t.Error("nil")
	}
}

func TestFormatMoney_Negative(t *testing.T) {
	got := formatMoney(-1500.25)
	if !strings.Contains(got, "-") {
		t.Errorf("expected leading -, got %q", got)
	}
	if !strings.Contains(got, "25") {
		t.Errorf("expected fractional 25 kop, got %q", got)
	}
}

func TestRunQuery_RowAccessFailsClosed(t *testing.T) {
	ctx, runner, _ := newRowAccessRunner(t)
	res := runner.Run(ctx, &metadata.Widget{
		Name:  "Товары",
		Type:  metadata.WidgetTypeList,
		Query: "ВЫБРАТЬ Наименование ИЗ Справочник.Товар",
	})
	if !strings.Contains(res.Error, "строковые ограничения") {
		t.Fatalf("expected row-access fail-closed error, got %q", res.Error)
	}
}

func TestRunRecent_RowAccessFiltersHiddenRows(t *testing.T) {
	ctx, runner, entity := newRowAccessRunner(t)
	if err := runner.Store.EnsureAuditSchema(ctx); err != nil {
		t.Fatalf("EnsureAuditSchema: %v", err)
	}

	allowedID := uuid.New()
	hiddenID := uuid.New()
	if err := runner.Store.Upsert(ctx, entity.Name, allowedID, map[string]any{"Наименование": "Allowed", "Owner": "u"}, entity); err != nil {
		t.Fatalf("upsert allowed: %v", err)
	}
	if err := runner.Store.Upsert(ctx, entity.Name, hiddenID, map[string]any{"Наименование": "Hidden", "Owner": "other"}, entity); err != nil {
		t.Fatalf("upsert hidden: %v", err)
	}
	for _, id := range []uuid.UUID{hiddenID, allowedID} {
		if err := runner.Store.Log(ctx, &storage.AuditEntry{
			UserLogin:  "u",
			Action:     "update",
			EntityKind: string(entity.Kind),
			EntityName: entity.Name,
			RecordID:   id.String(),
		}); err != nil {
			t.Fatalf("Log(%s): %v", id, err)
		}
	}

	res := runner.Run(ctx, &metadata.Widget{
		Name:     "Недавние",
		Type:     metadata.WidgetTypeRecent,
		Limit:    10,
		Scope:    "all",
		Entities: []string{entity.Name},
	})
	if res.Error != "" {
		t.Fatalf("Run recent error: %s", res.Error)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("expected only one visible recent row, got %#v", res.Rows)
	}
	if got := fmt.Sprint(res.Rows[0]["record_id"]); got != allowedID.String() {
		t.Fatalf("visible recent row = %s, want %s; rows=%#v", got, allowedID, res.Rows)
	}
}

func newRowAccessRunner(t *testing.T) (context.Context, *Runner, *metadata.Entity) {
	t.Helper()

	ctx := context.Background()
	entity := &metadata.Entity{
		Name: "Товар",
		Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "Owner", Type: metadata.FieldTypeString},
		},
	}
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "widgets-rls.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: []*metadata.Entity{entity}})

	runner := &Runner{
		Reg:         reg,
		Store:       db,
		CurrentUser: "u",
		User: &auth.User{Login: "u", Roles: []*auth.Role{{
			Permissions: auth.Permission{
				Catalogs: map[string][]string{entity.Name: {"read"}},
				RowAccess: auth.RowAccess{Catalogs: map[string]auth.RowPolicies{
					entity.Name: {"read": {Field: "Owner", Op: "eq", Value: auth.RowValue{Literal: "u"}}},
				}},
			},
		}}},
	}
	return ctx, runner, entity
}
