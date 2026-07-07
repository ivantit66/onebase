// Package llm реализует ядро ИИ-помощника OneBase: конфигурацию провайдеров и
// моделей, маршрутизацию вызовов по задачам и фолбэк-движок. Пакет намеренно не
// зависит от storage/dsl, чтобы его можно было переиспользовать из builtin'ов,
// UI и конфигуратора без циклов импорта (план 51).
package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Kind — протокол/семейство API, по которому обращаемся к провайдеру.
type Kind string

const (
	KindGemini     Kind = "gemini"     // Google Generative Language API (vision)
	KindAnthropic  Kind = "anthropic"  // Anthropic Messages API (он же z.ai/GLM по base_url)
	KindOpenAI     Kind = "openai"     // OpenAI chat/completions
	KindCompatible Kind = "compatible" // OpenAI-совместимый (Ollama/LM Studio/прокси)
)

// Endpoint — именованное подключение к провайдеру: куда ходить и каким ключом.
type Endpoint struct {
	Name       string            `json:"name" yaml:"name"`
	Kind       Kind              `json:"kind" yaml:"kind"`
	BaseURL    string            `json:"base_url,omitempty" yaml:"base_url,omitempty"` // для z.ai/локальных; пусто → дефолт провайдера
	APIKey     string            `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`         // дополнительные заголовки
	TimeoutSec int               `json:"timeout_sec,omitempty" yaml:"timeout_sec,omitempty"` // 0 → DefaultTimeoutSec
}

// Model — конкретная модель у провайдера. Vision помечает мультимодальные модели,
// пригодные для распознавания документов (см. профиль "документы").
type Model struct {
	Name      string `json:"name" yaml:"name"`         // имя модели у провайдера, напр. gemini-2.5-flash
	Endpoint  string `json:"endpoint" yaml:"endpoint"` // ссылка на Endpoint.Name
	Vision    bool   `json:"vision,omitempty" yaml:"vision,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"` // 0 → DefaultMaxTokens
}

