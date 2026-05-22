package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Attachment represents a file attached to a document or catalog record.
type Attachment struct {
	ID         uuid.UUID `json:"id"`
	OwnerKind  string    `json:"owner_kind"`
	OwnerName  string    `json:"owner_name"`
	OwnerID    uuid.UUID `json:"owner_id"`
	Filename   string    `json:"filename"`
	MimeType   string    `json:"mime_type"`
	SizeBytes  int64     `json:"size_bytes"`
	UploadedAt time.Time `json:"uploaded_at"`
	UploadedBy string    `json:"uploaded_by"`
}

// EnsureAttachmentTable creates the _attachments table if it does not exist.
func (db *DB) EnsureAttachmentTable(ctx context.Context) error {
	d := db.dialect
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS _attachments (
			id          %s PRIMARY KEY,
			owner_kind  TEXT NOT NULL,
			owner_name  TEXT NOT NULL,
			owner_id    %s NOT NULL,
			filename    TEXT NOT NULL,
			mime_type   TEXT NOT NULL DEFAULT '',
			size_bytes  BIGINT NOT NULL DEFAULT 0,
			uploaded_at %s NOT NULL DEFAULT %s,
			uploaded_by TEXT NOT NULL DEFAULT ''
		)`, d.TypeUUID(), d.TypeUUID(), d.TypeTimestamp(), d.CurrentTimestampTZ())
	_, err := db.Exec(ctx, ddl)
	return err
}

// ListAttachments returns all attachments for a given owner.
func (db *DB) ListAttachments(ctx context.Context, ownerKind, ownerName string, ownerID uuid.UUID) ([]Attachment, error) {
	d := db.dialect
	q := fmt.Sprintf(`
		SELECT id, owner_kind, owner_name, owner_id, filename, mime_type, size_bytes, uploaded_at, uploaded_by
		FROM _attachments
		WHERE owner_kind=%s AND owner_name=%s AND owner_id=%s
		ORDER BY uploaded_at DESC
	`, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3))
	rows, err := db.Query(ctx, q, ownerKind, ownerName, ownerID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *a)
	}
	return result, nil
}

// UploadAttachment saves a file to disk and records metadata in the database.
func (db *DB) UploadAttachment(ctx context.Context, ownerKind, ownerName string, ownerID uuid.UUID, filename, mimeType, uploadedBy string, r io.Reader, maxSizeBytes int64) (Attachment, error) {
	d := db.dialect
	id := uuid.New()
	dir := filepath.Join(db.filesDir, ownerName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Attachment{}, err
	}
	filePath := filepath.Join(dir, id.String())
	f, err := os.Create(filePath)
	if err != nil {
		return Attachment{}, err
	}
	defer f.Close()

	limited := io.LimitReader(r, maxSizeBytes+1)
	n, err := io.Copy(f, limited)
	if err != nil {
		os.Remove(filePath)
		return Attachment{}, err
	}
	if n > maxSizeBytes {
		os.Remove(filePath)
		return Attachment{}, fmt.Errorf("файл превышает максимальный размер %d МБ", maxSizeBytes/(1024*1024))
	}

	q := fmt.Sprintf(`
			INSERT INTO _attachments (id, owner_kind, owner_name, owner_id, filename, mime_type, size_bytes, uploaded_by)
			VALUES (%s,%s,%s,%s,%s,%s,%s,%s)
		`, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4),
		d.Placeholder(5), d.Placeholder(6), d.Placeholder(7), d.Placeholder(8))
	if _, err := db.Exec(ctx, q,
		id.String(), ownerKind, ownerName, ownerID.String(), filename, mimeType, n, uploadedBy,
	); err != nil {
		os.Remove(filePath)
		return Attachment{}, err
	}
	a, err := db.GetAttachment(ctx, id)
	if err != nil {
		os.Remove(filePath)
		return Attachment{}, err
	}
	return *a, nil
}

// GetAttachment returns attachment metadata by ID.
func (db *DB) GetAttachment(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	d := db.dialect
	q := fmt.Sprintf(`
		SELECT id, owner_kind, owner_name, owner_id, filename, mime_type, size_bytes, uploaded_at, uploaded_by
		FROM _attachments WHERE id=%s
	`, d.Placeholder(1))
	row := db.QueryRow(ctx, q, id.String())
	return scanAttachment(row)
}

// OpenAttachment opens the file for a given attachment ID and returns its metadata.
func (db *DB) OpenAttachment(ctx context.Context, id uuid.UUID) (*os.File, *Attachment, error) {
	a, err := db.GetAttachment(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	filePath := filepath.Join(db.filesDir, a.OwnerName, id.String())
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	return f, a, nil
}

// DeleteAttachment removes the file from disk and deletes the database record.
func (db *DB) DeleteAttachment(ctx context.Context, id uuid.UUID) error {
	d := db.dialect
	a, err := db.GetAttachment(ctx, id)
	if err != nil {
		return err
	}
	filePath := filepath.Join(db.filesDir, a.OwnerName, id.String())
	os.Remove(filePath)
	q := fmt.Sprintf(`DELETE FROM _attachments WHERE id=%s`, d.Placeholder(1))
	_, err = db.Exec(ctx, q, id.String())
	return err
}

// attachmentScanner is satisfied by both sql.Row and sql.Rows.
type attachmentScanner interface{ Scan(dest ...any) error }

func scanAttachment(row attachmentScanner) (*Attachment, error) {
	var idStr, ownerIDStr string
	var uploadedAtRaw any
	var a Attachment
	if err := row.Scan(&idStr, &a.OwnerKind, &a.OwnerName, &ownerIDStr, &a.Filename, &a.MimeType, &a.SizeBytes, &uploadedAtRaw, &a.UploadedBy); err != nil {
		return nil, err
	}
	a.UploadedAt = parseAuditTime(uploadedAtRaw)
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("attachment id: %w", err)
	}
	a.ID = id
	ownerID, err := uuid.Parse(ownerIDStr)
	if err != nil {
		return nil, fmt.Errorf("attachment owner_id: %w", err)
	}
	a.OwnerID = ownerID
	return &a, nil
}
