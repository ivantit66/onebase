package backup

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/configdb"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/storage"
)

// ErrLegacyFormat is returned by ImportUniversal when the archive is not in
// the universal v2 format.
var ErrLegacyFormat = errors.New("archive is not in universal format (use legacy restore)")

// ImportReport summarises what was loaded during ImportUniversal.
type ImportReport struct {
	Tables map[string]int // table name → rows inserted
	Files  int            // attachment files restored
}

// systemTables is the ordered list of system tables included in the universal
// backup. The order matters for import (users before sessions, etc.).
var systemTables = []string{
	"_users",
	"_constants",
	"_numerators",
	"_attachments",
	"_audit",
	"_scheduled_runs",
}

// byteColumns lists known binary columns in system tables.
// Key: "table.column", value: true.
var byteColumns = map[string]bool{
	"_users.password_hash": true,
}

// ----------------------------------------------------------------------------
// Export
// ----------------------------------------------------------------------------

// ExportUniversal writes a v2 universal .obz archive to w.
// configSource: "database" → read config from _onebase_config;
//
//	"file"     → read config from configDir on disk.
func ExportUniversal(
	ctx context.Context,
	db *storage.DB,
	configSource, configDir string,
	attachmentsDir string,
	baseName string,
	w io.Writer,
) error {
	zw := zip.NewWriter(w)

	// --- 1. DATA tables -------------------------------------------------------
	appTables, err := listAppTables(ctx, db)
	if err != nil {
		zw.Close()
		return fmt.Errorf("export: list tables: %w", err)
	}
	manifest := make(map[string]int)
	for _, tbl := range appTables {
		entryName := "data/" + tbl + ".jsonl"
		fw, err := zw.Create(entryName)
		if err != nil {
			zw.Close()
			return err
		}
		n, err := dumpTableJSONL(ctx, db, tbl, fw)
		if err != nil {
			zw.Close()
			return fmt.Errorf("export table %s: %w", tbl, err)
		}
		manifest[entryName] = n
	}

	// --- 2. SYSTEM tables -----------------------------------------------------
	for _, tbl := range systemTables {
		entryName := "system/" + tbl + ".jsonl"
		fw, err := zw.Create(entryName)
		if err != nil {
			zw.Close()
			return err
		}
		n, err := dumpTableJSONL(ctx, db, tbl, fw)
		if err != nil {
			// System table may not exist (e.g. fresh base) — skip silently.
			_ = n
		} else {
			manifest[entryName] = n
		}
	}

	// --- 3. CONFIG ------------------------------------------------------------
	if err := exportConfig(ctx, db, configSource, configDir, zw); err != nil {
		zw.Close()
		return fmt.Errorf("export config: %w", err)
	}

	// --- 4. ATTACHMENTS (binary files) ----------------------------------------
	hasAttachments := false
	if attachmentsDir != "" {
		if _, err := os.Stat(attachmentsDir); err == nil {
			fileCount, ferr := exportAttachments(attachmentsDir, zw)
			if ferr == nil && fileCount > 0 {
				hasAttachments = true
				manifest["attachments/"] = fileCount
			}
		}
	}

	// --- 5. manifest.json ----------------------------------------------------
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	mf, _ := zw.Create("manifest.json")
	mf.Write(manifestJSON)

	// --- 6. META.txt ----------------------------------------------------------
	dbType := db.Dialect().Name()
	meta := fmt.Sprintf(
		"onebase_full_export\nversion=2\nformat=universal\nsource_db_type=%s\nsource_base=%s\ndate=%s\nhas_attachments=%v\n",
		dbType, baseName, time.Now().UTC().Format(time.RFC3339), hasAttachments,
	)
	mfw, _ := zw.Create("META.txt")
	mfw.Write([]byte(meta))

	return zw.Close()
}

