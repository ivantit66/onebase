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
