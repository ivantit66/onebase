package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestBlobRoundtrip_DiskAndDB проверяет оба режима хранения бинарников (как тома
// на диске и в БД в 1С): запись, чтение, наличие/отсутствие файла на диске,
// удаление.
func TestBlobRoundtrip_DiskAndDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := ConnectSQLite(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	defer db.Close()
	db.filesDir = filepath.Join(dir, "files")
	if err := db.EnsureBlobTable(ctx); err != nil {
		t.Fatalf("EnsureBlobTable: %v", err)
	}

	payload := []byte("\x89PNG\r\n\x1a\n притворимся картинкой")

	t.Run("disk", func(t *testing.T) {
		b, err := db.PutBlob(ctx, "image/png", bytes.NewReader(payload), 1<<20)
		if err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
		if b.Mime != "image/png" || b.Size != int64(len(payload)) {
			t.Fatalf("метаданные blob: mime=%q size=%d", b.Mime, b.Size)
		}
		fp := filepath.Join(db.filesDir, blobsDirName, b.ID.String())
		if _, err := os.Stat(fp); err != nil {
			t.Fatalf("на диске нет файла бинарника: %v", err)
		}
		if got := readBlobBytes(t, db, b.ID); !bytes.Equal(got, payload) {
			t.Fatalf("содержимое не совпало: %d байт", len(got))
		}
		if err := db.DeleteBlob(ctx, b.ID); err != nil {
			t.Fatalf("DeleteBlob: %v", err)
		}
		if _, _, err := db.OpenBlob(ctx, b.ID); err == nil {
			t.Fatal("OpenBlob после удаления должен вернуть ошибку")
		}
	})

	t.Run("db", func(t *testing.T) {
		if err := db.SaveFileStorageMode(ctx, FileStorageDB); err != nil {
			t.Fatalf("SaveFileStorageMode: %v", err)
		}
		if got := db.GetFileStorageMode(ctx); got != FileStorageDB {
			t.Fatalf("режим = %q, ожидался db", got)
		}
		b, err := db.PutBlob(ctx, "image/jpeg", bytes.NewReader(payload), 1<<20)
		if err != nil {
			t.Fatalf("PutBlob(db): %v", err)
		}
		fp := filepath.Join(db.filesDir, blobsDirName, b.ID.String())
		if _, err := os.Stat(fp); !os.IsNotExist(err) {
			t.Fatalf("в db-режиме файла на диске быть не должно, stat err=%v", err)
		}
		if got := readBlobBytes(t, db, b.ID); !bytes.Equal(got, payload) {
			t.Fatalf("содержимое (db) не совпало: %d байт", len(got))
		}
	})
}

func readBlobBytes(t *testing.T, db *DB, id uuid.UUID) []byte {
	t.Helper()
	_, rc, err := db.OpenBlob(context.Background(), id)
	if err != nil {
		t.Fatalf("OpenBlob: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return data
}