// listAppTables returns all non-system table names in the database, sorted.
func listAppTables(ctx context.Context, db *storage.DB) ([]string, error) {
	var q string
	if db.IsSQLite() {
		q = `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE '\_%' ESCAPE '\' ORDER BY name`
	} else {
		q = `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT LIKE '\_%' ESCAPE '\' ORDER BY tablename`
	}
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// dumpTableJSONL streams all rows of tableName into w as JSONL.
// Line 1: schema header {"_schema":1,"btypes":["col1","col2"]}
// Lines 2+: data rows as JSON objects.
// Returns the number of data rows written.
func dumpTableJSONL(ctx context.Context, db *storage.DB, tableName string, w io.Writer) (int, error) {
	bw := bufio.NewWriterSize(w, 256*1024)
	defer bw.Flush()

	// Detect byte columns.
	byCols, err := detectByteCols(ctx, db, tableName)
	if err != nil {
		return 0, err
	}

	// Write schema line.
	schemaObj := map[string]any{"_schema": 1}
	if len(byCols) > 0 {
		list := make([]string, 0, len(byCols))
		for c := range byCols {
			list = append(list, c)
		}
		schemaObj["btypes"] = list
	}
	sl, _ := json.Marshal(schemaObj)
	bw.Write(sl)
	bw.WriteByte('\n')

	// Stream rows.
	rows, err := db.Query(ctx, "SELECT * FROM "+quotedIdent(db, tableName))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	cols := rows.FieldNames()
	n := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}

		obj := make(map[string]any, len(cols))
		for i, col := range cols {
			obj[col] = marshalValue(vals[i], byCols[col])
		}
		line, err := json.Marshal(obj)
		if err != nil {
			return n, err
		}
		bw.Write(line)
		bw.WriteByte('\n')
		n++
	}
	return n, rows.Err()
}

// detectByteCols returns the set of columns in tableName that store binary data.
func detectByteCols(ctx context.Context, db *storage.DB, tableName string) (map[string]bool, error) {
	// Check our hardcoded list first.
	result := make(map[string]bool)
	for key := range byteColumns {
		parts := strings.SplitN(key, ".", 2)
		if len(parts) == 2 && parts[0] == tableName {
			result[parts[1]] = true
		}
	}

	// Supplement with DB schema metadata.
	if db.IsSQLite() {
		rows, err := db.Query(ctx, "PRAGMA table_info("+sqliteQuote(tableName)+")")
		if err != nil {
			return result, nil
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt any
			if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) != nil {
				continue
			}
			if strings.ToUpper(ctype) == "BLOB" {
				result[name] = true
			}
		}
	} else {
		rows, err := db.Query(ctx,
			`SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND data_type='bytea'`,
			tableName)
		if err != nil {
			return result, nil
		}
		defer rows.Close()
		for rows.Next() {
			var col string
			if rows.Scan(&col) == nil {
				result[col] = true
			}
		}
	}
	return result, nil
}

// marshalValue converts a scanned DB value to a JSON-safe Go value.
// For bytes columns, returns base64 string. For Numeric, returns exact decimal string.
func marshalValue(v any, isBytesCol bool) any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []byte:
		if isBytesCol {
			return base64.StdEncoding.EncodeToString(t)
		}
		// Non-bytes []byte (e.g. JSON BLOB in SQLite) — return as string if valid UTF-8.
		return string(t)
	case [16]byte:
		// UUID from pgx
		s := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			t[0:4], t[4:6], t[6:8], t[8:10], t[10:16])
		return s
	case pgtype.Numeric:
		if !t.Valid || t.NaN {
			return nil
		}
		return numericToString(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case bool:
		return t
	}
	return v
}

// numericToString converts a pgtype.Numeric to an exact decimal string.
func numericToString(n pgtype.Numeric) string {
	if n.Int == nil {
		return "0"
	}
	negative := n.Int.Sign() < 0
	abs := new(big.Int).Abs(n.Int)
	s := abs.String()

	var result string
	if n.Exp >= 0 {
		// Value = s * 10^Exp — append zeros.
		result = s + strings.Repeat("0", int(n.Exp))
	} else {
		exp := int(-n.Exp)
		for len(s) <= exp {
			s = "0" + s
		}
		intPart := s[:len(s)-exp]
		fracPart := s[len(s)-exp:]
		result = intPart + "." + strings.TrimRight(fracPart, "0")
		if strings.HasSuffix(result, ".") {
			result = result[:len(result)-1]
		}
	}
	if negative {
		result = "-" + result
	}
	return result
}

