package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// completeAnthropicTools — агентный цикл tool-use по Anthropic Messages API
// (покрывает Anthropic и GLM через z.ai). Модель может несколько раз запрашивать
// инструменты; exec их исполняет, результаты возвращаются модели, пока она не
// выдаст финальный текст (или не исчерпается лимит итераций).
func completeAnthropicTools(ctx context.Context, hc *http.Client, rm ResolvedModel, req ChatRequest, tools []Tool, exec ToolExecutor, maxRounds int) (ChatResponse, error) {
	base := rm.Endpoint.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	url := strings.TrimRight(base, "/") + "/v1/messages"

	headers := map[string]string{
		"x-api-key":         rm.Endpoint.APIKey,
		"anthropic-version": "2023-06-01",
	}
	toolDefs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		schema := t.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		toolDefs = append(toolDefs, map[string]any{
			"name": t.Name, "description": t.Description, "input_schema": schema,
		})
	}

	// messages — сырые блоки контента, накапливаются между раундами.
	messages := make([]map[string]any, 0, len(req.Messages)+4)
	for _, m := range req.Messages {
		var sb strings.Builder
		for _, p := range m.Parts {
			if !p.isImage() {
				sb.WriteString(p.Text)
			}
		}
		messages = append(messages, map[string]any{
			"role":    string(m.Role),
			"content": []map[string]any{{"type": "text", "text": sb.String()}},
		})
	}

	var totalIn, totalOut int
	for iter := 0; iter < maxRounds; iter++ {
		body := map[string]any{
			"model":      rm.Model.Name,
			"max_tokens": maxTokens(rm.Model, req),
			"messages":   messages,
			"tools":      toolDefs,
		}
		if sys := anthropicSystem(req); sys != "" {
			body["system"] = sys
		}
		// temperature не отправляем по Anthropic-протоколу (Opus 4.7/4.8 → 400).

		data, err := postJSON(ctx, hc, "anthropic", url, body, headers, rm.Endpoint.Headers)
		if err != nil {
			return ChatResponse{}, err
		}

		var out struct {
			Content    []json.RawMessage `json:"content"`
			StopReason string            `json:"stop_reason"`
			Usage      struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return ChatResponse{}, fmt.Errorf("anthropic: разбор ответа: %w", err)
		}
		totalIn += out.Usage.InputTokens
		totalOut += out.Usage.OutputTokens

		// Разбираем блоки: текст и запросы инструментов.
		var text strings.Builder
		var calls []ToolCall
		for _, raw := range out.Content {
			var blk struct {
				Type  string         `json:"type"`
				Text  string         `json:"text"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			}
			if err := json.Unmarshal(raw, &blk); err != nil {
				continue
			}
			switch blk.Type {
			case "text":
				text.WriteString(blk.Text)
			case "tool_use":
				calls = append(calls, ToolCall{ID: blk.ID, Name: blk.Name, Input: blk.Input})
			}
		}

		if out.StopReason != "tool_use" || len(calls) == 0 {
			return ChatResponse{Text: text.String(), Model: rm.Model.Name, InputTokens: totalIn, OutputTokens: totalOut}, nil
		}

		// Ассистентский ход возвращаем модели как есть (сырые блоки).
		messages = append(messages, map[string]any{"role": "assistant", "content": out.Content})

		// Исполняем инструменты и формируем tool_result.
		results := make([]map[string]any, 0, len(calls))
		for _, c := range calls {
			res := exec(ctx, c)
			block := map[string]any{"type": "tool_result", "tool_use_id": c.ID, "content": res.Content}
			if res.IsError {
				block["is_error"] = true
			}
			results = append(results, block)
		}
		messages = append(messages, map[string]any{"role": "user", "content": results})
	}
	return ChatResponse{}, fmt.Errorf("anthropic: превышен лимит раундов инструментов (%d)", maxRounds)
}
