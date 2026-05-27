package sqlite

import (
	"context"
	"log/slog"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
)

func TestP4RetrievalMigrationDefaultsAndConstraints(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 24, 14, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `insert into retrieval_trace(id, created_at) values (?, ?)`, "rt_defaults", now); err != nil {
		t.Fatalf("insert retrieval_trace defaults: %v", err)
	}
	var status string
	var usedFTS, usedVector, usedRelation bool
	var candidateCount, injectedCount int
	if err := store.db.QueryRowContext(ctx, `select status, used_fts, used_vector, used_relation, candidate_count, injected_count
		from retrieval_trace where id = ?`, "rt_defaults").Scan(&status, &usedFTS, &usedVector, &usedRelation, &candidateCount, &injectedCount); err != nil {
		t.Fatalf("query retrieval_trace defaults: %v", err)
	}
	if status != "completed" || usedFTS || usedVector || usedRelation || candidateCount != 0 || injectedCount != 0 {
		t.Fatalf("retrieval_trace defaults = status:%s fts:%t vector:%t relation:%t candidate:%d injected:%d, want completed/false/0",
			status, usedFTS, usedVector, usedRelation, candidateCount, injectedCount)
	}

	if _, err := store.db.ExecContext(ctx, `insert into memory_access_log(
		id, memory_id, event_type, event_weight, created_at
	) values (?, ?, ?, ?, ?)`, "mal_defaults", "mem_001", "retrieved", 0.2, now); err != nil {
		t.Fatalf("insert memory_access_log defaults: %v", err)
	}
	var sourceQuality float64
	var usedInContext bool
	if err := store.db.QueryRowContext(ctx, `select source_quality, used_in_context
		from memory_access_log where id = ?`, "mal_defaults").Scan(&sourceQuality, &usedInContext); err != nil {
		t.Fatalf("query memory_access_log defaults: %v", err)
	}
	if math.Abs(sourceQuality-0.7) > 0.000001 || usedInContext {
		t.Fatalf("memory_access_log defaults = source_quality:%v used_in_context:%t, want 0.7/false", sourceQuality, usedInContext)
	}
}

func TestP4RetrievalMigrationEmbeddingSupportsModelVersions(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	insert := func(model string) error {
		_, err := store.db.ExecContext(ctx, `insert into memory_embedding(
			memory_id, embedding_model, embedding_dim, embedding, created_at, updated_at
		) values (?, ?, ?, ?, ?, ?)`, "mem_embedding", model, 3, []byte{1, 2, 3}, now, now)
		return err
	}
	if err := insert("model_a"); err != nil {
		t.Fatalf("insert model_a embedding: %v", err)
	}
	if err := insert("model_b"); err != nil {
		t.Fatalf("insert model_b embedding: %v", err)
	}
	if err := insert("model_a"); err == nil {
		t.Fatal("duplicate memory_id + embedding_model insert error = nil, want primary key violation")
	}
}

func TestP4RetrievalMigrationDocSnapshotDedupUsesEmptyScopeValues(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `insert into doc_snapshot(
		id, workspace_id, doc_path, content_hash, created_at
	) values (?, ?, ?, ?, ?)`, "doc_001", "ws", "doc/P4.md", "sha256:doc", now); err != nil {
		t.Fatalf("insert doc_snapshot with default project/repo: %v", err)
	}
	var projectID, repoID string
	if err := store.db.QueryRowContext(ctx, `select project_id, repo_id from doc_snapshot where id = ?`, "doc_001").Scan(&projectID, &repoID); err != nil {
		t.Fatalf("query doc_snapshot default scope values: %v", err)
	}
	if projectID != "" || repoID != "" {
		t.Fatalf("doc_snapshot project/repo defaults = %q/%q, want empty strings", projectID, repoID)
	}
	_, err = store.db.ExecContext(ctx, `insert into doc_snapshot(
		id, workspace_id, doc_path, content_hash, created_at
	) values (?, ?, ?, ?, ?)`, "doc_duplicate", "ws", "doc/P4.md", "sha256:doc", now)
	if err == nil {
		t.Fatal("duplicate doc_snapshot insert error = nil, want unique constraint violation")
	}
}
