package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestGeminiKeyNotInTransportError проверяет, что при сетевом сбое API-ключ
// не просачивается в строку ошибки (порт 1 — соединение всегда отклоняется).
func TestGeminiKeyNotInTransportError(t *testing.T) {
	const secretKey = "SECRET_GEMINI_KEY_12345"
	rm := ResolvedModel{
		Endpoint: Endpoint{
			Kind:    KindGemini,
			BaseURL: "http://127.0.0.1:1", // порт 1 — connection refused
			APIKey:  secretKey,
		},
		Model: Model{Name: "gemini-test"},
	}
	req := ChatRequest{
		Messages: []Message{UserText("проверка")},
	}
	hc := &http.Client{Timeout: 2 * time.Second}
	_, err := completeGemini(context.Background(), hc, rm, req)
	if err == nil {
		t.Fatal("ожидалась ошибка при недоступном сервере")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("API-ключ утёк в строку ошибки: %s", err.Error())
	}
}

// TestSafeErrRedactsSecrets проверяет, что SafeErr маскирует секреты в query-параметрах.
func TestSafeErrRedactsSecrets(t *testing.T) {
	raw := "Get \"https://x/y?key=SUPERSECRET&z=1\": connection refused"
	got := SafeErr(errors.New(raw))
	if strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("SafeErr не замаскировал секрет: %s", got)
	}
	if !strings.Contains(got, "key=REDACTED") {
		t.Fatalf("SafeErr не содержит key=REDACTED: %s", got)
	}
}

// TestSafeErrNil проверяет, что SafeErr(nil) возвращает пустую строку без паники.
func TestSafeErrNil(t *testing.T) {
	if s := SafeErr(nil); s != "" {
		t.Fatalf("SafeErr(nil) вернул %q, ожидалась пустая строка", s)
	}
}