// exportConfig writes config files into the config/ directory inside zw.
func exportConfig(ctx context.Context, db *storage.DB, configSource, configDir string, zw *zip.Writer) error {
	if configSource == "database" {
		rows, err := db.Query(ctx, `SELECT path, content FROM _onebase_config ORDER BY path`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var path string
			var content []byte
			if rows.Scan(&path, &content) != nil {
				continue
			}
			entryPath := "config/" + strings.ReplaceAll(path, `\`, "/")
			fw, err := zw.Create(entryPath)
			if err != nil {
				return err
			}
			fw.Write(content)
		}
		return rows.Err()
	}

	// File source: walk configDir.
	return filepath.WalkDir(configDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(configDir, path)
		rel = strings.ReplaceAll(rel, `\`, "/")
		if strings.HasPrefix(rel, "backups/") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fw, err := zw.Create("config/" + rel)
		if err != nil {
			return err
		}
		fw.Write(content)
		return nil
	})
}

// exportAttachments copies attachment binary files into attachments/ in the ZIP.
func exportAttachments(attachmentsDir string, zw *zip.Writer) (int, error) {
	count := 0
	err := filepath.WalkDir(attachmentsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(attachmentsDir, path)
		rel = strings.ReplaceAll(rel, `\`, "/")
		fw, err := zw.Create("attachments/" + rel)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		io.Copy(fw, f)
		count++
		return nil
	})
	return count, err
}

// quotedIdent returns a safely quoted table name for SELECT * FROM.
func quotedIdent(db *storage.DB, name string) string {
	if db.IsSQLite() {
		return sqliteQuote(name)
	}
	safe := strings.ReplaceAll(name, `"`, `""`)
	return `"` + safe + `"`
}

// sqliteQuote returns a double-quoted SQLite identifier.
func sqliteQuote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ----------------------------------------------------------------------------
// Import
// ----------------------------------------------------------------------------

// ImportUniversal restores a universal v2 .obz archive into db.
// configDest: "database" → store config in _onebase_config table;
//
//	"file"     → write config YAML files to cfgFileDir on disk.
func ImportUniversal(
	ctx context.Context,
	db *storage.DB,
	configDest, cfgFileDir string,
	attachmentsDir string,
	r io.ReaderAt,
	size int64,
) (*ImportReport, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("import: open zip: %w", err)
	}

	// --- 1. Read and validate META.txt ----------------------------------------
	meta, err := readMeta(zr)
	if err != nil {
		return nil, err
	}
	if meta["format"] != "universal" {
		return nil, ErrLegacyFormat
	}

	// --- 2. Extract all entries to a temp directory ---------------------------
	tmpDir, err := os.MkdirTemp("", "onebase-univ-import-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		outPath := filepath.Join(tmpDir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, err
		}
		if err := extractFile(f, outPath); err != nil {
			return nil, err
		}
	}

	// --- 3. Import configuration ----------------------------------------------
	configDir := filepath.Join(tmpDir, "config")
	if _, err := os.Stat(configDir); err == nil {
		if err := importConfig(ctx, db, configDest, cfgFileDir, configDir); err != nil {
			return nil, fmt.Errorf("import config: %w", err)
		}
	}

	// --- 4. Run schema migration ----------------------------------------------
	if err := migrateSchema(ctx, db, configDest, cfgFileDir); err != nil {
		return nil, fmt.Errorf("import: schema migration: %w", err)
	}

	// --- 5. Import data and system tables -------------------------------------
	report := &ImportReport{Tables: make(map[string]int)}

	// Disable FK constraint enforcement for the bulk load: tables are imported
	// in alphabetical order which may not respect FK dependency order (e.g.
	// поступлениетоваров → склады where с > п alphabetically).
	fkCleanup, err := db.DisableFKForImport(ctx)
	if err != nil {
		return report, fmt.Errorf("import: disable FK: %w", err)
	}
	defer fkCleanup()

	// Import data/ tables (application tables).
	dataDir := filepath.Join(tmpDir, "data")
	if _, err := os.Stat(dataDir); err == nil {
		if err := importDir(ctx, db, dataDir, report, nil); err != nil {
			return report, fmt.Errorf("import data: %w", err)
		}
	}

	// Import system/ tables.
	sysDir := filepath.Join(tmpDir, "system")
	if _, err := os.Stat(sysDir); err == nil {
		if err := importDir(ctx, db, sysDir, report, nil); err != nil {
			return report, fmt.Errorf("import system: %w", err)
		}
	}

	// --- 6. Restore attachment files ------------------------------------------
	attachSrc := filepath.Join(tmpDir, "attachments")
	if _, err := os.Stat(attachSrc); err == nil {
		n, err := restoreAttachments(attachSrc, attachmentsDir)
		if err != nil {
			return report, fmt.Errorf("import attachments: %w", err)
		}
		report.Files = n
	}

	return report, nil
}

// readMeta parses META.txt from the ZIP and returns key→value map.
func readMeta(zr *zip.Reader) (map[string]string, error) {
	for _, f := range zr.File {
		if f.Name != "META.txt" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, _ := io.ReadAll(rc)
		rc.Close()

		m := make(map[string]string)
		for _, line := range strings.Split(string(data), "\n") {
			if idx := strings.IndexByte(line, '='); idx > 0 {
				m[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
			}
		}
		return m, nil
	}
	return map[string]string{}, nil // no META.txt → legacy format
}

// extractFile extracts one ZIP entry to outPath.
func extractFile(f *zip.File, outPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// importConfig imports config files into the database or filesystem.
func importConfig(ctx context.Context, db *storage.DB, configDest, cfgFileDir, configDir string) error {
	if configDest == "database" {
		repo := configdb.New(db)
		return repo.ImportFromDir(ctx, configDir)
	}
	// File destination: copy YAML files to cfgFileDir.
	return filepath.WalkDir(configDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(configDir, path)
		dst := filepath.Join(cfgFileDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, content, 0o644)
	})
}

// migrateSchema creates all required tables in the target database by loading
// the project configuration and running all Migrate* calls.
// configDest: "database" → load from _onebase_config; "file" → load from cfgFileDir.
func migrateSchema(ctx context.Context, db *storage.DB, configDest, cfgFileDir string) error {
	var proj *project.Project
	var err error

	if configDest == "file" && cfgFileDir != "" {
		proj, err = project.Load(cfgFileDir)
		if err != nil {
			return fmt.Errorf("load project from files: %w", err)
		}
	} else {
		repo := configdb.New(db)
		if err := repo.EnsureSchema(ctx); err != nil {
			return err
		}
		proj, err = project.LoadFromDB(ctx, repo)
		if err != nil {
			return fmt.Errorf("load project for migration: %w", err)
		}
	}
	defer proj.Close()

	if err := db.Migrate(ctx, proj.Entities); err != nil {
		return fmt.Errorf("migrate entities: %w", err)
	}
	if err := db.MigrateRegisters(ctx, proj.Registers); err != nil {
		return fmt.Errorf("migrate registers: %w", err)
	}
	if err := db.MigrateInfoRegisters(ctx, proj.InfoRegisters); err != nil {
		return fmt.Errorf("migrate info registers: %w", err)
	}
	if err := db.MigrateConstants(ctx, proj.Constants); err != nil {
		return fmt.Errorf("migrate constants: %w", err)
	}
	if err := db.SyncAccounts(ctx, proj.ChartsOfAccounts); err != nil {
		return fmt.Errorf("sync accounts: %w", err)
	}
	if err := db.MigrateAccountRegisters(ctx, proj.AccountRegisters); err != nil {
		return fmt.Errorf("migrate account registers: %w", err)
	}
	if err := db.EnsureAuditSchema(ctx); err != nil {
		return fmt.Errorf("ensure audit schema: %w", err)
	}
	if err := db.EnsureScheduledRunsTable(ctx); err != nil {
		return fmt.Errorf("ensure scheduled runs: %w", err)
	}
	if err := db.EnsureAttachmentTable(ctx); err != nil {
		return fmt.Errorf("ensure attachments: %w", err)
	}
	authRepo := auth.NewRepo(db)
	if err := authRepo.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("ensure auth schema: %w", err)
	}
	return nil
}

// importDir walks a directory of .jsonl files and imports each into the DB.
func importDir(ctx context.Context, db *storage.DB, dir string, report *ImportReport, skip map[string]bool) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// Derive table name from filename: "Номенклатура.jsonl" → "номенклатура"
		base := filepath.Base(path)
		tableName := strings.TrimSuffix(base, ".jsonl")

		if skip[strings.ToLower(tableName)] {
			return nil
		}

		n, err := importTableJSONL(ctx, db, tableName, path)
		if err != nil {
			return fmt.Errorf("import table %s: %w", tableName, err)
		}
		report.Tables[tableName] = n
		return nil
	})
}

