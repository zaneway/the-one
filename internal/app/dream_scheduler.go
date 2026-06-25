package app

import (
	"context"
	"log/slog"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/dream"
)

const defaultDreamExportInterval = time.Hour

type dreamExportRunner interface {
	Run(ctx context.Context, req dream.RunRequest) (dream.RunResponse, error)
}

func startDreamExportScheduler(ctx context.Context, cfg config.DreamConfig, runner dreamExportRunner, logger *slog.Logger) bool {
	if !cfg.Enabled || !cfg.Scheduler.Enabled || runner == nil {
		return false
	}
	interval := dreamExportInterval(cfg.Scheduler.IntervalMS)
	initialDelay := time.Duration(cfg.Scheduler.InitialDelayMS) * time.Millisecond
	maxRunDuration := time.Duration(cfg.Scheduler.MaxRunDurationMS) * time.Millisecond
	skipIfRunning := cfg.Scheduler.SkipIfPreviousRunning
	go runDreamExportScheduler(ctx, initialDelay, interval, cfg.Scheduler.JitterRatio, maxRunDuration, skipIfRunning, runner, logger)
	return true
}

func runDreamExportScheduler(ctx context.Context, initialDelay, interval time.Duration, jitterRatio float64, maxRunDuration time.Duration, skipIfRunning bool, runner dreamExportRunner, logger *slog.Logger) {
	if initialDelay > 0 {
		timer := time.NewTimer(initialDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	var running atomic.Bool
	triggerExport := func() {
		if err := ctx.Err(); err != nil {
			return
		}
		if skipIfRunning {
			if !running.CompareAndSwap(false, true) {
				logger.Warn("dream export job skipped because previous run is still active")
				return
			}
		}
		go func() {
			if skipIfRunning {
				defer running.Store(false)
			}
			runCtx := ctx
			cancel := func() {}
			if maxRunDuration > 0 {
				runCtx, cancel = context.WithTimeout(ctx, maxRunDuration)
			}
			defer cancel()
			resp, err := runner.Run(runCtx, dream.RunRequest{DryRun: false})
			if err != nil {
				logger.Error("dream export job failed", "error", err)
				return
			}
			logger.Info("dream export job completed",
				"planned", resp.Planned,
				"written", resp.Written,
				"skipped", resp.Skipped,
			)
		}()
	}

	triggerExport()
	wait := dreamExportTickInterval(interval, jitterRatio)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			triggerExport()
			wait = dreamExportTickInterval(interval, jitterRatio)
			timer.Reset(wait)
		}
	}
}

func dreamExportTickInterval(interval time.Duration, jitterRatio float64) time.Duration {
	if interval <= 0 || jitterRatio <= 0 {
		return interval
	}
	delta := float64(interval) * jitterRatio * (rand.Float64()*2 - 1)
	next := interval + time.Duration(delta)
	if next <= 0 {
		return interval
	}
	return next
}

func dreamExportInterval(intervalMS int) time.Duration {
	if intervalMS <= 0 {
		return defaultDreamExportInterval
	}
	return time.Duration(intervalMS) * time.Millisecond
}
