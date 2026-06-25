package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zaneway/theone/internal/prompts"
)

// Config theone 主配置结构体
// theone 的最小配置模型，保持默认可启动
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
	// 默认身份和工作区，内容边界限制
	Memory MemoryConfig `yaml:"memory" json:"memory"`

	// Capture 捕获配置
	// observe 事件捕获的内容边界和默认值
	Capture CaptureConfig `yaml:"capture" json:"capture"`

	// Adapter 接入层配置（ingest / ExpandMode）
	Adapter AdapterConfig `yaml:"adapter" json:"adapter"`

	// Retrieval 检索配置
	// 在线检索默认值，检索实现会使用这些限制
	Retrieval RetrievalConfig `yaml:"retrieval" json:"retrieval"`

	// Embedding 嵌入配置
	// embedding provider配置，默认none，保证无外部依赖也能启动
	Embedding EmbeddingConfig `yaml:"embedding" json:"embedding"`

	// CodeIndex 代码索引配置
	// 默认 local_basic，只做本地文件/符号轻量解析，不依赖外部服务
	CodeIndex CodeIndexConfig `yaml:"codeindex" json:"codeindex"`

	// DocIndex 文档索引配置
	// 默认启用 Markdown snapshot，只保存路径、hash、标题和摘要，不保存完整正文
	DocIndex DocIndexConfig `yaml:"docindex" json:"docindex"`

	// VectorIndex 向量索引配置
	// 默认 none，向量能力作为可选增强，不影响 FTS + metadata + relation 基础路径
	VectorIndex VectorIndexConfig `yaml:"vector_index" json:"vector_index"`

	// AccessLog 访问反馈日志配置
	// 控制 retrieved/injected 明细保留周期和清理前是否需要聚合
	AccessLog AccessLogConfig `yaml:"access_log" json:"access_log"`

	// Retention 保留配置
	// retention job 默认策略，默认不启动后台 retention job
	Retention RetentionConfig `yaml:"retention" json:"retention"`

	// Processor 处理器配置
	// 自动 evidence/candidate 抽取 Provider 和内容上限
	Processor ProcessorConfig `yaml:"processor" json:"processor"`

	// Automation 自动处理配置
	// 本地异步 worker 的轮询、批量和重试策略
	Automation AutomationConfig `yaml:"automation" json:"automation"`

	// Dream Obsidian 只读增量导出配置
	// 默认关闭；启用后将 SQLite 记忆离线投影为可配置目录结构的 Markdown Vault。
	Dream DreamConfig `yaml:"dream" json:"dream"`
}

