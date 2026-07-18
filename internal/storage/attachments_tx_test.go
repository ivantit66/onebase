package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestAttachmentFilesFollowTransactionOutcome(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, filepath.Join(t.TempDir(), "attachments.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	filesDir := filepath.Join(t.TempDir(), "files")
	db.SetFilesDir(filesDir)
	if err := db.EnsureAttachmentTable(ctx); err != nil {
		t.Fatal(err)
	}

	ownerID := uuid.New()
	tx, txCtx, err := db.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := db.UploadAttachment(txCtx, "catalog", "Товары", ownerID,
		"rollback.txt", "text/plain", "", bytes.NewBufferString("rollback"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	rolledBackPath := filepath.Join(filesDir, "Товары", rolledBack.ID.String())
	if _, err := os.Stat(rolledBackPath); err != nil {
		t.Fatalf("uploaded file missing before rollback: %v", err)
	}
	if err := tx.Rollback(txCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rolledBackPath); !os.IsNotExist(err) {
		t.Fatalf("rolled-back upload left file on disk: %v", err)
	}
	if _, err := db.GetAttachment(ctx, rolledBack.ID); !IsNotFound(err) {
		t.Fatalf("rolled-back upload left metadata: %v", err)
	}

	kept, err := db.UploadAttachment(ctx, "catalog", "Товары", ownerID,
		"keep.txt", "text/plain", "", bytes.NewBufferString("keep"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	keptPath := filepath.Join(filesDir, "Товары", kept.ID.String())
	tx, txCtx, err = db.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteAttachment(txCtx, kept.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keptPath); err != nil {
		t.Fatalf("file removed before transaction outcome: %v", err)
	}
	if err := tx.Rollback(txCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAttachment(ctx, kept.ID); err != nil {
		t.Fatalf("rollback did not restore attachment metadata: %v", err)
	}
	if _, err := os.Stat(keptPath); err != nil {
		t.Fatalf("rollback did not preserve attachment file: %v", err)
	}

	tx, txCtx, err = db.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteAttachment(txCtx, kept.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(txCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keptPath); !os.IsNotExist(err) {
		t.Fatalf("committed delete left file on disk: %v", err)
	}
	if _, err := db.GetAttachment(ctx, kept.ID); !IsNotFound(err) {
		t.Fatalf("committed delete left metadata: %v", err)
	}
}
