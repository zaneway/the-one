package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是 memoryd 的最小配置模型。P0 保持默认可启动，embedding 和 retention 默认关闭。
type Config struct {
	Storage   StorageConfig   `yaml:"storage" json:"storage"`
	Server    ServerConfig    `yaml:"server" json:"server"`
	Logging   LoggingConfig   `yaml:"logging" json:"logging"`
	Memory    MemoryConfig    `yaml:"memory" json:"memory"`
	Capture   CaptureConfig   `yaml:"capture" json:"capture"`
	Retrieval RetrievalConfig `yaml:"retrieval" json:"retrieval"`
	Embedding EmbeddingConfig `yaml:"embedding" json:"embedding"`
	Retention RetentionConfig `yaml:"retention" json:"retention"`
}

// StorageConfig 描述 P0 SQLite 后端配置。BusyTimeoutMS 是写锁等待上限，避免请求无限阻塞。
type StorageConfig struct {
	Backend          string `yaml:"backend" json:"backend"`
	Path             string `yaml:"path" json:"path"`
	SQLiteVecEnabled string `yaml:"sqlite_vec_enabled" json:"sqlite_vec_enabled"`
	BusyTimeoutMS    int    `yaml:"busy_timeout_ms" json:"busy_timeout_ms"`
}

// ServerConfig 描述 P0 服务入口配置。当前只支持 stdio。
type ServerConfig struct {
	MCPAddr string `yaml:"mcp_addr" json:"mcp_addr"`
}

// LoggingConfig 描述本地日志级别和格式。日志输出到 stderr，避免污染 stdio 响应。
type LoggingConfig struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
}

// MemoryConfig 保存 P0/P1 默认身份和工作区，后续 scope validator 会复用这些默认值。
type MemoryConfig struct {
	DefaultUserID       string `yaml:"default_user_id" json:"default_user_id"`
	DefaultWorkspace    string `yaml:"default_workspace" json:"default_workspace"`
	MaxContentChars     int    `yaml:"max_content_chars" json:"max_content_chars"`
	MaxEvidenceChars    int    `yaml:"max_evidence_chars" json:"max_evidence_chars"`
	MaxKeywordCount     int    `yaml:"max_keyword_count" json:"max_keyword_count"`
	MaxSalientSpanCount int    `yaml:"max_salient_span_count" json:"max_salient_span_count"`
}

// CaptureConfig 保存 P2 observe 事件捕获的内容边界和默认值。
type CaptureConfig struct {
	RequireSessionForAgentEvents bool   `yaml:"require_session_for_agent_events" json:"require_session_for_agent_events"`
	MaxInputSummaryChars         int    `yaml:"max_input_summary_chars" json:"max_input_summary_chars"`
	MaxOutputSummaryChars        int    `yaml:"max_output_summary_chars" json:"max_output_summary_chars"`
	MaxContentSummaryChars       int    `yaml:"max_content_summary_chars" json:"max_content_summary_chars"`
	MaxSourceRefsChars           int    `yaml:"max_source_refs_chars" json:"max_source_refs_chars"`
	MaxSalientSpanChars          int    `yaml:"max_salient_span_chars" json:"max_salient_span_chars"`
	MaxSalientSpanCount          int    `yaml:"max_salient_span_count" json:"max_salient_span_count"`
	MaxKeywordCount              int    `yaml:"max_keyword_count" json:"max_keyword_count"`
	DefaultAgentType             string `yaml:"default_agent_type" json:"default_agent_type"`
}

// RetrievalConfig 保存在线检索默认值。P0 只暴露 status，P1 检索实现会使用这些限制。
type RetrievalConfig struct {
	DefaultLimit       int `yaml:"default_limit" json:"default_limit"`
	DefaultTokenBudget int `yaml:"default_token_budget" json:"default_token_budget"`
	OnlineTimeoutMS    int `yaml:"online_timeout_ms" json:"online_timeout_ms"`
}

// EmbeddingConfig 描述 embedding provider。默认 none，保证无外部依赖也能启动。
type EmbeddingConfig struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
}

// RetentionConfig 描述 retention job 默认策略。P0 不启动后台 retention job。
type RetentionConfig struct {
	JobEnabled       bool `yaml:"job_enabled" json:"job_enabled"`
	TemporaryTTLDays int  `yaml:"temporary_ttl_days" json:"temporary_ttl_days"`
	ShortTermTTLDays int  `yaml:"short_term_ttl_days" json:"short_term_ttl_days"`
}

// Overrides 表示命令行覆盖项。空值表示不覆盖配置文件和默认值。
type Overrides struct {
	ConfigPath string
	DataDir    string
	DBPath     string
	MCPAddr    string
	LogLevel   string
}

