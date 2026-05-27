package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retention"
)

func TestRetentionRepositoryListsAndArchivesExpiredTemporary(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	evidence := memory.Evidence{
		ID:                   "ev_temp",
		RawEventID:           "evt_temp",
		SourceType:           "tool_result",
		InterpretedStatement: "临时任务状态。",
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:          "mem_temp_expired",
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		MemoryType:  memory.TypeTemporaryState,
		Content:     "临时任务状态等待清理。",
		State:       memory.StateProvisional,
		Tier:        memory.TierTemporary,
		CreatedAt:   now.Add(-10 * 24 * time.Hour),
		UpdatedAt:   now.Add(-10 * 24 * time.Hour),
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "update memory_item set valid_until = ? where id = ?",
		now.Add(-24*time.Hour).Format(time.RFC3339Nano), item.ID,
	); err != nil {
		t.Fatalf("update valid_until error = %v", err)
	}

	records, err := store.ListExpiredTemporaryMemories(ctx, retention.ListRequest{
		WorkspaceID:      "ws",
		TemporaryTTLDays: 5,
		Now:              now,
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListExpiredTemporaryMemories() error = %v", err)
	}
	if len(records) != 1 || records[0].ID != item.ID {
		t.Fatalf("records = %+v, want expired temporary memory", records)
	}

	if err := store.ArchiveTemporaryMemory(ctx, item.ID, now); err != nil {
		t.Fatalf("ArchiveTemporaryMemory() error = %v", err)
	}
	archived, err := store.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if archived.State != memory.StateArchived || archived.Tier != memory.TierArchived {
		t.Fatalf("archived = %+v, want archived state and tier", archived)
	}
	results, _, err := store.searchByFTS(ctx, memory.SearchRequest{
		Query:       "临时任务状态",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
	}, "临时任务状态", 10)
	if err != nil {
		t.Fatalf("searchByFTS() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("fts results = %+v, want archived memory removed from fts", results)
	}
}

func TestRetentionServiceRecomputeScoresUpdatesTier(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	evidence := memory.Evidence{
		ID:                   "ev_stable",
		RawEventID:           "evt_stable",
		SourceType:           "user_declared",
		InterpretedStatement: "用户偏好先写测试。",
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:            "mem_stable_score",
		Scope:         memory.ScopeProjectLocal,
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		MemoryType:    memory.TypePreference,
		Content:       "推进功能前先写测试。",
		State:         memory.StateStable,
		Tier:          memory.TierShortTerm,
		UserConfirmed: true,
		Confidence:    0.9,
		Importance:    0.8,
		CreatedAt:     now.Add(-48 * time.Hour),
		UpdatedAt:     now.Add(-48 * time.Hour),
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}

	cfg := storeCfgFromTestStore(t, store)
	service := retention.NewService(cfg, store)
	resp, err := service.Run(ctx, retention.RunRequest{
		Mode:   retention.ModeRecomputeScores,
		DryRun: false,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Processed != 1 {
		t.Fatalf("processed = %d, want 1", resp.Processed)
	}
	updated, err := store.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if updated.RetentionScore <= 0 || updated.Tier == memory.TierShortTerm {
		t.Fatalf("updated = %+v, want recomputed score and promoted tier", updated)
	}
}

func storeCfgFromTestStore(t *testing.T, store *Store) config.Config {
	t.Helper()
	status := store.Status()
	cfg := config.Default()
	cfg.Storage.Path = status.DBPath
	return cfg
}
