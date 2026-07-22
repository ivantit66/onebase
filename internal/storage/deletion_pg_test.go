//go:build integration

package storage

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/metadata"
)

func TestDeletion_PostgresWithoutOptionalColumns(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	db, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.EnsureAuditSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}

	for _, kind := range []metadata.Kind{metadata.KindDocument, metadata.KindCatalog} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			name := "DeletionPG" + strings.ReplaceAll(uuid.NewString(), "-", "")
			entity := &metadata.Entity{
				Name:   name,
				Kind:   kind,
				Fields: []metadata.Field{{Name: "Название", Type: metadata.FieldTypeString}},
			}
			table := metadata.TableName(name)
			t.Cleanup(func() {
				_, _ = db.Exec(context.Background(), "DROP TABLE IF EXISTS "+pgQuoteIdent(table))
			})

			if err := db.Migrate(ctx, []*metadata.Entity{entity}); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			id := uuid.New()
			if err := db.Upsert(ctx, name, id, map[string]any{"Название": "Проверка"}, entity); err != nil {
				t.Fatalf("upsert: %v", err)
			}

			if err := db.WithTx(ctx, func(txCtx context.Context) error {
				return db.MarkForDeletion(txCtx, name, id, true)
			}); err != nil {
				t.Fatalf("mark for deletion in transaction: %v", err)
			}
			marked, err := db.IsMarkedForDeletion(ctx, name, id)
			if err != nil {
				t.Fatalf("read deletion mark: %v", err)
			}
			if !marked {
				t.Fatal("deletion mark was not set")
			}

			if err := db.WithTx(ctx, func(txCtx context.Context) error {
				return db.Delete(txCtx, name, id)
			}); err != nil {
				t.Fatalf("delete in transaction: %v", err)
			}
			var count int
			if err := db.QueryRow(ctx,
				"SELECT COUNT(*) FROM "+table+" WHERE id = $1", id,
			).Scan(&count); err != nil {
				t.Fatalf("count after delete: %v", err)
			}
			if count != 0 {
				t.Fatalf("record still exists after delete: count=%d", count)
			}
		})
	}
}
