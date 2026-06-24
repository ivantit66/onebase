// internal/llm/gemini_tools.go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// completeGeminiTools — агентный цикл function-calling по Gemini generateContent.
// Конец цикла — ответ без functionCall в parts. Ответы функций уходят одним
// сообщением role=user с частями functionResponse.
func completeGeminiTools(ctx context.Context, hc *http.Client, rm ResolvedModel, req ChatRequest, tools []Tool, exec ToolExecutor, maxRounds int) (ChatResponse, error) {
	base := rm.Endpoint.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", strings.TrimRight(base, "/"), rm.Model.Name)
	headers := map[string]string{"x-goog-api-key": rm.Endpoint.APIKey}

	decls := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		schema := t.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		decls = append(decls, map[string]any{"name": t.Name, "description": t.Description, "parameters": schema})
	}
	toolsBody := []map[string]any{{"functionDeclarations": decls}}

	contents := make([]map[string]any, 0, len(req.Messages)+4)
	for _, m := range req.Messages {
		var sb strings.Builder
		for _, p := range m.Parts {
			if !p.isImage() {
				sb.WriteString(p.Text)
			}
		}
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": []map[string]any{{"text": sb.String()}}})
	}

	var totalIn, totalOut int
	for iter := 0; iter < maxRounds; iter++ {
		body := map[string]any{
			"contents":         contents,
			"tools":            toolsBody,
			"generationConfig": map[string]any{"maxOutputTokens": maxTokens(rm.Model, req)},
		}
		if req.System != "" {
			body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": req.System}}}
		}
		data, err := postJSON(ctx, hc, "gemini", endpoint, body, headers, rm.Endpoint.Headers)
		if err != nil {
			return ChatResponse{}, err
		}
		var out struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text         string `json:"text"`
						FunctionCall *struct {
							Name string         `json:"name"`
							Args map[string]any `json:"args"`
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return ChatResponse{}, fmt.Errorf("gemini: разбор ответа: %w", err)
		}
		totalIn += out.UsageMetadata.PromptTokenCount
		totalOut += out.UsageMetadata.CandidatesTokenCount
		if len(out.Candidates) == 0 {
			return ChatResponse{}, fmt.Errorf("gemini: пустой ответ (нет candidates)")
		}

		var text strings.Builder
		var calls []ToolCall
		modelParts := make([]map[string]any, 0)
		for _, p := range out.Candidates[0].Content.Parts {
			if p.FunctionCall != nil {
				calls = append(calls, ToolCall{Name: p.FunctionCall.Name, Input: p.FunctionCall.Args})
				modelParts = append(modelParts, map[string]any{"functionCall": map[string]any{"name": p.FunctionCall.Name, "args": p.FunctionCall.Args}})
			} else if p.Text != "" {
				text.WriteString(p.Text)
				modelParts = append(modelParts, map[string]any{"text": p.Text})
			}
		}
		if len(calls) == 0 {
			return ChatResponse{Text: text.String(), Model: rm.Model.Name, InputTokens: totalIn, OutputTokens: totalOut}, nil
		}

		contents = append(contents, map[string]any{"role": "model", "parts": modelParts})
		respParts := make([]map[string]any, 0, len(calls))
		for _, c := range calls {
			res := exec(ctx, c)
			respParts = append(respParts, map[string]any{
				"functionResponse": map[string]any{"name": c.Name, "response": map[string]any{"result": res.Content}},
			})
		}
		contents = append(contents, map[string]any{"role": "user", "parts": respParts})
	}
	return ChatResponse{}, fmt.Errorf("gemini: превышен лимит раундов инструментов (%d)", maxRounds)
}