// importTableJSONL reads a JSONL file and inserts all rows into tableName.
// The first line must be a schema header ({"_schema":1,"btypes":[...]}).
func importTableJSONL(ctx context.Context, db *storage.DB, tableName, filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	// Read schema line.
	if !scanner.Scan() {
		return 0, nil // empty file
	}
	type schemaLine struct {
		Schema int      `json:"_schema"`
		Btypes []string `json:"btypes"`
	}
	var schema schemaLine
	if err := json.Unmarshal(scanner.Bytes(), &schema); err != nil {
		return 0, fmt.Errorf("schema line: %w", err)
	}
	btypes := make(map[string]bool)
	for _, c := range schema.Btypes {
		btypes[c] = true
	}

	// Check table exists; skip if not (e.g. a table from a different config version).
	if !tableExists(ctx, db, tableName) {
		return 0, nil
	}

	// Get the columns that actually exist in the target table.
	existingCols, err := getTableCols(ctx, db, tableName)
	if err != nil {
		return 0, fmt.Errorf("get columns for %s: %w", tableName, err)
	}

	// Clear existing data — we do a full replace.
	if _, err := db.Exec(ctx, "DELETE FROM "+quotedIdent(db, tableName)); err != nil {
		return 0, fmt.Errorf("clear table %s: %w", tableName, err)
	}

	n := 0
	colsChecked := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			return n, fmt.Errorf("parse row %d: %w", n+1, err)
		}

		// On first data row discover columns that exist in the archive but not in
		// the target table (e.g. stale columns from schema evolution in source DB).
		// Add them so no data is silently dropped during a full restore.
		if !colsChecked {
			colsChecked = true
			for col := range raw {
				if !existingCols[col] {
					_ = db.AddColumnIfMissing(ctx, tableName, col, db.Dialect().TypeText())
					existingCols[col] = true
				}
			}
		}

		if err := insertRow(ctx, db, tableName, raw, btypes, existingCols); err != nil {
			return n, fmt.Errorf("insert row %d into %s: %w", n+1, tableName, err)
		}
		n++
	}
	return n, scanner.Err()
}

