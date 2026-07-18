package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	cronlib "github.com/robfig/cron/v3"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dslvars"
	oblog "github.com/ivantit66/onebase/internal/logging"
	"github.com/ivantit66/onebase/internal/mailer"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// ErrNetworkLocked — отказ предохранителя сети (план 62) для DSL заданий.
var ErrNetworkLocked = errors.New("сетевые возможности отключены предохранителем — включите «Разрешить сетевые операции» в конфигураторе")

type Scheduler struct {
	cron    *cronlib.Cron
	jobs    []*metadata.ScheduledJob
	db      *storage.DB
	reg     *runtime.Registry
	interp  *interpreter.Interpreter
	log     *slog.Logger
	mailer  *mailer.Mailer
	msgSink func(userID, text string)
	// varsBuilder — внешний сборщик полного DSL-окружения заданий (обычно
	// ui.Server.BuildJobDSLVars): даёт заданиям Справочники/Документы/вложения/
	// транзакции наравне с обработками. nil → базовый набор dslvars.Common.
	varsBuilder VarsBuilder

	mu         sync.Mutex
	running    bool
	stopping   bool
	rootCtx    context.Context
	rootCancel context.CancelFunc
	wg         sync.WaitGroup
	activeRuns map[uuid.UUID]*activeRun
}

const (
	defaultShutdownTimeout = 30 * time.Second
	interruptUpdateTimeout = 5 * time.Second

	runStatusSuccess     = "success"
	runStatusError       = "error"
	runStatusTimeout     = "timeout"
	runStatusInterrupted = "interrupted"
)

type activeRun struct {
	id        uuid.UUID
	jobName   string
	startedAt time.Time
	finalized bool
}

// SetMessageSink hooks Сообщить() output into an external store (e.g. UI message panel).
// userID is empty string for scheduler context (anonymous/system).
func (s *Scheduler) SetMessageSink(f func(userID, text string)) { s.msgSink = f }

// VarsBuilder строит DSL-окружение для запуска обработки задания.
type VarsBuilder func(ctx context.Context, mc *runtime.MovementsCollector) map[string]any

// SetVarsBuilder подключает внешний сборщик DSL-окружения (см. поле varsBuilder).
func (s *Scheduler) SetVarsBuilder(b VarsBuilder) { s.varsBuilder = b }

func New(db *storage.DB, reg *runtime.Registry, interp *interpreter.Interpreter) *Scheduler {
	return &Scheduler{
		cron:   cronlib.New(),
		db:     db,
		reg:    reg,
		interp: interp,
		log:    oblog.Component("scheduler"),
	}
}

func (s *Scheduler) SetMailer(m *mailer.Mailer) {
	s.mailer = m
}

// RegisterGoJob добавляет нативное Go-задание в планировщик.
// Результат записывается в _scheduled_runs как обычное задание.
func (s *Scheduler) RegisterGoJob(name, title, schedule string, fn func(ctx context.Context) error) error {
	_, err := s.cron.AddFunc(schedule, func() {
		ctx, done, ok := s.beginJob()
		if !ok {
			return
		}
		defer done()
		s.executeGoJob(ctx, name, fn)
	})
	if err != nil {
		return fmt.Errorf("scheduler: RegisterGoJob %s: %w", name, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, &metadata.ScheduledJob{
		Name:     name,
		Title:    title,
		Schedule: schedule,
		Enabled:  true,
	})
	return nil
}

func (s *Scheduler) LoadJobs(jobs []*metadata.ScheduledJob) error {
	s.mu.Lock()
	s.jobs = jobs
	s.mu.Unlock()
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		j := job // capture
		_, err := s.cron.AddFunc(j.Schedule, func() {
			ctx, done, ok := s.beginJob()
			if !ok {
				return
			}
			defer done()
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
	s.mu.Lock()
	oldCron := s.cron
	s.cron = cronlib.New()
	running := s.running
	s.mu.Unlock()

	oldCron.Stop()
	if err := s.LoadJobs(jobs); err != nil {
		return err
	}
	if running {
		s.cron.Start()
	}
	return nil
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.ensureRootLocked()
	s.stopping = false
	s.running = true
	cron := s.cron
	s.mu.Unlock()

	cron.Start()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		s.log.Warn("scheduler: shutdown timed out", "err", err)
	}
}

func (s *Scheduler) Stop() {
	if err := s.Shutdown(context.Background()); err != nil {
		s.log.Warn("scheduler: stop failed", "err", err)
	}
}

// Shutdown stops cron triggers and waits until already-started jobs finish. If
// ctx expires first, all scheduler job contexts are cancelled and active runs
// are marked as interrupted in _scheduled_runs.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.stopping && !s.running && len(s.activeRuns) == 0 {
		s.mu.Unlock()
		return nil
	}
	s.stopping = true
	cron := s.cron
	s.mu.Unlock()

	stopCtx := cron.Stop()
	done := make(chan struct{})
	go func() {
		<-stopCtx.Done()
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.finishShutdown()
		return nil
	case <-ctx.Done():
		s.cancelActiveJobs()
		s.interruptActiveRuns("scheduler shutdown interrupted")
		return ctx.Err()
	}
}