// StorageConfig 存储配置结构体
// 描述 SQLite 后端配置
type StorageConfig struct {
	// Backend 存储后端
	// 当前只支持sqlite
	Backend string `yaml:"backend" json:"backend"`

	// Path 数据库路径
	// SQLite数据库文件路径，默认$HOME/.theone/memory.db
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
// 描述服务入口配置
type ServerConfig struct {
	// MCPAddr MCP服务地址
	// 当前只支持stdio，通过标准输入输出与Agent通信
	MCPAddr string `yaml:"mcp_addr" json:"mcp_addr"`
}

// LoggingConfig 日志配置结构体
// 描述本地日志级别、格式和落盘路径
// 设计约束：默认同时输出到 stderr 和日志文件，既保留本地排障体验，也避免日志只存在于终端会话。
type LoggingConfig struct {
	// Level 日志级别
	// 可选值：debug、info、warn、error
	Level string `yaml:"level" json:"level"`

	// Format 日志格式
	// 可选值：text、json
	Format string `yaml:"format" json:"format"`

	// Path 日志文件路径
	// 支持 ~ 展开；默认写入 ~/.theone/logs/theone.log
	Path string `yaml:"path" json:"path"`
}

// MemoryConfig 记忆配置结构体
// 保存默认身份和工作区，后续 scope validator 会复用这些默认值
type MemoryConfig struct {
	// DefaultUserID 默认用户ID
	// 用于user_global作用域的默认值，一期是本地个人工具
	DefaultUserID string `yaml:"default_user_id" json:"default_user_id"`

	// DefaultWorkspace 默认工作空间
	// 用于scope过滤的默认值
	DefaultWorkspace string `yaml:"default_workspace" json:"default_workspace"`

	// MaxContentChars 最大内容字符数
	// content字段的长度限制，默认20000
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

// AdapterConfig 接入层（ingest / SessionBinder / ExpandMode）配置。
type AdapterConfig struct {
	// ExpandMode 事件展开模式：legacy | v2
	ExpandMode string `yaml:"expand_mode" json:"expand_mode"`

	// AtomicStripTurnFields v2 下 turn.completed 误带 tool/file 时剥离（dogfood，非默认验收路径）
	AtomicStripTurnFields bool `yaml:"atomic_strip_turn_fields" json:"atomic_strip_turn_fields"`

	// PrefetchTimeoutMS prefetch-context 调用 memory.context 超时（毫秒）
	PrefetchTimeoutMS int `yaml:"prefetch_timeout_ms" json:"prefetch_timeout_ms"`

	// MaxInjectChars 注入 Markdown 最大字符数
	MaxInjectChars int `yaml:"max_inject_chars" json:"max_inject_chars"`

	// PromptCacheUserSummaryMaxChars beforeSubmitPrompt 本地 prompt-cache 用户摘要最大字符数
	PromptCacheUserSummaryMaxChars int `yaml:"prompt_cache_user_summary_max_chars" json:"prompt_cache_user_summary_max_chars"`

	// SuppressRawEventTypes ingest 平面不写 raw_event 的事件类型列表。
	// 省略时使用内置默认；显式配置 [] 表示不抑制任何事件类型。
	SuppressRawEventTypes []string `yaml:"suppress_raw_event_types,omitempty" json:"suppress_raw_event_types,omitempty"`
}

// CaptureConfig 捕获配置结构体
// 保存 observe 事件捕获的内容边界和默认值
type CaptureConfig struct {
	// RequireSessionForAgentEvents Agent事件是否要求session
	// 默认true，Agent自动捕获事件必须绑定session
	RequireSessionForAgentEvents bool `yaml:"require_session_for_agent_events" json:"require_session_for_agent_events"`

	// MaxInputSummaryChars 兼容旧配置项
	// input_summary 不再限制长度，该字段仅保留用于读取旧配置
	MaxInputSummaryChars int `yaml:"max_input_summary_chars" json:"max_input_summary_chars"`

	// MaxOutputSummaryChars 兼容旧配置项
	// output_summary 不再限制长度，该字段仅保留用于读取旧配置
	MaxOutputSummaryChars int `yaml:"max_output_summary_chars" json:"max_output_summary_chars"`

	// MaxContentSummaryChars 最大内容摘要字符数
	// content_summary字段的长度限制，默认6000
	MaxContentSummaryChars int `yaml:"max_content_summary_chars" json:"max_content_summary_chars"`

	// MaxSourceRefsChars 最大来源引用字符数
	// source_refs_json字段的长度限制，默认4000
	MaxSourceRefsChars int `yaml:"max_source_refs_chars" json:"max_source_refs_chars"`

	// MaxRawPayloadChars 最大原始载荷字符数
	// raw_payload_json字段的长度限制，默认1MiB，尽量保留原始事实
	MaxRawPayloadChars int `yaml:"max_raw_payload_chars" json:"max_raw_payload_chars"`

	// MaxSalientSpanChars 最大显著片段字符数
	// 单个salient_span的长度限制，默认500
	MaxSalientSpanChars int `yaml:"max_salient_span_chars" json:"max_salient_span_chars"`

	// MaxSalientSpanCount 最大显著片段数量
	// salient_spans数组的长度限制，默认10
	MaxSalientSpanCount int `yaml:"max_salient_span_count" json:"max_salient_span_count"`

	// MaxKeywordCount 最大关键词数量
	// keywords数组的长度限制，默认30
	MaxKeywordCount int `yaml:"max_keyword_count" json:"max_keyword_count"`

	// SuppressRawEventTypes 抑制写入 raw_event 的事件类型列表。
	// nil 时 fallback 到 adapter.suppress_raw_event_types，再 fallback 到内置默认；显式 [] 表示不抑制。
	SuppressRawEventTypes []string `yaml:"suppress_raw_event_types" json:"suppress_raw_event_types"`

	// DefaultAgentType 默认Agent类型
	// 当请求未指定agent_type时使用，默认unknown
	DefaultAgentType string `yaml:"default_agent_type" json:"default_agent_type"`
}

// RetrievalConfig 检索配置结构体
// 保存在线检索默认值，status/检索实现会使用这些限制
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

	// MaxRelationExpansion 单次 relation expansion 上限，默认 20。
	MaxRelationExpansion int `yaml:"max_relation_expansion" json:"max_relation_expansion"`

	// MaxCandidatesBeforeRerank rerank 前候选上限，默认 80。
	MaxCandidatesBeforeRerank int `yaml:"max_candidates_before_rerank" json:"max_candidates_before_rerank"`

	// EnableTrace 是否写入 retrieval_trace，默认 true。
	EnableTrace bool `yaml:"enable_trace" json:"enable_trace"`

	// EnableAccessLog 是否写入 memory_access_log，默认 true。
	EnableAccessLog bool `yaml:"enable_access_log" json:"enable_access_log"`

	// EnableRelationExpansion 是否启用一跳 relation expansion，默认 true。
	EnableRelationExpansion bool `yaml:"enable_relation_expansion" json:"enable_relation_expansion"`

	// EnableCodeRefResolution 是否在线解析 code_ref，默认 true。
	EnableCodeRefResolution bool `yaml:"enable_code_ref_resolution" json:"enable_code_ref_resolution"`

	// EnableDocIndex 是否在架构复查任务中启用 Doc Index strategy，默认 true。
	EnableDocIndex bool `yaml:"enable_doc_index" json:"enable_doc_index"`
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

	// QueryCacheSize 查询 embedding 进程内缓存容量，默认 256。
	QueryCacheSize int `yaml:"query_cache_size" json:"query_cache_size"`

	// OnlineQueryEmbeddingEnabled 是否允许在线生成 query embedding，默认 false。
	OnlineQueryEmbeddingEnabled bool `yaml:"online_query_embedding_enabled" json:"online_query_embedding_enabled"`

	// MemoryEmbeddingEnabled 是否允许异步生成 memory embedding(K)，默认 false。
	MemoryEmbeddingEnabled bool `yaml:"memory_embedding_enabled" json:"memory_embedding_enabled"`
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

// VectorIndexConfig 向量索引配置结构体。
// backend=none 是默认路径；sqlite_vec 只在后续可选增强中启用。
type VectorIndexConfig struct {
	// Backend 向量索引后端，可选 none、blob、sqlite_vec。
	Backend string `yaml:"backend" json:"backend"`

	// SQLiteVecEnabled sqlite-vec 能力开关，auto/true/false。
	// 为空时兼容回退到 storage.sqlite_vec_enabled。
	SQLiteVecEnabled string `yaml:"sqlite_vec_enabled" json:"sqlite_vec_enabled"`
}

// AccessLogConfig 访问反馈日志配置结构体。
// 清理任务只删除低价值明细，不影响在线检索结果。
type AccessLogConfig struct {
	// RetentionDaysRetrieved retrieved 明细保留天数，默认 30。
	RetentionDaysRetrieved int `yaml:"retention_days_retrieved" json:"retention_days_retrieved"`

	// RetentionDaysInjected injected 明细保留天数，默认 180。
	RetentionDaysInjected int `yaml:"retention_days_injected" json:"retention_days_injected"`

	// AggregateBeforeCleanup 清理前是否要求聚合，默认 true；当前仅保留配置语义。
	AggregateBeforeCleanup bool `yaml:"aggregate_before_cleanup" json:"aggregate_before_cleanup"`
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
// 描述 retention job 默认策略，默认启动轻量后台刷新。
type RetentionConfig struct {
	// JobEnabled 是否启用保留任务
	// 默认 true，serve 模式周期性消费 access log 并刷新记忆强化字段。
	JobEnabled bool `yaml:"job_enabled" json:"job_enabled"`

	// JobIntervalMS 保留任务执行间隔（毫秒）
	// 默认 1 分钟；启用 job 后按该间隔周期触发遗忘清理和分数重算。
	JobIntervalMS int `yaml:"job_interval_ms" json:"job_interval_ms"`

	// TemporaryTTLDays 临时记忆生存天数
	// temporary层级的默认保留天数，默认5天
	TemporaryTTLDays int `yaml:"temporary_ttl_days" json:"temporary_ttl_days"`

	// ShortTermTTLDays 短期记忆生存天数
	// short_term层级的默认保留天数，默认90天
	ShortTermTTLDays int `yaml:"short_term_ttl_days" json:"short_term_ttl_days"`
}

// ProcessorConfig 自动记忆处理器配置结构体
// 控制 rule_based 抽取的近邻事件与候选数量上限
type ProcessorConfig struct {
	// Provider 处理器提供者，支持 rule_based、openai（二选一，互斥）
	// rule_based：ExtractEvidence + GenerateCandidates 均本地规则
	// openai：ExtractEvidence + GenerateCandidates 均调外部模型
	Provider string `yaml:"provider" json:"provider"`

	// MaxRelatedEvents 已废弃：外部模型只接收当前事件正文，不再读取近邻事件。
	MaxRelatedEvents int `yaml:"max_related_events" json:"max_related_events"`

	// MaxCandidatesPerEvent 单个事件最多生成候选数量
	MaxCandidatesPerEvent int `yaml:"max_candidates_per_event" json:"max_candidates_per_event"`

	// OpenAI OpenAI provider 配置；provider=openai 时生效
	OpenAI OpenAIProcessorConfig `yaml:"openai" json:"openai"`
}

// OpenAIProcessorConfig 描述外部 OpenAI 兼容模型处理器配置。
// APIKey 支持配置文件直填；环境变量 THEONE_OPENAI_API_KEY 或 OPENAI_API_KEY 非空时会覆盖配置文件值。
type OpenAIProcessorConfig struct {
	// Model Chat Completions API 使用的模型 ID
	Model string `yaml:"model" json:"model"`

	// BaseURL 可选 API base URL；为空时使用 openai-go 默认值
	BaseURL string `yaml:"base_url" json:"base_url"`

	// APIKey API key 明文；生产环境建议通过环境变量覆盖，避免提交到版本库
	APIKey string `yaml:"api_key" json:"api_key"`

	// TimeoutMS 单次外部模型请求超时（毫秒）
	TimeoutMS int `yaml:"timeout_ms" json:"timeout_ms"`

	// MaxOutputTokens 单次结构化输出上限
	MaxOutputTokens int `yaml:"max_output_tokens" json:"max_output_tokens"`

	// ExtractEvidencePrompt OpenAI raw_event 联合处理提示词。
	// provider=openai 时自动链路用一次模型调用同时产出 evidence 与 memory candidate。
	ExtractEvidencePrompt string `yaml:"extract_evidence_prompt" json:"extract_evidence_prompt"`

	// GenerateCandidatesPrompt 已废弃，仅保留兼容旧配置和直接调用 GenerateCandidates 的测试/工具。
	// provider=openai 的自动 raw_event 链路不再读取该字段。
	GenerateCandidatesPrompt string `yaml:"generate_candidates_prompt" json:"generate_candidates_prompt,omitempty"`

	// SemanticEnhancePrompt 旧版 observe 语义等价简化提示词。
	// capture 主链路不再使用；仅保留给兼容 provider EnhanceObserve 能力。
	SemanticEnhancePrompt string `yaml:"semantic_enhance_prompt" json:"semantic_enhance_prompt"`
}

// AutomationConfig 自动处理配置结构体
// 控制 本地 worker 轮询间隔、批大小与失败重试策略（serve 模式始终启动 worker）
type AutomationConfig struct {
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

// DreamConfig 描述 Obsidian Dream 只读导出能力。
// 设计约束：Dream 是离线投影，不参与 memory.search/context 在线路径。
type DreamConfig struct {
	Enabled   bool                 `yaml:"enabled" json:"enabled"`
	Vault     DreamVaultConfig     `yaml:"vault" json:"vault"`
	Scheduler DreamSchedulerConfig `yaml:"scheduler" json:"scheduler"`
	Curation  DreamCurationConfig  `yaml:"curation" json:"curation"`
}

// DreamVaultConfig 描述 Vault 根目录和可配置目录布局。
type DreamVaultConfig struct {
	Root           string               `yaml:"root" json:"root"`
	SystemDir      string               `yaml:"system_dir" json:"system_dir"`
	Directories    DreamDirectoryConfig `yaml:"directories" json:"directories"`
	MemoryTypeDirs map[string]string    `yaml:"memory_type_dirs" json:"memory_type_dirs"`
	TopicDirs      map[string]string    `yaml:"topic_dirs" json:"topic_dirs"`
	UserNotesDir   string               `yaml:"user_notes_dir" json:"user_notes_dir"`
}

type DreamDirectoryConfig struct {
	Inbox     string `yaml:"inbox" json:"inbox"`
	Projects  string `yaml:"projects" json:"projects"`
	Knowledge string `yaml:"knowledge" json:"knowledge"`
	Thinking  string `yaml:"thinking" json:"thinking"`
	Skills    string `yaml:"skills" json:"skills"`
	MOC       string `yaml:"moc" json:"moc"`
	Archive   string `yaml:"archive" json:"archive"`
}

type DreamSchedulerConfig struct {
	Enabled               bool    `yaml:"enabled" json:"enabled"`
	IntervalMS            int     `yaml:"interval_ms" json:"interval_ms"`
	InitialDelayMS        int     `yaml:"initial_delay_ms" json:"initial_delay_ms"`
	JitterRatio           float64 `yaml:"jitter_ratio" json:"jitter_ratio"`
	MaxRunDurationMS      int     `yaml:"max_run_duration_ms" json:"max_run_duration_ms"`
	SkipIfPreviousRunning bool    `yaml:"skip_if_previous_running" json:"skip_if_previous_running"`
}

type DreamCurationConfig struct {
	Enabled                bool `yaml:"enabled" json:"enabled"`
	MaxInputMemories       int  `yaml:"max_input_memories" json:"max_input_memories"`
	MaxInputChars          int  `yaml:"max_input_chars" json:"max_input_chars"`
	TimeoutMS              int  `yaml:"timeout_ms" json:"timeout_ms"`
	MinGroupSize           int  `yaml:"min_group_size" json:"min_group_size"`
	RequireSourceMemoryIDs bool `yaml:"require_source_memory_ids" json:"require_source_memory_ids"`
	FallbackRules          bool `yaml:"fallback_to_rule_export" json:"fallback_to_rule_export"`
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

// defaultDataDir 返回内置默认数据根目录：$HOME/.theone。
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".theone")
}

// DataDirFromStoragePath 从 SQLite 库文件路径推导数据根目录（与 memory.db 同级）。
func DataDirFromStoragePath(dbPath string) string {
	return filepath.Dir(expandHome(dbPath))
}

// LogFilePath 返回数据目录下的默认日志文件路径。
func LogFilePath(dataDir string) string {
	return filepath.Join(dataDir, "logs", "theone.log")
}

// Default 返回可直接启动的默认配置
// 默认数据库路径位于 $HOME/.theone/memory.db
// 设计原则：默认配置能直接启动，避免用户先理解完整系统才能使用
func Default() Config {
	dataDir := defaultDataDir()
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
			Path:   LogFilePath(dataDir),
		},
		Memory: MemoryConfig{
			DefaultUserID:       "local_default_user",
			DefaultWorkspace:    "local_default_workspace",
			MaxContentChars:     20000,
			MaxEvidenceChars:    1200,
			MaxKeywordCount:     30,
			MaxSalientSpanCount: 10,
		},
		Adapter: AdapterConfig{
			ExpandMode:                     "legacy",
			PrefetchTimeoutMS:              3000,
			MaxInjectChars:                 4000,
			PromptCacheUserSummaryMaxChars: 3000,
		},
		Capture: CaptureConfig{
			RequireSessionForAgentEvents: true,
			MaxInputSummaryChars:         1200,
			MaxOutputSummaryChars:        2000,
			MaxContentSummaryChars:       6000,
			MaxSourceRefsChars:           4000,
			MaxRawPayloadChars:           1048576,
			MaxSalientSpanChars:          500,
			MaxSalientSpanCount:          10,
			MaxKeywordCount:              30,
			DefaultAgentType:             "unknown",
		},
		Retrieval: RetrievalConfig{
			DefaultLimit:              10,
			DefaultTokenBudget:        1800,
			OnlineTimeoutMS:           100,
			MaxRelationExpansion:      20,
			MaxCandidatesBeforeRerank: 80,
			EnableTrace:               true,
			EnableAccessLog:           true,
			EnableRelationExpansion:   true,
			EnableCodeRefResolution:   true,
			EnableDocIndex:            true,
		},
		Embedding: EmbeddingConfig{
			Provider:                    "none",
			Model:                       "",
			QueryCacheSize:              256,
			OnlineQueryEmbeddingEnabled: false,
			MemoryEmbeddingEnabled:      false,
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
		VectorIndex: VectorIndexConfig{
			Backend:          "none",
			SQLiteVecEnabled: "auto",
		},
		AccessLog: AccessLogConfig{
			RetentionDaysRetrieved: 30,
			RetentionDaysInjected:  180,
			AggregateBeforeCleanup: true,
		},
		Retention: RetentionConfig{
			JobEnabled:       true,
			JobIntervalMS:    60 * 1000,
			TemporaryTTLDays: 5,
			ShortTermTTLDays: 90,
		},
		Processor: ProcessorConfig{
			Provider:              "rule_based",
			MaxRelatedEvents:      20,
			MaxCandidatesPerEvent: 3,
			OpenAI: OpenAIProcessorConfig{
				Model:                    "gpt-5-mini",
				APIKey:                   "",
				TimeoutMS:                30000,
				MaxOutputTokens:          1200,
				ExtractEvidencePrompt:    prompts.OpenAIProcessRawEventPrompt,
				GenerateCandidatesPrompt: "",
				SemanticEnhancePrompt:    prompts.OpenAISemanticEnhancePrompt,
			},
		},
		Automation: AutomationConfig{
			PollIntervalMS:   1000,
			BatchSize:        10,
			MaxAttempts:      3,
			RetryBaseDelayMS: 1000,
			RunningTimeoutMS: 300000,
		},
		Dream: DreamConfig{
			Enabled: false,
			Vault: DreamVaultConfig{
				Root:         "",
				SystemDir:    ".theone",
				UserNotesDir: "99-user-notes",
				Directories: DreamDirectoryConfig{
					Inbox:     "00-inbox",
					Projects:  "10-projects",
					Knowledge: "20-knowledge",
					Thinking:  "30-thinking",
					Skills:    "40-skills",
					MOC:       "80-moc",
					Archive:   "90-archive",
				},
				MemoryTypeDirs: map[string]string{
					"decision":          "decisions",
					"constraint":        "constraints",
					"failure":           "failures",
					"review_checkpoint": "reviews",
					"project_fact":      "facts",
					"procedure":         "procedures",
					"preference":        "preferences",
					"open_issue":        "open-issues",
				},
				TopicDirs: map[string]string{
					"memory-system":      "memory-systems",
					"distributed-system": "distributed-systems",
					"pki":                "pki",
					"security":           "security",
					"database":           "databases",
					"ai-agent":           "ai-agents",
				},
			},
			Scheduler: DreamSchedulerConfig{
				Enabled:               false,
				IntervalMS:            60 * 60 * 1000,
				InitialDelayMS:        30 * 1000,
				JitterRatio:           0.1,
				MaxRunDurationMS:      5 * 60 * 1000,
				SkipIfPreviousRunning: true,
			},
			Curation: DreamCurationConfig{
				Enabled:                false,
				MaxInputMemories:       50,
				MaxInputChars:          30000,
				TimeoutMS:              60000,
				MinGroupSize:           2,
				RequireSourceMemoryIDs: true,
				FallbackRules:          true,
			},
		},
	}
}

// Load 加载配置
// 按默认值、配置文件、环境变量、命令行的顺序合成配置，并执行基础校验
// 优先级：命令行 > 环境变量 > 配置文件 > 默认值
func Load(overrides Overrides) (Config, error) {
	cfg := Default()
	defaultLogPath := cfg.Logging.Path
	configPath := firstNonEmpty(overrides.ConfigPath, os.Getenv("THEONE_CONFIG"))
	if configPath != "" {
		if err := loadYAML(configPath, &cfg); err != nil {
			return Config{}, err
		}
	}
	yamlExplicitLog := cfg.Logging.Path != defaultLogPath

	dataDir := os.Getenv("THEONE_DATA_DIR")
	dbPath := os.Getenv("THEONE_DB_PATH")
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

	if level := firstNonEmpty(overrides.LogLevel, os.Getenv("THEONE_LOG_LEVEL")); level != "" {
		cfg.Logging.Level = level
	}
	if addr := firstNonEmpty(overrides.MCPAddr, os.Getenv("THEONE_MCP_ADDR")); addr != "" {
		cfg.Server.MCPAddr = addr
	}
	if apiKey := firstNonEmpty(os.Getenv("THEONE_OPENAI_API_KEY"), os.Getenv("OPENAI_API_KEY")); apiKey != "" {
		cfg.Processor.OpenAI.APIKey = apiKey
	}
	cfg.Storage.Path = expandHome(cfg.Storage.Path)
	if logPath := os.Getenv("THEONE_LOG_PATH"); logPath != "" {
		cfg.Logging.Path = expandHome(logPath)
	} else if !yamlExplicitLog {
		cfg.Logging.Path = LogFilePath(DataDirFromStoragePath(cfg.Storage.Path))
	} else {
		cfg.Logging.Path = expandHome(cfg.Logging.Path)
	}
	return cfg, validate(cfg)
}

// loadYAML 从给定路径读取 YAML 配置并反序列化到 cfg。
// 入参：path（配置文件路径，支持 ~ 展开）、cfg（接收反序列化结果的结构体指针）。
// 返回：读取/解析错误；所有错误统一以 CONFIG_INVALID 错误码包装，便于上层判断。
// 设计约束：解析阶段不校验字段，由 validate 在 Load 末尾统一做。
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
	if cfg.Capture.MaxContentSummaryChars <= 0 ||
		cfg.Capture.MaxSourceRefsChars <= 0 || cfg.Capture.MaxRawPayloadChars <= 0 || cfg.Capture.MaxSalientSpanChars <= 0 ||
		cfg.Capture.MaxSalientSpanCount <= 0 || cfg.Capture.MaxKeywordCount <= 0 {
		return errors.New("CONFIG_INVALID: capture content limits must be positive")
	}
	if cfg.Adapter.PrefetchTimeoutMS <= 0 || cfg.Adapter.MaxInjectChars <= 0 || cfg.Adapter.PromptCacheUserSummaryMaxChars <= 0 {
		return errors.New("CONFIG_INVALID: adapter content limits must be positive")
	}
	if strings.TrimSpace(cfg.Capture.DefaultAgentType) == "" {
		return errors.New("CONFIG_INVALID: capture.default_agent_type is required")
	}
	if strings.TrimSpace(cfg.Processor.Provider) == "" || cfg.Processor.MaxRelatedEvents <= 0 || cfg.Processor.MaxCandidatesPerEvent <= 0 {
		return errors.New("CONFIG_INVALID: processor config values must be positive and provider is required")
	}
	switch cfg.Processor.Provider {
	case "rule_based", "openai":
	default:
		return fmt.Errorf("CONFIG_INVALID: unsupported processor provider %q", cfg.Processor.Provider)
	}
	if strings.TrimSpace(cfg.Processor.OpenAI.Model) == "" ||
		cfg.Processor.OpenAI.TimeoutMS <= 0 || cfg.Processor.OpenAI.MaxOutputTokens <= 0 {
		return errors.New("CONFIG_INVALID: processor.openai config values must be positive and model is required")
	}
	if cfg.Retrieval.DefaultLimit <= 0 || cfg.Retrieval.DefaultTokenBudget <= 0 || cfg.Retrieval.OnlineTimeoutMS <= 0 ||
		cfg.Retrieval.MaxRelationExpansion <= 0 || cfg.Retrieval.MaxCandidatesBeforeRerank <= 0 {
		return errors.New("CONFIG_INVALID: retrieval config values must be positive")
	}
	if cfg.Embedding.QueryCacheSize <= 0 {
		return errors.New("CONFIG_INVALID: embedding.query_cache_size must be positive")
	}
	if cfg.AccessLog.RetentionDaysRetrieved <= 0 || cfg.AccessLog.RetentionDaysInjected <= 0 {
		return errors.New("CONFIG_INVALID: access_log retention values must be positive")
	}
	if strings.TrimSpace(cfg.VectorIndex.Backend) == "" {
		cfg.VectorIndex.Backend = "none"
	}
	switch cfg.VectorIndex.Backend {
	case "none", "blob", "sqlite_vec":
	default:
		return fmt.Errorf("CONFIG_INVALID: unsupported vector_index backend %q", cfg.VectorIndex.Backend)
	}
	if strings.TrimSpace(cfg.CodeIndex.Provider) == "" || cfg.CodeIndex.MaxFileSizeKB <= 0 || cfg.CodeIndex.MaxResolveRefs <= 0 {
		return errors.New("CONFIG_INVALID: codeindex config values must be positive and provider is required")
	}
	if cfg.CodeIndex.Provider != "local_basic" && cfg.CodeIndex.Provider != "none" {
		return fmt.Errorf("CONFIG_INVALID: unsupported codeindex provider %q", cfg.CodeIndex.Provider)
	}
	if cfg.DocIndex.MaxDocSizeKB <= 0 || cfg.DocIndex.MaxSections <= 0 || cfg.DocIndex.MaxSnapshotsPerDoc <= 0 {
		return errors.New("CONFIG_INVALID: docindex limits must be positive")
	}
	if cfg.Automation.PollIntervalMS <= 0 || cfg.Automation.BatchSize <= 0 || cfg.Automation.MaxAttempts <= 0 ||
		cfg.Automation.RetryBaseDelayMS <= 0 || cfg.Automation.RunningTimeoutMS <= 0 {
		return errors.New("CONFIG_INVALID: automation worker limits must be positive")
	}
	if err := validateDream(cfg.Dream); err != nil {
		return err
	}
	if cfg.Server.MCPAddr == "" {
		return errors.New("CONFIG_INVALID: server.mcp_addr is required")
	}
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("CONFIG_INVALID: unsupported log level %q", cfg.Logging.Level)
	}
	if strings.TrimSpace(cfg.Logging.Path) == "" {
		return errors.New("CONFIG_INVALID: logging.path is required")
	}
	return nil
}

