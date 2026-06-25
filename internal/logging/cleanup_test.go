package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupLogFilesRemovesNestedLogs(t *testing.T) {
	root := t.TempDir()
	logsDir := filepath.Join(root, "logs")
	cursorDir := filepath.Join(logsDir, "cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := map[string]string{
		filepath.Join(logsDir, "theone.log"):        "main",
		filepath.Join(cursorDir, "theone.log"):      "cursor",
		filepath.Join(cursorDir, "hook.log"):        "hook",
		filepath.Join(logsDir, "keep.txt"):          "note",
		filepath.Join(logsDir, "dead_letter.jsonl"): "{}",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	removed, err := CleanupLogFiles([]string{logsDir})
	if err != nil {
		t.Fatalf("CleanupLogFiles() error = %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	for _, path := range []string{
		filepath.Join(logsDir, "theone.log"),
		filepath.Join(cursorDir, "theone.log"),
		filepath.Join(cursorDir, "hook.log"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("log file still exists: %s", path)
		}
	}
	for _, path := range []string{
		filepath.Join(logsDir, "keep.txt"),
		filepath.Join(logsDir, "dead_letter.jsonl"),
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("non-log file missing: %s (%v)", path, statErr)
		}
	}
}

func TestCleanupLogFilesSkipsMissingDir(t *testing.T) {
	removed, err := CleanupLogFiles([]string{filepath.Join(t.TempDir(), "missing", "logs")})
	if err != nil {
		t.Fatalf("CleanupLogFiles() error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestCleanupLogFilesRejectsRoot(t *testing.T) {
	if _, err := CleanupLogFiles([]string{string(filepath.Separator)}); err == nil {
		t.Fatal("CleanupLogFiles() error = nil, want root rejection")
	}
}
