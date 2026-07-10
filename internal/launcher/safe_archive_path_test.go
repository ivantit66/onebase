package launcher

import (
	"path/filepath"
	"runtime"
	"testing"
)

// safeArchivePath защищает распаковку ZIP/OBZ-архивов (config/import-zip,
// backup/full-import) от zip-slip (CWE-22/CWE-23): записи с «../», абсолютные
// пути и обратные слэши не должны выходить за пределы каталога распаковки,
// но валидные подкаталоги (config/module.yaml) обязаны проходить.
func TestSafeArchivePath(t *testing.T) {
	dir := t.TempDir()

	good := []string{
		"somefile.yaml",
		"config/module.yaml",
		"database.db",
		"data/documents/order.json",
	}
	for _, name := range good {
		fp, err := safeArchivePath(dir, name)
		if err != nil {
			t.Errorf("ожидался валидный путь для %q, got err: %v", name, err)
			continue
		}
		want := filepath.Join(dir, filepath.FromSlash(name))
		if fp != want {
			t.Errorf("для %q: got %q, want %q", name, fp, want)
		}
	}

	bad := []string{
		"",
		"..",
		"../secret",
		"../../etc/passwd",
		"config/../../etc/passwd",
		`..\..\windows\system32`, // обратный слэш — traversal при распаковке на Windows
		"a\x00b",
	}
	if runtime.GOOS == "windows" {
		bad = append(bad, `C:\Windows\system.ini`)
	}
	for _, name := range bad {
		if _, err := safeArchivePath(dir, name); err == nil {
			t.Errorf("ожидалась ошибка для опасного имени %q, но путь принят", name)
		}
	}
}
