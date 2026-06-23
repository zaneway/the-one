package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
)

func TestP4B5EmbeddingRepositoryUpsertReplacesModelVersion(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.UpsertMemoryEmbedding(ctx, "mem_embedding", "local_stub", []float32{0.1, 0.2}); err != nil {
		t.Fatalf("UpsertMemoryEmbedding() error = %v", err)
	}
	if err := store.UpsertMemoryEmbedding(ctx, "mem_embedding", "local_stub", []float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatalf("UpsertMemoryEmbedding replace error = %v", err)
	}
	var dim int
	var blob []byte
	if err := store.db.QueryRowContext(ctx, `select embedding_dim, embedding from memory_embedding
		where memory_id = ? and embedding_model = ?`, "mem_embedding", "local_stub").Scan(&dim, &blob); err != nil {
		t.Fatalf("query memory_embedding: %v", err)
	}
	if dim != 3 || len(blob) != 12 {
		t.Fatalf("embedding dim/blob = %d/%d, want 3/12", dim, len(blob))
	}
}

func TestEmbeddingRepositorySearchVectorRanksByCosine(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range []struct {
		id      string
		content string
		vector  []float32
	}{
		{id: "mem_vector_high", content: "QKV 语义召回应优先命中。", vector: []float32{1, 0}},
		{id: "mem_vector_low", content: "无关的存储实现说明。", vector: []float32{0, 1}},
	} {
		if _, err := store.db.ExecContext(ctx, `insert into memory_item(
			id, scope, workspace_id, project_id, memory_type, content, search_text,
			state, confidence, importance, decay_rate, tier, created_at, updated_at
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.id, memory.ScopeProjectLocal, "ws", "project_vector", memory.TypeDecision,
			item.content, item.content, memory.StateStable, 0.8, 0.6, 0.8, memory.TierLongTerm, now, now,
		); err != nil {
			t.Fatalf("insert memory_item(%s) error = %v", item.id, err)
		}
		if err := store.UpsertMemoryEmbedding(ctx, item.id, "embedding-test", item.vector); err != nil {
			t.Fatalf("UpsertMemoryEmbedding(%s) error = %v", item.id, err)
		}
	}

	results, err := store.SearchVector(ctx, memory.SearchRequest{
		Query:       "qkv semantic recall",
		WorkspaceID: "ws",
		ProjectID:   "project_vector",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	}, "embedding-test", []float32{1, 0}, 10)
	if err != nil {
		t.Fatalf("SearchVector() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if results[0].MemoryID != "mem_vector_high" {
		t.Fatalf("top memory = %q, want mem_vector_high", results[0].MemoryID)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("scores not ranked by cosine: high=%f low=%f", results[0].Score, results[1].Score)
	}
	if !containsString(results[0].WhyIncluded, "vector_seed") {
		t.Fatalf("why_included = %+v, want vector_seed", results[0].WhyIncluded)
	}
}
