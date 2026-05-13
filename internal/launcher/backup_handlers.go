package launcher

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ivantit66/onebase/internal/backup"
	"github.com/ivantit66/onebase/internal/configdb"
	"gopkg.in/yaml.v3"
)

func (h *handler) backupDir(b *Base) string {
	custom := h.loadBackupDirSetting(b)
	if custom != "" {
		return custom
	}
	if b.Path != "" {
		return filepath.Join(b.Path, "backups")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".onebase", "backups", b.ID)
}

func (h *handler) loadBackupDirSetting(b *Base) string {
	if b.ConfigSource == "database" {
		db, err := OpenDB(context.Background(), b)
		if err != nil {
			return ""
		}
		defer db.Close()
		var content []byte
		if err := db.QueryRow(context.Background(),
			"SELECT content FROM _onebase_config WHERE path='config/app.yaml'").Scan(&content); err != nil {
			return ""
		}
		var tmp struct {
			Backup struct {
				Directory string `yaml:"directory"`
			} `yaml:"backup"`
		}
		yaml.Unmarshal(content, &tmp)
		return tmp.Backup.Directory
	}
	cfgPath := filepath.Join(b.Path, "config", "app.yaml")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return ""
	}
	var tmp struct {
		Backup struct {
			Directory string `yaml:"directory"`
		} `yaml:"backup"`
	}
	yaml.Unmarshal(raw, &tmp)
	return tmp.Backup.Directory
}

// dumpForBase chooses the right backup mechanism based on b.DBType.
func dumpForBase(ctx context.Context, b *Base, dir string) (string, error) {
	if b.DBType == "sqlite" {
		return backup.DumpSQLite(ctx, b.DBPath, dir)
	}
	return backup.Dump(ctx, b.DB, dir)
}

// restoreForBase chooses the right restore mechanism based on b.DBType.
func restoreForBase(ctx context.Context, b *Base, fp string) error {
	if b.DBType == "sqlite" {
		return backup.RestoreSQLite(ctx, b.DBPath, fp)
	}
	return backup.Restore(ctx, b.DB, fp)
}

func (h *handler) backupCreate(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dir := h.backupDir(b)
	outPath, dumpErr := dumpForBase(r.Context(), b, dir)
	data := h.loadCfgData(r.Context(), b, "backup")
	if dumpErr != nil {
		data.Error = "РћС€РёР±РєР° Р±СЌРєР°РїР°: " + dumpErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = "panel-backup"
		data.BackupMessage = "Р‘СЌРєР°Рї СЃРѕР·РґР°РЅ: " + filepath.Base(outPath)
	}
	renderCfg(w, data)
}

func (h *handler) backupDownload(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file := chi.URLParam(r, "file")
	dir := h.backupDir(b)
	fp := filepath.Join(dir, file)
	if _, err := os.Stat(fp); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename="+file)
	http.ServeFile(w, r, fp)
}

func (h *handler) backupDelete(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file := chi.URLParam(r, "file")
	os.Remove(filepath.Join(h.backupDir(b), file))
	data := h.loadCfgData(r.Context(), b, "backup")
	data.FieldsSaved = true
	data.FieldsSavedEntity = "panel-backup"
	renderCfg(w, data)
}

