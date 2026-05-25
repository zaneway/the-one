package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/memory"
)

func TestP4B3CodeRefRepositoryWriteGetAndListByMemory(t *testing.T) {
	ctx := context.Background()
	store := openP4B3TestStore(t)
	defer store.Close()

	longSummary := strings.Repeat("摘要", 400)
	ref, err := store.WriteCodeRef(ctx, memory.CodeRef{
		MemoryID:      "mem_code_001",
		RepoID:        "repo_p4",
		CommitHash:    "abc123",
		FilePath:      "internal//memory/../memory/service.go",
		Symbol:        "Service.Search",
		LineStart:     270,
		LineEnd:       308,
		ContentHash:   "sha256:old",
		RefSummary:    longSummary,
		ResolveStatus: memory.CodeRefStatusResolved,
	})
	if err != nil {
		t.Fatalf("WriteCodeRef() error = %v", err)
	}
	if ref.ID == "" {
		t.Fatal("code_ref id is empty, want generated id")
	}
	if ref.FilePath != "internal/memory/service.go" {
		t.Fatalf("normalized file_path = %q, want internal/memory/service.go", ref.FilePath)
	}
	if len([]rune(ref.RefSummary)) != maxCodeRefSummaryRune {
		t.Fatalf("summary rune length = %d, want %d", len([]rune(ref.RefSummary)), maxCodeRefSummaryRune)
	}
	var resolvedAt string
	if err := store.db.QueryRowContext(ctx, "select coalesce(resolved_at, '') from code_ref where id = ?", ref.ID).Scan(&resolvedAt); err != nil {
		t.Fatalf("query resolved_at: %v", err)
	}
	if resolvedAt == "" {
		t.Fatal("resolved_at is empty for resolved code_ref")
	}

	got, err := store.GetCodeRef(ctx, ref.ID)
	if err != nil {
		t.Fatalf("GetCodeRef() error = %v", err)
	}
	if got.MemoryID != "mem_code_001" || got.ResolveStatus != memory.CodeRefStatusResolved || got.LineStart != 270 {
		t.Fatalf("GetCodeRef() = %+v, want persisted fields", got)
	}

	refs, err := store.ListCodeRefs(ctx, memory.CodeRefQuery{MemoryID: "mem_code_001"})
	if err != nil {
		t.Fatalf("ListCodeRefs() error = %v", err)
	}
	if len(refs) != 1 || refs[0].ID != ref.ID {
		t.Fatalf("ListCodeRefs by memory = %+v, want one code_ref", refs)
	}
}

func TestP4B3CodeRefRepositoryUpdateStatusAndListByRepoFile(t *testing.T) {
	ctx := context.Background()
	store := openP4B3TestStore(t)
	defer store.Close()

	ref, err := store.WriteCodeRef(ctx, memory.CodeRef{
		MemoryID: "mem_code_002",
		RepoID:   "repo_p4",
		FilePath: "internal/storage/sqlite/code_ref_repository.go",
		Symbol:   "WriteCodeRef",
	})
	if err != nil {
		t.Fatalf("WriteCodeRef() error = %v", err)
	}
	if ref.ResolveStatus != memory.CodeRefStatusUnresolved {
		t.Fatalf("default resolve_status = %q, want unresolved", ref.ResolveStatus)
	}

	updated, err := store.UpdateCodeRefResolveStatus(ctx, ref.ID, memory.CodeRefStatusStale, "sha256:new", "函数签名已变更")
	if err != nil {
		t.Fatalf("UpdateCodeRefResolveStatus() error = %v", err)
	}
	if updated.ResolveStatus != memory.CodeRefStatusStale || updated.ContentHash != "sha256:new" || updated.RefSummary != "函数签名已变更" {
		t.Fatalf("updated code_ref = %+v, want stale status and refreshed metadata", updated)
	}
	var resolvedAt string
	if err := store.db.QueryRowContext(ctx, "select coalesce(resolved_at, '') from code_ref where id = ?", ref.ID).Scan(&resolvedAt); err != nil {
		t.Fatalf("query resolved_at: %v", err)
	}
	if resolvedAt != "" {
		t.Fatalf("resolved_at = %q for stale status, want empty", resolvedAt)
	}

	refs, err := store.ListCodeRefs(ctx, memory.CodeRefQuery{
		RepoID:        "repo_p4",
		FilePath:      "internal/storage/sqlite/code_ref_repository.go",
		Symbol:        "WriteCodeRef",
		ResolveStatus: memory.CodeRefStatusStale,
	})
	if err != nil {
		t.Fatalf("ListCodeRefs() error = %v", err)
	}
	if len(refs) != 1 || refs[0].ID != ref.ID {
		t.Fatalf("ListCodeRefs by repo/file = %+v, want updated code_ref", refs)
	}
}

