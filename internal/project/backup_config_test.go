package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_BackupSection(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`name: Test
backup:
  enabled: true
  schedule: "0 3 * * *"
  keep_last: 14
  directory: /var/backups/onebase
`)
	if err := os.WriteFile(filepath.Join(dir, "config", "app.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Backup == nil {
		t.Fatal("Backup is nil")
	}
	if !cfg.Backup.Enabled || cfg.Backup.Schedule != "0 3 * * *" || cfg.Backup.KeepLast != 14 {
		t.Fatalf("Backup parsed incorrectly: %+v", cfg.Backup)
	}
	if cfg.Backup.Directory != "/var/backups/onebase" {
		t.Fatalf("directory = %q", cfg.Backup.Directory)
	}
}
