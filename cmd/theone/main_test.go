package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTryWritePIDFileLogsWarningAndDoesNotFail(t *testing.T) {
	t.Setenv("HOME", "/dev/null")

	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, nil))
	original := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(original)

	tryWritePIDFile(12345)

	output := buffer.String()
	if !strings.Contains(output, "write pid file failed") {
		t.Fatalf("log output = %q, want warning message", output)
	}
}

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
