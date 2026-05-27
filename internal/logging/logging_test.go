package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestNewWritesLogFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "logs", "theone.log")
	logger, closer, err := New(config.LoggingConfig{
		Level:  "info",
		Format: "text",
		Path:   logPath,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.Info("test log entry", "component", "logging_test")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "test log entry") {
		t.Fatalf("log file content = %q, want message", content)
	}
}
