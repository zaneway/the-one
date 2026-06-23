package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if cfg.Logging.Path == "" {
		t.Fatal("logging path = empty, want default log file path")
	}
	if !strings.HasSuffix(cfg.Logging.Path, filepath.Join("logs", "theone.log")) {
		t.Fatalf("logging path = %q, want suffix logs/theone.log", cfg.Logging.Path)
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
	if cfg.Capture.MaxRawPayloadChars != 1048576 {
		t.Fatalf("capture max raw payload chars = %d, want 1048576", cfg.Capture.MaxRawPayloadChars)
	}
	if cfg.Automation.PollIntervalMS != 1000 || cfg.Automation.BatchSize != 10 || cfg.Automation.MaxAttempts != 3 || cfg.Automation.RetryBaseDelayMS != 1000 {
		t.Fatalf("automation defaults = %+v, want poll=1000 batch=10 attempts=3 retry=1000", cfg.Automation)
	}
	if cfg.Automation.RunningTimeoutMS != 300000 {
		t.Fatalf("automation running timeout = %d, want 300000", cfg.Automation.RunningTimeoutMS)
	}
	if cfg.Adapter.PromptCacheUserSummaryMaxChars != 3000 {
		t.Fatalf("adapter prompt cache user summary max chars = %d, want 3000", cfg.Adapter.PromptCacheUserSummaryMaxChars)
	}
	if cfg.Processor.Provider != "rule_based" || cfg.Processor.MaxRelatedEvents != 20 || cfg.Processor.MaxCandidatesPerEvent != 3 {
		t.Fatalf("processor defaults = %+v, want rule_based enabled with limits", cfg.Processor)
	}
	if cfg.Processor.OpenAI.Model != "gpt-5-mini" || cfg.Processor.OpenAI.APIKey != "" ||
		cfg.Processor.OpenAI.TimeoutMS != 30000 || cfg.Processor.OpenAI.MaxOutputTokens != 1200 {
		t.Fatalf("processor openai defaults = %+v, want bounded gpt-5-mini config", cfg.Processor.OpenAI)
	}
	if !strings.Contains(cfg.Processor.OpenAI.SemanticEnhancePrompt, "content_summary") ||
		!strings.Contains(cfg.Processor.OpenAI.SemanticEnhancePrompt, "semantic_equivalent=false") ||
		!strings.Contains(cfg.Processor.OpenAI.SemanticEnhancePrompt, "示例：") {
		t.Fatalf("semantic enhance prompt = %q, want project-specific safety instructions", cfg.Processor.OpenAI.SemanticEnhancePrompt)
	}
	if !strings.Contains(cfg.Processor.OpenAI.ExtractEvidencePrompt, "同时产出 evidence") ||
		!strings.Contains(cfg.Processor.OpenAI.ExtractEvidencePrompt, `{"evidence":[]}`) ||
		!strings.Contains(cfg.Processor.OpenAI.ExtractEvidencePrompt, "candidate_reason") ||
		!strings.Contains(cfg.Processor.OpenAI.ExtractEvidencePrompt, "user_correction") ||
		!strings.Contains(cfg.Processor.OpenAI.ExtractEvidencePrompt, "示例：") {
		t.Fatalf("extract evidence prompt = %q, want combined evidence/candidate instructions", cfg.Processor.OpenAI.ExtractEvidencePrompt)
	}
	if strings.Contains(cfg.Processor.OpenAI.ExtractEvidencePrompt, "字段：") || len([]rune(cfg.Processor.OpenAI.ExtractEvidencePrompt)) > 4500 {
		t.Fatalf("extract evidence prompt = %q, want compact example-driven instructions", cfg.Processor.OpenAI.ExtractEvidencePrompt)
	}
	if cfg.Processor.OpenAI.GenerateCandidatesPrompt != "" {
		t.Fatalf("generate candidates prompt = %q, want empty deprecated default", cfg.Processor.OpenAI.GenerateCandidatesPrompt)
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
		t.Fatalf("retrieval retrieval defaults = %+v, want enabled bounded retrieval", cfg.Retrieval)
	}
	if cfg.Embedding.QueryCacheSize != 256 || cfg.Embedding.OnlineQueryEmbeddingEnabled || cfg.Embedding.MemoryEmbeddingEnabled {
		t.Fatalf("embedding retrieval defaults = %+v, want cache=256 and online disabled", cfg.Embedding)
	}
	if cfg.VectorIndex.Backend != "none" || cfg.VectorIndex.SQLiteVecEnabled != "auto" {
		t.Fatalf("vector index defaults = %+v, want none/auto", cfg.VectorIndex)
	}
	if cfg.AccessLog.RetentionDaysRetrieved != 30 || cfg.AccessLog.RetentionDaysInjected != 180 || !cfg.AccessLog.AggregateBeforeCleanup {
		t.Fatalf("access log defaults = %+v, want retrieved=30 injected=180 aggregate=true", cfg.AccessLog)
	}
}

func TestLoadDataDirSyncsLogPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "project", ".theone-data")
	cfg, err := Load(Overrides{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantDB := filepath.Join(dataDir, "memory.db")
	if cfg.Storage.Path != wantDB {
		t.Fatalf("db path = %q, want %q", cfg.Storage.Path, wantDB)
	}
	wantLog := filepath.Join(dataDir, "logs", "theone.log")
	if cfg.Logging.Path != wantLog {
		t.Fatalf("log path = %q, want %q", cfg.Logging.Path, wantLog)
	}
}

func TestLoadYAMLStoragePathSyncsLogPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "yaml-data")
	path := filepath.Join(t.TempDir(), "theone.yaml")
	data := []byte("storage:\n  path: \"" + filepath.Join(dataDir, "memory.db") + "\"\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(Overrides{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantLog := filepath.Join(dataDir, "logs", "theone.log")
	if cfg.Logging.Path != wantLog {
		t.Fatalf("log path = %q, want %q", cfg.Logging.Path, wantLog)
	}
}

