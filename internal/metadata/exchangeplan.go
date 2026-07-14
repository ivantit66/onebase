package metadata

// ExchangePlan — план обмена (аналог «Плана обмена» 1С, план 86): состав
// синхронизируемых данных + узлы-участники + правило разрешения конфликтов.
// Загружается из exchange/<имя>.yaml.
//
// «Этот узел» в YAML НЕ хранится: конфигурация симметрична для всех баз (центр
// и филиал берут один и тот же exchange/*.yaml). Код текущего узла — база-
// специфичная настройка (_settings, ключ exchange.this_node), задаётся командой
// `onebase exchange init --plan X --node <код>`.

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Допустимые правила разрешения конфликтов версий при загрузке пакета.
const (
	ConflictByTime         = "by_time"          // побеждает позже изменённый объект
	ConflictByNodePriority = "by_node_priority" // побеждает узел с большим priority
	ConflictByHook         = "hook"             // выбор делает DSL-хук ПриКонфликтеОбмена
)

// ExchangePlan описывает один план обмена.
type ExchangePlan struct {
	Name     string            `yaml:"name"`
	Title    string            `yaml:"title"`
	Titles   map[string]string `yaml:"titles"`
	Content  []string          `yaml:"content"` // "Справочник.X" / "Документ.X" / "X"
	Nodes    []ExchangeNode    `yaml:"nodes"`
	Conflict string            `yaml:"conflict"` // by_time | by_node_priority | hook

	// parsed — разобранный Content, заполняется в Normalize (при загрузке).
	// Кэшируется, потому что Includes зовётся на каждой записи объекта.
	parsed []ContentEntry
}

// ExchangeNode — узел (участник) плана обмена.
type ExchangeNode struct {
	Code     string `yaml:"code"`
	Name     string `yaml:"name"`
	Priority int    `yaml:"priority"` // используется правилом by_node_priority
}

// ContentEntry — разобранная запись состава обмена. Kind == "" означает «любой
// вид с таким именем» (запись без префикса Справочник./Документ.).
type ContentEntry struct {
	Kind Kind
	Name string
}

// DisplayName возвращает заголовок плана обмена с учётом языка.
func (p *ExchangePlan) DisplayName(lang string) string {
	if lang != "" {
		if v, ok := p.Titles[lang]; ok && v != "" {
			return v
		}
	}
	if p.Title != "" {
		return p.Title
	}
	return p.Name
}

// Normalize приводит план к каноничному виду: тримует имена, дефолтит правило
// конфликта, разбирает состав. Вызывается загрузчиком; экспортирован для
// программного построения планов (в т.ч. в тестах — без него Includes всегда
// вернёт false).
func (p *ExchangePlan) Normalize() {
	p.Name = strings.TrimSpace(p.Name)
	p.Conflict = strings.ToLower(strings.TrimSpace(p.Conflict))
	if p.Conflict == "" {
		p.Conflict = ConflictByTime
	}
	for i := range p.Nodes {
		p.Nodes[i].Code = strings.TrimSpace(p.Nodes[i].Code)
	}
	p.parsed = p.parsed[:0]
	for _, c := range p.Content {
		if strings.TrimSpace(c) == "" {
			continue
		}
		p.parsed = append(p.parsed, parseContentEntry(c))
	}
}

// ParsedContent возвращает разобранный состав обмена (для валидации и движка).
func (p *ExchangePlan) ParsedContent() []ContentEntry {
	return p.parsed
}

// Includes сообщает, входит ли сущность в состав обмена. Сверяет имя
// регистронезависимо; если у записи состава указан вид (Справочник./Документ.),
// он должен совпасть с видом сущности.
func (p *ExchangePlan) Includes(e *Entity) bool {
	if e == nil {
		return false
	}
	for _, c := range p.parsed {
		if !strings.EqualFold(c.Name, e.Name) {
			continue
		}
		if c.Kind == "" || c.Kind == e.Kind {
			return true
		}
	}
	return false
}

// Node ищет узел плана по коду (регистронезависимо). nil, если нет.
func (p *ExchangePlan) Node(code string) *ExchangeNode {
	for i := range p.Nodes {
		if strings.EqualFold(p.Nodes[i].Code, code) {
			return &p.Nodes[i]
		}
	}
	return nil
}

// parseContentEntry разбирает запись состава «Справочник.X» / «Документ.X» / «X».
// Префикс сравнивается регистронезависимо; для русских префиксов длина в байтах
// совпадает с оригиналом (пары регистра кириллицы одинаковой длины в UTF-8),
// поэтому срез по длине префикса безопасен.
func parseContentEntry(s string) ContentEntry {
	s = strings.TrimSpace(s)
	for _, pfx := range []struct {
		p    string
		kind Kind
	}{
		{"справочник.", KindCatalog},
		{"catalog.", KindCatalog},
		{"документ.", KindDocument},
		{"document.", KindDocument},
	} {
		if len(s) >= len(pfx.p) && strings.EqualFold(s[:len(pfx.p)], pfx.p) {
			return ContentEntry{Kind: pfx.kind, Name: strings.TrimSpace(s[len(pfx.p):])}
		}
	}
	return ContentEntry{Name: s}
}

// LoadExchangePlanFile читает один exchange/<имя>.yaml.
func LoadExchangePlanFile(path string) (*ExchangePlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p ExchangePlan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		p.Name = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}
	p.Normalize()
	return &p, nil
}

// LoadExchangePlanDir читает все планы обмена из каталога exchange/.
// Отсутствие каталога — не ошибка (обмен не настроен).
func LoadExchangePlanDir(dir string) ([]*ExchangePlan, error) {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var plans []*ExchangePlan
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			continue
		}
		p, err := LoadExchangePlanFile(filepath.Join(dir, item.Name()))
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, nil
}
