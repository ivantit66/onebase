package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	cronlib "github.com/robfig/cron/v3"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/mailer"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

type Scheduler struct {
	cron    *cronlib.Cron
	jobs    []*metadata.ScheduledJob
	db      *storage.DB
	reg     *runtime.Registry
	interp  *interpreter.Interpreter
	log     *slog.Logger
	mailer  *mailer.Mailer
	msgSink func(userID, text string)
}

// SetMessageSink hooks Сообщить() output into an external store (e.g. UI message panel).
// userID is empty string for scheduler context (anonymous/system).
func (s *Scheduler) SetMessageSink(f func(userID, text string)) { s.msgSink = f }

func New(db *storage.DB, reg *runtime.Registry, interp *interpreter.Interpreter) *Scheduler {
	return &Scheduler{
		cron:   cronlib.New(),
		db:     db,
		reg:    reg,
		interp: interp,
		log:    slog.Default(),
	}
}

func (s *Scheduler) SetMailer(m *mailer.Mailer) {
	s.mailer = m
}

// RegisterGoJob добавляет нативное Go-задание в планировщик.
// Результат записывается в _scheduled_runs как обычное задание.
func (s *Scheduler) RegisterGoJob(name, title, schedule string, fn func(ctx context.Context) error) error {
	_, err := s.cron.AddFunc(schedule, func() {
		ctx := context.Background()
		start := time.Now()
		runID, _ := s.db.InsertScheduledRun(ctx, name, start)
		runErr := fn(ctx)
		elapsed := time.Since(start).Milliseconds()
		status, errStr := "success", ""
		if runErr != nil {
			status = "error"
			errStr = runErr.Error()
			s.log.Error("go job failed", "job", name, "err", runErr)
		} else {
			s.log.Info("go job done", "job", name, "ms", elapsed)
		}
		s.db.UpdateScheduledRun(ctx, runID, status, "", errStr, elapsed)
	})
	if err != nil {
		return fmt.Errorf("scheduler: RegisterGoJob %s: %w", name, err)
	}
	s.jobs = append(s.jobs, &metadata.ScheduledJob{
		Name:     name,
		Title:    title,
		Schedule: schedule,
		Enabled:  true,
	})
	return nil
}

func (s *Scheduler) LoadJobs(jobs []*metadata.ScheduledJob) error {
	s.jobs = jobs
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		j := job // capture
		_, err := s.cron.AddFunc(j.Schedule, func() {
			ctx := context.Background()
			if j.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(j.Timeout)*time.Second)
				defer cancel()
			}
			s.executeJob(ctx, j)
		})
		if err != nil {
			return fmt.Errorf("scheduler: invalid schedule for %s: %w", job.Name, err)
		}
	}
	return nil
}

// Reload stops the current cron, replaces jobs, and restarts it.
func (s *Scheduler) Reload(jobs []*metadata.ScheduledJob) error {
	s.cron.Stop()
	s.cron = cronlib.New()
	return s.LoadJobs(jobs)
}

func (s *Scheduler) Start(ctx context.Context) {
	s.cron.Start()
	<-ctx.Done()
	s.cron.Stop()
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}

func (s *Scheduler) Jobs() []*metadata.ScheduledJob {
	return s.jobs
}

func (s *Scheduler) GetJob(name string) *metadata.ScheduledJob {
	nl := strings.ToLower(name)
	for _, j := range s.jobs {
		if strings.ToLower(j.Name) == nl {
			return j
		}
	}
	return nil
}

func (s *Scheduler) RunNow(ctx context.Context, jobName string) error {
	job := s.GetJob(jobName)
	if job == nil {
		return fmt.Errorf("job not found: %s", jobName)
	}
	// Use background context: request context will be cancelled after redirect
	go s.executeJob(context.Background(), job)
	return nil
}

func (s *Scheduler) Runs(ctx context.Context, jobName string, limit int) ([]storage.ScheduledRun, error) {
	return s.db.ScheduledRuns(ctx, jobName, limit)
}

func (s *Scheduler) executeJob(ctx context.Context, job *metadata.ScheduledJob) {
	startedAt := time.Now()
	runID, err := s.db.InsertScheduledRun(ctx, job.Name, startedAt)
	if err != nil {
		s.log.Error("scheduler: insert run", "job", job.Name, "err", err)
		return
	}

	output, runErr := s.runProcessor(ctx, job)

	status := "success"
	errText := ""
	if runErr != nil {
		status = "error"
		errText = runErr.Error()
	}
	if ctx.Err() == context.DeadlineExceeded {
		status = "timeout"
		errText = "timeout exceeded"
	}

	durationMs := time.Since(startedAt).Milliseconds()
	if err := s.db.UpdateScheduledRun(ctx, runID, status, output, errText, durationMs); err != nil {
		// Use background ctx in case the original was cancelled
		bgCtx := context.Background()
		_ = s.db.UpdateScheduledRun(bgCtx, runID, status, output, errText, durationMs)
	}

	s.log.Info("scheduler: job finished", "job", job.Name, "status", status, "duration_ms", durationMs)
}

