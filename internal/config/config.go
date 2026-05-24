package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config memoryd 主配置结构体
// memoryd 的最小配置模型，P0 保持默认可启动
// 设计原则：embedding 和 retention 默认关闭，保证无外部依赖也能启动
type Config struct {
	// Storage 存储配置
	// SQLite后端配置，包括数据库路径、WAL模式等
	Storage StorageConfig `yaml:"storage" json:"storage"`

	// Server 服务配置
	// MCP服务入口配置，当前只支持stdio
	Server ServerConfig `yaml:"server" json:"server"`

	// Logging 日志配置
	// 日志级别和格式配置
	Logging LoggingConfig `yaml:"logging" json:"logging"`

	// Memory 记忆配置
	// P0/P1 默认身份和工作区，内容边界限制
	Memory MemoryConfig `yaml:"memory" json:"memory"`

	// Capture 捕获配置
	// P2 observe 事件捕获的内容边界和默认值
	Capture CaptureConfig `yaml:"capture" json:"capture"`

	// Retrieval 检索配置
	// 在线检索默认值，P1检索实现会使用这些限制
	Retrieval RetrievalConfig `yaml:"retrieval" json:"retrieval"`

	// Embedding 嵌入配置
	// embedding provider配置，默认none，保证无外部依赖也能启动
	Embedding EmbeddingConfig `yaml:"embedding" json:"embedding"`

	// CodeIndex 代码索引配置
	// P4 默认 local_basic，只做本地文件/符号轻量解析，不依赖外部服务
	CodeIndex CodeIndexConfig `yaml:"codeindex" json:"codeindex"`

	// DocIndex 文档索引配置
	// P4 默认启用 Markdown snapshot，只保存路径、hash、标题和摘要，不保存完整正文
	DocIndex DocIndexConfig `yaml:"docindex" json:"docindex"`

	// Retention 保留配置
	// retention job 默认策略，P0不启动后台retention job
	Retention RetentionConfig `yaml:"retention" json:"retention"`

	// Processor 处理器配置
	// P3 自动 evidence/candidate 抽取 Provider 和内容上限
	Processor ProcessorConfig `yaml:"processor" json:"processor"`

	// Automation 自动处理配置
	// P3 本地异步 worker 的轮询、批量和重试策略
	Automation AutomationConfig `yaml:"automation" json:"automation"`
}

// StorageConfig 存储配置结构体
// 描述 P0 SQLite 后端配置
type StorageConfig struct {
	// Backend 存储后端
	// 当前只支持sqlite
	Backend string `yaml:"backend" json:"backend"`

	// Path 数据库路径
	// SQLite数据库文件路径，默认$HOME/.memoryd/memory.db
	Path string `yaml:"path" json:"path"`

	// SQLiteVecEnabled sqlite-vec启用状态
	// auto: 自动检测，true: 强制启用，false: 强制禁用
	// sqlite-vec是可选的向量索引增强能力
	SQLiteVecEnabled string `yaml:"sqlite_vec_enabled" json:"sqlite_vec_enabled"`

	// BusyTimeoutMS 忙碌超时（毫秒）
	// 写锁等待上限，避免请求无限阻塞，默认1000ms
	BusyTimeoutMS int `yaml:"busy_timeout_ms" json:"busy_timeout_ms"`
}

// ServerConfig 服务配置结构体
// 描述 P0 服务入口配置
type ServerConfig struct {
	// MCPAddr MCP服务地址
	// 当前只支持stdio，通过标准输入输出与Agent通信
	MCPAddr string `yaml:"mcp_addr" json:"mcp_addr"`
}

// LoggingConfig 日志配置结构体
// 描述本地日志级别和格式
type LoggingConfig struct {
	// Level 日志级别
	// 可选值：debug、info、warn、error
	Level string `yaml:"level" json:"level"`

	// Format 日志格式
	// 可选值：text、json
	// 日志输出到stderr，避免污染stdio响应
	Format string `yaml:"format" json:"format"`
}