func (h *handler) backupSettings(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	type backupCfg struct {
		Enabled   bool   `yaml:"enabled"`
		Schedule  string `yaml:"schedule"`
		KeepLast  int    `yaml:"keep_last"`
		Directory string `yaml:"directory"`
	}
	type appCfgWithBackup struct {
		Name    string    `yaml:"name"`
		Version string    `yaml:"version,omitempty"`
		Backup  backupCfg `yaml:"backup,omitempty"`
	}
	keepLast, _ := strconv.Atoi(r.FormValue("backup_keep"))
	cfg := backupCfg{
		Enabled:   r.FormValue("backup_enabled") == "on",
		Schedule:  strings.TrimSpace(r.FormValue("backup_schedule")),
		KeepLast:  keepLast,
		Directory: strings.TrimSpace(r.FormValue("backup_dir")),
	}
	out, _ := yaml.Marshal(appCfgWithBackup{Name: b.Name, Backup: cfg})
	var saveErr error
	if b.ConfigSource == "database" {
		db, cerr := OpenDB(r.Context(), b)
		if cerr != nil {
			saveErr = cerr
		} else {
			defer db.Close()
			saveErr = cfgUpsert(r.Context(), db, "config/app.yaml", out)
		}
	} else {
		dir := filepath.Join(b.Path, "config")
		os.MkdirAll(dir, 0o755)
		saveErr = os.WriteFile(filepath.Join(dir, "app.yaml"), out, 0o644)
	}
	data := h.loadCfgData(r.Context(), b, "backup")
	if saveErr != nil {
		data.Error = fmt.Sprintf("РћС€РёР±РєР° СЃРѕС…СЂР°РЅРµРЅРёСЏ: %s", saveErr.Error())
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = "panel-backup"
		data.BackupMessage = "РќР°СЃС‚СЂРѕР№РєРё Р±СЌРєР°РїР° СЃРѕС…СЂР°РЅРµРЅС‹"
	}
	renderCfg(w, data)
}

func (h *handler) backupUpload(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dir := h.backupDir(b)
	os.MkdirAll(dir, 0o755)

	file, header, err := r.FormFile("backup_file")
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "backup")
		data.Error = "РћС€РёР±РєР° Р·Р°РіСЂСѓР·РєРё: " + err.Error()
		renderCfg(w, data)
		return
	}
	defer file.Close()

	name := header.Filename
	outPath := filepath.Join(dir, name)
	f, err := os.Create(outPath)
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "backup")
		data.Error = "РћС€РёР±РєР° СЃРѕС…СЂР°РЅРµРЅРёСЏ: " + err.Error()
		renderCfg(w, data)
		return
	}
	defer f.Close()
	io.Copy(f, file)

	data := h.loadCfgData(r.Context(), b, "backup")
	data.FieldsSaved = true
	data.FieldsSavedEntity = "panel-backup"
	data.BackupMessage = "Р¤Р°Р№Р» Р·Р°РіСЂСѓР¶РµРЅ: " + name
	renderCfg(w, data)
}

func (h *handler) backupRestore(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file := chi.URLParam(r, "file")
	dir := h.backupDir(b)
	fp := filepath.Join(dir, file)
	if _, err := os.Stat(fp); err != nil {
		data := h.loadCfgData(r.Context(), b, "backup")
		data.Error = "Р¤Р°Р№Р» РЅРµ РЅР°Р№РґРµРЅ: " + file
		renderCfg(w, data)
		return
	}

	wasRunning := h.runner.IsRunning(b.ID)
	if wasRunning {
		h.runner.Stop(b.ID)
		waitPortFree(b.Port, 3*time.Second)
	}

	restoreErr := restoreForBase(r.Context(), b, fp)
	data := h.loadCfgData(r.Context(), b, "backup")
	if restoreErr != nil {
		data.Error = "РћС€РёР±РєР° РІРѕСЃСЃС‚Р°РЅРѕРІР»РµРЅРёСЏ: " + restoreErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = "panel-backup"
		msg := "Р‘Р°Р·Р° РґР°РЅРЅС‹С… РІРѕСЃСЃС‚Р°РЅРѕРІР»РµРЅР° РёР·: " + file
		if wasRunning {
			msg += ". Р‘Р°Р·Р° РѕСЃС‚Р°РЅРѕРІР»РµРЅР° вЂ” Р·Р°РїСѓСЃС‚РёС‚Рµ РµС‘ Р·Р°РЅРѕРІРѕ РґР»СЏ РїСЂРёРјРµРЅРµРЅРёСЏ РёР·РјРµРЅРµРЅРёР№."
		}
		data.BackupMessage = msg
	}
	renderCfg(w, data)
}

