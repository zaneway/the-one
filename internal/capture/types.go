package capture

import "time"

// ============================================================================
// 事件类型（Event Type）常量定义
// 定义了P2阶段支持的所有事件类型
// ============================================================================
const (
	// EventSessionStart 会话开始事件
	// Agent工作会话开始时触发
	EventSessionStart = "session.start"

	// EventSessionEnd 会话结束事件
	// Agent工作会话结束时触发
	EventSessionEnd = "session.end"

	// EventTaskStart 任务开始事件
	// 明确任务开始时触发
	EventTaskStart = "task.start"

	// EventTaskResult 任务结果事件
	// 任务完成或失败时触发，记录任务结果
	EventTaskResult = "task.result"

	// EventConversationMessage 对话消息事件
	// 用户消息摘要，取决于Agent能力
	EventConversationMessage = "conversation.message"

	// EventAgentResponseSummary Agent回复摘要事件
	// Agent回复的摘要，取决于Agent能力
	EventAgentResponseSummary = "agent.response.summary"

	// EventToolCall 工具调用事件
	// 工具调用时触发，记录工具名和输入摘要
	EventToolCall = "tool.call"

	// EventToolResultSummary 工具结果摘要事件
	// 工具执行完成时触发，记录输出摘要和错误签名
	EventToolResultSummary = "tool.result.summary"

	// EventFileEditSummary 文件编辑摘要事件
	// 文件编辑时触发，记录文件路径和diff摘要
	EventFileEditSummary = "file.edit.summary"

	// EventUserCorrection 用户纠正事件
	// 用户纠正Agent行为时触发
	EventUserCorrection = "user.correction"

	// EventUserDeclaration 用户显式声明事件
	// 用户显式声明偏好、决策等时触发
	EventUserDeclaration = "user.declaration"

	// EventAgentDecision Agent决策事件
	// Agent在任务中形成中间决策时触发
	EventAgentDecision = "agent.decision"
)

// ============================================================================
// 来源渠道（Source Channel）常量定义
// 定义事件的来源渠道
// ============================================================================
const (
	// SourceChannelAgentSession Agent会话来源
	// 来自真实Agent session的自动或半自动捕获
	SourceChannelAgentSession = "agent_session"

	// SourceChannelMCPTool MCP工具来源
	// Agent主动调用MCP工具上报
	SourceChannelMCPTool = "mcp_tool"

	// SourceChannelManualCLI 手动CLI来源
	// 本地命令或验收脚本手动写入
	SourceChannelManualCLI = "manual_cli"
)

// ============================================================================
// 行为者（Actor）常量定义
// 定义事件的行为者类型
// ============================================================================
const (
	// ActorUser 用户
	ActorUser = "user"

	// ActorAgent Agent
	ActorAgent = "agent"

	// ActorTool 工具
	ActorTool = "tool"

	// ActorAdapter 适配器
	ActorAdapter = "adapter"

	// ActorSystem 系统
	ActorSystem = "system"
)

// ============================================================================
// 捕获方法（Capture Method）常量定义
// 定义事件的捕获技术细节，保存在source_refs.capture_method中
// ============================================================================
const (
	// CaptureMethodAdapterHook 适配器Hook
	// 通过Agent的hook机制捕获事件
	CaptureMethodAdapterHook = "adapter_hook"

	// CaptureMethodWrapperLog 包装器日志
	// 通过wrapper或日志collector捕获事件
	CaptureMethodWrapperLog = "wrapper_log"

	// CaptureMethodCursorRule Cursor规则
	// 通过Cursor rules引导Agent上报事件
	CaptureMethodCursorRule = "cursor_rule"

	// CaptureMethodManualMCPCall 手动MCP调用
	// 用户手动调用MCP工具上报事件
	CaptureMethodManualMCPCall = "manual_mcp_call"

	// CaptureMethodFilesystemWatcher 文件系统监视器
	// 通过文件系统监视器捕获文件变更
	CaptureMethodFilesystemWatcher = "filesystem_watcher"

	// CaptureMethodGitDiff Git差异
	// 通过git diff捕获文件变更
	CaptureMethodGitDiff = "git_diff"
)