// MemoryConfig 记忆配置结构体
// 保存 P0/P1 默认身份和工作区，后续 scope validator 会复用这些默认值
type MemoryConfig struct {
	// DefaultUserID 默认用户ID
	// 用于user_global作用域的默认值，一期是本地个人工具
	DefaultUserID string `yaml:"default_user_id" json:"default_user_id"`

	// DefaultWorkspace 默认工作空间
	// 用于scope过滤的默认值
	DefaultWorkspace string `yaml:"default_workspace" json:"default_workspace"`

	// MaxContentChars 最大内容字符数
	// content字段的长度限制，默认4000
	MaxContentChars int `yaml:"max_content_chars" json:"max_content_chars"`

	// MaxEvidenceChars 最大证据字符数
	// evidence.interpreted_statement的长度限制，默认1200
	MaxEvidenceChars int `yaml:"max_evidence_chars" json:"max_evidence_chars"`

	// MaxKeywordCount 最大关键词数量
	// keywords数组的长度限制，默认30
	MaxKeywordCount int `yaml:"max_keyword_count" json:"max_keyword_count"`

	// MaxSalientSpanCount 最大显著片段数量
	// salient_spans数组的长度限制，默认10
	MaxSalientSpanCount int `yaml:"max_salient_span_count" json:"max_salient_span_count"`
}

// CaptureConfig 捕获配置结构体
// 保存 P2 observe 事件捕获的内容边界和默认值
type CaptureConfig struct {
	// RequireSessionForAgentEvents Agent事件是否要求session
	// 默认true，Agent自动捕获事件必须绑定session
	RequireSessionForAgentEvents bool `yaml:"require_session_for_agent_events" json:"require_session_for_agent_events"`

	// MaxInputSummaryChars 最大输入摘要字符数
	// input_summary字段的长度限制，默认1200
	MaxInputSummaryChars int `yaml:"max_input_summary_chars" json:"max_input_summary_chars"`

	// MaxOutputSummaryChars 最大输出摘要字符数
	// output_summary字段的长度限制，默认2000
	MaxOutputSummaryChars int `yaml:"max_output_summary_chars" json:"max_output_summary_chars"`

	// MaxContentSummaryChars 最大内容摘要字符数
	// content_summary字段的长度限制，默认2000
	MaxContentSummaryChars int `yaml:"max_content_summary_chars" json:"max_content_summary_chars"`

	// MaxSourceRefsChars 最大来源引用字符数
	// source_refs_json字段的长度限制，默认4000
	MaxSourceRefsChars int `yaml:"max_source_refs_chars" json:"max_source_refs_chars"`

	// MaxSalientSpanChars 最大显著片段字符数
	// 单个salient_span的长度限制，默认500
	MaxSalientSpanChars int `yaml:"max_salient_span_chars" json:"max_salient_span_chars"`

	// MaxSalientSpanCount 最大显著片段数量
	// salient_spans数组的长度限制，默认10
	MaxSalientSpanCount int `yaml:"max_salient_span_count" json:"max_salient_span_count"`

	// MaxKeywordCount 最大关键词数量
	// keywords数组的长度限制，默认30
	MaxKeywordCount int `yaml:"max_keyword_count" json:"max_keyword_count"`

	// DefaultAgentType 默认Agent类型
	// 当请求未指定agent_type时使用，默认unknown
	DefaultAgentType string `yaml:"default_agent_type" json:"default_agent_type"`
}

// RetrievalConfig 检索配置结构体
// 保存在线检索默认值，P0 只暴露 status，P1 检索实现会使用这些限制
type RetrievalConfig struct {
	// DefaultLimit 默认结果数量限制
	// memory.search的默认limit，默认10
	DefaultLimit int `yaml:"default_limit" json:"default_limit"`

	// DefaultTokenBudget 默认Token预算
	// memory.context的默认token_budget，默认1800
	DefaultTokenBudget int `yaml:"default_token_budget" json:"default_token_budget"`

	// OnlineTimeoutMS 在线超时（毫秒）
	// 在线检索的超时时间，默认100ms
	// 外部模型调用不能进入该时间预算
	OnlineTimeoutMS int `yaml:"online_timeout_ms" json:"online_timeout_ms"`
}

// EmbeddingConfig 嵌入配置结构体
// 描述 embedding provider，默认 none，保证无外部依赖也能启动
type EmbeddingConfig struct {
	// Provider 嵌入提供者
	// 可选值：none、local、openai、deepseek
	// 默认none，系统仍可运行，检索降级为FTS + metadata + relation
	Provider string `yaml:"provider" json:"provider"`

	// Model 嵌入模型
	// 嵌入模型名称，Provider为none时为空
	Model string `yaml:"model" json:"model"`
}

