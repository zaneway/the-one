package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.Backend != "sqlite" {
		t.Fatalf("backend = %q, want sqlite", cfg.Storage.Backend)
	}
	if cfg.Server.MCPAddr != "stdio" {
		t.Fatalf("mcp addr = %q, want stdio", cfg.Server.MCPAddr)
	}
	if cfg.Embedding.Provider != "none" {
		t.Fatalf("embedding provider = %q, want none", cfg.Embedding.Provider)
	}
	if !cfg.Capture.RequireSessionForAgentEvents {
		t.Fatal("capture require session = false, want true")
	}
	if cfg.Capture.MaxOutputSummaryChars != 2000 {
		t.Fatalf("capture max output summary = %d, want 2000", cfg.Capture.MaxOutputSummaryChars)
	}
}

func TestLoadEnvAndOverrides(t *testing.T) {
	t.Setenv("MEMORYD_DATA_DIR", filepath.Join(t.TempDir(), "from-env"))
	t.Setenv("MEMORYD_LOG_LEVEL", "debug")

	dbPath := filepath.Join(t.TempDir(), "explicit.db")
	cfg, err := Load(Overrides{DBPath: dbPath, LogLevel: "warn"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.Path != dbPath {
		t.Fatalf("db path = %q, want %q", cfg.Storage.Path, dbPath)
	}
	if cfg.Logging.Level != "warn" {
		t.Fatalf("log level = %q, want warn", cfg.Logging.Level)
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	_, err := Load(Overrides{LogLevel: "verbose"})
	if err == nil {
		t.Fatal("Load() error = nil, want invalid log level")
	}
}
