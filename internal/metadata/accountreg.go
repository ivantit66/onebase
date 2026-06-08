package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AccountRegister is a double-entry bookkeeping register linked to a ChartOfAccounts.
type AccountRegister struct {
	Name      string            `yaml:"name"`
	Title     string            `yaml:"title"`
	Titles    map[string]string `yaml:"titles"`
	Accounts  string            `yaml:"accounts"` // name of the ChartOfAccounts
	Resources []Field           `yaml:"-"`        // parsed from raw
	// Subconto — аналитические разрезы на проводках (по аналогии с 1С). Порядок
	// объявления задаёт нумерацию Субконто1, Субконто2, … для краткой записи в DSL
	// и стабильные имена колонок субконто<N> в таблице регистра.
	Subconto []Field `yaml:"-"`
}

// DisplayName возвращает заголовок регистра бухгалтерии с учётом языка.
func (ar *AccountRegister) DisplayName(lang string) string {
	if lang != "" {
		if v, ok := ar.Titles[lang]; ok && v != "" {
			return v
		}
	}
	if ar.Title != "" {
		return ar.Title
	}
	return ar.Name
}

type rawAccountReg struct {
	Name      string            `yaml:"name"`
	Title     string            `yaml:"title"`
	Titles    map[string]string `yaml:"titles"`
	Accounts  string            `yaml:"accounts"`
	Resources []struct {
		Name   string            `yaml:"name"`
		Title  string            `yaml:"title"`
		Titles map[string]string `yaml:"titles"`
		Type   string            `yaml:"type"`
	} `yaml:"resources"`
	Subconto []struct {
		Name   string            `yaml:"name"`
		Title  string            `yaml:"title"`
		Titles map[string]string `yaml:"titles"`
		Type   string            `yaml:"type"`
	} `yaml:"subconto"`
}

func LoadAccountRegisterFile(path string) (*AccountRegister, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("accountreg: read %s: %w", path, err)
	}
	var raw rawAccountReg
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("accountreg: parse %s: %w", path, err)
	}
	ar := &AccountRegister{
		Name:     raw.Name,
		Title:    raw.Title,
		Titles:   raw.Titles,
		Accounts: raw.Accounts,
	}
	if ar.Title == "" {
		ar.Title = ar.Name
	}
	for _, r := range raw.Resources {
		ar.Resources = append(ar.Resources, parseField(rawField{
			Name:   r.Name,
			Title:  r.Title,
			Titles: r.Titles,
			Type:   r.Type,
		}))
	}
	for _, s := range raw.Subconto {
		ar.Subconto = append(ar.Subconto, parseField(rawField{
			Name:   s.Name,
			Title:  s.Title,
			Titles: s.Titles,
			Type:   s.Type,
		}))
	}
	return ar, nil
}

func LoadAccountRegisterDir(dir string) ([]*AccountRegister, error) {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("accountreg: readdir %s: %w", dir, err)
	}
	var regs []*AccountRegister
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		ar, err := LoadAccountRegisterFile(filepath.Join(dir, item.Name()))
		if err != nil {
			return nil, err
		}
		regs = append(regs, ar)
	}
	return regs, nil
}

// AccountRegTableName returns the PostgreSQL table name for an account register.
func AccountRegTableName(name string) string {
	return "акк_" + strings.ToLower(name)
}

// SubcontoColumn возвращает имя колонки субконто по его порядковому номеру (1-based).
// Имя стабильно при переименовании поля субконто, что упрощает краткую запись
// СубконтоN в DSL и запросах.
func SubcontoColumn(idx int) string {
	return fmt.Sprintf("субконто%d", idx)
}
