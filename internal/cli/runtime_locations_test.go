package cli

import (
	"path/filepath"
	"testing"
)

func TestRuntimeLocations_FileConfigAndSQLiteAreAbsolute(t *testing.T) {
	wantProject, err := filepath.Abs("project")
	if err != nil {
		t.Fatal(err)
	}
	wantDB, err := filepath.Abs(filepath.Join("data", "app.db"))
	if err != nil {
		t.Fatal(err)
	}

	if got := runtimeConfigLocation("file", "project", "sqlite", "", filepath.Join("data", "app.db")); got != wantProject {
		t.Errorf("config location = %q, want %q", got, wantProject)
	}
	if got := runtimeDatabaseLocation("sqlite", "", filepath.Join("data", "app.db")); got != wantDB {
		t.Errorf("database location = %q, want %q", got, wantDB)
	}
}

func TestRuntimeLocations_DatabaseConfigUsesDatabaseLocation(t *testing.T) {
	dsn := "postgres://onebase:secret@db.example/production"
	if got := runtimeConfigLocation("database", "ignored", "postgres", dsn, ""); got != dsn {
		t.Errorf("database-backed config location = %q, want %q", got, dsn)
	}
	if got := runtimeDatabaseType(""); got != "postgres" {
		t.Errorf("empty legacy database type = %q, want postgres", got)
	}
}
