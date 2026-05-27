package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePIDFileCreatesAndOverwrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := writePIDFile(12345); err != nil {
		t.Fatalf("writePIDFile() error = %v", err)
	}
	path := filepath.Join(home, ".theone", "theone.pid")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != "12345\n" {
		t.Fatalf("pid file content = %q, want %q", got, "12345\\n")
	}

	if err := writePIDFile(67890); err != nil {
		t.Fatalf("writePIDFile() overwrite error = %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after overwrite error = %v", err)
	}
	if got := string(data); got != "67890\n" {
		t.Fatalf("pid file content after overwrite = %q, want %q", got, "67890\\n")
	}
}

func TestPIDFilePathUsesHomeTheoneDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := pidFilePath()
	if err != nil {
		t.Fatalf("pidFilePath() error = %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(".theone", "theone.pid")) {
		t.Fatalf("pid file path = %q, want suffix .theone/theone.pid", path)
	}
	if path != filepath.Join(home, ".theone", "theone.pid") {
		t.Fatalf("pid file path = %q, want %q", path, filepath.Join(home, ".theone", "theone.pid"))
	}
}
