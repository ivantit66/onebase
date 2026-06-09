package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// completeGemini вызывает Google Generative Language API (generateContent).
// Поддерживает мультимодальный ввод (inlineData) — основа распознавания документов.
func completeGemini(ctx context.Context, hc *http.Client, rm ResolvedModel, req ChatRequest) (ChatResponse, error) {
	base := rm.Endpoint.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	endpoint := fmt.Sprintf("%s/models/%s:generateContent",
		strings.TrimRight(base, "/"), rm.Model.Name)

	type part struct {
		Text       string         `json:"text,omitempty"`
		InlineData map[string]any `json:"inlineData,omitempty"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}

	contents := make([]content, 0, len(req.Messages))
	for _, m := range req.Messages {
		// Gemini различает только роли user/model.
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		parts := make([]part, 0, len(m.Parts))
		for _, p := range m.Parts {
			if p.isImage() {
				parts = append(parts, part{InlineData: map[string]any{
					"mimeType": p.MimeType, "data": p.ImageB64,
				}})
			} else {
				parts = append(parts, part{Text: p.Text})
			}
		}
		contents = append(contents, content{Role: role, Parts: parts})
	}

	genCfg := map[string]any{"maxOutputTokens": maxTokens(rm.Model, req)}
	if req.Temperature > 0 {
		genCfg["temperature"] = req.Temperature
	}
	if req.JSON {
		genCfg["responseMimeType"] = "application/json"
	}
	body := map[string]any{
		"contents":         contents,
		"generationConfig": genCfg,
	}
	if req.System != "" {
		body["systemInstruction"] = map[string]any{"parts": []part{{Text: req.System}}}
	}

	data, err := postJSON(ctx, hc, "gemini", endpoint, body, map[string]string{"x-goog-api-key": rm.Endpoint.APIKey}, rm.Endpoint.Headers)
	if err != nil {
		return ChatResponse{}, err
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
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
	var sb strings.Builder
	if len(out.Candidates) > 0 {
		for _, p := range out.Candidates[0].Content.Parts {
			sb.WriteString(p.Text)
		}
	}
	return ChatResponse{
		Text:         sb.String(),
		Model:        rm.Model.Name,
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
	}, nil
}
