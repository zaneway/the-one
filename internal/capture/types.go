package capture

import "time"

const (
	EventSessionStart         = "session.start"
	EventSessionEnd           = "session.end"
	EventTaskStart            = "task.start"
	EventTaskResult           = "task.result"
	EventConversationMessage  = "conversation.message"
	EventAgentResponseSummary = "agent.response.summary"
	EventToolCall             = "tool.call"
	EventToolResultSummary    = "tool.result.summary"
	EventFileEditSummary      = "file.edit.summary"
	EventUserCorrection       = "user.correction"
	EventUserDeclaration      = "user.declaration"
	EventAgentDecision        = "agent.decision"

	SourceChannelAgentSession = "agent_session"
	SourceChannelMCPTool      = "mcp_tool"
	SourceChannelManualCLI    = "manual_cli"

	ActorUser    = "user"
	ActorAgent   = "agent"
	ActorTool    = "tool"
	ActorAdapter = "adapter"
	ActorSystem  = "system"

	CaptureMethodAdapterHook       = "adapter_hook"
	CaptureMethodWrapperLog        = "wrapper_log"
	CaptureMethodCursorRule        = "cursor_rule"
	CaptureMethodManualMCPCall     = "manual_mcp_call"
	CaptureMethodFilesystemWatcher = "filesystem_watcher"
	CaptureMethodGitDiff           = "git_diff"

	StatusActive      = "active"
	StatusCompleted   = "completed"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
	StatusUnknown     = "unknown"

	SensitivityNormal = "normal"
)

// SourceRef 保存事件事实的外部引用、hash 和摘要元数据，禁止放入完整输出、完整 diff 或完整 prompt。
type SourceRef map[string]any

// CaptureCapabilities 是 Adapter 在 session.start 或首次 observe 时声明的捕获能力。
type CaptureCapabilities struct {
	ConversationCapture    bool `json:"conversation_capture"`
	ToolCallCapture        bool `json:"tool_call_capture"`
	ToolOutputCapture      bool `json:"tool_output_capture"`
	FileEditCapture        bool `json:"file_edit_capture"`
	SessionLifecycle       bool `json:"session_lifecycle"`
	MCPObserve             bool `json:"mcp_observe"`
	RequiresWrapper        bool `json:"requires_wrapper,omitempty"`
	RequiresRulesInjection bool `json:"requires_rules_injection,omitempty"`
}

// SessionInput 是 observe 请求中用于创建或更新 agent_session 的最小会话摘要。
type SessionInput struct {
	GoalSummary string `json:"goal_summary"`
	Status      string `json:"status"`
}

// TaskInput 是 observe 请求中用于创建或更新 agent_task 的最小任务摘要。
type TaskInput struct {
	TaskSummary    string `json:"task_summary"`
	Status         string `json:"status"`
	OutcomeSummary string `json:"outcome_summary"`
}

// ObserveRequest 是 P2 memory.observe 的服务层 DTO，只承载最小化后的事件事实。
type ObserveRequest struct {
	SessionID           string              `json:"session_id"`
	TaskID              string              `json:"task_id"`
	AgentType           string              `json:"agent_type"`
	WorkspaceID         string              `json:"workspace_id"`
	ProjectID           string              `json:"project_id"`
	RepoID              string              `json:"repo_id"`
	EventType           string              `json:"event_type"`
	SourceChannel       string              `json:"source_channel"`
	OccurredAt          string              `json:"occurred_at"`
	Actor               string              `json:"actor"`
	ToolName            string              `json:"tool_name"`
	InputSummary        string              `json:"input_summary"`
	OutputSummary       string              `json:"output_summary"`
	ContentSummary      string              `json:"content_summary"`
	Keywords            []string            `json:"keywords"`
	SalientSpans        []string            `json:"salient_spans"`
	SourceRefs          []SourceRef         `json:"source_refs"`
	ContentHash         string              `json:"content_hash"`
	Sensitivity         string              `json:"sensitivity"`
	RetentionHint       string              `json:"retention_hint"`
	CaptureCapabilities CaptureCapabilities `json:"capture_capabilities"`
	Session             *SessionInput       `json:"session"`
	Task                *TaskInput          `json:"task"`
}

