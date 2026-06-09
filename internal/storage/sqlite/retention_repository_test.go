package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retention"
	"github.com/zaneway/theone/internal/retrieval"
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
		SourceType:    "user_declared",
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
	if !updated.UpdatedAt.Equal(item.UpdatedAt) {
		t.Fatalf("updated_at = %s, want content lifecycle timestamp unchanged %s", updated.UpdatedAt, item.UpdatedAt)
	}
}

func TestWriteMemoryAccessLogRefreshesLastAccessedAt(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	seedRetentionMemory(t, ctx, store, "mem_accessed", "ev_accessed", "evt_accessed", now.Add(-72*time.Hour))

	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:            "mal_accessed",
		MemoryID:      "mem_accessed",
		EventType:     "injected",
		EventWeight:   retention.AccessLogEventWeight("injected"),
		SourceQuality: 0.7,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog() error = %v", err)
	}

	var lastAccessed string
	if err := store.db.QueryRowContext(ctx, "select last_accessed_at from memory_item where id = ?", "mem_accessed").Scan(&lastAccessed); err != nil {
		t.Fatalf("query last_accessed_at error = %v", err)
	}
	if got := parseTime(lastAccessed); !got.Equal(now) {
		t.Fatalf("last_accessed_at = %s, want %s", got, now)
	}
}

func TestListMemoriesForScoreRecalcPrioritizesUnmaterializedAccess(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	seedRetentionMemory(t, ctx, store, "mem_old_without_hit", "ev_old_without_hit", "evt_old_without_hit", now.Add(-72*time.Hour))
	seedRetentionMemory(t, ctx, store, "mem_recent_with_hit", "ev_recent_with_hit", "evt_recent_with_hit", now.Add(-time.Hour))
	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:            "mal_recent_hit",
		MemoryID:      "mem_recent_with_hit",
		EventType:     "retrieved",
		EventWeight:   retention.AccessLogEventWeight("retrieved"),
		SourceQuality: 0.7,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog() error = %v", err)
	}

	records, err := store.ListMemoriesForScoreRecalc(ctx, retention.ListRequest{
		WorkspaceID: "ws",
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("ListMemoriesForScoreRecalc() error = %v", err)
	}
	if len(records) != 1 || records[0].ID != "mem_recent_with_hit" {
		t.Fatalf("records = %+v, want dirty hit memory prioritized", records)
	}
}

