package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/configfmt"
)

func TestSchemaByKind_Aliases(t *testing.T) {
	s, ok := schemaByKind("виджет")
	if !ok {
		t.Fatal("schema alias виджет not resolved")
	}
	if s["title"] != "OneBase widget" {
		t.Fatalf("unexpected widget schema title: %v", s["title"])
	}
	if _, ok := schemaByKind("unknown-kind"); ok {
		t.Fatal("unknown schema kind resolved")
	}
}

func TestWrapEvalSource(t *testing.T) {
	expr := wrapEvalSource("1+2;", false)
	if !strings.Contains(expr, "Возврат 1+2;") {
		t.Fatalf("expression not wrapped as return:\n%s", expr)
	}
	snippet := wrapEvalSource("Возврат 3;", true)
	if strings.Contains(snippet, "Возврат Возврат") || !strings.Contains(snippet, "Возврат 3;") {
		t.Fatalf("snippet wrapped incorrectly:\n%s", snippet)
	}
}

func TestFormatYAMLFile_SortsKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := os.WriteFile(path, []byte("b: 2\na: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := configfmt.FormatYAMLFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected --check to detect non-canonical YAML")
	}
	ok, err = configfmt.FormatYAMLFile(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("format should report changed file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "a: 1\nb: 2\n" {
		t.Fatalf("unexpected formatted YAML:\n%s", got)
	}
}

func TestScanImpact_ObjectAndField(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("reports/r.yaml", "query: |\n  ВЫБРАТЬ Клиент.Наименование ИЗ Справочник.Клиент\n")
	rep, err := scanImpact(dir, "Клиент", "Наименование", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Matches) == 0 {
		t.Fatal("expected impact matches")
	}
	var hasQualified bool
	for _, m := range rep.Matches {
		if m.Kind == "qualified-field" {
			hasQualified = true
		}
	}
	if !hasQualified {
		t.Fatalf("qualified field not detected: %+v", rep.Matches)
	}
	if rep.Summary["qualified-field"] == 0 {
		t.Fatalf("expected impact summary, got %+v", rep.Summary)
	}
	if len(rep.MigrationNotes) == 0 {
		t.Fatal("expected migration notes")
	}
}

func TestRefactorRenameFieldPreviewAndWrite(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("catalogs/клиент.yaml", "name: Клиент\nfields:\n  - name: Наименование\n    type: string\n")
	mustWrite("reports/r.yaml", "name: R\nquery: |\n  ВЫБРАТЬ Клиент.Наименование ИЗ Справочник.Клиент\n")

	ops, impact, err := buildRefactorOps(dir, refactorRequest{typ: "rename-field", object: "Клиент", from: "Наименование", to: "ПолноеНаименование"})
	if err != nil {
		t.Fatal(err)
	}
	if impact == nil || len(impact.Matches) == 0 {
		t.Fatalf("expected impact for refactor, got %+v", impact)
	}
	if len(ops) == 0 {
		t.Fatal("expected refactor ops")
	}
	check, rolledBack, err := applyRefactorOps(dir, ops)
	if err != nil {
		t.Fatalf("applyRefactorOps: %v check=%+v", err, check)
	}
	if rolledBack {
		t.Fatal("did not expect rollback")
	}
	data, err := os.ReadFile(filepath.Join(dir, "catalogs", "клиент.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ПолноеНаименование") {
		t.Fatalf("field not renamed:\n%s", data)
	}
}

func TestRefactorRenameObjectRenamesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalogs", "клиент.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("name: Клиент\nfields:\n  - name: Наименование\n    type: string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops, _, err := buildRefactorOps(dir, refactorRequest{typ: "rename-object", from: "Клиент", to: "Покупатель"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].renameTo != "catalogs/покупатель.yaml" {
		t.Fatalf("expected file rename op, got %+v", ops)
	}
}
