package llm

import (
	"fmt"
	"regexp"
)

// Role — роль сообщения в диалоге.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Part — фрагмент сообщения: либо текст, либо изображение (для vision).
// Заполняется ровно одно из: Text или (ImageB64 + MimeType).
type Part struct {
	Text     string
	ImageB64 string // base64 без префикса data:
	MimeType string // image/png, image/jpeg, application/pdf
}

// TextPart — удобный конструктор текстовой части.
func TextPart(s string) Part { return Part{Text: s} }

// ImagePart — удобный конструктор части-изображения.
func ImagePart(b64, mime string) Part { return Part{ImageB64: b64, MimeType: mime} }

func (p Part) isImage() bool { return p.ImageB64 != "" }

// Message — одно сообщение диалога (может содержать текст и картинки вместе).
type Message struct {
	Role  Role
	Parts []Part
}

// UserText — сообщение пользователя из одного текста.
func UserText(s string) Message {
	return Message{Role: RoleUser, Parts: []Part{TextPart(s)}}
}

// ChatRequest — запрос к модели. System выносится отдельно (так требуют и
// Anthropic, и Gemini); JSON просит провайдера вернуть строгий JSON.
type ChatRequest struct {
	System      string
	Messages    []Message
	Temperature float64
	MaxTokens   int  // 0 → берётся из Model.MaxTokens или DefaultMaxTokens
	JSON        bool // запросить ответ в формате JSON
}

// HasImages сообщает, нужен ли vision (есть ли хоть одна картинка в запросе).
func (r ChatRequest) HasImages() bool {
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			if p.isImage() {
				return true
			}
		}
	}
	return false
}

// ChatResponse — ответ модели.
type ChatResponse struct {
	Text         string
	Model        string // какая модель фактически ответила (важно при фолбэке)
	InputTokens  int
	OutputTokens int
}

// APIError — ошибка вызова провайдера с HTTP-статусом. Движок по нему решает,
// ретраить ли на следующей модели.
type APIError struct {
	StatusCode int
	Provider   string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm: %s вернул HTTP %d: %s", e.Provider, e.StatusCode, truncate(e.Body, 300))
}

// retryable — стоит ли пробовать следующую модель цепочки. Ретраим лимиты (429),
// перегрузку/временные сбои (5xx) и сетевые ошибки; ошибки запроса (4xx, кроме
// 429) — это вина конфигурации/промпта, фолбэк не поможет.
func (e *APIError) retryable() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var secretQueryRe = regexp.MustCompile(`([?&](?:key|api_key|apikey|access_token|token)=)[^&\s"']+`)

// SafeErr возвращает текст ошибки с замаскированными секретами в query-параметрах URL.
// Защита второго уровня на случай, если провайдер или base_url содержит ключ в URL,
// а *url.Error встраивает полный URL в строку ошибки.
func SafeErr(err error) string {
	if err == nil {
		return ""
	}
	return secretQueryRe.ReplaceAllString(err.Error(), "${1}REDACTED")
}
