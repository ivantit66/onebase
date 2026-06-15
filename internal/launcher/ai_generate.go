package launcher

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ivantit66/onebase/internal/configcheck"
	"github.com/ivantit66/onebase/internal/project"
)

// GenChange — один предложенный объект в diff генерации.
type GenChange struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"` // "новый" | "изменён"
	NewContent string `json:"newContent"`
	OldContent string `json:"oldContent,omitempty"`
}

// genSession — staging-оверлей конфигурации + накопленные изменения одной генерации.
type genSession struct {
	srcDir  string
	overlay string
	changed map[string]bool // относительные пути (slash) созданных/изменённых файлов
}

// kindSubdir сопоставляет тип объекта подкаталогу конфигурации (как в
// configcheck.CheckDir). Регистронезависимо, по синонимам.
func kindSubdir(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "справочник", "каталог", "catalog":
		return "catalogs", true
	case "документ", "document":
		return "documents", true
	case "регистр накопления", "регистрнакопления", "регистр", "register":
		return "registers", true
	case "регистр сведений", "регистрсведений", "inforegister":
		return "inforegs", true
	case "перечисление", "enum":
		return "enums", true
	case "план счетов", "плансчетов", "chartofaccounts":
		return "accounts", true
	case "регистр бухгалтерии", "регистрбухгалтерии", "accountregister":
		return "accountregs", true
	default:
		return "", false
	}
}

// safeFileName проверяет имя объекта и возвращает имя файла (lower + .yaml).
func safeFileName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return "", fmt.Errorf("пустое имя объекта")
	}
	if n == "." || strings.ContainsAny(n, "/\\") || strings.Contains(n, "..") {
		return "", fmt.Errorf("недопустимое имя объекта: %q", name)
	}
	return strings.ToLower(n) + ".yaml", nil
}

// newGenSession делает рекурсивную копию srcDir во временный overlay.
func newGenSession(srcDir string) (*genSession, error) {
	overlay, err := os.MkdirTemp("", "onebase-gen-")
	if err != nil {
		return nil, err
	}
	if err := copyTree(srcDir, overlay); err != nil {
		os.RemoveAll(overlay)
		return nil, err
	}
	return &genSession{srcDir: srcDir, overlay: overlay, changed: map[string]bool{}}, nil
}

func (g *genSession) close() {
	if g.overlay != "" {
		os.RemoveAll(g.overlay)
	}
}

// createObject записывает YAML объекта в overlay по типу. Пишет только внутрь
// overlay (имя валидируется).
func (g *genSession) createObject(kind, name, yamlText string) error {
	subdir, ok := kindSubdir(kind)
	if !ok {
		return fmt.Errorf("неизвестный тип объекта: %q (допустимо: справочник, документ, регистр накопления, регистр сведений, перечисление, план счетов, регистр бухгалтерии)", kind)
	}
	fname, err := safeFileName(name)
	if err != nil {
		return err
	}
	rel := subdir + "/" + fname
	full := filepath.Join(g.overlay, subdir, fname)
	cleanOverlay := filepath.Clean(g.overlay)
	if !strings.HasPrefix(filepath.Clean(full), cleanOverlay+string(os.PathSeparator)) {
		return fmt.Errorf("путь вне overlay: %q", rel)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, []byte(yamlText), 0o644); err != nil {
		return err
	}
	g.changed[rel] = true
	return nil
}

// check валидирует overlay без исполнения кода: CheckDir (парс YAML) + project.Load
// (кросс-ссылки; модули парсятся, не исполняются). CheckQueries НЕ зовём — он
// исполняет запросы. Возвращает человекочитаемый текст для модели.
func (g *genSession) check() string {
	issues, _ := configcheck.CheckDir(g.overlay)
	if proj, err := project.Load(g.overlay); err == nil {
		proj.Close()
	} else if !configcheck.AlreadyReported(issues, err.Error()) {
		issues = append(issues, configcheck.Issue{Message: "Project.Load: " + err.Error()})
	}
	if len(issues) == 0 {
		return "Нет ошибок."
	}
	var b strings.Builder
	b.WriteString("Найдены ошибки:\n")
	for _, is := range issues {
		// Capitalize object name for readability (e.g. "заявка" → "Заявка").
		obj := is.Object
		if r, size := utf8.DecodeRuneInString(obj); size > 0 {
			obj = strings.ToUpper(string(r)) + obj[size:]
		}
		if is.File != "" {
			fmt.Fprintf(&b, "- %s %s (%s): %s\n", is.Kind, obj, is.File, is.Message)
		} else {
			fmt.Fprintf(&b, "- %s\n", is.Message)
		}
	}
	return b.String()
}

// showObject возвращает YAML существующего объекта (ищет по имени во всех
// подкаталогах метаданных overlay). Для контекста модели.
func (g *genSession) showObject(name string) string {
	fname, err := safeFileName(name)
	if err != nil {
		return "ошибка: " + err.Error()
	}
	for _, sub := range []string{"catalogs", "documents", "registers", "inforegs", "enums", "accounts", "accountregs"} {
		p := filepath.Join(g.overlay, sub, fname)
		if data, err := os.ReadFile(p); err == nil {
			return string(data)
		}
	}
	return fmt.Sprintf("объект %q не найден", name)
}

// diff возвращает предложенные изменения (по changed): новый или изменён.
func (g *genSession) diff() []GenChange {
	rels := make([]string, 0, len(g.changed))
	for rel := range g.changed {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	out := make([]GenChange, 0, len(rels))
	for _, rel := range rels {
		newData, err := os.ReadFile(filepath.Join(g.overlay, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		ch := GenChange{Path: rel, Kind: "новый", NewContent: string(newData)}
		if oldData, err := os.ReadFile(filepath.Join(g.srcDir, filepath.FromSlash(rel))); err == nil {
			ch.Kind = "изменён"
			ch.OldContent = string(oldData)
		}
		out = append(out, ch)
	}
	return out
}

// copyTree рекурсивно копирует содержимое src в dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		// Возвращаем ошибку Close — она ловит сбой сброса буфера (напр. диск
		// заполнен), иначе усечённая копия молча сошла бы за успех.
		return out.Close()
	})
}
