package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/retention"
)

const defaultRetentionJobInterval = 24 * time.Hour

type retentionCleanupRunner interface {
	RunRetention(ctx context.Context, req retention.RunRequest) (retention.RunResponse, error)
}

func startRetentionCleanupScheduler(ctx context.Context, cfg config.RetentionConfig, runner retentionCleanupRunner, logger *slog.Logger) bool {
	if !cfg.JobEnabled || runner == nil {
		return false
	}
	interval := retentionJobInterval(cfg.JobIntervalMS)
	go runRetentionCleanupScheduler(ctx, interval, runner, logger)
	return true
}

func runRetentionCleanupScheduler(ctx context.Context, interval time.Duration, runner retentionCleanupRunner, logger *slog.Logger) {
	runCleanup := func() {
		if err := ctx.Err(); err != nil {
			return
		}
		resp, err := runner.RunRetention(ctx, retention.RunRequest{
			Mode:   retention.ModeCleanupTemporary,
			DryRun: false,
		})
		if err != nil {
			logger.Error("retention cleanup job failed", "error", err)
			return
		}
		logger.Info("retention cleanup job completed",
			"processed", resp.Processed,
			"item_count", len(resp.Items),
		)
	}

	runCleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCleanup()
		}
	}
}

func retentionJobInterval(intervalMS int) time.Duration {
	if intervalMS <= 0 {
		return defaultRetentionJobInterval
	}
	return time.Duration(intervalMS) * time.Millisecond
}