// backupFullExport creates a single .obz file containing both database dump and configuration.
func (h *handler) backupFullExport(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Database dump
	tmpDir, err := os.MkdirTemp("", "onebase-obz-dump-*")
	if err != nil {
		http.Error(w, "Temp dir error: "+err.Error(), 500)
		return
	}
	defer os.RemoveAll(tmpDir)

	dumpPath, dumpErr := dumpForBase(r.Context(), b, tmpDir)
	if dumpErr != nil {
		http.Error(w, "Dump error: "+dumpErr.Error(), 500)
		return
	}

	dumpData, err := os.ReadFile(dumpPath)
	if err != nil {
		http.Error(w, "Read dump error: "+err.Error(), 500)
		return
	}
	f, _ := zw.Create("database.sql.gz")
	f.Write(dumpData)

	// Configuration
	if b.ConfigSource == "database" {
		db, cerr := OpenDB(r.Context(), b)
		if cerr == nil {
			defer db.Close()
			rows, qerr := db.Query(r.Context(), `SELECT path, content FROM _onebase_config ORDER BY path`)
			if qerr == nil {
				defer rows.Close()
				for rows.Next() {
					var p string
					var content []byte
					if rows.Scan(&p, &content) != nil {
						continue
					}
					cf, _ := zw.Create("config/" + strings.ReplaceAll(p, `\`, "/"))
					cf.Write(content)
				}
			}
		}
	} else {
		srcDir := b.Path
		filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(srcDir, path)
			rel = strings.ReplaceAll(rel, `\`, "/")
			if strings.HasPrefix(rel, "backups/") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			cf, _ := zw.Create("config/" + rel)
			cf.Write(content)
			return nil
		})
	}

	// Metadata
	meta := fmt.Sprintf("onebase_full_export\nversion=1.0\ndate=%s\nbase=%s\nsource=%s\n",
		time.Now().Format("2006-01-02T15:04:05"), b.Name, b.ConfigSource)
	mf, _ := zw.Create("META.txt")
	mf.Write([]byte(meta))

	zw.Close()

	name := b.Name + "_" + time.Now().Format("2006-01-02_15-04") + ".obz"
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	w.Write(buf.Bytes())
}

// backupFullImport restores both database and configuration from a .obz file.
func (h *handler) backupFullImport(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	file, _, err := r.FormFile("obz_file")
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "backup")
		data.Error = "РћС€РёР±РєР° Р·Р°РіСЂСѓР·РєРё С„Р°Р№Р»Р°: " + err.Error()
		renderCfg(w, data)
		return
	}
	defer file.Close()

	dtData, err := io.ReadAll(file)
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "backup")
		data.Error = "РћС€РёР±РєР° С‡С‚РµРЅРёСЏ С„Р°Р№Р»Р°: " + err.Error()
		renderCfg(w, data)
		return
	}

	reader, err := zip.NewReader(bytes.NewReader(dtData), int64(len(dtData)))
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "backup")
		data.Error = "РќРµРІРµСЂРЅС‹Р№ С„РѕСЂРјР°С‚ С„Р°Р№Р»Р° .obz: " + err.Error()
		renderCfg(w, data)
		return
	}

	tmpDir, err := os.MkdirTemp("", "onebase-obz-import-*")
	if err != nil {
		data := h.loadCfgData(r.Context(), b, "backup")
		data.Error = "Temp dir error: " + err.Error()
		renderCfg(w, data)
		return
	}
	defer os.RemoveAll(tmpDir)

	var dumpFile string
	var configDir string

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			os.MkdirAll(filepath.Join(tmpDir, f.Name), 0o755)
			continue
		}
		outPath := filepath.Join(tmpDir, f.Name)
		os.MkdirAll(filepath.Dir(outPath), 0o755)
		rc, err := f.Open()
		if err != nil {
			continue
		}
		outFile, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			continue
		}
		io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if f.Name == "database.sql.gz" {
			dumpFile = outPath
		}
		if strings.HasPrefix(f.Name, "config/") && configDir == "" {
			configDir = filepath.Join(tmpDir, "config")
		}
	}

	// Р—Р°РїСѓС‰РµРЅРЅС‹Р№ РїСЂРѕС†РµСЃСЃ РґРµСЂР¶РёС‚ СЃС‚Р°СЂСѓСЋ РєРѕРЅС„РёРіСѓСЂР°С†РёСЋ РІ РїР°РјСЏС‚Рё Рё СЃРµСЃСЃРёСЋ Рє Р‘Р” вЂ”
	// РёРЅР°С‡Рµ РїРѕСЃР»Рµ restore РјРёРіСЂР°С†РёСЏ Рё РЅРѕРІС‹Рµ .os-С„Р°Р№Р»С‹ РЅРµ Р±СѓРґСѓС‚ РІРёРґРЅС‹ РґРѕ РїРµСЂРµР·Р°РїСѓСЃРєР°.
	wasRunning := h.runner.IsRunning(b.ID)
	if wasRunning {
		h.runner.Stop(b.ID)
		waitPortFree(b.Port, 3*time.Second)
	}

	// Restore database
	var restoreErr error
	if dumpFile != "" {
		restoreErr = restoreForBase(r.Context(), b, dumpFile)
	} else {
		restoreErr = fmt.Errorf("С„Р°Р№Р» database.sql.gz РЅРµ РЅР°Р№РґРµРЅ РІ Р°СЂС…РёРІРµ")
	}

	// Import configuration
	var configErr error
	if configDir != "" {
		if b.ConfigSource == "database" {
			db, cerr := OpenDB(r.Context(), b)
			if cerr != nil {
				configErr = cerr
			} else {
				defer db.Close()
				repo := configdb.New(db)
				configErr = repo.ImportFromDir(r.Context(), configDir)
			}
		} else {
			filepath.WalkDir(configDir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(configDir, path)
				dst := filepath.Join(b.Path, rel)
				os.MkdirAll(filepath.Dir(dst), 0o755)
				content, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				os.WriteFile(dst, content, 0o644)
				return nil
			})
		}
	}

	if configErr == nil {
		h.runner.MigrateBase(r.Context(), b)
	}

	data := h.loadCfgData(r.Context(), b, "backup")
	if restoreErr != nil {
		data.Error = "РћС€РёР±РєР° РІРѕСЃСЃС‚Р°РЅРѕРІР»РµРЅРёСЏ Р‘Р”: " + restoreErr.Error()
	} else if configErr != nil {
		data.Error = "РћС€РёР±РєР° РёРјРїРѕСЂС‚Р° РєРѕРЅС„РёРіСѓСЂР°С†РёРё: " + configErr.Error()
	} else {
		data.FieldsSaved = true
		data.FieldsSavedEntity = "panel-backup"
		msg := "РџРѕР»РЅРѕРµ РІРѕСЃСЃС‚Р°РЅРѕРІР»РµРЅРёРµ РІС‹РїРѕР»РЅРµРЅРѕ: Р±Р°Р·Р° РґР°РЅРЅС‹С… + РєРѕРЅС„РёРіСѓСЂР°С†РёСЏ"
		if wasRunning {
			msg += ". Р‘Р°Р·Р° РѕСЃС‚Р°РЅРѕРІР»РµРЅР° вЂ” Р·Р°РїСѓСЃС‚РёС‚Рµ РµС‘ Р·Р°РЅРѕРІРѕ РґР»СЏ РїСЂРёРјРµРЅРµРЅРёСЏ РёР·РјРµРЅРµРЅРёР№."
		}
		data.BackupMessage = msg
	}
	renderCfg(w, data)
}
