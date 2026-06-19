package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

// list_mode из YAML должен нормализоваться (регистр/пробелы), иначе
// resolveListMode (точное сравнение с "feed") молча откатывался бы на постранично.
func TestLoadFile_ListModeNormalized(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "к.yaml")
	if err := os.WriteFile(p, []byte("name: Клиент\nlist_mode: \"  Feed \"\nfields:\n  - name: Имя\n    type: string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := LoadFile(p, KindCatalog)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if e.ListMode != "feed" {
		t.Errorf("list_mode не нормализован: %q (ожидалось \"feed\")", e.ListMode)
	}
}