func (s *Scheduler) ensureRootLocked() {
	if s.rootCtx != nil && s.rootCtx.Err() == nil {
		return
	}
	s.rootCtx, s.rootCancel = context.WithCancel(context.Background())
	if s.activeRuns == nil {
		s.activeRuns = make(map[uuid.UUID]*activeRun)
	}
}

func (s *Scheduler) beginJob() (context.Context, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, nil, false
	}
	s.ensureRootLocked()
	ctx, cancel := context.WithCancel(s.rootCtx)
	s.wg.Add(1)
	done := func() {
		cancel()
		s.wg.Done()
	}
	return ctx, done, true
}

func (s *Scheduler) finishShutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rootCancel != nil {
		s.rootCancel()
	}
	s.rootCtx = nil
	s.rootCancel = nil
	s.running = false
	s.stopping = false
}

func (s *Scheduler) cancelActiveJobs() {
	s.mu.Lock()
	cancel := s.rootCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Scheduler) trackActiveRun(id uuid.UUID, jobName string, startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeRuns == nil {
		s.activeRuns = make(map[uuid.UUID]*activeRun)
	}
	s.activeRuns[id] = &activeRun{
		id:        id,
		jobName:   jobName,
		startedAt: startedAt,
	}
}

// finishActiveRun removes an active run and reports whether the caller should
// write the final status. It returns false when shutdown has already marked
// this run as interrupted.
func (s *Scheduler) finishActiveRun(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.activeRuns[id]
	if run == nil {
		return true
	}
	delete(s.activeRuns, id)
	return !run.finalized
}

func (s *Scheduler) interruptActiveRuns(reason string) {
	s.mu.Lock()
	runs := make([]activeRun, 0, len(s.activeRuns))
	for _, run := range s.activeRuns {
		if run.finalized {
			continue
		}
		run.finalized = true
		runs = append(runs, *run)
	}
	s.mu.Unlock()

	for _, run := range runs {
		durationMs := time.Since(run.startedAt).Milliseconds()
		ctx, cancel := context.WithTimeout(context.Background(), interruptUpdateTimeout)
		if err := s.db.UpdateScheduledRun(ctx, run.id, runStatusInterrupted, "", reason, durationMs); err != nil {
			s.log.Warn("scheduler: mark interrupted run failed", "job", run.jobName, "run_id", run.id.String(), "err", err)
		}
		cancel()
		s.log.Warn("scheduler: active job interrupted", "job", run.jobName, "run_id", run.id.String())
	}
}

func (s *Scheduler) Jobs() []*metadata.ScheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*metadata.ScheduledJob, len(s.jobs))
	copy(result, s.jobs)
	return result
}

// ActiveRunCount returns the number of scheduled jobs currently executing.
func (s *Scheduler) ActiveRunCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.activeRuns)
}

func (s *Scheduler) GetJob(name string) *metadata.ScheduledJob {
	nl := strings.ToLower(name)
	s.mu.Lock()
	defer s.mu.Unlock()
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
	jobCtx, done, ok := s.beginJob()
	if !ok {
		return errors.New("scheduler is stopping")
	}
	go func() {
		defer done()
		s.executeJob(jobCtx, job)
	}()
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
	s.trackActiveRun(runID, job.Name, startedAt)

	output, runErr := s.runProcessor(ctx, job)

	status, errText := scheduledRunStatus(ctx, runErr)

	durationMs := time.Since(startedAt).Milliseconds()
	if s.finishActiveRun(runID) {
		s.updateRun(ctx, runID, status, output, errText, durationMs)
	}

	s.log.Info("scheduler: job finished", "job", job.Name, "status", status, "duration_ms", durationMs)
}