func TestRetentionServiceRecomputeScoresDeletesInvalidWeakMemory(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	evidence := memory.Evidence{
		ID:                   "ev_invalid_weak",
		RawEventID:           "evt_invalid_weak",
		SourceType:           "auto_log",
		InterpretedStatement: "过期的临时弱信号。",
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:            "mem_invalid_weak",
		Scope:         memory.ScopeSession,
		WorkspaceID:   "ws",
		SessionID:     "session_a",
		MemoryType:    memory.TypeTemporaryState,
		SourceType:    "auto_log",
		Content:       "过期的临时弱信号。",
		State:         memory.StateProvisional,
		Tier:          memory.TierShortTerm,
		Confidence:    0.2,
		Importance:    0.0,
		EncodingDepth: 0,
		DecayRate:     1.2,
		CreatedAt:     now.Add(-30 * 24 * time.Hour),
		UpdatedAt:     now.Add(-30 * 24 * time.Hour),
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
	if resp.Processed != 1 || len(resp.Items) != 1 {
		t.Fatalf("response = %+v, want one processed invalid memory", resp)
	}
	if resp.Items[0].Action != retention.ActionDelete || resp.Items[0].Reason != retention.ReasonInvalidRetentionScore {
		t.Fatalf("item = %+v, want delete invalid_retention_score", resp.Items[0])
	}
	deleted, err := store.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if deleted.State != memory.StateDeleted {
		t.Fatalf("deleted state = %q, want deleted", deleted.State)
	}
	results, _, err := store.searchByFTS(ctx, memory.SearchRequest{
		Query:       "过期 临时 弱信号",
		WorkspaceID: "ws",
		SessionID:   "session_a",
	}, "过期 OR 临时 OR 弱信号", 10)
	if err != nil {
		t.Fatalf("searchByFTS() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("fts results = %+v, want deleted memory removed from fts", results)
	}
}

func TestRetentionServiceRecomputeScoresKeepsRecentlyHitWeakMemory(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	evidence := memory.Evidence{
		ID:                   "ev_recent_weak",
		RawEventID:           "evt_recent_weak",
		SourceType:           "auto_log",
		InterpretedStatement: "最近命中的临时弱信号。",
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:            "mem_recent_weak",
		Scope:         memory.ScopeSession,
		WorkspaceID:   "ws",
		SessionID:     "session_a",
		MemoryType:    memory.TypeTemporaryState,
		SourceType:    "auto_log",
		Content:       "最近命中的临时弱信号。",
		State:         memory.StateProvisional,
		Tier:          memory.TierShortTerm,
		Confidence:    0.2,
		Importance:    0.0,
		EncodingDepth: 0,
		DecayRate:     1.2,
		CreatedAt:     now.Add(-30 * 24 * time.Hour),
		UpdatedAt:     now.Add(-30 * 24 * time.Hour),
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:            "mal_recent_weak",
		MemoryID:      item.ID,
		EventType:     "retrieved",
		EventWeight:   retention.AccessLogEventWeight("retrieved"),
		SourceQuality: 0.7,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog() error = %v", err)
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
	if len(resp.Items) != 1 || resp.Items[0].Action == retention.ActionDelete {
		t.Fatalf("response = %+v, want recently hit weak memory kept", resp)
	}
	kept, err := store.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if kept.State == memory.StateDeleted {
		t.Fatalf("state = %q, want not deleted after recent hit", kept.State)
	}
	if kept.RetentionScore <= 0 {
		t.Fatalf("retention_score = %v, want materialized access signal", kept.RetentionScore)
	}
}

func TestRetentionServiceRecomputeScoresAutoPromotesProvisionalMemory(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	now := time.Now().UTC()
	item := seedLifecyclePromotionMemory(t, ctx, store, lifecyclePromotionSeed{
		MemoryID: "mem_auto_promote_provisional",
		State:    memory.StateProvisional,
		Tier:     memory.TierShortTerm,
	})
	writePositiveAccessLogs(t, ctx, store, item.ID, now.Add(-4*24*time.Hour), []string{
		"task_success", "task_success", "task_success", "task_success", "task_success",
	})

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
	if updated.State != memory.StateStable {
		t.Fatalf("state = %q, want stable after repeated positive access", updated.State)
	}
	assertAutoConsolidationReview(t, ctx, store, item.ID, "auto_consolidated_from_provisional")
}

func TestRetentionServiceRecomputeScoresAutoPromotesPendingReviewWithStrongerEvidence(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	now := time.Now().UTC()
	item := seedLifecyclePromotionMemory(t, ctx, store, lifecyclePromotionSeed{
		MemoryID: "mem_auto_promote_pending",
		State:    memory.StatePendingReview,
		Tier:     memory.TierShortTerm,
	})
	writePositiveAccessLogs(t, ctx, store, item.ID, now.Add(-7*24*time.Hour), []string{
		"user_declared", "task_success", "task_success", "task_success", "task_success", "task_success",
	})

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
	if updated.State != memory.StateStable {
		t.Fatalf("state = %q, want stable after strong repeated positive access", updated.State)
	}
	assertAutoConsolidationReview(t, ctx, store, item.ID, "auto_consolidated_from_pending_review")
}

func TestRetentionServiceRecomputeScoresPromotesHitTemporaryMemoryOutOfTemporaryTier(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	now := time.Now().UTC()
	item := seedLifecyclePromotionMemory(t, ctx, store, lifecyclePromotionSeed{
		MemoryID:   "mem_hit_temporary_promoted",
		State:      memory.StateProvisional,
		Tier:       memory.TierTemporary,
		ValidUntil: now.Add(48 * time.Hour),
	})
	writePositiveAccessLogs(t, ctx, store, item.ID, now.Add(-4*24*time.Hour), []string{
		"task_success", "task_success", "task_success", "task_success", "task_success",
	})

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
	if updated.State != memory.StateStable {
		t.Fatalf("state = %q, want stable after repeated positive access", updated.State)
	}
	if updated.Tier == memory.TierTemporary {
		t.Fatalf("tier = %q, want hit temporary memory promoted into persistent tier", updated.Tier)
	}
}

func seedRetentionMemory(t *testing.T, ctx context.Context, store *Store, memoryID, evidenceID, rawEventID string, updatedAt time.Time) {
	t.Helper()
	evidence := memory.Evidence{
		ID:                   evidenceID,
		RawEventID:           rawEventID,
		SourceType:           "agent_summary",
		InterpretedStatement: "用于 retention 重算排序测试。",
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence(%s) error = %v", evidenceID, err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item: memory.MemoryItem{
			ID:            memoryID,
			Scope:         memory.ScopeProjectLocal,
			WorkspaceID:   "ws",
			ProjectID:     "project_a",
			MemoryType:    memory.TypeDecision,
			SourceType:    "agent_summary",
			Content:       "用于 retention 重算排序测试。",
			State:         memory.StateStable,
			Tier:          memory.TierShortTerm,
			Confidence:    0.8,
			Importance:    0.8,
			EncodingDepth: 2,
			DecayRate:     0.8,
			CreatedAt:     updatedAt,
			UpdatedAt:     updatedAt,
		},
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory(%s) error = %v", memoryID, err)
	}
}

type lifecyclePromotionSeed struct {
	MemoryID   string
	State      string
	Tier       string
	ValidUntil time.Time
}

func seedLifecyclePromotionMemory(t *testing.T, ctx context.Context, store *Store, seed lifecyclePromotionSeed) memory.MemoryItem {
	t.Helper()
	now := time.Now().UTC()
	item := memory.MemoryItem{
		ID:            seed.MemoryID,
		Scope:         memory.ScopeProjectLocal,
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		MemoryType:    memory.TypeProjectFact,
		SourceType:    "agent_summary",
		Content:       "项目采用 retention 自动巩固策略。",
		State:         seed.State,
		Tier:          seed.Tier,
		Confidence:    0.95,
		Importance:    0.95,
		EncodingDepth: 4,
		DecayRate:     0.4,
		CreatedAt:     now.Add(-10 * 24 * time.Hour),
		UpdatedAt:     now.Add(-10 * 24 * time.Hour),
	}
	validUntil := any(nil)
	if !seed.ValidUntil.IsZero() {
		validUntil = seed.ValidUntil.Format(time.RFC3339Nano)
	}
	if _, err := store.db.ExecContext(ctx, `insert into memory_item(
		id, scope, workspace_id, project_id, memory_type, source_type, created_by,
		source_quality, title, content, normalized_content, search_text,
		state, confidence, importance, encoding_depth, decay_rate,
		retention_score, tier, valid_until, created_at, updated_at, pinned, user_confirmed, version
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Scope, item.WorkspaceID, item.ProjectID, item.MemoryType, item.SourceType, "test",
		item.SourceQuality, item.Title, item.Content, item.Content, item.Content,
		item.State, item.Confidence, item.Importance, item.EncodingDepth, item.DecayRate,
		item.RetentionScore, item.Tier, validUntil, item.CreatedAt.Format(time.RFC3339Nano),
		item.UpdatedAt.Format(time.RFC3339Nano), item.Pinned, item.UserConfirmed, 1,
	); err != nil {
		t.Fatalf("insert memory_item(%s) error = %v", item.ID, err)
	}
	return item
}

func writePositiveAccessLogs(t *testing.T, ctx context.Context, store *Store, memoryID string, start time.Time, eventTypes []string) {
	t.Helper()
	for index, eventType := range eventTypes {
		if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
			ID:            memoryID + "_mal_" + eventType + "_" + time.Duration(index).String(),
			MemoryID:      memoryID,
			EventType:     eventType,
			EventWeight:   retention.AccessLogEventWeight(eventType),
			SourceQuality: 1.0,
			CreatedAt:     start.Add(time.Duration(index) * 24 * time.Hour),
		}); err != nil {
			t.Fatalf("WriteMemoryAccessLog(%s) error = %v", eventType, err)
		}
	}
}

func assertAutoConsolidationReview(t *testing.T, ctx context.Context, store *Store, memoryID, reason string) {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(ctx, `select count(*) from memory_review
		where memory_id = ? and review_type = ? and status = ? and reviewer = ? and feedback = ?`,
		memoryID, "auto_consolidation", "auto_promoted", "retention", reason,
	).Scan(&count); err != nil {
		t.Fatalf("query memory_review error = %v", err)
	}
	if count != 1 {
		t.Fatalf("auto consolidation review count = %d, want 1", count)
	}
}

func storeCfgFromTestStore(t *testing.T, store *Store) config.Config {
	t.Helper()
	status := store.Status()
	cfg := config.Default()
	cfg.Storage.Path = status.DBPath
	return cfg
}
