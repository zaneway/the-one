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
	if cfg.CodeIndex.Provider != "local_basic" || cfg.CodeIndex.MaxFileSizeKB != 512 || cfg.CodeIndex.MaxResolveRefs != 30 {
		t.Fatalf("codeindex defaults = %+v, want local_basic with bounded local resolver", cfg.CodeIndex)
	}
	if !cfg.DocIndex.Enabled || cfg.DocIndex.MaxDocSizeKB != 512 || cfg.DocIndex.MaxSections != 200 || cfg.DocIndex.MaxSnapshotsPerDoc != 10 {
		t.Fatalf("docindex defaults = %+v, want enabled bounded markdown snapshot", cfg.DocIndex)
	}
	if !cfg.Retrieval.EnableTrace || !cfg.Retrieval.EnableAccessLog || !cfg.Retrieval.EnableRelationExpansion ||
		!cfg.Retrieval.EnableCodeRefResolution || !cfg.Retrieval.EnableDocIndex ||
		cfg.Retrieval.MaxRelationExpansion != 20 || cfg.Retrieval.MaxCandidatesBeforeRerank != 80 {
		t.Fatalf("retrieval P4 defaults = %+v, want enabled bounded retrieval", cfg.Retrieval)
	}
	if cfg.Embedding.QueryCacheSize != 256 || cfg.Embedding.OnlineQueryEmbeddingEnabled {
		t.Fatalf("embedding P4 defaults = %+v, want cache=256 and online disabled", cfg.Embedding)
	}
	if cfg.VectorIndex.Backend != "none" || cfg.VectorIndex.SQLiteVecEnabled != "auto" {
		t.Fatalf("vector index defaults = %+v, want none/auto", cfg.VectorIndex)
	}
	if cfg.AccessLog.RetentionDaysRetrieved != 30 || cfg.AccessLog.RetentionDaysInjected != 180 || !cfg.AccessLog.AggregateBeforeCleanup {
		t.Fatalf("access log defaults = %+v, want retrieved=30 injected=180 aggregate=true", cfg.AccessLog)
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

func TestValidateRejectsInvalidCodeIndexConfig(t *testing.T) {
	cfg := Default()
	cfg.CodeIndex.Provider = "remote_lsp"
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid codeindex provider")
	}
}

func TestValidateAllowsDisabledCodeIndexProvider(t *testing.T) {
	cfg := Default()
	cfg.CodeIndex.Provider = "none"
	if err := validate(cfg); err != nil {
		t.Fatalf("validate() error = %v, want provider=none allowed", err)
	}
}

func TestValidateRejectsInvalidDocIndexConfig(t *testing.T) {
	cfg := Default()
	cfg.DocIndex.MaxSections = 0
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid docindex config")
	}
}
