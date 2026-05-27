package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retrieval"
)

func TestP4B2RetrievalTraceRepositoryCreateUpdateList(t *testing.T) {
	ctx := context.Background()
	store := openP4B2TestStore(t)
	defer store.Close()

	longQuery := strings.Repeat("架构复查", 200)
	trace, err := store.CreateRetrievalTrace(ctx, retrieval.TraceRecord{
		WorkspaceID: "ws_p4",
		ProjectID:   "proj_p4",
		RepoID:      "repo_p4",
		SessionID:   "sess_p4",
		TaskID:      "task_p4",
		Query:       longQuery,
		Intent:      retrieval.IntentArchitectureReview,
		Mode:        retrieval.ModeFTSMetadata,
		UsedFTS:     true,
	})
	if err != nil {
		t.Fatalf("CreateRetrievalTrace() error = %v", err)
	}
	if trace.ID == "" || trace.Status != retrieval.TraceStarted {
		t.Fatalf("created trace = %+v, want generated id and started status", trace)
	}
	if len([]rune(trace.Query)) != maxTraceTextRunes {
		t.Fatalf("trace query rune length = %d, want %d", len([]rune(trace.Query)), maxTraceTextRunes)
	}

	if err := store.UpdateRetrievalTrace(ctx, retrieval.TraceRecord{
		ID:             trace.ID,
		WorkspaceID:    "ws_p4",
		ProjectID:      "proj_p4",
		RepoID:         "repo_p4",
		Intent:         retrieval.IntentArchitectureReview,
		Mode:           retrieval.ModeCheckpointAware,
		UsedFTS:        true,
		UsedDocIndex:   true,
		FallbackReason: `["vector_disabled"]`,
		CandidateCount: 7,
		InjectedCount:  2,
		LatencyMS:      18,
		Status:         retrieval.TraceCompleted,
	}); err != nil {
		t.Fatalf("UpdateRetrievalTrace() error = %v", err)
	}

	traces, err := store.ListRetrievalTraces(ctx, retrieval.TraceQuery{
		WorkspaceID: "ws_p4",
		ProjectID:   "proj_p4",
		Status:      retrieval.TraceCompleted,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListRetrievalTraces() error = %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traces))
	}
	got := traces[0]
	if got.ID != trace.ID || got.Mode != retrieval.ModeCheckpointAware || !got.UsedFTS || !got.UsedDocIndex ||
		got.CandidateCount != 7 || got.InjectedCount != 2 || got.LatencyMS != 18 {
		t.Fatalf("listed trace = %+v, want updated diagnostics", got)
	}
	if _, err := store.ListRetrievalTraces(ctx, retrieval.TraceQuery{}); err == nil {
		t.Fatal("ListRetrievalTraces() without workspace error = nil, want validation error")
	}
}

func TestP4B2AccessLogRepositoryWriteBatchAndList(t *testing.T) {
	ctx := context.Background()
	store := openP4B2TestStore(t)
	defer store.Close()

	trace, err := store.CreateRetrievalTrace(ctx, retrieval.TraceRecord{
		WorkspaceID: "ws_p4",
		ProjectID:   "proj_p4",
		Status:      retrieval.TraceStarted,
	})
	if err != nil {
		t.Fatalf("CreateRetrievalTrace() error = %v", err)
	}
	longFeedback := strings.Repeat("反馈", 400)
	records, err := store.WriteMemoryAccessLogs(ctx, []retrieval.AccessLogRecord{
		{
			MemoryID:         "mem_001",
			SessionID:        "sess_p4",
			TaskID:           "task_p4",
			RetrievalTraceID: trace.ID,
			EventType:        "retrieved",
			SourceType:       "manual",
			Query:            "Kafka 架构决策",
			Rank:             1,
			Score:            0.82,
			ScoreBreakdown:   memory.ScoreBreakdown{BM25: 0.7, TaskFit: 0.8, Final: 0.82},
			InclusionReasons: []string{"task_match", "scope_match"},
		},
		{
			MemoryID:         "mem_001",
			SessionID:        "sess_p4",
			TaskID:           "task_p4",
			RetrievalTraceID: trace.ID,
			EventType:        "injected",
			Rank:             1,
			Score:            0.82,
			ScoreBreakdown:   memory.ScoreBreakdown{BM25: 0.7, TaskFit: 0.8, Final: 0.82},
			InclusionReasons: []string{"context_budget", "task_match"},
			UsedInContext:    true,
			Feedback:         longFeedback,
		},
	})
	if err != nil {
		t.Fatalf("WriteMemoryAccessLogs() error = %v", err)
	}
	if len(records) != 2 || records[0].ID == "" || records[1].ID == "" {
		t.Fatalf("written access logs = %+v, want generated ids", records)
	}
	if records[0].EventWeight != 0.2 || records[1].EventWeight != 0.5 {
		t.Fatalf("event weights = %v/%v, want retrieved/injected defaults", records[0].EventWeight, records[1].EventWeight)
	}
	if len([]rune(records[1].Feedback)) != maxAccessTextRunes {
		t.Fatalf("feedback rune length = %d, want %d", len([]rune(records[1].Feedback)), maxAccessTextRunes)
	}

	logs, err := store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{
		RetrievalTraceID: trace.ID,
		EventType:        "injected",
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListMemoryAccessLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("access log count = %d, want 1 injected log", len(logs))
	}
	got := logs[0]
	if got.MemoryID != "mem_001" || got.ScoreBreakdown.Final != 0.82 || !got.UsedInContext ||
		!containsString(got.InclusionReasons, "context_budget") {
		t.Fatalf("listed access log = %+v, want decoded score and inclusion reasons", got)
	}
	if _, err := store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{}); err == nil {
		t.Fatal("ListMemoryAccessLogs() without trace or memory error = nil, want validation error")
	}
}