func (s *Scheduler) runProcessor(ctx context.Context, job *metadata.ScheduledJob) (output string, runErr error) {
	proc := s.reg.GetProcessor(job.Processor)
	if proc == nil {
		return "", fmt.Errorf("processor not found: %s", job.Processor)
	}

	procDecl := s.reg.GetProcedure(proc.Name, "Выполнить")
	if procDecl == nil {
		return "", fmt.Errorf("procedure Выполнить not found in processor %s", proc.Name)
	}

	resolvedParams := resolveParamTemplates(job.Params)

	var messages []string
	msgFunc := interpreter.BuiltinFunc(func(args []any, file string, line int) (any, error) {
		if len(args) > 0 {
			text := fmt.Sprintf("%v", args[0])
			messages = append(messages, text)
			if s.msgSink != nil {
				s.msgSink("", text)
			}
		}
		return nil, nil
	})

	paramValues := make(map[string]any)
	for _, p := range proc.Params {
		if v, ok := resolvedParams[p.Name]; ok {
			paramValues[p.Name] = v
		}
	}

	paramsThis := &interpreter.MapThis{M: paramValues}
	mc := runtime.NewMovementsCollector("scheduler", uuid.Nil)
	dslVars := s.buildDSLVars(ctx, mc)
	dslVars["Параметры"] = paramsThis
	dslVars["Сообщить"] = msgFunc
	dslVars["Message"] = msgFunc

	err := s.interp.Run(procDecl, paramsThis, dslVars)
	output = strings.Join(messages, "\n")
	return output, err
}

func (s *Scheduler) buildDSLVars(ctx context.Context, mc *runtime.MovementsCollector) map[string]any {
	enumsMap := make(map[string]any)
	for _, e := range s.reg.Enums() {
		inner := make(map[string]any, len(e.Values))
		for _, v := range e.Values {
			inner[v] = v
		}
		enumsMap[e.Name] = &interpreter.MapThis{M: inner}
	}
	constsMap := make(map[string]any)
	if vals, err := s.db.ListConstants(ctx); err == nil {
		constsMap = vals
	}
	queryFactory := interpreter.NewQueryFactory(ctx, s.db, s.reg)
	predefined := interpreter.NewPredefinedRoot(ctx, s.db)
	vars := map[string]any{
		"Движения":                  mc,
		"Перечисления":              &interpreter.MapThis{M: enumsMap},
		"Константы":                 &interpreter.MapThis{M: constsMap},
		"__factory_Запрос":          queryFactory,
		"__factory_Query":           queryFactory,
		"ПредопределённыеЗначения": predefined,
		"PredefinedValues":          predefined,
	}
	for k, v := range interpreter.NewHTTPFunctions() {
		vars[k] = v
	}
	for k, v := range interpreter.NewEmailFunctions(s.mailer) {
		vars[k] = v
	}
	return vars
}

// resolveParamTemplates replaces template expressions like {{today}} with actual values.
func resolveParamTemplates(params map[string]any) map[string]any {
	return resolveParamTemplatesAt(params, time.Now())
}

// ResolveParamTemplates is the exported entry point used by other subsystems
// (widgets, ad-hoc query callers) that need the same {{today|...}} grammar
// as scheduled jobs.
func ResolveParamTemplates(params map[string]any) map[string]any {
	return resolveParamTemplatesAt(params, time.Now())
}

func resolveParamTemplatesAt(params map[string]any, now time.Time) map[string]any {
	if len(params) == 0 {
		return params
	}
	result := make(map[string]any, len(params))
	for k, v := range params {
		if s, ok := v.(string); ok {
			result[k] = resolveTemplate(s, now)
		} else {
			result[k] = v
		}
	}
	return result
}

func resolveTemplate(s string, now time.Time) any {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") {
		return s
	}
	expr := strings.TrimSpace(s[2 : len(s)-2])
	parts := strings.SplitN(expr, "|", 2)
	base := strings.TrimSpace(parts[0])

	var t time.Time
	switch base {
	case "now":
		t = now
	case "today":
		t = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	default:
		return s
	}

	if len(parts) == 1 {
		return t
	}

	transform := strings.TrimSpace(parts[1])
	tparts := strings.SplitN(transform, ":", 2)
	op := strings.TrimSpace(tparts[0])
	var n int
	if len(tparts) == 2 {
		fmt.Sscanf(strings.TrimSpace(tparts[1]), "%d", &n)
	}

	switch op {
	case "minus_days":
		return t.AddDate(0, 0, -n)
	case "minus_hours":
		return t.Add(-time.Duration(n) * time.Hour)
	case "minus_months":
		return t.AddDate(0, -n, 0)
	case "start_of_month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	case "end_of_month":
		return time.Date(t.Year(), t.Month()+1, 0, 23, 59, 59, 0, t.Location())
	}
	return t
}
