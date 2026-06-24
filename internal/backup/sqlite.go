package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ivantit66/onebase/internal/storage"
)

// DumpSQLite creates a backup of the SQLite database via `VACUUM INTO`.
// This is an atomic online backup that does not block readers.
// The output file is plain SQLite (.db), not compressed — SQLite is already
// compact and many users want random-restore by file copy.
//
// Returns the full path of the created file.
func DumpSQLite(ctx context.Context, dbPath, outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(dbPath)
	for _, ext := range []string{".db", ".sqlite", ".sqlite3"} {
		if len(base) > len(ext) && base[len(base)-len(ext):] == ext {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	ts := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("backup_%s_%s.db", base, ts)
	outPath := filepath.Join(outDir, filename)
	tmp, err := os.CreateTemp(outDir, "."+filename+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	// VACUUM INTO fails if the file exists. Reserve a unique name with
	// CreateTemp, then remove it before SQLite writes the actual backup.
	_ = os.Remove(tmpPath)
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	db, err := storage.ConnectSQLite(ctx, dbPath)
	if err != nil {
		return "", fmt.Errorf("sqlite backup: open source: %w", err)
	}
	defer db.Close()

	// VACUUM INTO 'path' — atomic, no locks held longer than necessary.
	// We can't use parameters (it's not a query but a meta-command); embed
	// the path with simple single-quote escaping.
	escaped := ""
	for _, c := range tmpPath {
		if c == '\'' {
			escaped += "''"
			continue
		}
		escaped += string(c)
	}
	if _, err := db.Exec(ctx, "VACUUM INTO '"+escaped+"'"); err != nil {
		return "", fmt.Errorf("sqlite VACUUM INTO: %w", err)
	}
	_ = os.Remove(outPath)
	if err := os.Rename(tmpPath, outPath); err != nil {
		return "", err
	}
	committed = true
	return outPath, nil
}

// RestoreSQLite replaces the target database file with the backup file.
// The caller must ensure no process is currently using the target file
// (the launcher stops the running base before calling restore).
func RestoreSQLite(ctx context.Context, dbPath, backupPath string) error {
	srcInfo, err := os.Stat(backupPath)
	if err != nil {
		return fmt.Errorf("sqlite restore: backup not found: %w", err)
	}
	if srcInfo.IsDir() {
		return fmt.Errorf("sqlite restore: backup path is a directory")
	}

	src, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	dst, err := os.Create(dbPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("sqlite restore: copy: %w", err)
	}
	// Also remove any -wal / -shm sidecar files (stale data from the previous DB).
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	return nil
}
