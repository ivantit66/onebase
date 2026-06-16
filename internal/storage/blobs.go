package storage

// Хранилище бинарников (картинки полей типа image). Поддерживает два режима,
// как хранилища файлов в 1С: «тома на диске» (FileStorageDisk, по умолчанию) и
// «в информационной базе» (FileStorageDB, BLOB-колонка). Поле image в таблице
// сущности хранит только ссылку — UUID бинарника; раздаётся отдельным HTTP-
// обработчиком.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/i18n/i18nerr"
)

// Blob — метаданные бинарника. Содержимое лежит либо на диске
// (filesDir/_blobs/<id>), либо в колонке data таблицы _blobs.
type Blob struct {
	ID   uuid.UUID
	Mime string
	Size int64
}

// blobsDirName — подкаталог filesDir для дискового режима хранения.
const blobsDirName = "_blobs"

// EnsureBlobTable создаёт таблицу _blobs (метаданные + данные для db-режима).
// Колонка data заполняется только в режиме FileStorageDB; на диске она NULL.
func (db *DB) EnsureBlobTable(ctx context.Context) error {
	d := db.dialect
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS _blobs (
			id   %s PRIMARY KEY,
			mime TEXT NOT NULL DEFAULT '',
			size BIGINT NOT NULL DEFAULT 0,
			data %s
		)`, d.TypeUUID(), d.TypeBytes())
	if _, err := db.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("blobs: create _blobs: %w", err)
	}
	return nil
}

// PutBlob сохраняет бинарник и возвращает его метаданные с новым ID. Режим
// (disk|db) берётся из ui.file_storage. Размер ограничен maxSizeBytes
// (<=0 → 50 МБ по умолчанию).
func (db *DB) PutBlob(ctx context.Context, mime string, r io.Reader, maxSizeBytes int64) (Blob, error) {
	if maxSizeBytes <= 0 {
		maxSizeBytes = 50 * 1024 * 1024
	}
	id := uuid.New()
	d := db.dialect
	limited := io.LimitReader(r, maxSizeBytes+1)

	if db.GetFileStorageMode(ctx) == FileStorageDB {
		data, err := io.ReadAll(limited)
		if err != nil {
			return Blob{}, err
		}
		if int64(len(data)) > maxSizeBytes {
			return Blob{}, i18nerr.Errorf("файл превышает максимальный размер %d МБ", maxSizeBytes/(1024*1024))
		}
		q := fmt.Sprintf(`INSERT INTO _blobs (id, mime, size, data) VALUES (%s,%s,%s,%s)`,
			d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4))
		if _, err := db.Exec(ctx, q, id.String(), mime, int64(len(data)), data); err != nil {
			return Blob{}, err
		}
		return Blob{ID: id, Mime: mime, Size: int64(len(data))}, nil
	}

	// Дисковый режим: файл на диске, в _blobs только метаданные.
	dir := filepath.Join(db.filesDir, blobsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Blob{}, err
	}
	fp := filepath.Join(dir, id.String())
	f, err := os.Create(fp)
	if err != nil {
		return Blob{}, err
	}
	n, err := io.Copy(f, limited)
	f.Close()
	if err != nil {
		os.Remove(fp)
		return Blob{}, err
	}
	if n > maxSizeBytes {
		os.Remove(fp)
		return Blob{}, i18nerr.Errorf("файл превышает максимальный размер %d МБ", maxSizeBytes/(1024*1024))
	}
	q := fmt.Sprintf(`INSERT INTO _blobs (id, mime, size) VALUES (%s,%s,%s)`,
		d.Placeholder(1), d.Placeholder(2), d.Placeholder(3))
	if _, err := db.Exec(ctx, q, id.String(), mime, n); err != nil {
		os.Remove(fp)
		return Blob{}, err
	}
	return Blob{ID: id, Mime: mime, Size: n}, nil
}

// OpenBlob возвращает метаданные и читателя содержимого бинарника. Вызывающий
// обязан закрыть rc. Источник (БД/диск) определяется наличием данных в колонке.
func (db *DB) OpenBlob(ctx context.Context, id uuid.UUID) (Blob, io.ReadCloser, error) {
	d := db.dialect
	var mime string
	var size int64
	var data []byte
	err := db.QueryRow(ctx,
		fmt.Sprintf(`SELECT mime, size, data FROM _blobs WHERE id=%s`, d.Placeholder(1)),
		id.String()).Scan(&mime, &size, &data)
	if err != nil {
		return Blob{}, nil, err
	}
	b := Blob{ID: id, Mime: mime, Size: size}
	if len(data) > 0 {
		return b, io.NopCloser(bytes.NewReader(data)), nil
	}
	f, err := os.Open(filepath.Join(db.filesDir, blobsDirName, id.String()))
	if err != nil {
		return Blob{}, nil, err
	}
	return b, f, nil
}

// DeleteBlob удаляет бинарник (файл на диске, если есть) и строку метаданных.
func (db *DB) DeleteBlob(ctx context.Context, id uuid.UUID) error {
	os.Remove(filepath.Join(db.filesDir, blobsDirName, id.String()))
	d := db.dialect
	_, err := db.Exec(ctx,
		fmt.Sprintf(`DELETE FROM _blobs WHERE id=%s`, d.Placeholder(1)), id.String())
	return err
}
