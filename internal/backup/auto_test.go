package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/scheduler"
)

func TestRegisterAutoBackup_Defaults(t *testing.T) {
	sched := scheduler.New(nil, nil, nil)
	cfg := &project.BackupConfig{Enabled: true}

	if err := RegisterAutoBackup(cfg, AutoTarget{ProjectDir: t.TempDir()}, sched); err != nil {
		t.Fatalf("RegisterAutoBackup: %v", err)
	}
	jobs := sched.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(jobs))
	}
	if jobs[0].Name != "AutoBackup" || jobs[0].Schedule != defaultAutoBackupSchedule {
		t.Fatalf("job = %+v", jobs[0])
	}
}

func TestRegisterAutoBackup_DisabledNoop(t *testing.T) {
	sched := scheduler.New(nil, nil, nil)
	if err := RegisterAutoBackup(&project.BackupConfig{}, AutoTarget{}, sched); err != nil {
		t.Fatalf("RegisterAutoBackup disabled: %v", err)
	}
	if len(sched.Jobs()) != 0 {
		t.Fatalf("disabled config registered jobs: %+v", sched.Jobs())
	}
}

func TestCreateAutoBackup_RotatesWithDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := &project.BackupConfig{Directory: dir}
	for i := 0; i < defaultAutoBackupKeepLast; i++ {
		touchBackup(t, dir, "backup_old_"+string(rune('a'+i))+".sql.gz", time.Now().Add(-time.Duration(i+10)*time.Hour))
	}

	created, err := createAutoBackup(context.Background(), cfg, AutoTarget{}, func(_ context.Context, _ AutoTarget, outDir string) (string, error) {
		path := filepath.Join(outDir, "backup_new.sql.gz")
		if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
			return "", err
		}
		now := time.Now()
		if err := os.Chtimes(path, now, now); err != nil {
			return "", err
		}
		return path, nil
	})
	if err != nil {
		t.Fatalf("createAutoBackup: %v", err)
	}
	if filepath.Base(created) != "backup_new.sql.gz" {
		t.Fatalf("created = %s", created)
	}
	files, err := BackupFiles(dir)
	if err != nil {
		t.Fatalf("BackupFiles: %v", err)
	}
	if len(files) != defaultAutoBackupKeepLast {
		t.Fatalf("files len = %d, want %d", len(files), defaultAutoBackupKeepLast)
	}
	if filepath.Base(files[0].Path) != "backup_new.sql.gz" {
		t.Fatalf("newest = %s", files[0].Path)
	}
}

func TestBackupFiles_KnownExtensionsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	old := touchBackup(t, dir, "backup_a.sql.gz", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	newer := touchBackup(t, dir, "backup_b.db", time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC))
	_ = touchBackup(t, dir, "notes.txt", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	files, err := BackupFiles(dir)
	if err != nil {
		t.Fatalf("BackupFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Path != newer || files[1].Path != old {
		t.Fatalf("unexpected order: %+v", files)
	}
}

func touchBackup(t *testing.T, dir, name string, ts time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
	return path
}