type modelWire struct {
	Name      string `json:"name" yaml:"name"`
	Endpoint  string `json:"endpoint" yaml:"endpoint"`
	Provider  string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Vision    bool   `json:"vision,omitempty" yaml:"vision,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
}

// UnmarshalJSON accepts the legacy UI name "provider" as an alias for
// Model.Endpoint. The canonical serialized form remains "endpoint".
func (m *Model) UnmarshalJSON(data []byte) error {
	var w modelWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*m = modelFromWire(w)
	return nil
}

// UnmarshalYAML keeps app.yaml tolerant to the same provider/endpoint naming
// split as the configurator UI.
func (m *Model) UnmarshalYAML(value *yaml.Node) error {
	var w modelWire
	if err := value.Decode(&w); err != nil {
		return err
	}
	*m = modelFromWire(w)
	return nil
}

func modelFromWire(w modelWire) Model {
	endpoint := w.Endpoint
	if endpoint == "" {
		endpoint = w.Provider
	}
	return Model{
		Name:      w.Name,
		Endpoint:  endpoint,
		Vision:    w.Vision,
		MaxTokens: w.MaxTokens,
	}
}

// Profile — маршрут одной задачи: упорядоченная цепочка моделей. Движок идёт по
// списку сверху вниз, переключаясь на следующую при лимитах/временных ошибках.
type Profile struct {
	Task   string   `json:"task" yaml:"task"`     // документы | анализ | чат | конфигуратор | ...
	Models []string `json:"models" yaml:"models"` // имена Model в порядке предпочтения
}

// Config — весь LLM-конфиг базы. Хранится одним JSON-значением в _settings
// (ключ llm.config), чтобы не плодить десятки ключей.
type Config struct {
	Enabled        bool       `json:"enabled" yaml:"enabled"`
	Endpoints      []Endpoint `json:"endpoints" yaml:"endpoints"`
	Models         []Model    `json:"models" yaml:"models"`
	Profiles       []Profile  `json:"profiles" yaml:"profiles"`
	DefaultProfile string     `json:"default_profile,omitempty" yaml:"default_profile,omitempty"` // для неуказанных задач
	LogHistory     bool       `json:"log_history,omitempty" yaml:"log_history,omitempty"`         // вести журнал ИИ-обращений конфигуратора
	MaxToolRounds  int        `json:"max_tool_rounds,omitempty" yaml:"max_tool_rounds,omitempty"` // 0 → MaxToolIterations
}

// Дефолты, применяемые когда в конфиге не задано иное.
const (
	DefaultTimeoutSec = 60
	DefaultMaxTokens  = 4096

	// TaskAnalysis — профиль по умолчанию для большинства текстовых задач.
	TaskAnalysis = "анализ"
	// TaskDocuments — профиль распознавания документов (требует vision-модель).
	TaskDocuments = "документы"
)

// ResolvedModel — модель вместе с её endpoint'ом, готовая к вызову.
type ResolvedModel struct {
	Model    Model
	Endpoint Endpoint
}

// ParseConfig разбирает JSON из _settings. Пустая строка → выключенный конфиг.
func ParseConfig(raw string) (Config, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Config{}, nil
	}
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, fmt.Errorf("llm: разбор конфига: %w", err)
	}
	return c, nil
}

// JSON сериализует конфиг для сохранения в _settings.
func (c Config) JSON() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("llm: сериализация конфига: %w", err)
	}
	return string(b), nil
}

// Redacted возвращает копию конфига с замаскированными ключами — для вывода в UI,
// describe и экспорт конфигурации (ключи не должны утекать).
func (c Config) Redacted() Config {
	out := c
	out.Endpoints = make([]Endpoint, len(c.Endpoints))
	for i, e := range c.Endpoints {
		if e.APIKey != "" {
			e.APIKey = maskKey(e.APIKey)
		}
		out.Endpoints[i] = e
	}
	return out
}

func maskKey(k string) string {
	if len(k) <= 4 {
		return "****"
	}
	return "****" + k[len(k)-4:]
}

// UnmaskKeys восстанавливает реальные API-ключи для endpoint'ов, чьи ключи пришли
// замаскированными (префикс "****") — т.е. админ не менял их в форме Redacted().
// Заданный заново ключ (без префикса "****") и явно очищённый (пустой) сохраняются
// как есть. prev — ранее сохранённый конфиг (источник реальных ключей).
func (c Config) UnmaskKeys(prev Config) Config {
	out := c
	out.Endpoints = make([]Endpoint, len(c.Endpoints))
	for i, e := range c.Endpoints {
		if strings.HasPrefix(e.APIKey, "****") {
			if pe, ok := prev.endpoint(e.Name); ok {
				e.APIKey = pe.APIKey
			}
		}
		out.Endpoints[i] = e
	}
	return out
}

func (c Config) endpoint(name string) (Endpoint, bool) {
	for _, e := range c.Endpoints {
		if strings.EqualFold(e.Name, name) {
			return e, true
		}
	}
	return Endpoint{}, false
}

func (c Config) model(name string) (Model, bool) {
	for _, m := range c.Models {
		if strings.EqualFold(m.Name, name) {
			return m, true
		}
	}
	return Model{}, false
}

func (c Config) profile(task string) (Profile, bool) {
	for _, p := range c.Profiles {
		if strings.EqualFold(p.Task, task) {
			return p, true
		}
	}
	return Profile{}, false
}

// Resolve возвращает упорядоченную цепочку моделей для задачи. Если профиль задачи
// не найден — используется DefaultProfile. Каждая модель связывается со своим
// endpoint'ом; модели с битыми ссылками пропускаются (но это не ошибка, пока в
// цепочке остаётся хоть одна валидная).
func (c Config) Resolve(task string) ([]ResolvedModel, error) {
	if !c.Enabled {
		return nil, fmt.Errorf("ИИ-помощник выключен в настройках")
	}
	p, ok := c.profile(task)
	if !ok && c.DefaultProfile != "" {
		p, ok = c.profile(c.DefaultProfile)
	}
	if !ok {
		return nil, fmt.Errorf("не найден профиль задачи %q (и нет профиля по умолчанию)", task)
	}
	var out []ResolvedModel
	for _, mname := range p.Models {
		m, ok := c.model(mname)
		if !ok {
			continue
		}
		ep, ok := c.endpoint(m.Endpoint)
		if !ok {
			continue
		}
		out = append(out, ResolvedModel{Model: m, Endpoint: ep})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("профиль задачи %q не содержит ни одной настроенной модели", p.Task)
	}
	return out, nil
}