func validateDream(cfg DreamConfig) error {
	if cfg.Enabled && strings.TrimSpace(cfg.Vault.Root) == "" {
		return errors.New("CONFIG_INVALID: dream.vault.root is required when dream is enabled")
	}
	if strings.TrimSpace(cfg.Vault.SystemDir) == "" || strings.TrimSpace(cfg.Vault.UserNotesDir) == "" {
		return errors.New("CONFIG_INVALID: dream vault directories are required")
	}
	if !isVaultRelativePath(cfg.Vault.SystemDir) {
		return errors.New("CONFIG_INVALID: dream.vault.system_dir must be vault-relative")
	}
	if !isVaultRelativePath(cfg.Vault.UserNotesDir) {
		return errors.New("CONFIG_INVALID: dream.vault.user_notes_dir must be vault-relative")
	}
	for name, value := range map[string]string{
		"inbox":     cfg.Vault.Directories.Inbox,
		"projects":  cfg.Vault.Directories.Projects,
		"knowledge": cfg.Vault.Directories.Knowledge,
		"thinking":  cfg.Vault.Directories.Thinking,
		"skills":    cfg.Vault.Directories.Skills,
		"moc":       cfg.Vault.Directories.MOC,
		"archive":   cfg.Vault.Directories.Archive,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("CONFIG_INVALID: dream.vault.directories.%s is required", name)
		}
		if !isVaultRelativePath(value) {
			return fmt.Errorf("CONFIG_INVALID: dream.vault.directories.%s must be vault-relative", name)
		}
	}
	for name, value := range cfg.Vault.MemoryTypeDirs {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("CONFIG_INVALID: dream.vault.memory_type_dirs.%s is required", name)
		}
		if !isVaultRelativePath(value) {
			return fmt.Errorf("CONFIG_INVALID: dream.vault.memory_type_dirs.%s must be vault-relative", name)
		}
	}
	for name, value := range cfg.Vault.TopicDirs {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("CONFIG_INVALID: dream.vault.topic_dirs.%s is required", name)
		}
		if !isVaultRelativePath(value) {
			return fmt.Errorf("CONFIG_INVALID: dream.vault.topic_dirs.%s must be vault-relative", name)
		}
	}
	if cfg.Scheduler.IntervalMS <= 0 || cfg.Scheduler.InitialDelayMS < 0 || cfg.Scheduler.MaxRunDurationMS <= 0 {
		return errors.New("CONFIG_INVALID: dream scheduler limits must be positive")
	}
	if cfg.Scheduler.JitterRatio < 0 || cfg.Scheduler.JitterRatio > 1 {
		return errors.New("CONFIG_INVALID: dream.scheduler.jitter_ratio must be between 0 and 1")
	}
	if cfg.Curation.MaxInputMemories <= 0 || cfg.Curation.MaxInputChars <= 0 || cfg.Curation.TimeoutMS <= 0 || cfg.Curation.MinGroupSize <= 0 {
		return errors.New("CONFIG_INVALID: dream curation limits must be positive")
	}
	return nil
}

func isVaultRelativePath(value string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(value))
	return cleaned != "" && cleaned != "." && !filepath.IsAbs(cleaned) && cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

// firstNonEmpty 返回第一个非空字符串；全部为空时返回空串。
// 与 config/app 的同名函数一致，用于命令行/环境变量/配置文件的优先级合并。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// expandHome 把 "~" 或 "~/..." 开头的路径展开为 $HOME。
// 仅处理前缀匹配，不解析中间出现的 ~；解析失败时原样返回，避免 Load 流程中断。
// 设计约束：不依赖外部 glob 库，使用 filepath.Join 维持跨平台路径拼接行为。
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