// insertRow builds and executes an INSERT statement for one JSON row.
// existingCols is the set of column names that exist in the target table;
// columns not in this set are skipped (handles source/target schema differences).
func insertRow(ctx context.Context, db *storage.DB, tableName string, raw map[string]json.RawMessage, btypes map[string]bool, existingCols map[string]bool) error {
	d := db.Dialect()

	cols := make([]string, 0, len(raw))
	args := make([]any, 0, len(raw))
	placeholders := make([]string, 0, len(raw))
	idx := 1

	for col, rawVal := range raw {
		if len(existingCols) > 0 && !existingCols[col] {
			continue // column absent in target table — skip
		}
		var goVal any

		if string(rawVal) == "null" {
			goVal = nil
		} else if btypes[col] {
			var b64 string
			if err := json.Unmarshal(rawVal, &b64); err != nil {
				return fmt.Errorf("col %s: base64 unmarshal: %w", col, err)
			}
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return fmt.Errorf("col %s: base64 decode: %w", col, err)
			}
			goVal = decoded
		} else {
			// Decode the JSON value as a generic Go type.
			// Strings cover UUIDs, timestamps, numeric amounts.
			// Booleans and numbers are decoded natively.
			// PG handles implicit TEXT→UUID/TIMESTAMPTZ/NUMERIC casts.
			var v any
			if err := json.Unmarshal(rawVal, &v); err != nil {
				return fmt.Errorf("col %s: unmarshal: %w", col, err)
			}
			switch tv := v.(type) {
			case float64:
				// Preserve integer values as int64 to avoid ".0" in text columns.
				if tv == float64(int64(tv)) {
					goVal = int64(tv)
				} else {
					goVal = tv
				}
			default:
				goVal = v
			}
		}

		cols = append(cols, quotedIdent(db, col))
		args = append(args, goVal)
		placeholders = append(placeholders, d.Placeholder(idx))
		idx++
	}

	if len(cols) == 0 {
		return nil
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		quotedIdent(db, tableName),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)
	_, err := db.Exec(ctx, sql, args...)
	return err
}

// getTableCols returns the set of column names that exist in tableName.
func getTableCols(ctx context.Context, db *storage.DB, tableName string) (map[string]bool, error) {
	cols := make(map[string]bool)
	if db.IsSQLite() {
		rows, err := db.Query(ctx, "PRAGMA table_info("+sqliteQuote(tableName)+")")
		if err != nil {
			return cols, err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt any
			if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil {
				cols[name] = true
			}
		}
	} else {
		rows, err := db.Query(ctx,
			`SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1`,
			tableName)
		if err != nil {
			return cols, err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				cols[name] = true
			}
		}
	}
	return cols, nil
}

// tableExists reports whether tableName exists in the target DB.
func tableExists(ctx context.Context, db *storage.DB, tableName string) bool {
	var exists bool
	if db.IsSQLite() {
		db.QueryRow(ctx,
			`SELECT COUNT(*)>0 FROM sqlite_master WHERE type='table' AND name=?`, tableName,
		).Scan(&exists)
	} else {
		db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname='public' AND tablename=$1)`, tableName,
		).Scan(&exists)
	}
	return exists
}

// restoreAttachments copies attachment binary files from srcDir to dstDir.
// File structure in srcDir: owner/uuid or flat uuid.
func restoreAttachments(srcDir, dstDir string) (int, error) {
	if dstDir == "" {
		return 0, nil
	}
	count := 0
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, path)
		rel = filepath.FromSlash(rel)
		dst := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer src.Close()
		out, err := os.Create(dst)
		if err != nil {
			return nil
		}
		defer out.Close()
		io.Copy(out, src)
		count++
		return nil
	})
	return count, err
}
