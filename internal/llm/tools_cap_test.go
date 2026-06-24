package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// capServer — сервер, который ВСЕГДА возвращает stop_reason:"tool_use", никогда
// не завершая разговор. Считает число запросов атомарно.
func capServer(t *testing.T, counter *int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(counter, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"нет_конца","input":{}}],"usage":{}}`)
	}))
}

// TestRunWithToolsCapReached проверяет, что RunWithTools возвращает ошибку
// "превышен лимит раундов" и делает ровно MaxToolIterations запросов.
func TestRunWithToolsCapReached(t *testing.T) {
	var reqCount int64
	srv := capServer(t, &reqCount)
	defer srv.Close()

	cfg := Config{
		Enabled:   true,
		Endpoints: []Endpoint{{Name: "ep", Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k"}},
		Models:    []Model{{Name: "m", Endpoint: "ep"}},
		Profiles:  []Profile{{Task: "чат", Models: []string{"m"}}},
	}

	exec := func(ctx context.Context, call ToolCall) ToolResult {
		return ToolResult{ID: call.ID, Content: "dummy"}
	}
	tools := []Tool{{Name: "нет_конца", Description: "never ends"}}

	r := New(cfg, nil)
	_, err := r.RunWithTools(context.Background(), "чат",
		ChatRequest{Messages: []Message{UserText("начни")}}, tools, exec)

	if err == nil {
		t.Fatal("ожидалась ошибка превышения лимита, получен nil")
	}
	if !strings.Contains(err.Error(), "превышен лимит раундов") {
		t.Fatalf("ошибка не содержит ожидаемой строки: %v", err)
	}
	if got := atomic.LoadInt64(&reqCount); got != int64(MaxToolIterations) {
		t.Fatalf("ожидалось %d запросов к серверу, получено %d", MaxToolIterations, got)
	}
}

func TestRunWithToolsUsesConfiguredMaxToolRounds(t *testing.T) {
	var reqCount int64
	srv := capServer(t, &reqCount)
	defer srv.Close()

	cfg := Config{
		Enabled:       true,
		Endpoints:     []Endpoint{{Name: "ep", Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k"}},
		Models:        []Model{{Name: "m", Endpoint: "ep"}},
		Profiles:      []Profile{{Task: "чат", Models: []string{"m"}}},
		MaxToolRounds: 3,
	}
	r := New(cfg, nil)
	_, err := r.RunWithTools(context.Background(), "чат", ChatRequest{Messages: []Message{UserText("начни")}},
		[]Tool{{Name: "нет_конца"}}, func(context.Context, ToolCall) ToolResult { return ToolResult{Content: "dummy"} })
	if err == nil || !strings.Contains(err.Error(), "(3)") {
		t.Fatalf("expected configured max rounds error, got %v", err)
	}
	if got := atomic.LoadInt64(&reqCount); got != 3 {
		t.Fatalf("ожидалось 3 запроса к серверу, получено %d", got)
	}
}

// TestRunContextCancelAbortsChain проверяет, что уже отменённый контекст
// вызывающего не вызывает перебор всей цепочки моделей (Fix 2).
func TestRunContextCancelAbortsChain(t *testing.T) {
	// Оба сервера не должны быть достигнуты, но даже если первый будет вызван —
	// ctx уже отменён, и Run должен немедленно вернуть context.Canceled.
	srv1 := anthropicServer(t, 200, "никогда")
	defer srv1.Close()
	srv2 := anthropicServer(t, 200, "никогда2")
	defer srv2.Close()

	cfg := Config{
		Enabled: true,
		Endpoints: []Endpoint{
			{Name: "ep1", Kind: KindAnthropic, BaseURL: srv1.URL, APIKey: "k1"},
			{Name: "ep2", Kind: KindAnthropic, BaseURL: srv2.URL, APIKey: "k2"},
		},
		Models: []Model{
			{Name: "m1", Endpoint: "ep1"},
			{Name: "m2", Endpoint: "ep2"},
		},
		Profiles: []Profile{{Task: TaskAnalysis, Models: []string{"m1", "m2"}}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем немедленно

	r := New(cfg, nil)
	_, err := r.Run(ctx, TaskAnalysis, ChatRequest{Messages: []Message{UserText("x")}})
	if err == nil {
		t.Fatal("ожидалась ошибка, получен nil")
	}
	// Должна быть именно context.Canceled, а не "все модели исчерпаны".
	if !isContextCanceled(err) {
		t.Fatalf("ожидалась context.Canceled, получена: %v", err)
	}
}

// isContextCanceled проверяет, что ошибка является context.Canceled (напрямую).
func isContextCanceled(err error) bool {
	return err == context.Canceled
}
