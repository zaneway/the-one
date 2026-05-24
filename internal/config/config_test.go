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
	if !cfg.Automation.WorkerEnabled {
		t.Fatal("automation worker enabled = false, want true")
	}
	if cfg.Automation.PollIntervalMS != 1000 || cfg.Automation.BatchSize != 10 || cfg.Automation.MaxAttempts != 3 || cfg.Automation.RetryBaseDelayMS != 1000 {
		t.Fatalf("automation defaults = %+v, want poll=1000 batch=10 attempts=3 retry=1000", cfg.Automation)
	}
	if cfg.Automation.RunningTimeoutMS != 300000 {
		t.Fatalf("automation running timeout = %d, want 300000", cfg.Automation.RunningTimeoutMS)
	}
	if cfg.Processor.Provider != "rule_based" || !cfg.Processor.EnableAutoProcessing || cfg.Processor.MaxRelatedEvents != 20 || cfg.Processor.MaxCandidatesPerEvent != 3 {
		t.Fatalf("processor defaults = %+v, want rule_based enabled with limits", cfg.Processor)
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

func TestValidateRejectsInvalidAutomationConfig(t *testing.T) {
	cfg := Default()
	cfg.Automation.BatchSize = 0
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid automation config")
	}
}

func TestValidateRejectsInvalidProcessorConfig(t *testing.T) {
	cfg := Default()
	cfg.Processor.Provider = ""
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid processor config")
	}
}
