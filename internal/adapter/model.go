package adapter

// ProtocolV1 是当前接入层协议版本。
const ProtocolV1 = "v1"

// IngestEnvelope 表示接入层统一包络。
// 该结构用于接入层追踪、重试和诊断，不要求逐字段映射到 ObserveRequest 顶层。
type IngestEnvelope struct {
	IngestID        string         `json:"ingest_id"`
	ProtocolVersion string         `json:"protocol_version"`
	Producer        string         `json:"producer"`
	AgentType       string         `json:"agent_type,omitempty"`
	Kind            string         `json:"kind,omitempty"`
	SessionID       string         `json:"session_id"`
	TurnID          string         `json:"turn_id,omitempty"`
	EventType       string         `json:"event_type"`
	OccurredAt      string         `json:"occurred_at,omitempty"`
	Payload         map[string]any `json:"payload"`
}

// SessionModel 表示接入层会话模型。
type SessionModel struct {
	SessionID           string                 `json:"session_id"`
	AgentType           string                 `json:"agent_type"`
	WorkspaceID         string                 `json:"workspace_id"`
	ProjectID           string                 `json:"project_id"`
	RepoID              string                 `json:"repo_id"`
	GoalSummary         string                 `json:"goal_summary"`
	Status              string                 `json:"status"`
	CaptureCapabilities map[string]any         `json:"capture_capabilities,omitempty"`
	StartedAt           string                 `json:"started_at,omitempty"`
	EndedAt             string                 `json:"ended_at,omitempty"`
	Extra               map[string]interface{} `json:"extra,omitempty"`
}

// TaskModel 表示接入层任务模型。
type TaskModel struct {
	TaskID          string `json:"task_id"`
	SessionID       string `json:"session_id"`
	TaskSummary     string `json:"task_summary"`
	Status          string `json:"status"`
	OutcomeSummary  string `json:"outcome_summary,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	EndedAt         string `json:"ended_at,omitempty"`
	OutcomeRequired bool   `json:"outcome_required,omitempty"`
}

// TurnModel 表示接入层回合模型。
// started_at/completed_at 可在落库前填充，不强制在回合创建时立即提供。
type TurnModel struct {
	TurnID          string   `json:"turn_id"`
	SessionID       string   `json:"session_id"`
	TaskID          string   `json:"task_id"`
	UserSummary     string   `json:"user_summary"`
	AgentSummary    string   `json:"agent_summary"`
	ToolResults     []string `json:"tool_results,omitempty"`
	FileEdits       []string `json:"file_edits,omitempty"`
	DecisionSummary string   `json:"decision_summary,omitempty"`
	StartedAt       string   `json:"started_at,omitempty"`
	CompletedAt     string   `json:"completed_at,omitempty"`
	IsSubstantive   bool     `json:"is_substantive"`
}

// DeliveryResult 表示接入端口提交结果。
type DeliveryResult struct {
	Accepted     bool   `json:"accepted"`
	Deduped      bool   `json:"deduped"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
}