// CodeIndexConfig 代码索引配置结构体。
// local_basic 只做 repo-relative 文件定位、轻量符号扫描和 code_ref 状态刷新，不构建全量调用图。
type CodeIndexConfig struct {
	// Provider 代码索引提供者，默认 local_basic。
	Provider string `yaml:"provider" json:"provider"`

	// EnableCTags 是否允许调用 ctags。
	// 当前 local_basic 不调用外部 ctags，该字段为后续增强预留。
	EnableCTags bool `yaml:"enable_ctags" json:"enable_ctags"`

	// MaxFileSizeKB 在线解析允许读取的单文件上限，默认 512KB。
	MaxFileSizeKB int `yaml:"max_file_size_kb" json:"max_file_size_kb"`

	// MaxResolveRefs 单次在线解析 code_ref 的最大数量，默认 30。
	MaxResolveRefs int `yaml:"max_resolve_refs" json:"max_resolve_refs"`
}

// DocIndexConfig 文档索引配置结构体。
// 设计约束：Doc Index 只构建 Markdown 文档的结构化快照，不保存完整文档正文。
type DocIndexConfig struct {
	// Enabled 是否启用 Doc Index 在线快照能力。
	Enabled bool `yaml:"enabled" json:"enabled"`

	// MaxDocSizeKB 在线解析允许读取的单文档上限，超过后只记录文件级 hash。
	MaxDocSizeKB int `yaml:"max_doc_size_kb" json:"max_doc_size_kb"`

	// MaxSections 单个文档最多记录的章节数量。
	MaxSections int `yaml:"max_sections" json:"max_sections"`

	// MaxSnapshotsPerDoc 单文档保留快照数量上限，供后续清理策略使用。
	MaxSnapshotsPerDoc int `yaml:"max_snapshots_per_doc" json:"max_snapshots_per_doc"`

	// StoreSectionSummary 是否保存章节摘要；摘要只来自标题路径，不包含正文原文。
	StoreSectionSummary bool `yaml:"store_section_summary" json:"store_section_summary"`
}

// RetentionConfig 保留配置结构体
// 描述 retention job 默认策略，P0 不启动后台 retention job
type RetentionConfig struct {
	// JobEnabled 是否启用保留任务
	// 默认false，P0不启动后台retention job
	JobEnabled bool `yaml:"job_enabled" json:"job_enabled"`

	// TemporaryTTLDays 临时记忆生存天数
	// temporary层级的默认保留天数，默认5天
	TemporaryTTLDays int `yaml:"temporary_ttl_days" json:"temporary_ttl_days"`

	// ShortTermTTLDays 短期记忆生存天数
	// short_term层级的默认保留天数，默认90天
	ShortTermTTLDays int `yaml:"short_term_ttl_days" json:"short_term_ttl_days"`
}

// ProcessorConfig 自动记忆处理器配置结构体
// 控制 P3 使用哪个 Provider，以及 observe 后是否自动入队处理
type ProcessorConfig struct {
	// Provider 处理器提供者
	// P3 默认 rule_based；none 表示只保存 raw_event，不生成 evidence/candidate
	Provider string `yaml:"provider" json:"provider"`

	// EnableAutoProcessing 是否启用自动处理
	// false 时 memory.observe 只写 raw_event，不 enqueue extract_evidence
	EnableAutoProcessing bool `yaml:"enable_auto_processing" json:"enable_auto_processing"`

	// MaxRelatedEvents Provider 抽取时读取的近邻事件上限
	MaxRelatedEvents int `yaml:"max_related_events" json:"max_related_events"`

	// MaxCandidatesPerEvent 单个事件最多生成候选数量
	MaxCandidatesPerEvent int `yaml:"max_candidates_per_event" json:"max_candidates_per_event"`
}