func (s *Scheduler) executeGoJob(ctx context.Context, name string, fn func(ctx context.Context) error) {
	startedAt := time.Now()
	runID, err := s.db.InsertScheduledRun(ctx, name, startedAt)
	if err != nil {
		s.log.Error("scheduler: insert go run", "job", name, "err", err)
		return
	}
	s.trackActiveRun(runID, name, startedAt)

	runErr := fn(ctx)
	durationMs := time.Since(startedAt).Milliseconds()
	status, errText := scheduledRunStatus(ctx, runErr)
	if s.finishActiveRun(runID) {
		s.updateRun(ctx, runID, status, "", errText, durationMs)
	}
	if runErr != nil {
		s.log.Error("go job failed", "job", name, "status", status, "err", runErr)
		return
	}
	s.log.Info("go job done", "job", name, "status", status, "ms", durationMs)
}

func scheduledRunStatus(ctx context.Context, runErr error) (status, errText string) {
	if ctx.Err() == context.DeadlineExceeded {
		return runStatusTimeout, "timeout exceeded"
	}
	if ctx.Err() == context.Canceled {
		if runErr != nil {
			return runStatusInterrupted, runErr.Error()
		}
		return runStatusInterrupted, "scheduler shutdown interrupted"
	}
	if runErr != nil {
		return runStatusError, runErr.Error()
	}
	return runStatusSuccess, ""
}

func (s *Scheduler) updateRun(ctx context.Context, runID uuid.UUID, status, output, errText string, durationMs int64) {
	if err := s.db.UpdateScheduledRun(ctx, runID, status, output, errText, durationMs); err != nil {
		// Use background ctx in case the original was cancelled
		bgCtx := context.Background()
		_ = s.db.UpdateScheduledRun(bgCtx, runID, status, output, errText, durationMs)
	}
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
	// Полное DSL-окружение (Справочники/Документы/вложения/транзакции) строит
	// внешний VarsBuilder (ui), если подключён; иначе — базовый набор Common.
	var dslVars map[string]any
	if s.varsBuilder != nil {
		dslVars = s.varsBuilder(ctx, mc)
	} else {
		dslVars = s.buildDSLVars(ctx, mc)
	}
	dslVars["Параметры"] = paramsThis
	dslVars["Сообщить"] = msgFunc
	dslVars["Message"] = msgFunc
	interpreter.InjectMaket(dslVars, proc.Layout)

	err := s.interp.Run(procDecl, paramsThis, dslVars)
	output = strings.Join(messages, "\n")
	return output, err
}

func (s *Scheduler) buildDSLVars(ctx context.Context, mc *runtime.MovementsCollector) map[string]any {
	// Базовый набор переменных совпадает с тем, что UI handlers инжектируют
	// для обработчиков OnWrite/OnPost. Caller-specific переменные (Параметры,
	// Сообщить с привязкой к log задания) добавляются в runScheduledJob сверху.
	return dslvars.Common{
		Ctx:       ctx,
		Reg:       s.reg,
		Store:     s.db,
		Mailer:    s.mailer,
		Movements: mc,
		Interp:    s.interp, // hook-правило конфликта в ПланыОбмена.ЗагрузитьПакет
		// Предохранитель сети (план 62): регламентные задания тоже инициируют
		// HTTP/email из конфигурации — гейтим тем же флагом.
		NetGuard: func() error {
			if s.db.GetNetworkEnabled(ctx) {
				return nil
			}
			return ErrNetworkLocked
		},
		// Команды ОС (план 67): тем же флагом базы exec.enabled. nil-guard в
		// dslvars означал бы запрет, но регламентные задания — доверенный
		// серверный код, поэтому гейтим по настройке, как сеть.
		ExecGuard: func() error {
			if s.db.GetExecEnabled(ctx) {
				return nil
			}
			return errors.New("выполнение команд ОС отключено")
		},
	}.Build()
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
