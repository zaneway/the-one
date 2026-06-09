package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/retention"
)

const defaultRetentionRefreshInterval = time.Minute

type retentionMaintenanceRunner interface {
	RunRetention(ctx context.Context, req retention.RunRequest) (retention.RunResponse, error)
}

func startRetentionMaintenanceScheduler(ctx context.Context, cfg config.RetentionConfig, runner retentionMaintenanceRunner, logger *slog.Logger) bool {
	if !cfg.JobEnabled || runner == nil {
		return false
	}
	interval := retentionJobInterval(cfg.JobIntervalMS)
	go runRetentionMaintenanceScheduler(ctx, interval, runner, logger)
	return true
}

func runRetentionMaintenanceScheduler(ctx context.Context, interval time.Duration, runner retentionMaintenanceRunner, logger *slog.Logger) {
	runMaintenance := func() {
		if err := ctx.Err(); err != nil {
			return
		}
		runRetentionMode(ctx, runner, logger, retention.ModeRecomputeScores)
		runRetentionMode(ctx, runner, logger, retention.ModeCleanupTemporary)
	}

	runMaintenance()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runMaintenance()
		}
	}
}

func runRetentionMode(ctx context.Context, runner retentionMaintenanceRunner, logger *slog.Logger, mode string) {
	resp, err := runner.RunRetention(ctx, retention.RunRequest{
		Mode:   mode,
		DryRun: false,
	})
	if err != nil {
		logger.Error("retention job failed", "mode", mode, "error", err)
		return
	}
	logger.Info("retention job completed",
		"mode", mode,
		"processed", resp.Processed,
		"item_count", len(resp.Items),
	)
}

func retentionJobInterval(intervalMS int) time.Duration {
	if intervalMS <= 0 {
		return defaultRetentionRefreshInterval
	}
	return time.Duration(intervalMS) * time.Millisecond
}
