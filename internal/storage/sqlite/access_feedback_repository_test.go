package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retention"
	"github.com/zaneway/theone/internal/retrieval"
)

func TestAggregateAccessFeedbackAndRecomputeScores(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable")
	}

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	evidence := memory.Evidence{ID: "ev_feedback", SourceType: "agent_summary", InterpretedStatement: "先写测试", Confidence: 0.8, CreatedAt: now}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item: memory.MemoryItem{
			ID: "mem_feedback", Scope: memory.ScopeProjectLocal, WorkspaceID: "ws", ProjectID: "proj",
			MemoryType: memory.TypePreference, SourceType: "agent_summary", Title: "偏好", Content: "先写测试",
			State: memory.StateStable, Confidence: 0.8, Importance: 0.7, EncodingDepth: 2, DecayRate: 0.4,
			Tier: memory.TierShortTerm, CreatedAt: now, UpdatedAt: now,
		},
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID: "mal_injected", MemoryID: "mem_feedback", EventType: "injected", EventWeight: 0.5,
		SourceQuality: 1.0, CreatedAt: now,
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog(injected) error = %v", err)
	}
	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID: "mal_confirmed", MemoryID: "mem_feedback", EventType: "user_confirmed", EventWeight: 2.0,
		SourceQuality: 1.0, CreatedAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog(user_confirmed) error = %v", err)
	}

	service := retention.NewService(cfg, store)
	resp, err := service.Run(ctx, retention.RunRequest{Mode: retention.ModeRecomputeScores, WorkspaceID: "ws"})
	if err != nil {
		t.Fatalf("Run(recompute_scores) error = %v", err)
	}
	if resp.Processed == 0 {
		t.Fatalf("processed = 0, want at least one memory updated")
	}
	events, err := store.ListAccessEvents(ctx, []string{"mem_feedback"})
	if err != nil {
		t.Fatalf("ListAccessEvents() error = %v", err)
	}
	signals := retention.ComputeAccessSignals(events["mem_feedback"], 0.8, now)
	if signals.EffectiveReinforcement <= 0 || signals.BaseActivationNorm <= 0 {
		t.Fatalf("signals = %+v, want positive reinforcement and activation", signals)
	}
}
