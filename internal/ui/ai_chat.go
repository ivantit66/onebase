package ui

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ivantit66/onebase/internal/llm"
)

// aiChatSystemPrompt — роль ассистента. На фазе F3 у чата ещё нет прямого доступа
// к данным базы (это F4, tool-use): он отвечает на общие вопросы и рассуждает по
// тексту, который пользователь приводит сам.
const aiChatSystemPrompt = "Ты — встроенный ИИ-помощник учётной системы OneBase. " +
	"Отвечай по-русски, кратко и по делу. " +
	"Если тебе доступны инструменты запроса данных — пользуйся ими, чтобы отвечать на " +
	"вопросы по фактическим данным: сначала вызови «описание_данных», чтобы узнать " +
	"объекты и поля, затем «выполнить_запрос». Числа бери только из результатов " +
	"инструментов, не выдумывай. Если инструментов нет, а для ответа нужны данные из " +
	"базы — честно скажи об этом и подскажи, какой отчёт или обработку посмотреть."

// aiEnabled сообщает клиенту, показывать ли кнопку чата (помощник настроен).
func (s *Server) aiEnabled(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetLLMConfig(r.Context())
	enabled := err == nil && cfg.Enabled && len(cfg.Models) > 0
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
}

// aiChat принимает историю переписки и возвращает ответ модели по профилю "чат".
func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Некорректный запрос: " + err.Error()})
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Пустой запрос"})
		return
	}

	cfg, err := s.store.GetLLMConfig(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": "Конфиг ИИ повреждён: " + err.Error()})
		return
	}

	msgs := make([]llm.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		role := llm.RoleUser
		if m.Role == "assistant" {
			role = llm.RoleAssistant
		}
		msgs = append(msgs, llm.Message{Role: role, Parts: []llm.Part{llm.TextPart(m.Content)}})
	}

	runner := llm.New(cfg, nil)
	chatReq := llm.ChatRequest{System: aiChatSystemPrompt, Messages: msgs}
	// Tool-use (F4): администратору доступны инструменты чтения данных, чтобы ИИ
	// сам выполнял запросы. Остальным — обычный ответ без доступа к данным.
	tools, exec := s.aiTools(r)
	resp, err := runner.RunWithTools(r.Context(), "чат", chatReq, tools, exec)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": llm.SafeErr(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "text": resp.Text, "model": resp.Model})
}
