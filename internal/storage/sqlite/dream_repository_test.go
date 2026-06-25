package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/dream"
	"github.com/zaneway/theone/internal/memory"
)

func TestDreamRepositoryListsActiveMemoriesAndRelations(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, row := range []struct {
		id    string
		title string
		state string
	}{
		{id: "mem_dream_a", title: "Dream A", state: memory.StateStable},
		{id: "mem_dream_b", title: "Dream B", state: memory.StatePendingReview},
		{id: "mem_dream_deleted", title: "Deleted", state: memory.StateDeleted},
	} {
		if _, err := store.db.ExecContext(ctx, `insert into memory_item(
			id, scope, workspace_id, project_id, memory_type, title, content, search_text,
			state, confidence, importance, decay_rate, tier, created_at, updated_at, version
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id, memory.ScopeProjectLocal, "ws", "the-one", memory.TypeDecision, row.title,
			row.title+" content", row.title+" content", row.state, 0.8, 0.7, 0.8, memory.TierDurable, now, now, 1,
		); err != nil {
			t.Fatalf("insert memory %s: %v", row.id, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `insert into memory_relation(
		id, source_id, target_id, relation_type, weight, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?)`,
		"rel_dream", "mem_dream_a", "mem_dream_b", "supports", 1.0, now, now,
	); err != nil {
		t.Fatalf("insert relation: %v", err)
	}

	memories, err := store.ListMemoriesForDream(ctx, dream.ListRequest{WorkspaceID: "ws", ProjectID: "the-one"})
	if err != nil {
		t.Fatalf("ListMemoriesForDream() error = %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("memories = %+v, want active two memories", memories)
	}
	for _, item := range memories {
		if item.ID == "mem_dream_deleted" {
			t.Fatalf("deleted memory included: %+v", memories)
		}
	}
	relations, err := store.ListRelationsForDream(ctx, dream.ListRequest{WorkspaceID: "ws", ProjectID: "the-one"})
	if err != nil {
		t.Fatalf("ListRelationsForDream() error = %v", err)
	}
	if len(relations) != 1 || relations[0].SourceID != "mem_dream_a" || relations[0].TargetID != "mem_dream_b" {
		t.Fatalf("relations = %+v, want supports edge", relations)
	}
}
