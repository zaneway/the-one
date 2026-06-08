package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/retention"
)

type fakeRetentionMaintenanceRunner struct {
	calls chan retention.RunRequest
}

func (f *fakeRetentionMaintenanceRunner) RunRetention(ctx context.Context, req retention.RunRequest) (retention.RunResponse, error) {
	select {
	case f.calls <- req:
	case <-ctx.Done():
		return retention.RunResponse{}, ctx.Err()
	}
	return retention.RunResponse{Mode: req.Mode}, nil
}

func TestRetentionCleanupSchedulerDisabledDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Default().Retention
	cfg.JobEnabled = false
	runner := &fakeRetentionMaintenanceRunner{calls: make(chan retention.RunRequest, 1)}

	if started := startRetentionMaintenanceScheduler(ctx, cfg, runner, slog.Default()); started {
		t.Fatal("startRetentionMaintenanceScheduler() = true, want false when retention job is disabled")
	}
	select {
	case call := <-runner.calls:
		t.Fatalf("unexpected retention call: %+v", call)
	default:
	}
}

func TestRetentionMaintenanceSchedulerRunsCleanupAndRecompute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Default().Retention
	cfg.JobEnabled = true
	cfg.JobIntervalMS = 5
	runner := &fakeRetentionMaintenanceRunner{calls: make(chan retention.RunRequest, 8)}

	if started := startRetentionMaintenanceScheduler(ctx, cfg, runner, slog.Default()); !started {
		t.Fatal("startRetentionMaintenanceScheduler() = false, want true when retention job is enabled")
	}

	first := waitRetentionCall(t, runner.calls)
	if first.Mode != retention.ModeCleanupTemporary || first.DryRun {
		t.Fatalf("first retention call = %+v, want cleanup_temporary apply", first)
	}
	second := waitRetentionCall(t, runner.calls)
	if second.Mode != retention.ModeRecomputeScores || second.DryRun {
		t.Fatalf("second retention call = %+v, want recompute_scores apply", second)
	}
	third := waitRetentionCall(t, runner.calls)
	if third.Mode != retention.ModeCleanupTemporary || third.DryRun {
		t.Fatalf("third retention call = %+v, want interval cleanup_temporary apply", third)
	}
	fourth := waitRetentionCall(t, runner.calls)
	if fourth.Mode != retention.ModeRecomputeScores || fourth.DryRun {
		t.Fatalf("fourth retention call = %+v, want interval recompute_scores apply", fourth)
	}
}

func TestRetentionJobIntervalDefaultsWhenUnset(t *testing.T) {
	if got := retentionJobInterval(0); got != defaultRetentionJobInterval {
		t.Fatalf("retentionJobInterval(0) = %s, want %s", got, defaultRetentionJobInterval)
	}
	if got := retentionJobInterval(25); got != 25*time.Millisecond {
		t.Fatalf("retentionJobInterval(25) = %s, want 25ms", got)
	}
}

func waitRetentionCall(t *testing.T, calls <-chan retention.RunRequest) retention.RunRequest {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for retention cleanup call")
		return retention.RunRequest{}
	}
}
