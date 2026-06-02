package printform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DSLPrintForm describes a print form implemented as a DSL module (.os file).
type DSLPrintForm struct {
	Name       string          // form name (filename without extension)
	Document   string          // entity name (extracted from first comment line or directory)
	Source     string          // raw .os source code
	Layout     *LayoutTemplate // associated макет template (optional)
	LayoutPath string          // path to .layout.yaml file (empty if no layout)
}

// LoadFile parses a single YAML print form file.
func LoadFile(path string) (*PrintForm, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("printform: read %s: %w", path, err)
	}
	pf, err := ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("printform: parse %s: %w", path, err)
	}
	if pf.Name == "" {
		pf.Name = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}
	return pf, nil
}

// ParseBytes разбирает YAML печатной формы из памяти (без файла). Нужна для
// внешних форм, которые хранятся в БД (см. internal/extform). Имя формы здесь
// не подставляется из имени файла — вызывающий обязан задать его сам, если в
// YAML поле name пустое.
func ParseBytes(data []byte) (*PrintForm, error) {
	var pf PrintForm
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

// LoadDir loads all *.yaml and *.os files from the given directory as print forms.
// For subdirectories, .os files inside are associated with the subdirectory name as the Document.
// Returns nil, nil if the directory does not exist.
func LoadDir(dir string) ([]*PrintForm, []*DSLPrintForm, error) {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("printform: readdir %s: %w", dir, err)
	}
	var forms []*PrintForm
	var dslForms []*DSLPrintForm
	for _, item := range items {
		name := item.Name()
		if item.IsDir() {
			// Load .os files from subdirectory, using folder name as entity name
			subItems, err := os.ReadDir(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			for _, si := range subItems {
				if si.IsDir() || !strings.HasSuffix(si.Name(), ".os") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(dir, name, si.Name()))
				if err != nil {
					return nil, nil, fmt.Errorf("printform: read %s/%s: %w", name, si.Name(), err)
				}
				src := string(data)
				docName := extractDocument(src, name)
				lt, ltPath := loadLayoutForFile(filepath.Join(dir, name, si.Name()))
				dslForms = append(dslForms, &DSLPrintForm{
					Name:       strings.TrimSuffix(si.Name(), ".os"),
					Document:   docName,
					Source:     src,
					Layout:     lt,
					LayoutPath: ltPath,
				})
			}
			continue
		}
		if strings.HasSuffix(name, ".yaml") {
			pf, err := LoadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, nil, err
			}
			forms = append(forms, pf)
		} else if strings.HasSuffix(name, ".os") {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, nil, fmt.Errorf("printform: read %s: %w", name, err)
			}
			src := string(data)
			docName := extractDocument(src, "")
			lt2, ltPath2 := loadLayoutForFile(filepath.Join(dir, name))
			dslForms = append(dslForms, &DSLPrintForm{
				Name:       strings.TrimSuffix(name, ".os"),
				Document:   docName,
				Source:     src,
				Layout:     lt2,
				LayoutPath: ltPath2,
			})
		}
	}
	return forms, dslForms, nil
}

// extractDocument tries to extract the entity name from the first comment line
// like "// Документ: Счёт" or "// Document: Invoice". Falls back to folderName.
func extractDocument(src, folderName string) string {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
		comment := strings.TrimSpace(strings.TrimPrefix(line, "//"))
		for _, prefix := range []string{"Документ:", "Document:", "документ:"} {
			if strings.HasPrefix(comment, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(comment, prefix))
			}
		}
	}
	return folderName
}


// loadLayoutForFile tries to find and load a .layout.yaml file matching the given .os file.
func loadLayoutForFile(osPath string) (*LayoutTemplate, string) {
	layoutPath := FindLayoutFile(osPath)
	if layoutPath == "" {
		return nil, ""
	}
	lt, err := LoadLayout(layoutPath)
	if err != nil {
		return nil, ""
	}
	return lt, layoutPath
}
