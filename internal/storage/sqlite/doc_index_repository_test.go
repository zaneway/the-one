package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/docindex"
)

func TestP4B4DocSnapshotRepositoryWriteDedupAndList(t *testing.T) {
	ctx := context.Background()
	store := openP4B4TestStore(t)
	defer store.Close()

	longSummary := strings.Repeat("章节摘要", 200)
	input := docindex.DocumentSnapshot{
		WorkspaceID: "ws_p4",
		ProjectID:   "proj_p4",
		RepoID:      "repo_p4",
		Path:        "doc//design/../P4.md",
		Role:        "implementation_design",
		ContentHash: "sha256:doc",
		ModifiedAt:  time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC),
		Sections: []docindex.DocumentSection{
			{
				SectionID:   "8.3",
				HeadingPath: []string{"8. Doc Index", "8.3 doc_index 数据模型"},
				Level:       3,
				StartLine:   42,
				EndLine:     80,
				ContentHash: "sha256:section",
				Summary:     longSummary,
			},
		},
	}
	snapshot, err := store.WriteDocSnapshot(ctx, input)
	if err != nil {
		t.Fatalf("WriteDocSnapshot() error = %v", err)
	}
	if snapshot.ID == "" || snapshot.Path != "doc/P4.md" || snapshot.SectionCount != 1 || len(snapshot.Sections) != 1 {
		t.Fatalf("snapshot = %+v, want generated id, normalized path and one section", snapshot)
	}
	if len([]rune(snapshot.Sections[0].Summary)) != maxDocSectionSummary {
		t.Fatalf("section summary rune length = %d, want %d", len([]rune(snapshot.Sections[0].Summary)), maxDocSectionSummary)
	}

	again, err := store.WriteDocSnapshot(ctx, input)
	if err != nil {
		t.Fatalf("WriteDocSnapshot duplicate error = %v", err)
	}
	if again.ID != snapshot.ID {
		t.Fatalf("duplicate snapshot id = %s, want existing %s", again.ID, snapshot.ID)
	}

	snapshots, err := store.ListDocSnapshots(ctx, docindex.SnapshotQuery{
		WorkspaceID:     "ws_p4",
		ProjectID:       "proj_p4",
		RepoID:          "repo_p4",
		Path:            "doc/P4.md",
		IncludeSections: true,
	})
	if err != nil {
		t.Fatalf("ListDocSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].ID != snapshot.ID || len(snapshots[0].Sections) != 1 {
		t.Fatalf("ListDocSnapshots() = %+v, want one snapshot with sections", snapshots)
	}
	if got := snapshots[0].Sections[0].HeadingPath; len(got) != 2 || got[1] != "8.3 doc_index 数据模型" {
		t.Fatalf("decoded heading path = %+v, want persisted heading path", got)
	}
}

func TestP4B4DocSnapshotRepositoryDeleteCleansSections(t *testing.T) {
	ctx := context.Background()
	store := openP4B4TestStore(t)
	defer store.Close()

	snapshot, err := store.WriteDocSnapshot(ctx, docindex.DocumentSnapshot{
		WorkspaceID: "ws_p4",
		Path:        "doc/P4.markdown",
		ContentHash: "sha256:doc_delete",
		Sections: []docindex.DocumentSection{
			{SectionID: "1", Level: 1, StartLine: 1, EndLine: 5, ContentHash: "sha256:s1"},
			{SectionID: "2", Level: 1, StartLine: 6, EndLine: 10, ContentHash: "sha256:s2"},
		},
	})
	if err != nil {
		t.Fatalf("WriteDocSnapshot() error = %v", err)
	}
	if err := store.DeleteDocSnapshot(ctx, snapshot.ID); err != nil {
		t.Fatalf("DeleteDocSnapshot() error = %v", err)
	}
	if _, err := store.GetDocSnapshot(ctx, snapshot.ID, true); err == nil {
		t.Fatal("GetDocSnapshot() after delete error = nil, want not found")
	}
	var sectionCount int
	if err := store.db.QueryRowContext(ctx, "select count(*) from doc_section_snapshot where snapshot_id = ?", snapshot.ID).Scan(&sectionCount); err != nil {
		t.Fatalf("query section count: %v", err)
	}
	if sectionCount != 0 {
		t.Fatalf("section count after delete = %d, want 0", sectionCount)
	}
}

func TestP4B4DocSnapshotRepositoryValidation(t *testing.T) {
	ctx := context.Background()
	store := openP4B4TestStore(t)
	defer store.Close()

	invalidSnapshots := []docindex.DocumentSnapshot{
		{WorkspaceID: "ws", Path: "../secret.md", ContentHash: "sha256:x"},
		{WorkspaceID: "ws", Path: "/tmp/secret.md", ContentHash: "sha256:x"},
		{WorkspaceID: "ws", Path: "doc/P4.txt", ContentHash: "sha256:x"},
		{WorkspaceID: "ws", Path: "doc/P4.md", ContentHash: "sha256:x", Sections: []docindex.DocumentSection{
			{SectionID: "dup", ContentHash: "sha256:a"},
			{SectionID: "dup", ContentHash: "sha256:b"},
		}},
		{WorkspaceID: "ws", Path: "doc/P4.md", ContentHash: "sha256:x", Sections: []docindex.DocumentSection{
			{SectionID: "bad", StartLine: 10, EndLine: 9, ContentHash: "sha256:a"},
		}},
	}
	for _, snapshot := range invalidSnapshots {
		if _, err := store.WriteDocSnapshot(ctx, snapshot); err == nil {
			t.Fatalf("WriteDocSnapshot(%+v) error = nil, want validation error", snapshot)
		}
	}
	if _, err := store.ListDocSnapshots(ctx, docindex.SnapshotQuery{WorkspaceID: "ws"}); err == nil {
		t.Fatal("ListDocSnapshots() without doc_path error = nil, want validation error")
	}
	if _, err := store.ListDocSnapshots(ctx, docindex.SnapshotQuery{WorkspaceID: "ws", Path: "../secret.md"}); err == nil {
		t.Fatal("ListDocSnapshots() with path traversal error = nil, want validation error")
	}
}

func openP4B4TestStore(t *testing.T) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(context.Background(), cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}
