package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/scheduler"
)

const (
	defaultAutoBackupSchedule = "0 2 * * *"
	defaultAutoBackupKeepLast = 7
)

// AutoTarget describes the database that automatic backup should dump.
type AutoTarget struct {
	DBType     string
	DSN        string
	SQLitePath string
	ProjectDir string
}

type autoDumper func(context.Context, AutoTarget, string) (string, error)

// RegisterAutoBackup registers the configured automatic backup as a scheduler
// Go job. Disabled or nil config is a no-op.
func RegisterAutoBackup(cfg *project.BackupConfig, target AutoTarget, sched *scheduler.Scheduler) error {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if sched == nil {
		return fmt.Errorf("auto backup: scheduler is nil")
	}
	schedule := strings.TrimSpace(cfg.Schedule)
	if schedule == "" {
		schedule = defaultAutoBackupSchedule
	}
	return sched.RegisterGoJob("AutoBackup", "Автоматический бэкап", schedule, func(ctx context.Context) error {
		_, err := CreateAutoBackup(ctx, cfg, target)
		return err
	})
}

// CreateAutoBackup creates one backup file and rotates older files according to
// cfg.KeepLast. It returns the created backup path.
func CreateAutoBackup(ctx context.Context, cfg *project.BackupConfig, target AutoTarget) (string, error) {
	return createAutoBackup(ctx, cfg, target, dumpAutoTarget)
}

func createAutoBackup(ctx context.Context, cfg *project.BackupConfig, target AutoTarget, dumper autoDumper) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("auto backup: config is nil")
	}
	dir := AutoBackupDir(cfg, target.ProjectDir)
	path, err := dumper(ctx, target, dir)
	if err != nil {
		return "", err
	}
	keepLast := cfg.KeepLast
	if keepLast <= 0 {
		keepLast = defaultAutoBackupKeepLast
	}
	if err := RotateBackups(dir, keepLast); err != nil {
		return path, err
	}
	return path, nil
}

// AutoBackupDir returns the effective backup directory.
func AutoBackupDir(cfg *project.BackupConfig, projectDir string) string {
	if cfg != nil && strings.TrimSpace(cfg.Directory) != "" {
		return strings.TrimSpace(cfg.Directory)
	}
	if projectDir != "" {
		return filepath.Join(projectDir, "backups")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".onebase", "backups", "default")
}

func dumpAutoTarget(ctx context.Context, target AutoTarget, dir string) (string, error) {
	if strings.EqualFold(target.DBType, "sqlite") || target.SQLitePath != "" {
		if target.SQLitePath == "" {
			return "", fmt.Errorf("auto backup: sqlite path is empty")
		}
		return DumpSQLite(ctx, target.SQLitePath, dir)
	}
	if target.DSN == "" {
		return "", fmt.Errorf("auto backup: PostgreSQL DSN is empty")
	}
	return Dump(ctx, target.DSN, dir)
}

// RotateBackups keeps the newest keepLast backup files and removes older ones.
func RotateBackups(dir string, keepLast int) error {
	if keepLast <= 0 {
		return nil
	}
	files, err := BackupFiles(dir)
	if err != nil {
		return err
	}
	if len(files) <= keepLast {
		return nil
	}
	for _, f := range files[keepLast:] {
		if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// FileInfo is one backup file discovered in a backup directory.
type FileInfo struct {
	Path string
	Info os.FileInfo
}

// BackupFiles returns backup_* files known to onebase, newest first.
func BackupFiles(dir string) ([]FileInfo, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "backup_*"))
	if err != nil {
		return nil, err
	}
	files := make([]FileInfo, 0, len(matches))
	for _, path := range matches {
		if !isBackupFile(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, FileInfo{Path: path, Info: info})
	}
	sort.Slice(files, func(i, j int) bool {
		ti := files[i].Info.ModTime()
		tj := files[j].Info.ModTime()
		if ti.Equal(tj) {
			return files[i].Path > files[j].Path
		}
		return ti.After(tj)
	})
	return files, nil
}

func isBackupFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(name, ".sql.gz") ||
		strings.HasSuffix(name, ".sql") ||
		strings.HasSuffix(name, ".db") ||
		strings.HasSuffix(name, ".sqlite") ||
		strings.HasSuffix(name, ".sqlite3")
}