func TestP4B2DeleteCleansP4ArtifactsAndKeepsAccessLogStats(t *testing.T) {
	ctx := context.Background()
	store := openP4B2TestStore(t)
	defer store.Close()

	item := seedP4B2Memory(t, ctx, store, "mem_delete")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `insert into memory_relation(
		id, source_id, target_id, relation_type, weight, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?)`, "rel_delete", item.ID, "mem_other", "supports", 1.0, now, now); err != nil {
		t.Fatalf("insert memory_relation: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `insert into code_ref(
		id, memory_id, repo_id, file_path, symbol, resolve_status, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?)`, "cr_delete", item.ID, "repo_p4", "internal/memory/service.go", "Service", "resolved", now, now); err != nil {
		t.Fatalf("insert code_ref: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `insert into memory_embedding(
		memory_id, embedding_model, embedding_dim, embedding, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?)`, item.ID, "local_stub", 3, []byte{1, 2, 3}, now, now); err != nil {
		t.Fatalf("insert memory_embedding: %v", err)
	}
	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		MemoryID:  item.ID,
		EventType: "retrieved",
		Query:     "删除一致性",
		Rank:      1,
		Score:     0.5,
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog() error = %v", err)
	}

	deleted, err := store.Delete(ctx, item.ID, "tester", "cleanup")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deleted.State != memory.StateDeleted {
		t.Fatalf("deleted state = %s, want deleted", deleted.State)
	}
	for table, query := range map[string]string{
		"memory_relation":  "select count(*) from memory_relation where source_id = 'mem_delete' or target_id = 'mem_delete'",
		"code_ref":         "select count(*) from code_ref where memory_id = 'mem_delete'",
		"memory_embedding": "select count(*) from memory_embedding where memory_id = 'mem_delete'",
	} {
		var count int
		if err := store.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			t.Fatalf("query %s count: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s remaining count = %d, want 0", table, count)
		}
	}
	var accessLogCount int
	if err := store.db.QueryRowContext(ctx, "select count(*) from memory_access_log where memory_id = ?", item.ID).Scan(&accessLogCount); err != nil {
		t.Fatalf("query access log count: %v", err)
	}
	if accessLogCount != 1 {
		t.Fatalf("access log count after delete = %d, want retained stats", accessLogCount)
	}
}

func openP4B2TestStore(t *testing.T) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(context.Background(), cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}

func seedP4B2Memory(t *testing.T, ctx context.Context, store *Store, memoryID string) memory.MemoryItem {
	t.Helper()
	now := time.Now().UTC()
	item := memory.MemoryItem{
		ID:                memoryID,
		Scope:             memory.ScopeProjectLocal,
		WorkspaceID:       "ws_p4",
		ProjectID:         "proj_p4",
		RepoID:            "repo_p4",
		MemoryType:        memory.TypeDecision,
		SourceType:        "manual",
		CreatedBy:         "test",
		SourceQuality:     0.8,
		Title:             "P4 删除一致性",
		Content:           "删除 memory 时需要清理 P4 关联表。",
		NormalizedContent: "删除 memory 时需要清理 P4 关联表。",
		SearchText:        "P4 删除一致性 删除 memory 时需要清理 P4 关联表。",
		State:             memory.StateStable,
		Confidence:        0.9,
		Importance:        0.8,
		EncodingDepth:     2,
		DecayRate:         0.8,
		RetentionScore:    0.7,
		Tier:              memory.TierLongTerm,
		CreatedAt:         now,
		UpdatedAt:         now,
		Version:           1,
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if err := insertMemoryItem(ctx, tx, item); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insertMemoryItem() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit memory seed: %v", err)
	}
	return item
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