// Default 返回 P0 可直接启动的配置。默认数据库路径位于 $HOME/.memoryd/memory.db。
func Default() Config {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	dataDir := filepath.Join(home, ".memoryd")
	return Config{
		Storage: StorageConfig{
			Backend:          "sqlite",
			Path:             filepath.Join(dataDir, "memory.db"),
			SQLiteVecEnabled: "auto",
			BusyTimeoutMS:    1000,
		},
		Server: ServerConfig{
			MCPAddr: "stdio",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Memory: MemoryConfig{
			DefaultUserID:       "local_default_user",
			DefaultWorkspace:    "local_default_workspace",
			MaxContentChars:     4000,
			MaxEvidenceChars:    1200,
			MaxKeywordCount:     30,
			MaxSalientSpanCount: 10,
		},
		Capture: CaptureConfig{
			RequireSessionForAgentEvents: true,
			MaxInputSummaryChars:         1200,
			MaxOutputSummaryChars:        2000,
			MaxContentSummaryChars:       2000,
			MaxSourceRefsChars:           4000,
			MaxSalientSpanChars:          500,
			MaxSalientSpanCount:          10,
			MaxKeywordCount:              30,
			DefaultAgentType:             "unknown",
		},
		Retrieval: RetrievalConfig{
			DefaultLimit:       10,
			DefaultTokenBudget: 1800,
			OnlineTimeoutMS:    100,
		},
		Embedding: EmbeddingConfig{
			Provider: "none",
			Model:    "",
		},
		Retention: RetentionConfig{
			JobEnabled:       false,
			TemporaryTTLDays: 5,
			ShortTermTTLDays: 90,
		},
	}
}

// Load 按默认值、配置文件、环境变量、命令行的顺序合成配置，并执行基础校验。
func Load(overrides Overrides) (Config, error) {
	cfg := Default()
	configPath := firstNonEmpty(overrides.ConfigPath, os.Getenv("MEMORYD_CONFIG"))
	if configPath != "" {
		if err := loadYAML(configPath, &cfg); err != nil {
			return Config{}, err
		}
	}

	dataDir := os.Getenv("MEMORYD_DATA_DIR")
	dbPath := os.Getenv("MEMORYD_DB_PATH")
	if overrides.DataDir != "" {
		dataDir = overrides.DataDir
	}
	if overrides.DBPath != "" {
		dbPath = overrides.DBPath
	}
	if dataDir != "" && dbPath == "" {
		cfg.Storage.Path = filepath.Join(expandHome(dataDir), "memory.db")
	}
	if dbPath != "" {
		cfg.Storage.Path = expandHome(dbPath)
	}

	if level := firstNonEmpty(overrides.LogLevel, os.Getenv("MEMORYD_LOG_LEVEL")); level != "" {
		cfg.Logging.Level = level
	}
	if addr := firstNonEmpty(overrides.MCPAddr, os.Getenv("MEMORYD_MCP_ADDR")); addr != "" {
		cfg.Server.MCPAddr = addr
	}
	cfg.Storage.Path = expandHome(cfg.Storage.Path)
	return cfg, validate(cfg)
}

func loadYAML(path string, cfg *Config) error {
	data, err := os.ReadFile(expandHome(path))
	if err != nil {
		return fmt.Errorf("CONFIG_INVALID: read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("CONFIG_INVALID: parse config: %w", err)
	}
	return nil
}

func validate(cfg Config) error {
	if cfg.Storage.Backend != "sqlite" {
		return fmt.Errorf("CONFIG_INVALID: unsupported storage backend %q", cfg.Storage.Backend)
	}
	if strings.TrimSpace(cfg.Storage.Path) == "" {
		return errors.New("CONFIG_INVALID: storage.path is required")
	}
	if cfg.Storage.BusyTimeoutMS <= 0 {
		return errors.New("CONFIG_INVALID: storage.busy_timeout_ms must be positive")
	}
	if cfg.Memory.MaxContentChars <= 0 || cfg.Memory.MaxEvidenceChars <= 0 || cfg.Memory.MaxKeywordCount <= 0 || cfg.Memory.MaxSalientSpanCount <= 0 {
		return errors.New("CONFIG_INVALID: memory content limits must be positive")
	}
	if cfg.Capture.MaxInputSummaryChars <= 0 || cfg.Capture.MaxOutputSummaryChars <= 0 || cfg.Capture.MaxContentSummaryChars <= 0 ||
		cfg.Capture.MaxSourceRefsChars <= 0 || cfg.Capture.MaxSalientSpanChars <= 0 || cfg.Capture.MaxSalientSpanCount <= 0 || cfg.Capture.MaxKeywordCount <= 0 {
		return errors.New("CONFIG_INVALID: capture content limits must be positive")
	}
	if strings.TrimSpace(cfg.Capture.DefaultAgentType) == "" {
		return errors.New("CONFIG_INVALID: capture.default_agent_type is required")
	}
	if cfg.Server.MCPAddr == "" {
		return errors.New("CONFIG_INVALID: server.mcp_addr is required")
	}
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("CONFIG_INVALID: unsupported log level %q", cfg.Logging.Level)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
