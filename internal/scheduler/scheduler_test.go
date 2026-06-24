package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/storage"
	"github.com/stretchr/testify/assert"
)

func openSchedulerTestDB(t *testing.T) (*storage.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.EnsureScheduledRunsTable(ctx); err != nil {
		t.Fatalf("EnsureScheduledRunsTable: %v", err)
	}
	return db, ctx
}

func TestShutdownDrainsRunningGoJob(t *testing.T) {
	db, ctx := openSchedulerTestDB(t)
	sched := New(db, nil, nil)

	started := make(chan struct{})
	release := make(chan struct{})
	jobCtx, done, ok := sched.beginJob()
	assert.True(t, ok)
	go func() {
		defer done()
		sched.executeGoJob(jobCtx, "SlowJob", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- sched.Shutdown(context.Background())
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown returned before active job completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	assert.NoError(t, <-shutdownDone)

	runs, err := db.ScheduledRuns(ctx, "SlowJob", 1)
	assert.NoError(t, err)
	if assert.Len(t, runs, 1) {
		assert.Equal(t, runStatusSuccess, runs[0].Status)
		assert.NotNil(t, runs[0].FinishedAt)
	}
}

func TestShutdownDeadlineMarksRunningGoJobInterrupted(t *testing.T) {
	db, ctx := openSchedulerTestDB(t)
	sched := New(db, nil, nil)

	started := make(chan struct{})
	release := make(chan struct{})
	jobDone := make(chan struct{})
	jobCtx, done, ok := sched.beginJob()
	assert.True(t, ok)
	go func() {
		defer close(jobDone)
		defer done()
		sched.executeGoJob(jobCtx, "BlockedJob", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := sched.Shutdown(shutdownCtx)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)

	runs, err := db.ScheduledRuns(ctx, "BlockedJob", 1)
	assert.NoError(t, err)
	if assert.Len(t, runs, 1) {
		assert.Equal(t, runStatusInterrupted, runs[0].Status)
		assert.Equal(t, "scheduler shutdown interrupted", runs[0].Error)
	}

	close(release)
	<-jobDone

	runs, err = db.ScheduledRuns(ctx, "BlockedJob", 1)
	assert.NoError(t, err)
	if assert.Len(t, runs, 1) {
		assert.Equal(t, runStatusInterrupted, runs[0].Status)
	}
}

func TestResolveTemplate_Today(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	result := resolveTemplate("{{today}}", now)
	got, ok := result.(time.Time)
	assert.True(t, ok)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.May, got.Month())
	assert.Equal(t, 5, got.Day())
	assert.Equal(t, 0, got.Hour())
}

func TestResolveTemplate_MinusDays(t *testing.T) {
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	result := resolveTemplate("{{today | minus_days:7}}", now)
	got, ok := result.(time.Time)
	assert.True(t, ok)
	assert.Equal(t, time.May, got.Month())
	assert.Equal(t, 3, got.Day())
}

func TestResolveTemplate_MinusMonths(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	result := resolveTemplate("{{today | minus_months:1}}", now)
	got, ok := result.(time.Time)
	assert.True(t, ok)
	assert.Equal(t, time.April, got.Month())
}

func TestResolveTemplate_StartOfMonth(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	result := resolveTemplate("{{today | start_of_month}}", now)
	got, ok := result.(time.Time)
	assert.True(t, ok)
	assert.Equal(t, 1, got.Day())
}

func TestResolveTemplate_NoTemplate(t *testing.T) {
	now := time.Now()
	result := resolveTemplate("просто строка", now)
	assert.Equal(t, "просто строка", result)
}

func TestResolveParamTemplates_Mixed(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	params := map[string]any{
		"Дата":     "{{today | minus_days:7}}",
		"Процент":  float64(10),
		"Название": "тест",
	}
	result := resolveParamTemplatesAt(params, now)
	got, ok := result["Дата"].(time.Time)
	assert.True(t, ok)
	assert.Equal(t, 28, got.Day()) // 2026-05-05 minus 7 days = April 28
	assert.Equal(t, time.April, got.Month())
	assert.Equal(t, float64(10), result["Процент"])
	assert.Equal(t, "тест", result["Название"])
}