func TestP4B3CodeRefRepositoryListForRefreshRequiresRepo(t *testing.T) {
	ctx := context.Background()
	store := openP4B3TestStore(t)
	defer store.Close()

	if _, err := store.ListCodeRefsForRefresh(ctx, "", 10); err == nil {
		t.Fatal("ListCodeRefsForRefresh() without repo_id error = nil, want validation error")
	}
	if _, err := store.WriteCodeRef(ctx, memory.CodeRef{
		ID:            "cr_refresh_unresolved",
		MemoryID:      "mem_refresh",
		RepoID:        "repo_refresh",
		FilePath:      "a.go",
		ResolveStatus: memory.CodeRefStatusUnresolved,
	}); err != nil {
		t.Fatalf("WriteCodeRef(unresolved) error = %v", err)
	}
	if _, err := store.WriteCodeRef(ctx, memory.CodeRef{
		ID:            "cr_refresh_missing",
		MemoryID:      "mem_refresh",
		RepoID:        "repo_refresh",
		FilePath:      "b.go",
		ResolveStatus: memory.CodeRefStatusMissing,
	}); err != nil {
		t.Fatalf("WriteCodeRef(missing) error = %v", err)
	}
	refs, err := store.ListCodeRefsForRefresh(ctx, "repo_refresh", 10)
	if err != nil {
		t.Fatalf("ListCodeRefsForRefresh() error = %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "cr_refresh_unresolved" {
		t.Fatalf("refresh refs = %+v, want unresolved/resolved/stale only", refs)
	}
}

func TestP4B3CodeRefRepositoryDeleteAndValidation(t *testing.T) {
	ctx := context.Background()
	store := openP4B3TestStore(t)
	defer store.Close()

	if _, err := store.WriteCodeRef(ctx, memory.CodeRef{MemoryID: "mem_invalid", RepoID: "repo_p4", FilePath: "../secret.go"}); err == nil {
		t.Fatal("WriteCodeRef() with parent path error = nil, want validation error")
	}
	if _, err := store.WriteCodeRef(ctx, memory.CodeRef{MemoryID: "mem_invalid", RepoID: "repo_p4", FilePath: "a.go", ResolveStatus: "unknown"}); err == nil {
		t.Fatal("WriteCodeRef() with invalid status error = nil, want validation error")
	}
	if _, err := store.ListCodeRefs(ctx, memory.CodeRefQuery{}); err == nil {
		t.Fatal("ListCodeRefs() without bounded filter error = nil, want validation error")
	}

	refA, err := store.WriteCodeRef(ctx, memory.CodeRef{
		MemoryID: "mem_delete_code",
		RepoID:   "repo_p4",
		FilePath: "a.go",
		Symbol:   "A",
	})
	if err != nil {
		t.Fatalf("WriteCodeRef(refA) error = %v", err)
	}
	refB, err := store.WriteCodeRef(ctx, memory.CodeRef{
		MemoryID: "mem_delete_code",
		RepoID:   "repo_p4",
		FilePath: "b.go",
		Symbol:   "B",
	})
	if err != nil {
		t.Fatalf("WriteCodeRef(refB) error = %v", err)
	}
	if err := store.DeleteCodeRef(ctx, refA.ID); err != nil {
		t.Fatalf("DeleteCodeRef() error = %v", err)
	}
	if _, err := store.GetCodeRef(ctx, refA.ID); err == nil {
		t.Fatal("GetCodeRef() after delete error = nil, want not found")
	}
	if err := store.DeleteCodeRefsByMemory(ctx, "mem_delete_code"); err != nil {
		t.Fatalf("DeleteCodeRefsByMemory() error = %v", err)
	}
	refs, err := store.ListCodeRefs(ctx, memory.CodeRefQuery{MemoryID: "mem_delete_code"})
	if err != nil {
		t.Fatalf("ListCodeRefs() error = %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("remaining refs = %+v after deleting memory refs; refB=%s", refs, refB.ID)
	}
}

func openP4B3TestStore(t *testing.T) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(context.Background(), cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}