// ============================================================================
// 状态（Status）常量定义
// 定义session和task的状态
// ============================================================================
const (
	// StatusActive 活跃状态
	// session或task正在进行中
	StatusActive = "active"

	// StatusCompleted 已完成状态
	// session正常完成
	StatusCompleted = "completed"

	// StatusSucceeded 成功状态
	// task成功完成
	StatusSucceeded = "succeeded"

	// StatusFailed 失败状态
	// session或task失败
	StatusFailed = "failed"

	// StatusInterrupted 中断状态
	// session或task被中断
	StatusInterrupted = "interrupted"

	// StatusUnknown 未知状态
	// 状态不明确
	StatusUnknown = "unknown"
)

// ============================================================================
// 敏感度（Sensitivity）常量定义
// ============================================================================
const (
	// SensitivityNormal 普通敏感度
	// 默认敏感度级别
	SensitivityNormal = "normal"
)

// SourceRef 来源引用
// 保存事件事实的外部引用、hash 和摘要元数据
// 设计约束：禁止放入完整输出、完整 diff 或完整 prompt
type SourceRef map[string]any

// CaptureCapabilities 捕获能力声明
// Adapter 在 session.start 或首次 observe 时声明的捕获能力
// 用于计算capture_level和评估捕获质量
type CaptureCapabilities struct {
	// ConversationCapture 对话捕获能力
	// 是否能捕获用户消息和Agent回复摘要
	ConversationCapture bool `json:"conversation_capture"`

	// ToolCallCapture 工具调用捕获能力
	// 是否能捕获工具调用事件
	ToolCallCapture bool `json:"tool_call_capture"`

	// ToolOutputCapture 工具输出捕获能力
	// 是否能捕获工具输出摘要
	ToolOutputCapture bool `json:"tool_output_capture"`

	// FileEditCapture 文件编辑捕获能力
	// 是否能捕获文件编辑摘要
	FileEditCapture bool `json:"file_edit_capture"`

	// SessionLifecycle 会话生命周期捕获能力
	// 是否能捕获session.start和session.end事件
	SessionLifecycle bool `json:"session_lifecycle"`

	// MCPObserve MCP观察上报能力
	// 是否能通过MCP主动上报事件
	MCPObserve bool `json:"mcp_observe"`

	// RequiresWrapper 是否需要包装器
	// 是否需要额外的wrapper才能捕获事件
	RequiresWrapper bool `json:"requires_wrapper,omitempty"`

	// RequiresRulesInjection 是否需要规则注入
	// 是否需要注入规则才能引导Agent上报事件
	RequiresRulesInjection bool `json:"requires_rules_injection,omitempty"`
}

// SessionInput 会话输入结构体
// observe 请求中用于创建或更新 agent_session 的最小会话摘要
type SessionInput struct {
	// GoalSummary 目标摘要
	// 当前session的任务目标摘要，不保存完整prompt
	GoalSummary string `json:"goal_summary"`

	// Status 会话状态
	// active、completed、failed、interrupted、unknown
	Status string `json:"status"`
}

// TaskInput 任务输入结构体
// observe 请求中用于创建或更新 agent_task 的最小任务摘要
type TaskInput struct {
	// TaskSummary 任务摘要
	// 任务描述摘要，不保存完整用户prompt
	TaskSummary string `json:"task_summary"`

	// Status 任务状态
	// active、succeeded、failed、interrupted、unknown
	Status string `json:"status"`

	// OutcomeSummary 结果摘要
	// 任务结果摘要，不保存完整Agent回复
	OutcomeSummary string `json:"outcome_summary"`
}

