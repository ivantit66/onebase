package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Logf — необязательный логгер прогресса фолбэка (какая модель пробуется/ответила).
type Logf func(format string, args ...any)

// Runner исполняет запросы согласно конфигу: резолвит цепочку моделей задачи и
// идёт по ней с фолбэком при лимитах/временных ошибках.
type Runner struct {
	cfg  Config
	logf Logf
}

// New создаёт Runner поверх конфига. logf может быть nil.
func New(cfg Config, logf Logf) *Runner {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Runner{cfg: cfg, logf: logf}
}

// Config возвращает конфиг (read-only нужды UI/диагностики).
func (r *Runner) Config() Config { return r.cfg }

// Run выполняет запрос для задачи task, перебирая модели профиля с фолбэком.
// Если запрос содержит изображения, не-vision модели в цепочке пропускаются.
func (r *Runner) Run(ctx context.Context, task string, req ChatRequest) (ChatResponse, error) {
	chain, err := r.cfg.Resolve(task)
	if err != nil {
		return ChatResponse{}, err
	}
	needVision := req.HasImages()

	var lastErr error
	tried := 0
	for _, rm := range chain {
		if needVision && !rm.Model.Vision {
			continue
		}
		tried++
		r.logf("llm: задача %q → пробую модель %s (%s)", task, rm.Model.Name, rm.Endpoint.Kind)
		resp, err := complete(ctx, rm, req)
		if err == nil {
			r.logf("llm: ответила модель %s", rm.Model.Name)
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil { // вызывающий отменил/истёк дедлайн — не перебираем цепочку
			return ChatResponse{}, ctx.Err()
		}
		if !shouldFallback(err) {
			return ChatResponse{}, err
		}
		r.logf("llm: модель %s недоступна (%v) — фолбэк на следующую", rm.Model.Name, err)
	}
	if tried == 0 {
		if needVision {
			return ChatResponse{}, fmt.Errorf("задача %q: в цепочке нет vision-модели для распознавания изображений", task)
		}
		return ChatResponse{}, fmt.Errorf("задача %q: нет доступных моделей", task)
	}
	return ChatResponse{}, fmt.Errorf("задача %q: все модели исчерпаны: %w", task, lastErr)
}

// RunWithTools выполняет запрос с доступными инструментами (tool-use). Цикл
// реализован для всех протоколов (anthropic/openai/compatible/gemini) через
// completeTools с фолбэком по цепочке профиля. Если tools пуст — деградирует до
// обычного Run; если все модели цепочки без ключа — тоже Run (ответ без данных).
func (r *Runner) RunWithTools(ctx context.Context, task string, req ChatRequest, tools []Tool, exec ToolExecutor) (ChatResponse, error) {
	if len(tools) == 0 {
		return r.Run(ctx, task, req)
	}
	chain, err := r.cfg.Resolve(task)
	if err != nil {
		return ChatResponse{}, err
	}
	var lastErr error
	tried := 0
	for _, rm := range chain {
		if rm.Endpoint.APIKey == "" {
			lastErr = fmt.Errorf("endpoint %q: не задан API-ключ", rm.Endpoint.Name)
			continue
		}
		tried++
		r.logf("llm: задача %q (tools) → модель %s (%s)", task, rm.Model.Name, rm.Endpoint.Kind)
		resp, err := completeTools(ctx, httpClient(rm.Endpoint), rm, req, tools, exec, toolRoundLimit(r.cfg))
		if err == nil {
			r.logf("llm: ответила модель %s (tools)", rm.Model.Name)
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil { // вызывающий отменил/истёк дедлайн — не перебираем цепочку
			return ChatResponse{}, ctx.Err()
		}
		if !shouldFallback(err) {
			return ChatResponse{}, err
		}
		r.logf("llm: модель %s недоступна (%v) — фолбэк", rm.Model.Name, err)
	}
	if tried == 0 {
		// Нет моделей с ключом — отвечаем без инструментов.
		r.logf("llm: задача %q — нет доступных моделей для tool-use, ответ без инструментов", task)
		return r.Run(ctx, task, req)
	}
	return ChatResponse{}, fmt.Errorf("задача %q (tools): все модели исчерпаны: %w", task, lastErr)
}

// shouldFallback классифицирует ошибку: ретраить ли на следующей модели.
func shouldFallback(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.retryable()
	}
	// Сетевые ошибки/таймауты — пробуем следующую модель.
	return true
}

// complete диспетчеризует вызов по типу endpoint'а.
func complete(ctx context.Context, rm ResolvedModel, req ChatRequest) (ChatResponse, error) {
	if rm.Endpoint.APIKey == "" {
		return ChatResponse{}, fmt.Errorf("endpoint %q: не задан API-ключ", rm.Endpoint.Name)
	}
	hc := httpClient(rm.Endpoint)
	switch rm.Endpoint.Kind {
	case KindAnthropic:
		return completeAnthropic(ctx, hc, rm, req)
	case KindGemini:
		return completeGemini(ctx, hc, rm, req)
	case KindOpenAI, KindCompatible:
		return completeOpenAI(ctx, hc, rm, req)
	default:
		return ChatResponse{}, fmt.Errorf("endpoint %q: неизвестный тип провайдера %q", rm.Endpoint.Name, rm.Endpoint.Kind)
	}
}

// completeTools диспетчеризует tool-use по типу endpoint'а. Поддерживаются все
// протоколы; неизвестный тип → ошибка (модель в RunWithTools пропускается фолбэком).
func completeTools(ctx context.Context, hc *http.Client, rm ResolvedModel, req ChatRequest, tools []Tool, exec ToolExecutor, maxRounds int) (ChatResponse, error) {
	switch rm.Endpoint.Kind {
	case KindAnthropic:
		return completeAnthropicTools(ctx, hc, rm, req, tools, exec, maxRounds)
	case KindOpenAI, KindCompatible:
		return completeOpenAITools(ctx, hc, rm, req, tools, exec, maxRounds)
	case KindGemini:
		return completeGeminiTools(ctx, hc, rm, req, tools, exec, maxRounds)
	default:
		return ChatResponse{}, fmt.Errorf("endpoint %q: тип %q не поддерживает tool-use", rm.Endpoint.Name, rm.Endpoint.Kind)
	}
}

func toolRoundLimit(cfg Config) int {
	if cfg.MaxToolRounds <= 0 {
		return MaxToolIterations
	}
	if cfg.MaxToolRounds > 64 {
		return 64
	}
	return cfg.MaxToolRounds
}

func httpClient(ep Endpoint) *http.Client {
	sec := ep.TimeoutSec
	if sec <= 0 {
		sec = DefaultTimeoutSec
	}
	return &http.Client{Timeout: time.Duration(sec) * time.Second}
}

func maxTokens(m Model, req ChatRequest) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	if m.MaxTokens > 0 {
		return m.MaxTokens
	}
	return DefaultMaxTokens
}
