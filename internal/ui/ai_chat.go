package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/llm"
	"github.com/ivantit66/onebase/internal/storage"
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
	"базы — честно скажи об этом и подскажи, какой отчёт или обработку посмотреть. " +
	"Инструменты «создать_документ» и «создать_элемент_справочника» лишь готовят черновик: " +
	"запись выполняется только после того, как пользователь подтвердит действие кнопкой в чате. " +
	"Перед подготовкой создания выясни структуру через «описание_данных», а UUID ссылочных " +
	"значений находи через «выполнить_запрос». Проведение документов тебе недоступно. " +
	"Инструментом «открыть_форму» давай пользователю кнопку перехода к списку, записи, отчёту или обработке."

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

	// Лимит частоты (план 54): один запрос чата — до 12 раундов tool-use ×
	// вызовы LLM; без лимита это вектор cost-DoS.
	user := auth.UserFromContext(r.Context())
	limitKey := "anon"
	if user != nil {
		limitKey = user.ID
	}
	if !s.aiChatLimit.Allow(limitKey) {
		writeJSON(w, http.StatusTooManyRequests,
			map[string]any{"error": "Слишком много запросов к ИИ — подождите минуту."})
		return
	}

	// Суточный потолок токенов (план 54): ai.daily_token_cap в _settings,
	// 0 = без лимита. Расход считается по журналу _ai_audit.
	if cap := s.store.GetAIDailyTokenCap(r.Context()); cap > 0 {
		now := time.Now()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if used, err := s.store.AITokensUsedSince(r.Context(), midnight); err == nil && used >= cap {
			writeJSON(w, http.StatusOK,
				map[string]any{"error": "Суточный лимит токенов ИИ исчерпан — попробуйте завтра или увеличьте ai.daily_token_cap."})
			return
		}
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
	tools, exec, pending := s.aiTools(r)
	resp, err := runner.RunWithTools(r.Context(), "чат", chatReq, tools, exec)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": llm.SafeErr(err)})
		return
	}

	// Журнал ИИ (план 54): кто, какая модель, сколько токенов. Текст диалога
	// намеренно не пишем (приватность) — запросы инструментов журналирует
	// aiRunQuery отдельными записями.
	entry := storage.AIAuditEntry{Task: "чат", Model: resp.Model,
		InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens}
	if user != nil {
		entry.UserID, entry.UserLogin = user.ID, user.Login
	}
	s.store.LogAIQuery(r.Context(), entry)

	payload := map[string]any{"ok": true, "text": resp.Text, "model": resp.Model}
	// Отложенные действия (план 51): черновики/команды, подготовленные
	// инструментами в этом раунде, — клиент нарисует карточки с подтверждением.
	if pending != nil && len(pending.Actions) > 0 {
		payload["actions"] = pending.Actions
	}
	writeJSON(w, http.StatusOK, payload)
}