// ObserveRequest memory.observe 请求结构体
// P2 阶段的服务层 DTO，只承载最小化后的事件事实
// 设计原则：不保存完整会话、完整工具输出、完整diff或完整源码
type ObserveRequest struct {
	// SessionID 会话ID
	// Agent自动捕获事件必填，手动CLI写入可空
	SessionID string `json:"session_id"`

	// TaskID 任务ID
	// 可选，为空时服务端按session_id + normalized_task查找或创建
	TaskID string `json:"task_id"`

	// AgentType Agent类型
	// Agent自动捕获事件必填，如codex、claude_code、cursor
	AgentType string `json:"agent_type"`

	// WorkspaceID 工作空间ID
	// Agent自动捕获事件必填
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID
	// 可选，用于事件归属和查询过滤
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID
	// 可选，用于事件归属和查询过滤
	RepoID string `json:"repo_id"`

	// EventType 事件类型
	// 必填，如session.start、tool.result.summary等
	EventType string `json:"event_type"`

	// SourceChannel 来源渠道
	// 事件来源：agent_session、mcp_tool、manual_cli
	SourceChannel string `json:"source_channel"`

	// OccurredAt 发生时间
	// 可选，为空时服务端使用当前时间
	OccurredAt string `json:"occurred_at"`

	// Actor 行为者
	// 事件的行为者：user、agent、tool、adapter、system
	Actor string `json:"actor"`

	// ToolName 工具名称
	// tool.call和tool.result.summary事件时填写
	ToolName string `json:"tool_name"`

	// InputSummary 输入摘要
	// 工具输入摘要，不超过max_input_summary_chars限制
	InputSummary string `json:"input_summary"`

	// OutputSummary 输出摘要
	// 工具输出摘要，不超过max_output_summary_chars限制
	OutputSummary string `json:"output_summary"`

	// ContentSummary 内容摘要
	// 通用内容摘要，不超过max_content_summary_chars限制
	ContentSummary string `json:"content_summary"`

	// Keywords 关键词列表
	// 用于检索的关键词，不超过max_keyword_count限制
	Keywords []string `json:"keywords"`

	// SalientSpans 显著片段列表
	// 原始内容中的关键片段，不超过max_salient_span_count限制
	SalientSpans []string `json:"salient_spans"`

	// SourceRefs 来源引用列表
	// 保存hash、路径、符号、exit_code等引用，不保存全文
	SourceRefs []SourceRef `json:"source_refs"`

	// ContentHash 内容哈希
	// 推荐必填，用于幂等去重；为空时服务端用最小化字段计算
	ContentHash string `json:"content_hash"`

	// Sensitivity 敏感度
	// 默认normal，可标记敏感内容
	Sensitivity string `json:"sensitivity"`

	// RetentionHint 保留提示
	// 建议的保留策略，如short_term、long_term
	RetentionHint string `json:"retention_hint"`

	// CaptureCapabilities 捕获能力声明
	// Adapter声明的捕获能力，用于计算capture_level
	CaptureCapabilities CaptureCapabilities `json:"capture_capabilities"`

	// Session 会话信息
	// session.start事件或首次观察时使用
	Session *SessionInput `json:"session"`

	// Task 任务信息
	// task.start事件或default_task创建时使用
	Task *TaskInput `json:"task"`
}

