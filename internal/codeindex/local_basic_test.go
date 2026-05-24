package codeindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/memory"
)

func TestLocalBasicAdapterResolveCodeRefs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal/demo"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/demo/service.go"), []byte(`package demo

type Service struct{}

func (s *Service) Search(query string) string {
	return query
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	adapter := NewLocalBasicAdapter(config.CodeIndexConfig{Provider: "local_basic", MaxFileSizeKB: 512, MaxResolveRefs: 30}, root)
	resolved, err := adapter.ResolveCodeRefs(ctx, []memory.CodeRef{{
		ID:            "cr_1",
		MemoryID:      "mem_code",
		RepoID:        "repo_demo",
		FilePath:      "internal/demo/service.go",
		Symbol:        "Service.Search",
		ResolveStatus: memory.CodeRefStatusUnresolved,
	}})
	if err != nil {
		t.Fatalf("ResolveCodeRefs() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved len = %d, want 1", len(resolved))
	}
	ref := resolved[0]
	if ref.ResolveStatus != memory.CodeRefStatusResolved || ref.LineStart == 0 || ref.ContentHash == "" {
		t.Fatalf("resolved ref = %+v, want resolved line and hash", ref)
	}
	if ref.RefSummary == "" || ref.RefSummary == "return query" {
		t.Fatalf("ref summary = %q, want metadata summary without source", ref.RefSummary)
	}
}

func TestLocalBasicAdapterMarksStaleWhenHashChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	adapter := NewLocalBasicAdapter(config.CodeIndexConfig{Provider: "local_basic", MaxFileSizeKB: 512, MaxResolveRefs: 30}, root)

	resolved, err := adapter.ResolveCodeRefs(ctx, []memory.CodeRef{{
		ID:            "cr_stale",
		MemoryID:      "mem_code",
		RepoID:        "repo_demo",
		FilePath:      "main.go",
		Symbol:        "main",
		ContentHash:   "sha256:old",
		ResolveStatus: memory.CodeRefStatusResolved,
	}})
	if err != nil {
		t.Fatalf("ResolveCodeRefs() error = %v", err)
	}
	if resolved[0].ResolveStatus != memory.CodeRefStatusStale || resolved[0].ContentHash == "sha256:old" {
		t.Fatalf("resolved ref = %+v, want stale with refreshed hash", resolved[0])
	}
}
