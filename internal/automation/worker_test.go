package automation_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/automation"
	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/processor"
	"github.com/zaneway/the-one/internal/storage/sqlite"
)

func TestWorkerRetriesFailedJobThenMarksFailed(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	_, _, err = store.EnqueueJob(ctx, automation.AsyncJob{
		ID:         "job_retry",
		JobType:    "unsupported_job",
		TargetType: "raw_event",
		TargetID:   "target_1",
		MaxRetries: 2,
		NextRunAt:  now,
	})
	if err != nil {
		t.Fatalf("EnqueueJob() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	worker := automation.NewWorker(service, store, automation.WorkerConfig{
		BatchSize:        1,
		RetryBaseDelayMS: 1000,
	})

	result, err := worker.RunOnce(ctx, now)
	if err != nil {
		t.Fatalf("RunOnce first error = %v", err)
	}
	if result.Claimed != 1 || result.Succeeded != 0 || result.Retried != 1 || result.Failed != 0 {
		t.Fatalf("first result = %+v, want one retry", result)
	}
	retried, err := store.GetJob(ctx, "job_retry")
	if err != nil {
		t.Fatalf("GetJob after retry error = %v", err)
	}
	if retried.Status != automation.JobStatusPending || retried.RetryCount != 1 || retried.LastError == "" {
		t.Fatalf("retried job = %+v, want pending retry_count=1 with last_error", retried)
	}
	if !retried.NextRunAt.After(now) {
		t.Fatalf("retry next_run_at = %v, want after %v", retried.NextRunAt, now)
	}

	result, err = worker.RunOnce(ctx, retried.NextRunAt.Add(time.Second))
	if err != nil {
		t.Fatalf("RunOnce second error = %v", err)
	}
	if result.Claimed != 1 || result.Retried != 0 || result.Failed != 1 {
		t.Fatalf("second result = %+v, want one failed", result)
	}
	failed, err := store.GetJob(ctx, "job_retry")
	if err != nil {
		t.Fatalf("GetJob after failed error = %v", err)
	}
	if failed.Status != automation.JobStatusFailed || failed.RetryCount != 1 || failed.LastError == "" {
		t.Fatalf("failed job = %+v, want failed with preserved retry_count and last_error", failed)
	}
}

func TestWorkerRunStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(context.Background(), cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	worker := automation.NewWorker(service, store, automation.WorkerConfig{
		PollIntervalMS:   5,
		BatchSize:        1,
		RetryBaseDelayMS: 10,
	})
	cancel()
	if err := worker.Run(ctx); err != context.Canceled {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestWorkerRecoversStaleRunningJobsBeforePolling(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	_, _, err = store.EnqueueJob(ctx, automation.AsyncJob{
		ID:         "job_stale",
		JobType:    "unsupported_job",
		TargetType: "raw_event",
		TargetID:   "target_stale",
		NextRunAt:  base,
		CreatedAt:  base,
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("EnqueueJob() error = %v", err)
	}
	if _, err := store.ClaimJobs(ctx, base.Add(time.Second), 1); err != nil {
		t.Fatalf("ClaimJobs() error = %v", err)
	}
	running, err := store.GetJob(ctx, "job_stale")
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if running.Status != automation.JobStatusRunning {
		t.Fatalf("job status = %s, want running", running.Status)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	worker := automation.NewWorker(service, store, automation.WorkerConfig{
		BatchSize:        1,
		RetryBaseDelayMS: 1000,
		RunningTimeoutMS: 300000,
	})
	result, err := worker.RunOnce(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Recovered != 1 || result.Claimed != 1 || result.Retried != 1 {
		t.Fatalf("result = %+v, want recovered=1 claimed=1 retried=1", result)
	}
}