// AutomationConfig 自动处理配置结构体
// 控制 P3 本地 worker 是否启动，以及每轮领取任务和失败重试策略
type AutomationConfig struct {
	// WorkerEnabled 是否启用本地异步 worker
	// 默认true，serve模式下由后续 app 集成决定是否启动
	WorkerEnabled bool `yaml:"worker_enabled" json:"worker_enabled"`

	// PollIntervalMS 空轮询间隔（毫秒）
	// pending job 为空时 worker 的等待时间，默认1000ms
	PollIntervalMS int `yaml:"poll_interval_ms" json:"poll_interval_ms"`

	// BatchSize 每轮最多领取任务数
	// 控制单进程 worker 的批处理上限，默认10
	BatchSize int `yaml:"batch_size" json:"batch_size"`

	// MaxAttempts 最大尝试次数
	// 用于后续 worker/app 集成时设置 job 默认重试上限，默认3
	MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`

	// RetryBaseDelayMS 重试基础延迟（毫秒）
	// worker 按指数退避计算下一次运行时间，默认1000ms
	RetryBaseDelayMS int `yaml:"retry_base_delay_ms" json:"retry_base_delay_ms"`

	// RunningTimeoutMS running任务恢复超时（毫秒）
	// 进程崩溃遗留的running job超过该时间后可恢复为pending或failed
	RunningTimeoutMS int `yaml:"running_timeout_ms" json:"running_timeout_ms"`
}

// Overrides 命令行覆盖项结构体
// 表示命令行覆盖项，空值表示不覆盖配置文件和默认值
type Overrides struct {
	// ConfigPath 配置文件路径
	ConfigPath string

	// DataDir 数据目录
	DataDir string

	// DBPath 数据库路径
	DBPath string

	// MCPAddr MCP服务地址
	MCPAddr string

	// LogLevel 日志级别
	LogLevel string
}

// Default 返回 P0 可直接启动的默认配置
// 默认数据库路径位于 $HOME/.memoryd/memory.db
// 设计原则：默认配置能直接启动，避免用户先理解完整系统才能使用
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
		CodeIndex: CodeIndexConfig{
			Provider:       "local_basic",
			EnableCTags:    false,
			MaxFileSizeKB:  512,
			MaxResolveRefs: 30,
		},
		DocIndex: DocIndexConfig{
			Enabled:             true,
			MaxDocSizeKB:        512,
			MaxSections:         200,
			MaxSnapshotsPerDoc:  10,
			StoreSectionSummary: true,
		},
		Retention: RetentionConfig{
			JobEnabled:       false,
			TemporaryTTLDays: 5,
			ShortTermTTLDays: 90,
		},
		Processor: ProcessorConfig{
			Provider:              "rule_based",
			EnableAutoProcessing:  true,
			MaxRelatedEvents:      20,
			MaxCandidatesPerEvent: 3,
		},
		Automation: AutomationConfig{
			WorkerEnabled:    true,
			PollIntervalMS:   1000,
			BatchSize:        10,
			MaxAttempts:      3,
			RetryBaseDelayMS: 1000,
			RunningTimeoutMS: 300000,
		},
	}
}

// Load 加载配置
// 按默认值、配置文件、环境变量、命令行的顺序合成配置，并执行基础校验
// 优先级：命令行 > 环境变量 > 配置文件 > 默认值
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
	if strings.TrimSpace(cfg.Processor.Provider) == "" || cfg.Processor.MaxRelatedEvents <= 0 || cfg.Processor.MaxCandidatesPerEvent <= 0 {
		return errors.New("CONFIG_INVALID: processor config values must be positive and provider is required")
	}
	if strings.TrimSpace(cfg.CodeIndex.Provider) == "" || cfg.CodeIndex.MaxFileSizeKB <= 0 || cfg.CodeIndex.MaxResolveRefs <= 0 {
		return errors.New("CONFIG_INVALID: codeindex config values must be positive and provider is required")
	}
	if cfg.CodeIndex.Provider != "local_basic" {
		return fmt.Errorf("CONFIG_INVALID: unsupported codeindex provider %q", cfg.CodeIndex.Provider)
	}
	if cfg.DocIndex.MaxDocSizeKB <= 0 || cfg.DocIndex.MaxSections <= 0 || cfg.DocIndex.MaxSnapshotsPerDoc <= 0 {
		return errors.New("CONFIG_INVALID: docindex limits must be positive")
	}
	if cfg.Automation.PollIntervalMS <= 0 || cfg.Automation.BatchSize <= 0 || cfg.Automation.MaxAttempts <= 0 ||
		cfg.Automation.RetryBaseDelayMS <= 0 || cfg.Automation.RunningTimeoutMS <= 0 {
		return errors.New("CONFIG_INVALID: automation worker limits must be positive")
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
