// internal/llm/openai_tools_test.go
package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// openaiToolServer: пока в истории нет сообщения role=tool — просит вызвать
// инструмент; после исполнения — отдаёт финальный текст.
func openaiToolServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), `"role":"tool"`) {
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"Остаток: 42"}}],"usage":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"остаток","arguments":"{\"товар\":\"гвозди\"}"}}]}}],"usage":{}}`))
	}))
}

func TestRunWithToolsOpenAI(t *testing.T) {
	srv := openaiToolServer(t)
	defer srv.Close()
	cfg := Config{
		Enabled:   true,
		Endpoints: []Endpoint{{Name: "o", Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "k"}},
		Models:    []Model{{Name: "m", Endpoint: "o"}},
		Profiles:  []Profile{{Task: "чат", Models: []string{"m"}}},
	}
	var gotCall ToolCall
	exec := func(ctx context.Context, call ToolCall) ToolResult {
		gotCall = call
		return ToolResult{ID: call.ID, Content: "42"}
	}
	tools := []Tool{{Name: "остаток", Description: "остаток товара", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"товар": map[string]any{"type": "string"}},
	}}}
	r := New(cfg, nil)
	resp, err := r.RunWithTools(context.Background(), "чат",
		ChatRequest{Messages: []Message{UserText("сколько гвоздей?")}}, tools, exec)
	if err != nil {
		t.Fatalf("RunWithTools: %v", err)
	}
	if resp.Text != "Остаток: 42" {
		t.Fatalf("неожиданный текст: %q", resp.Text)
	}
	if gotCall.Name != "остаток" {
		t.Fatalf("инструмент не вызван корректно: %+v", gotCall)
	}
	if v, _ := gotCall.Input["товар"].(string); v != "гвозди" {
		t.Fatalf("аргумент не распознан: %+v", gotCall.Input)
	}
}

// TestRunWithToolsOpenAI_StopFinishReason — регрессия: OpenAI-совместимые
// провайдеры (Ollama, LM Studio, прокси) присылают tool_calls с
// finish_reason:"stop". Инструмент всё равно обязан исполниться, а не
// проглатываться (раньше гейт на == "tool_calls" терял вызов).
func TestRunWithToolsOpenAI_StopFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), `"role":"tool"`) {
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"Остаток: 42"}}],"usage":{}}`))
			return
		}
		// tool_calls присутствуют, но finish_reason — "stop", а не "tool_calls".
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"остаток","arguments":"{\"товар\":\"гвозди\"}"}}]}}],"usage":{}}`))
	}))
	defer srv.Close()
	cfg := Config{
		Enabled:   true,
		Endpoints: []Endpoint{{Name: "o", Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "k"}},
		Models:    []Model{{Name: "m", Endpoint: "o"}},
		Profiles:  []Profile{{Task: "чат", Models: []string{"m"}}},
	}
	var gotCall ToolCall
	exec := func(ctx context.Context, call ToolCall) ToolResult {
		gotCall = call
		return ToolResult{ID: call.ID, Content: "42"}
	}
	tools := []Tool{{Name: "остаток", Description: "остаток товара", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"товар": map[string]any{"type": "string"}},
	}}}
	r := New(cfg, nil)
	resp, err := r.RunWithTools(context.Background(), "чат",
		ChatRequest{Messages: []Message{UserText("сколько гвоздей?")}}, tools, exec)
	if err != nil {
		t.Fatalf("RunWithTools: %v", err)
	}
	if gotCall.Name != "остаток" {
		t.Fatalf("инструмент не исполнен при finish_reason=stop: %+v", gotCall)
	}
	if resp.Text != "Остаток: 42" {
		t.Fatalf("неожиданный текст: %q", resp.Text)
	}
}
