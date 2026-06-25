package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/dream"
)

type fakeDreamExportRunner struct {
	calls chan dream.RunRequest
}

func (f *fakeDreamExportRunner) Run(ctx context.Context, req dream.RunRequest) (dream.RunResponse, error) {
	select {
	case f.calls <- req:
	case <-ctx.Done():
		return dream.RunResponse{}, ctx.Err()
	}
	return dream.RunResponse{DryRun: req.DryRun}, nil
}

func TestDreamExportSchedulerDisabledDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Default().Dream
	cfg.Enabled = true
	cfg.Scheduler.Enabled = false
	runner := &fakeDreamExportRunner{calls: make(chan dream.RunRequest, 1)}

	if started := startDreamExportScheduler(ctx, cfg, runner, slog.Default()); started {
		t.Fatal("startDreamExportScheduler() = true, want false when dream scheduler is disabled")
	}
	select {
	case call := <-runner.calls:
		t.Fatalf("unexpected dream export call: %+v", call)
	default:
	}
}

func TestDreamExportSchedulerRunsApplyExport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Default().Dream
	cfg.Enabled = true
	cfg.Vault.Root = t.TempDir()
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.InitialDelayMS = 0
	cfg.Scheduler.IntervalMS = 5
	cfg.Scheduler.MaxRunDurationMS = 100
	runner := &fakeDreamExportRunner{calls: make(chan dream.RunRequest, 4)}

	if started := startDreamExportScheduler(ctx, cfg, runner, slog.Default()); !started {
		t.Fatal("startDreamExportScheduler() = false, want true when dream scheduler is enabled")
	}

	first := waitDreamExportCall(t, runner.calls)
	if first.DryRun {
		t.Fatalf("first dream export call = %+v, want apply export", first)
	}
	second := waitDreamExportCall(t, runner.calls)
	if second.DryRun {
		t.Fatalf("second dream export call = %+v, want interval apply export", second)
	}
}

func TestDreamExportSchedulerSkipsTickWhilePreviousRunIsActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Default().Dream
	cfg.Enabled = true
	cfg.Vault.Root = t.TempDir()
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.InitialDelayMS = 0
	cfg.Scheduler.IntervalMS = 5
	cfg.Scheduler.MaxRunDurationMS = 500
	cfg.Scheduler.SkipIfPreviousRunning = true
	runner := &blockingDreamExportRunner{
		started: make(chan dream.RunRequest, 1),
		release: make(chan struct{}),
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	if started := startDreamExportScheduler(ctx, cfg, runner, logger); !started {
		t.Fatal("startDreamExportScheduler() = false, want true")
	}
	waitDreamExportCall(t, runner.started)
	deadline := time.After(500 * time.Millisecond)
	for !strings.Contains(logs.String(), "previous run is still active") {
		select {
		case <-deadline:
			t.Fatalf("scheduler logs = %q, want active-run skip warning", logs.String())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	close(runner.release)
}

type blockingDreamExportRunner struct {
	started chan dream.RunRequest
	release chan struct{}
}

func (r *blockingDreamExportRunner) Run(ctx context.Context, req dream.RunRequest) (dream.RunResponse, error) {
	select {
	case r.started <- req:
	default:
	}
	select {
	case <-r.release:
		return dream.RunResponse{}, nil
	case <-ctx.Done():
		return dream.RunResponse{}, ctx.Err()
	}
}

func waitDreamExportCall(t *testing.T, calls <-chan dream.RunRequest) dream.RunRequest {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for dream export call")
		return dream.RunRequest{}
	}
}
