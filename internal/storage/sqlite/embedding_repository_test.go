package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/zaneway/the-one/internal/config"
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
