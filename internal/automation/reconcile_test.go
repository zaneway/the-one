package automation_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/automation"
	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/processor"
	"github.com/zaneway/the-one/internal/storage/sqlite"
)

func TestServiceReconcileDryRunFindsOrphanRawEvents(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 24, 11, 0, 0, 0, time.UTC)
	rawEvent := capture.RawEvent{
		ID:             "evt_orphan",
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelMCPTool,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "orphan raw_event without extract job",
		ContentHash:    "sha256:orphan",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	resp, err := service.Reconcile(ctx, automation.ReconcileRequest{
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Mode:        automation.ReconcileModeOrphanRawEvent,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !resp.DryRun || len(resp.Items) != 1 || resp.Items[0].RawEventID != rawEvent.ID || resp.Enqueued != 0 {
		t.Fatalf("response = %+v, want one orphan dry-run item", resp)
	}
	if resp.Items[0].Reason == "" {
		t.Fatalf("item reason = %q, want non-empty reason", resp.Items[0].Reason)
	}

	jobs, err := store.ListJobs(ctx, automation.ListJobsRequest{TargetID: rawEvent.ID})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want none after dry_run reconcile", jobs)
	}
}

func TestServiceReconcileEnqueuesExtractJobsWhenNotDryRun(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 24, 11, 10, 0, 0, time.UTC)
	rawEvent := capture.RawEvent{
		ID:             "evt_orphan_enqueue",
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelMCPTool,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "orphan raw_event to enqueue",
		ContentHash:    "sha256:orphan-enqueue",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	resp, err := service.Reconcile(ctx, automation.ReconcileRequest{
		WorkspaceID: "ws",
		Mode:        automation.ReconcileModeOrphanRawEvent,
		DryRun:      false,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if resp.Enqueued != 1 {
		t.Fatalf("enqueued = %d, want 1", resp.Enqueued)
	}

	jobs, err := store.ListJobs(ctx, automation.ListJobsRequest{
		TargetID: rawEvent.ID,
		JobType:  automation.JobTypeExtractEvidence,
	})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].TargetID != rawEvent.ID {
		t.Fatalf("jobs = %+v, want extract_evidence for orphan raw_event", jobs)
	}
}

func TestServiceReconcileReturnsProviderDisabledWithoutError(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Processor.Provider = "none"
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	resp, err := service.Reconcile(ctx, automation.ReconcileRequest{
		WorkspaceID: "ws",
		Mode:        automation.ReconcileModeOrphanRawEvent,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(resp.Diagnostics) != 1 || resp.Diagnostics[0] != "provider_disabled" {
		t.Fatalf("diagnostics = %+v, want provider_disabled", resp.Diagnostics)
	}
}
