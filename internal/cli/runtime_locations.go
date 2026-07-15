package cli

import "path/filepath"

func runtimeDatabaseType(dbType string) string {
	if dbType == "sqlite" {
		return "sqlite"
	}
	return "postgres"
}

// runtimeDatabaseLocation returns a stable, useful location for diagnostics.
// File paths are made absolute; PostgreSQL connection strings are masked later
// by the UI immediately before rendering.
func runtimeDatabaseLocation(dbType, dsn, sqlitePath string) string {
	if dbType == "sqlite" {
		return absoluteRuntimePath(sqlitePath)
	}
	return dsn
}

func runtimeConfigLocation(configSource, projectDir, dbType, dsn, sqlitePath string) string {
	if configSource == "database" {
		return runtimeDatabaseLocation(dbType, dsn, sqlitePath)
	}
	return absoluteRuntimePath(projectDir)
}

func absoluteRuntimePath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(abs)
}
