package metadata

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Journal struct {
	Name      string
	Title     string
	Documents []string
	Columns   []JournalColumn
	Filters   []JournalFilter
}

type JournalColumn struct {
	Field    string
	Label    string
	Fallback []string
	// Map — явное соответствие docTypeName → fieldName в этом документе.
	// Замечание #7: лучшая альтернатива fallback, потому что:
	//   1. Explicit — видно для какого документа какое поле.
	//   2. Не зависит от порядка обхода fallback (raw COALESCE мог давать
	//      нежелательные совпадения если в документе два совпадающих поля).
	//   3. UI может показывать tooltip «в документе X это поле называется Y».
	// Если Map[doc] есть — используется он, fallback не применяется. Если
	// не задан — работает старый fallback-механизм (back compat).
	Map      map[string]string
	Format   string // "date" | "number" | "" (auto)
}

type JournalFilter struct {
	Field string
	Type  string // "date_range" | "reference:X" | "string"
}

type rawJournal struct {
	Name      string `yaml:"name"`
	Title     string `yaml:"title"`
	Documents []string `yaml:"documents"`
	Columns   []struct {
		Field    string            `yaml:"field"`
		Label    string            `yaml:"label"`
		Fallback []string          `yaml:"fallback"`
		Map      map[string]string `yaml:"map"`
		Format   string            `yaml:"format"`
	} `yaml:"columns"`
	Filters []struct {
		Field string `yaml:"field"`
		Type  string `yaml:"type"`
	} `yaml:"filters"`
}

func LoadJournalFile(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw rawJournal
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Name == "" {
		raw.Name = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}
	if raw.Title == "" {
		raw.Title = raw.Name
	}
	j := &Journal{
		Name:      raw.Name,
		Title:     raw.Title,
		Documents: raw.Documents,
	}
	for _, rc := range raw.Columns {
		label := rc.Label
		if label == "" {
			label = rc.Field
		}
		j.Columns = append(j.Columns, JournalColumn{
			Field:    rc.Field,
			Label:    label,
			Fallback: rc.Fallback,
			Map:      rc.Map,
			Format:   rc.Format,
		})
	}
	for _, rf := range raw.Filters {
		ft := rf.Type
		if ft == "" {
			ft = "string"
		}
		j.Filters = append(j.Filters, JournalFilter{Field: rf.Field, Type: ft})
	}
	return j, nil
}

func LoadJournalDir(dir string) ([]*Journal, error) {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var journals []*Journal
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		j, err := LoadJournalFile(filepath.Join(dir, item.Name()))
		if err != nil {
			return nil, err
		}
		journals = append(journals, j)
	}
	return journals, nil
}
