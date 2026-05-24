package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retrieval"
)

func TestListRelationExpansionsUsesScopedDepthOneEdges(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	insertRelationTestMemory(t, store, "mem_seed", memory.ScopeProjectLocal, "ws", "project_a", memory.StateStable, "seed memory", now)
	insertRelationTestMemory(t, store, "mem_support", memory.ScopeProjectLocal, "ws", "project_a", memory.StateStable, "support memory", now)
	insertRelationTestMemory(t, store, "mem_other_project", memory.ScopeProjectLocal, "ws", "project_b", memory.StateStable, "other project memory", now)
	insertRelationTestMemory(t, store, "mem_archived", memory.ScopeProjectLocal, "ws", "project_a", memory.StateArchived, "archived memory", now)

	for _, relation := range []memory.MemoryRelation{
		{ID: "rel_support", SourceID: "mem_seed", TargetID: "mem_support", RelationType: retrieval.RelationTypeSupports, Weight: 0.8},
		{ID: "rel_other_project", SourceID: "mem_seed", TargetID: "mem_other_project", RelationType: retrieval.RelationTypeSupports, Weight: 0.8},
		{ID: "rel_archived", SourceID: "mem_seed", TargetID: "mem_archived", RelationType: retrieval.RelationTypeSupports, Weight: 0.8},
	} {
		if err := store.WriteMemoryRelation(ctx, relation); err != nil {
			t.Fatalf("WriteMemoryRelation(%s) error = %v", relation.ID, err)
		}
	}

	expansions, err := store.ListRelationExpansions(ctx, retrieval.RelationExpansionQuery{
		SeedMemoryIDs: []string{"mem_seed"},
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		Scopes:        []string{memory.ScopeProjectLocal},
		RelationTypes: []string{retrieval.RelationTypeSupports},
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListRelationExpansions() error = %v", err)
	}
	if len(expansions) != 1 {
		t.Fatalf("expansions len = %d, want only scoped active support edge: %+v", len(expansions), expansions)
	}
	if expansions[0].SeedMemoryID != "mem_seed" ||
		expansions[0].RelatedMemory.ID != "mem_support" ||
		expansions[0].Direction != retrieval.RelationDirectionOutgoing ||
		expansions[0].RelationType != retrieval.RelationTypeSupports {
		t.Fatalf("expansion = %+v, want mem_seed -> mem_support", expansions[0])
	}
}

func insertRelationTestMemory(t *testing.T, store *Store, id, scope, workspaceID, projectID, state, content, now string) {
	t.Helper()
	_, err := store.db.Exec(`insert into memory_item(
		id, scope, workspace_id, project_id, memory_type, source_type, source_quality,
		title, content, normalized_content, search_text, state, confidence, importance,
		encoding_depth, decay_rate, retention_score, tier, created_at, updated_at,
		pinned, user_confirmed, version
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, scope, workspaceID, projectID, memory.TypeDecision, "manual_review", 0.8,
		id, content, content, content, state, 0.8, 0.6,
		2, 0.8, 0.5, memory.TierLongTerm, now, now,
		false, true, 1,
	)
	if err != nil {
		t.Fatalf("insert memory %s: %v", id, err)
	}
}
