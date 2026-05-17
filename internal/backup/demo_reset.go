package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ivantit66/onebase/internal/storage"
)

// authTables — таблицы авторизации, которые никогда не сбрасываются при демо-сбросе.
var authTables = map[string]bool{
	"_users":      true,
	"_sessions":   true,
	"_roles":      true,
	"_user_roles": true,
	// историю запусков регл.заданий тоже оставляем
	"_scheduled_runs": true,
}

// DemoReset восстанавливает бизнес-данные из .obz бэкапа, сохраняя таблицы
// авторизации нетронутыми (_users, _sessions, _roles, _user_roles).
// Если backupPath пуст — только очищает бизнес-данные без восстановления.
func DemoReset(ctx context.Context, db *storage.DB, backupPath string) (*ImportReport, error) {
	report := &ImportReport{Tables: make(map[string]int)}

	if backupPath == "" {
		return report, nil
	}

	f, err := os.Open(backupPath)
	if err != nil {
		return nil, fmt.Errorf("demo reset: open backup %q: %w", backupPath, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("demo reset: stat backup: %w", err)
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("demo reset: open zip: %w", err)
	}

	meta, err := readMeta(zr)
	if err != nil {
		return nil, err
	}
	if meta["format"] != "universal" {
		return nil, ErrLegacyFormat
	}

	tmpDir, err := os.MkdirTemp("", "onebase-demo-reset-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		outPath := filepath.Join(tmpDir, filepath.FromSlash(zf.Name))
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, err
		}
		if err := extractFile(zf, outPath); err != nil {
			return nil, err
		}
	}

	fkCleanup, err := db.DisableFKForImport(ctx)
	if err != nil {
		return report, fmt.Errorf("demo reset: disable FK: %w", err)
	}
	defer fkCleanup()

	// Импортируем data/ и system/, пропуская таблицы авторизации
	dataDir := filepath.Join(tmpDir, "data")
	if _, err := os.Stat(dataDir); err == nil {
		if err := importDir(ctx, db, dataDir, report, authTables); err != nil {
			return report, fmt.Errorf("demo reset data: %w", err)
		}
	}

	sysDir := filepath.Join(tmpDir, "system")
	if _, err := os.Stat(sysDir); err == nil {
		if err := importDir(ctx, db, sysDir, report, authTables); err != nil {
			return report, fmt.Errorf("demo reset system: %w", err)
		}
	}

	return report, nil
}
