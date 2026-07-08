package llm

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseConfig_LogHistory(t *testing.T) {
	c, err := ParseConfig(`{"enabled":true,"log_history":true}`)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if !c.LogHistory {
		t.Error("log_history не распознан конфигом")
	}
	d, _ := ParseConfig(`{"enabled":true}`)
	if d.LogHistory {
		t.Error("LogHistory должен быть false по умолчанию")
	}
}

// TestResolveExpandsEnvRefs проверяет, что ${env:VAR} в ключе/base_url/заголовках
// endpoint'а разыменовывается в Resolve — так секрет работает и на пути _settings
// (базы из веб-конфигуратора), а не только в app.yaml. Оригинальный конфиг при
// этом не мутируется (в _settings/describe остаётся ссылка, не сам ключ).
func TestResolveExpandsEnvRefs(t *testing.T) {
	t.Setenv("ONEBASE_TEST_RESOLVE_KEY", "secret-xyz")
	t.Setenv("ONEBASE_TEST_RESOLVE_HDR", "hval")
	cfg := Config{
		Enabled: true,
		Endpoints: []Endpoint{{
			Name:    "z_ai",
			Kind:    KindAnthropic,
			BaseURL: "https://api.z.ai/${env:ONEBASE_TEST_RESOLVE_MISSING}",
			APIKey:  "${env:ONEBASE_TEST_RESOLVE_KEY}",
			Headers: map[string]string{"X-Extra": "${env:ONEBASE_TEST_RESOLVE_HDR}"},
		}},
		Models:   []Model{{Name: "glm", Endpoint: "z_ai"}},
		Profiles: []Profile{{Task: "чат", Models: []string{"glm"}}},
	}
	rm, err := cfg.Resolve("чат")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := rm[0].Endpoint.APIKey; got != "secret-xyz" {
		t.Errorf("ключ не разыменован: %q", got)
	}
	if got := rm[0].Endpoint.Headers["X-Extra"]; got != "hval" {
		t.Errorf("заголовок не разыменован: %q", got)
	}
	// Отсутствующая переменная → пустая подстановка.
	if got := rm[0].Endpoint.BaseURL; got != "https://api.z.ai/" {
		t.Errorf("base_url разыменован неверно: %q", got)
	}
	// Оригинал не тронут: в конфиге по-прежнему ссылка, а не секрет.
	if cfg.Endpoints[0].APIKey != "${env:ONEBASE_TEST_RESOLVE_KEY}" {
		t.Errorf("Resolve мутировал исходный конфиг: %q", cfg.Endpoints[0].APIKey)
	}
}

// TestRedactedKeepsEnvRef — ${env:VAR}-ссылка не секрет, поэтому Redacted
// оставляет её видимой (иначе админ не понял бы, откуда берётся ключ), а
// UnmaskKeys не должен путать её с реальным значением.
func TestRedactedKeepsEnvRef(t *testing.T) {
	cfg := Config{Endpoints: []Endpoint{{Name: "e", APIKey: "${env:MY_LLM_KEY}"}}}
	red := cfg.Redacted()
	if red.Endpoints[0].APIKey != "${env:MY_LLM_KEY}" {
		t.Errorf("env-ссылка не должна маскироваться: %q", red.Endpoints[0].APIKey)
	}
}

func TestParseConfig_ModelProviderAlias(t *testing.T) {
	c, err := ParseConfig(`{
		"enabled": true,
		"endpoints": [{"name": "z_ai", "kind": "anthropic"}],
		"models": [{"name": "glm-4.6", "provider": "z_ai"}],
		"profiles": [{"task": "чат", "models": ["glm-4.6"]}]
	}`)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got := c.Models[0].Endpoint; got != "z_ai" {
		t.Fatalf("provider alias was not normalized to endpoint: %q", got)
	}
	raw, err := c.JSON()
	if err != nil {
		t.Fatalf("Config.JSON: %v", err)
	}
	if strings.Contains(raw, "provider") || !strings.Contains(raw, `"endpoint":"z_ai"`) {
		t.Fatalf("canonical JSON must use endpoint only, got %s", raw)
	}
}

func TestModelYAMLProviderAlias(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte(`
enabled: true
models:
  - name: glm-4.6
    provider: z_ai
`), &c); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if len(c.Models) != 1 || c.Models[0].Endpoint != "z_ai" {
		t.Fatalf("provider alias was not normalized from YAML: %+v", c.Models)
	}
}

func TestUnmaskKeys(t *testing.T) {
	prev := Config{
		Endpoints: []Endpoint{
			{Name: "z", APIKey: "REAL-SECRET"},
		},
	}

	t.Run("masked key is restored from prev", func(t *testing.T) {
		submitted := Config{
			Endpoints: []Endpoint{
				{Name: "z", APIKey: "****CRET"},
			},
		}
		got := submitted.UnmaskKeys(prev)
		if got.Endpoints[0].APIKey != "REAL-SECRET" {
			t.Errorf("want REAL-SECRET, got %q", got.Endpoints[0].APIKey)
		}
	})

	t.Run("new real key is kept as-is", func(t *testing.T) {
		submitted := Config{
			Endpoints: []Endpoint{
				{Name: "z", APIKey: "NEW-KEY"},
			},
		}
		got := submitted.UnmaskKeys(prev)
		if got.Endpoints[0].APIKey != "NEW-KEY" {
			t.Errorf("want NEW-KEY, got %q", got.Endpoints[0].APIKey)
		}
	})

	t.Run("empty key stays empty", func(t *testing.T) {
		submitted := Config{
			Endpoints: []Endpoint{
				{Name: "z", APIKey: ""},
			},
		}
		got := submitted.UnmaskKeys(prev)
		if got.Endpoints[0].APIKey != "" {
			t.Errorf("want empty, got %q", got.Endpoints[0].APIKey)
		}
	})

	t.Run("unknown endpoint with masked key stays masked (no crash)", func(t *testing.T) {
		submitted := Config{
			Endpoints: []Endpoint{
				{Name: "unknown", APIKey: "****XXXX"},
			},
		}
		got := submitted.UnmaskKeys(prev)
		if got.Endpoints[0].APIKey != "****XXXX" {
			t.Errorf("want ****XXXX unchanged, got %q", got.Endpoints[0].APIKey)
		}
	})

	t.Run("round-trip: Redacted then UnmaskKeys restores original", func(t *testing.T) {
		// Keys must be >4 chars to actually get masked by Redacted().
		original := Config{
			Endpoints: []Endpoint{
				{Name: "a", APIKey: "LONGKEY1234"},
				{Name: "b", APIKey: "ANOTHERSECRET"},
			},
		}
		restored := original.Redacted().UnmaskKeys(original)
		for i, e := range original.Endpoints {
			if restored.Endpoints[i].APIKey != e.APIKey {
				t.Errorf("endpoint %q: want %q, got %q", e.Name, e.APIKey, restored.Endpoints[i].APIKey)
			}
		}
	})
}