// ObserveResponse memory.observe 响应结构体
// P2 阶段的统一响应结构，所有路径都应带 request_id 便于日志关联
type ObserveResponse struct {
	// RequestID 请求ID
	// 用于日志关联和问题追踪
	RequestID string `json:"request_id"`

	// RawEventID 原始事件ID
	// 新创建或已存在的事件ID
	RawEventID string `json:"raw_event_id,omitempty"`

	// SessionID 会话ID
	SessionID string `json:"session_id,omitempty"`

	// TaskID 任务ID
	TaskID string `json:"task_id,omitempty"`

	// Accepted 是否接受
	// 事件是否被成功处理
	Accepted bool `json:"accepted"`

	// Pipeline 处理管道
	// P2阶段固定为"raw_event_only"
	Pipeline string `json:"pipeline"`

	// Deduped 是否去重
	// 如果存在相同内容的事件，返回true
	Deduped bool `json:"deduped"`

	// CaptureLevel 捕获等级
	// 根据Adapter声明能力计算的捕获等级，范围1-4
	CaptureLevel int `json:"capture_level"`

	// Diagnostics 诊断标记
	// 用于表达非阻断问题，例如自动处理入队失败但 raw_event 已保留
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// AgentSession Agent会话结构体
// P2 agent_session 表的领域对象，记录一次Agent工作会话
// 后续 repository 会直接复用此结构
type AgentSession struct {
	// ID 会话ID
	// 全局唯一标识，格式如sess_001
	ID string `json:"session_id"`

	// AgentType Agent类型
	// codex、claude_code、cursor、unknown
	AgentType string `json:"agent_type"`

	// WorkspaceID 工作空间ID
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID
	ProjectID string `json:"project_id,omitempty"`

	// RepoID 仓库ID
	RepoID string `json:"repo_id,omitempty"`

	// CaptureLevel 捕获等级
	// 范围1-4，根据Adapter声明能力计算
	// Level1: 仅Agent主动调用memory tools
	// Level2: session lifecycle和显式声明/任务结果
	// Level3: 工具调用和工具结果摘要
	// Level4: conversation、tool、file edit、session lifecycle和memory observe
	CaptureLevel int `json:"capture_level"`

	// CaptureCapabilitiesJSON 捕获能力JSON
	// Adapter声明的捕获能力，不代表实际捕获完整度
	CaptureCapabilitiesJSON string `json:"capture_capabilities_json,omitempty"`

	// CaptureQualityJSON 捕获质量JSON
	// 本session已捕获事件的质量统计
	CaptureQualityJSON string `json:"capture_quality_json,omitempty"`

	// StartedAt 开始时间
	StartedAt time.Time `json:"started_at"`

	// EndedAt 结束时间
	EndedAt time.Time `json:"ended_at,omitempty"`

	// GoalSummary 目标摘要
	// 当前session的任务目标摘要，不保存完整prompt
	GoalSummary string `json:"goal_summary,omitempty"`

	// Status 会话状态
	// active、completed、failed、interrupted、unknown
	Status string `json:"status"`

	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentTask Agent任务结构体
// P2 agent_task 表的领域对象，在session内表达任务边界
// 设计约束：task_summary/outcome_summary 只能保存摘要
type AgentTask struct {
	// ID 任务ID
	// 全局唯一标识，格式如task_001
	ID string `json:"task_id"`

	// SessionID 会话ID
	// 可空，用于未来导入或批处理；P2 Agent自动捕获应尽量绑定session
	SessionID string `json:"session_id,omitempty"`

	// WorkspaceID 工作空间ID
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID
	ProjectID string `json:"project_id,omitempty"`

	// RepoID 仓库ID
	RepoID string `json:"repo_id,omitempty"`

	// TaskSummary 任务摘要
	// 任务描述摘要，不保存完整用户prompt
	TaskSummary string `json:"task_summary"`

	// Status 任务状态
	// active、succeeded、failed、interrupted、unknown
	Status string `json:"status"`

	// StartedAt 开始时间
	StartedAt time.Time `json:"started_at"`

	// EndedAt 结束时间
	EndedAt time.Time `json:"ended_at,omitempty"`

	// OutcomeSummary 结果摘要
	// 任务结果摘要，不保存完整Agent回复
	OutcomeSummary string `json:"outcome_summary,omitempty"`

	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// RawEvent 原始事件结构体
// P2 raw_event 表的领域对象，表示 append-only 事件事实
// 设计原则：不代表长期记忆，只保存最小化后的事件事实
type RawEvent struct {
	// ID 事件ID
	// 全局唯一标识，格式如evt_001
	ID string `json:"raw_event_id"`

	// SessionID 会话ID
	// 可空，用于支持手动CLI写入、历史导入、批处理巩固
	SessionID string `json:"session_id,omitempty"`

	// TaskID 任务ID
	// 可空，用于任务级归因
	TaskID string `json:"task_id,omitempty"`

	// WorkspaceID 工作空间ID
	// 可空，用于scope过滤
	WorkspaceID string `json:"workspace_id,omitempty"`

	// ProjectID 项目ID
	// 可空，用于scope过滤
	ProjectID string `json:"project_id,omitempty"`

	// RepoID 仓库ID
	// 可空，用于scope过滤
	RepoID string `json:"repo_id,omitempty"`

	// AgentType Agent类型
	// 可空，用于事件归属
	AgentType string `json:"agent_type,omitempty"`

	// EventType 事件类型
	// 必填，如session.start、tool.result.summary等
	EventType string `json:"event_type"`

	// SourceChannel 来源渠道
	// 事件来源：agent_session、mcp_tool、manual_cli
	SourceChannel string `json:"source_channel,omitempty"`

	// OccurredAt 发生时间
	OccurredAt time.Time `json:"occurred_at"`

	// Actor 行为者
	// 事件的行为者：user、agent、tool、adapter、system
	Actor string `json:"actor,omitempty"`

	// ToolName 工具名称
	// tool.call和tool.result.summary事件时填写
	ToolName string `json:"tool_name,omitempty"`

	// InputSummary 输入摘要
	// 工具输入摘要，不超过max_input_summary_chars限制
	InputSummary string `json:"input_summary,omitempty"`

	// OutputSummary 输出摘要
	// 工具输出摘要，不超过max_output_summary_chars限制
	OutputSummary string `json:"output_summary,omitempty"`

	// ContentSummary 内容摘要
	// 通用内容摘要，不超过max_content_summary_chars限制
	ContentSummary string `json:"content_summary,omitempty"`

	// KeywordsJSON 关键词JSON
	// 用于检索的关键词
	KeywordsJSON string `json:"keywords_json,omitempty"`

	// SalientSpansJSON 显著片段JSON
	// 原始内容中的关键片段
	SalientSpansJSON string `json:"salient_spans_json,omitempty"`

	// SourceRefsJSON 来源引用JSON
	// 保存hash、路径、符号、exit_code等引用
	SourceRefsJSON string `json:"source_refs_json,omitempty"`

	// ContentHash 内容哈希
	// 用于幂等去重
	ContentHash string `json:"content_hash,omitempty"`

	// Sensitivity 敏感度
	// 默认normal，可标记敏感内容
	Sensitivity string `json:"sensitivity,omitempty"`

	// RetentionHint 保留提示
	// 建议的保留策略，如short_term、long_term
	RetentionHint string `json:"retention_hint,omitempty"`

	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
}

// EventDedupKey 事件去重键结构体
// raw_event 幂等查询键，用于检测重复事件
// 去重规则：session_id 存在时优先按 session 维度去重
type EventDedupKey struct {
	// ContentHash 内容哈希
	// 事件内容的唯一标识
	ContentHash string

	// SessionID 会话ID
	// 存在时优先使用
	SessionID string

	// EventType 事件类型
	EventType string

	// SourceChannel 来源渠道
	SourceChannel string

	// WorkspaceID 工作空间ID
	WorkspaceID string

	// ProjectID 项目ID
	ProjectID string

	// RepoID 仓库ID
	RepoID string
}

// ListSessionsRequest 会话列表查询请求
// capture session 诊断查询过滤条件
type ListSessionsRequest struct {
	// WorkspaceID 工作空间ID过滤
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID过滤
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID过滤
	RepoID string `json:"repo_id"`

	// AgentType Agent类型过滤
	AgentType string `json:"agent_type"`

	// Status 状态过滤
	Status string `json:"status"`

	// Limit 结果数量限制
	Limit int `json:"limit"`
}

// ListTasksRequest 任务列表查询请求
// capture task 诊断查询过滤条件
type ListTasksRequest struct {
	// SessionID 会话ID过滤
	SessionID string `json:"session_id"`

	// WorkspaceID 工作空间ID过滤
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID过滤
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID过滤
	RepoID string `json:"repo_id"`

	// Status 状态过滤
	Status string `json:"status"`

	// Limit 结果数量限制
	Limit int `json:"limit"`
}

// ListEventsRequest 事件列表查询请求
// raw_event 诊断查询过滤条件
type ListEventsRequest struct {
	// SessionID 会话ID过滤
	SessionID string `json:"session_id"`

	// TaskID 任务ID过滤
	TaskID string `json:"task_id"`

	// WorkspaceID 工作空间ID过滤
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID过滤
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID过滤
	RepoID string `json:"repo_id"`

	// AgentType Agent类型过滤
	AgentType string `json:"agent_type"`

	// SourceChannel 来源渠道过滤
	SourceChannel string `json:"source_channel"`

	// EventType 事件类型过滤
	EventType string `json:"event_type"`

	// EventTypes 事件类型列表过滤
	EventTypes []string `json:"event_types"`

	// Limit 结果数量限制
	Limit int `json:"limit"`
}

// ListSessionsResponse 会话列表响应
type ListSessionsResponse struct {
	// Sessions 会话列表
	Sessions []AgentSession `json:"sessions"`
}

// ListTasksResponse 任务列表响应
type ListTasksResponse struct {
	// Tasks 任务列表
	Tasks []AgentTask `json:"tasks"`
}

// ListEventsResponse 事件列表响应
type ListEventsResponse struct {
	// Events 事件列表
	Events []RawEvent `json:"events"`
}

// QualityRequest 捕获质量查询请求
type QualityRequest struct {
	// SessionID 会话ID
	SessionID string `json:"session_id"`
}

// QualityResponse 捕获质量查询响应
type QualityResponse struct {
	// Report 质量报告
	Report CaptureQualityReport `json:"report"`
}

// CaptureQualityReport 捕获质量报告
// memory.capture.quality 的 repository 层结果
type CaptureQualityReport struct {
	// SessionID 会话ID
	SessionID string `json:"session_id"`

	// CaptureLevel 捕获等级
	// 范围1-4，根据Adapter声明能力计算
	CaptureLevel int `json:"capture_level"`

	// CaptureCapabilitiesJSON 捕获能力JSON
	// Adapter声明的捕获能力
	CaptureCapabilitiesJSON string `json:"capture_capabilities_json"`

	// CaptureQualityJSON 捕获质量JSON
	// 本session已捕获事件的质量统计
	CaptureQualityJSON string `json:"capture_quality_json"`
}