func TestLoadKeepsExplicitYAMLLogPath(t *testing.T) {
	customLog := filepath.Join(t.TempDir(), "custom.log")
	path := filepath.Join(t.TempDir(), "theone.yaml")
	data := []byte("logging:\n  path: \"" + customLog + "\"\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(Overrides{ConfigPath: path, DataDir: filepath.Join(t.TempDir(), "other-data")})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Logging.Path != customLog {
		t.Fatalf("log path = %q, want explicit yaml path %q", cfg.Logging.Path, customLog)
	}
}

func TestLoadEnvAndOverrides(t *testing.T) {
	t.Setenv("THEONE_DATA_DIR", filepath.Join(t.TempDir(), "from-env"))
	t.Setenv("THEONE_LOG_LEVEL", "debug")
	logPath := filepath.Join(t.TempDir(), "logs", "custom.log")
	t.Setenv("THEONE_LOG_PATH", logPath)

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
	if cfg.Logging.Path != logPath {
		t.Fatalf("log path = %q, want %q", cfg.Logging.Path, logPath)
	}
}

func TestLoadAdapterPromptCacheUserSummaryLimitFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theone.yaml")
	data := []byte("adapter:\n  prompt_cache_user_summary_max_chars: 42\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(Overrides{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Adapter.PromptCacheUserSummaryMaxChars != 42 {
		t.Fatalf("adapter prompt cache user summary max chars = %d, want 42", cfg.Adapter.PromptCacheUserSummaryMaxChars)
	}
}

func TestLoadOpenAISemanticEnhancePromptFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theone.yaml")
	data := []byte("processor:\n  openai:\n    semantic_enhance_prompt: |\n      custom semantic prompt\n      keep project taxonomy\n    extract_evidence_prompt: custom evidence prompt\n    generate_candidates_prompt: custom candidate prompt\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(Overrides{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !strings.Contains(cfg.Processor.OpenAI.SemanticEnhancePrompt, "custom semantic prompt") {
		t.Fatalf("semantic enhance prompt = %q, want YAML override", cfg.Processor.OpenAI.SemanticEnhancePrompt)
	}
	if cfg.Processor.OpenAI.ExtractEvidencePrompt != "custom evidence prompt" {
		t.Fatalf("extract evidence prompt = %q, want YAML override", cfg.Processor.OpenAI.ExtractEvidencePrompt)
	}
	if cfg.Processor.OpenAI.GenerateCandidatesPrompt != "custom candidate prompt" {
		t.Fatalf("generate candidates prompt = %q, want YAML override", cfg.Processor.OpenAI.GenerateCandidatesPrompt)
	}
}

func TestLoadOpenAIAPIKeyFromYAMLAndEnvOverride(t *testing.T) {
	t.Setenv("THEONE_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	path := filepath.Join(t.TempDir(), "theone.yaml")
	data := []byte("processor:\n  openai:\n    api_key: config-key\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(Overrides{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Processor.OpenAI.APIKey != "config-key" {
		t.Fatalf("api key = %q, want config key", cfg.Processor.OpenAI.APIKey)
	}

	t.Setenv("THEONE_OPENAI_API_KEY", "env-key")
	cfg, err = Load(Overrides{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load() with env override error = %v", err)
	}
	if cfg.Processor.OpenAI.APIKey != "env-key" {
		t.Fatalf("api key = %q, want env override", cfg.Processor.OpenAI.APIKey)
	}
}

func TestLoadOpenAIAPIKeyFromStandardEnv(t *testing.T) {
	t.Setenv("THEONE_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "standard-env-key")

	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Processor.OpenAI.APIKey != "standard-env-key" {
		t.Fatalf("api key = %q, want OPENAI_API_KEY override", cfg.Processor.OpenAI.APIKey)
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

func TestValidateRejectsInvalidAdapterPromptCacheUserSummaryLimit(t *testing.T) {
	cfg := Default()
	cfg.Adapter.PromptCacheUserSummaryMaxChars = 0
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid adapter prompt cache limit")
	}
}

func TestValidateRejectsInvalidCaptureRawPayloadLimit(t *testing.T) {
	cfg := Default()
	cfg.Capture.MaxRawPayloadChars = 0
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid capture raw payload limit")
	}
}

func TestValidateAllowsDisabledInputOutputSummaryLimits(t *testing.T) {
	cfg := Default()
	cfg.Capture.MaxInputSummaryChars = 0
	cfg.Capture.MaxOutputSummaryChars = 0
	if err := validate(cfg); err != nil {
		t.Fatalf("validate() error = %v, want nil for deprecated input/output limits", err)
	}
}

func TestValidateRejectsInvalidProcessorConfig(t *testing.T) {
	cfg := Default()
	cfg.Processor.Provider = ""
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid processor config")
	}
}

func TestValidateRejectsProcessorProviderNone(t *testing.T) {
	cfg := Default()
	cfg.Processor.Provider = "none"
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want unsupported processor provider")
	}
}

func TestValidateAllowsOpenAIProcessorProvider(t *testing.T) {
	cfg := Default()
	cfg.Processor.Provider = "openai"
	if err := validate(cfg); err != nil {
		t.Fatalf("validate() error = %v, want openai processor provider allowed", err)
	}
}

func TestValidateRejectsInvalidOpenAIProcessorConfig(t *testing.T) {
	cfg := Default()
	cfg.Processor.Provider = "openai"
	cfg.Processor.OpenAI.Model = ""
	if err := validate(cfg); err == nil {
		t.Fatal("validate() error = nil, want invalid openai processor config")
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