// ObserveResponse 是 P2 memory.observe 的统一响应结构，所有路径都应带 request_id 便于日志关联。
type ObserveResponse struct {
	RequestID    string `json:"request_id"`
	RawEventID   string `json:"raw_event_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	Accepted     bool   `json:"accepted"`
	Pipeline     string `json:"pipeline"`
	Deduped      bool   `json:"deduped"`
	CaptureLevel int    `json:"capture_level"`
}

// AgentSession 是 P2 agent_session 表的领域对象，后续 repository 会直接复用。
type AgentSession struct {
	ID                      string    `json:"session_id"`
	AgentType               string    `json:"agent_type"`
	WorkspaceID             string    `json:"workspace_id"`
	ProjectID               string    `json:"project_id,omitempty"`
	RepoID                  string    `json:"repo_id,omitempty"`
	CaptureLevel            int       `json:"capture_level"`
	CaptureCapabilitiesJSON string    `json:"capture_capabilities_json,omitempty"`
	CaptureQualityJSON      string    `json:"capture_quality_json,omitempty"`
	StartedAt               time.Time `json:"started_at"`
	EndedAt                 time.Time `json:"ended_at,omitempty"`
	GoalSummary             string    `json:"goal_summary,omitempty"`
	Status                  string    `json:"status"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// AgentTask 是 P2 agent_task 表的领域对象，task_summary/outcome_summary 只能保存摘要。
type AgentTask struct {
	ID             string    `json:"task_id"`
	SessionID      string    `json:"session_id,omitempty"`
	WorkspaceID    string    `json:"workspace_id"`
	ProjectID      string    `json:"project_id,omitempty"`
	RepoID         string    `json:"repo_id,omitempty"`
	TaskSummary    string    `json:"task_summary"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"started_at"`
	EndedAt        time.Time `json:"ended_at,omitempty"`
	OutcomeSummary string    `json:"outcome_summary,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// RawEvent 是 P2 raw_event 表的领域对象，表示 append-only 事件事实，不代表长期记忆。
type RawEvent struct {
	ID               string    `json:"raw_event_id"`
	SessionID        string    `json:"session_id,omitempty"`
	TaskID           string    `json:"task_id,omitempty"`
	WorkspaceID      string    `json:"workspace_id,omitempty"`
	ProjectID        string    `json:"project_id,omitempty"`
	RepoID           string    `json:"repo_id,omitempty"`
	AgentType        string    `json:"agent_type,omitempty"`
	EventType        string    `json:"event_type"`
	SourceChannel    string    `json:"source_channel,omitempty"`
	OccurredAt       time.Time `json:"occurred_at"`
	Actor            string    `json:"actor,omitempty"`
	ToolName         string    `json:"tool_name,omitempty"`
	InputSummary     string    `json:"input_summary,omitempty"`
	OutputSummary    string    `json:"output_summary,omitempty"`
	ContentSummary   string    `json:"content_summary,omitempty"`
	KeywordsJSON     string    `json:"keywords_json,omitempty"`
	SalientSpansJSON string    `json:"salient_spans_json,omitempty"`
	SourceRefsJSON   string    `json:"source_refs_json,omitempty"`
	ContentHash      string    `json:"content_hash,omitempty"`
	Sensitivity      string    `json:"sensitivity,omitempty"`
	RetentionHint    string    `json:"retention_hint,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// EventDedupKey 是 raw_event 幂等查询键；session_id 存在时优先按 session 维度去重。
type EventDedupKey struct {
	ContentHash   string
	SessionID     string
	EventType     string
	SourceChannel string
	WorkspaceID   string
	ProjectID     string
	RepoID        string
}

// ListSessionsRequest 是 capture session 诊断查询过滤条件。
type ListSessionsRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id"`
	AgentType   string `json:"agent_type"`
	Status      string `json:"status"`
	Limit       int    `json:"limit"`
}

// ListTasksRequest 是 capture task 诊断查询过滤条件。
type ListTasksRequest struct {
	SessionID   string `json:"session_id"`
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id"`
	Status      string `json:"status"`
	Limit       int    `json:"limit"`
}

// ListEventsRequest 是 raw_event 诊断查询过滤条件。
type ListEventsRequest struct {
	SessionID     string   `json:"session_id"`
	TaskID        string   `json:"task_id"`
	WorkspaceID   string   `json:"workspace_id"`
	ProjectID     string   `json:"project_id"`
	RepoID        string   `json:"repo_id"`
	AgentType     string   `json:"agent_type"`
	SourceChannel string   `json:"source_channel"`
	EventType     string   `json:"event_type"`
	EventTypes    []string `json:"event_types"`
	Limit         int      `json:"limit"`
}

type ListSessionsResponse struct {
	Sessions []AgentSession `json:"sessions"`
}

type ListTasksResponse struct {
	Tasks []AgentTask `json:"tasks"`
}

type ListEventsResponse struct {
	Events []RawEvent `json:"events"`
}

type QualityRequest struct {
	SessionID string `json:"session_id"`
}

type QualityResponse struct {
	Report CaptureQualityReport `json:"report"`
}

// CaptureQualityReport 是 memory.capture.quality 的 repository 层结果。
type CaptureQualityReport struct {
	SessionID               string `json:"session_id"`
	CaptureLevel            int    `json:"capture_level"`
	CaptureCapabilitiesJSON string `json:"capture_capabilities_json"`
	CaptureQualityJSON      string `json:"capture_quality_json"`
}
