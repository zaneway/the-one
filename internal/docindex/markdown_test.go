package docindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildMarkdownSnapshotExtractsSectionsWithoutBody(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "design.md")
	content := "# Design  \r\n\nintro\r\n\n## Scope\r\nbody should not be persisted\r\n\n```go\r\n# Not Heading\r\n```\r\n"
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	snapshot, err := BuildMarkdownSnapshot(MarkdownBuildOptions{
		WorkspaceID:         "ws",
		RepoID:              root,
		Path:                "design.md",
		MaxDocSizeKB:        64,
		MaxSections:         10,
		StoreSectionSummary: true,
	})
	if err != nil {
		t.Fatalf("BuildMarkdownSnapshot() error = %v", err)
	}

	if snapshot.Path != "design.md" || !strings.HasPrefix(snapshot.ContentHash, "sha256:") {
		t.Fatalf("snapshot metadata = %+v, want normalized path and hash", snapshot)
	}
	if snapshot.SectionCount != 2 || len(snapshot.Sections) != 2 {
		t.Fatalf("sections len = %d/%d, want 2", snapshot.SectionCount, len(snapshot.Sections))
	}
	if snapshot.Sections[0].SectionID != "design" || snapshot.Sections[1].SectionID != "design/scope" {
		t.Fatalf("section ids = %q, %q", snapshot.Sections[0].SectionID, snapshot.Sections[1].SectionID)
	}
	if strings.Contains(snapshot.Sections[1].Summary, "body should not be persisted") {
		t.Fatalf("section summary persisted body: %q", snapshot.Sections[1].Summary)
	}
}

func TestBuildMarkdownSnapshotRejectsUnsafePath(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildMarkdownSnapshot(MarkdownBuildOptions{
		WorkspaceID:  "ws",
		RepoID:       root,
		Path:         "../design.md",
		MaxDocSizeKB: 64,
		MaxSections:  10,
	}); err == nil {
		t.Fatal("BuildMarkdownSnapshot() error = nil, want unsafe path rejection")
	}
}

func TestBuildMarkdownSnapshotLargeFileOnlyHashesDocument(t *testing.T) {
	root := t.TempDir()
	content := "# Design\n" + strings.Repeat("x", 2048)
	if err := os.WriteFile(filepath.Join(root, "large.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	snapshot, err := BuildMarkdownSnapshot(MarkdownBuildOptions{
		WorkspaceID:  "ws",
		RepoID:       root,
		Path:         "large.md",
		MaxDocSizeKB: 1,
		MaxSections:  10,
	})
	if err != nil {
		t.Fatalf("BuildMarkdownSnapshot() error = %v", err)
	}
	if snapshot.ContentHash == "" || snapshot.SectionCount != 0 || len(snapshot.Sections) != 0 {
		t.Fatalf("snapshot = %+v, want file hash without sections", snapshot)
	}
}

func TestBuildMarkdownSnapshotHashIsStableForLineEndingAndTrailingSpaces(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "stable.md")
	if err := os.WriteFile(docPath, []byte("# Design  \r\nline with trailing spaces  \r\n## Scope\t \r\nbody\t \r\n"), 0o644); err != nil {
		t.Fatalf("write markdown v1: %v", err)
	}
	first, err := BuildMarkdownSnapshot(MarkdownBuildOptions{
		WorkspaceID:  "ws",
		RepoID:       root,
		Path:         "stable.md",
		MaxDocSizeKB: 64,
		MaxSections:  10,
	})
	if err != nil {
		t.Fatalf("BuildMarkdownSnapshot(v1) error = %v", err)
	}
	if err := os.WriteFile(docPath, []byte("# Design\nline with trailing spaces\n## Scope\nbody\n"), 0o644); err != nil {
		t.Fatalf("write markdown v2: %v", err)
	}
	second, err := BuildMarkdownSnapshot(MarkdownBuildOptions{
		WorkspaceID:  "ws",
		RepoID:       root,
		Path:         "stable.md",
		MaxDocSizeKB: 64,
		MaxSections:  10,
	})
	if err != nil {
		t.Fatalf("BuildMarkdownSnapshot(v2) error = %v", err)
	}
	if first.ContentHash != second.ContentHash {
		t.Fatalf("content hash changed: first=%s second=%s", first.ContentHash, second.ContentHash)
	}
	if len(first.Sections) != len(second.Sections) || first.Sections[1].ContentHash != second.Sections[1].ContentHash {
		t.Fatalf("section hash mismatch: first=%+v second=%+v", first.Sections, second.Sections)
	}
}

func TestBuildMarkdownSnapshotRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("# Outside\n"), 0o644); err != nil {
		t.Fatalf("write outside markdown: %v", err)
	}
	linkPath := filepath.Join(root, "link.md")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	_, err := BuildMarkdownSnapshot(MarkdownBuildOptions{
		WorkspaceID:  "ws",
		RepoID:       root,
		Path:         "link.md",
		MaxDocSizeKB: 64,
		MaxSections:  10,
	})
	if err == nil {
		t.Fatal("BuildMarkdownSnapshot() error = nil, want symlink escape rejection")
	}
}

func TestBuildMarkdownSnapshotRespectsMaxSections(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "limited.md"), []byte("# A\n\n## B\n\n## C\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	snapshot, err := BuildMarkdownSnapshot(MarkdownBuildOptions{
		WorkspaceID:  "ws",
		RepoID:       root,
		Path:         "limited.md",
		MaxDocSizeKB: 64,
		MaxSections:  1,
	})
	if err != nil {
		t.Fatalf("BuildMarkdownSnapshot() error = %v", err)
	}
	if snapshot.SectionCount != 1 || len(snapshot.Sections) != 1 || snapshot.Sections[0].SectionID != "a" {
		t.Fatalf("snapshot sections = %+v, want only first section", snapshot.Sections)
	}
}
