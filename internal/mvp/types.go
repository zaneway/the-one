package mvp

import (
	"context"
	"time"
)

const (
	RunModeSynthetic = "synthetic"
	RunModeRealAgent = "real_agent"
	RunModeMixed     = "mixed"
)

const (
	BaselineNoMemory        = "no_memory"
	BaselineFullChatHistory = "full_chat_history"
	BaselineSummaryOnly     = "summary_only"
)

const CandidateHybridMemory = "hybrid_memory"

const (
	RunStatusRunning = "running"
	RunStatusPassed  = "passed"
	RunStatusFailed  = "failed"
	RunStatusPartial = "partial"
	RunStatusAborted = "aborted"
)

const (
	TaskStatusRunning = "running"
	TaskStatusPassed  = "passed"
	TaskStatusFailed  = "failed"
	TaskStatusSkipped = "skipped"
)

const (
	MetricTokenSavings                 = "token_savings"
	MetricRepeatedExplanationReduction = "repeated_explanation_reduction"
	MetricDecisionRecallAccuracy       = "decision_recall_accuracy"
	MetricWrongMemoryInjectionRate     = "wrong_memory_injection_rate"
	MetricRetrievalLatencyP95MS        = "retrieval_latency_p95_ms"
	MetricCrossAgentRecallSuccessRate  = "cross_agent_recall_success_rate"
	MetricEventCaptureCompleteness     = "event_capture_completeness"
	MetricLevel4CapabilityCoverage     = "level4_capability_coverage"
	MetricReviewContextTokenSavings    = "review_context_token_savings"
	MetricWriteBlockingErrorCount      = "write_blocking_error_count"
	MetricTaskSuccessRate              = "task_success_rate"
)

const (
	MetricUnitRatio = "ratio"
	MetricUnitMS    = "ms"
	MetricUnitCount = "count"
)

const (
	ThresholdGreaterOrEqual = ">="
	ThresholdLessOrEqual    = "<="
	ThresholdEqual          = "="
)

const (
	AgentCodex      = "codex"
	AgentClaudeCode = "claude_code"
	AgentCursor     = "cursor"
)

// RequiredCertificationAgents 返回 P5-D 必须单独认证的 Agent 集合。
func RequiredCertificationAgents() []string {
	return []string{AgentCodex, AgentClaudeCode, AgentCursor}
}

// IsCertificationAgent 校验 agent_type 是否属于 P5-D 认证范围。
func IsCertificationAgent(agentType string) bool {
	for _, item := range RequiredCertificationAgents() {
		if agentType == item {
			return true
		}
	}
	return false
}

// IsTaskStatus 校验 P5 task 结果状态，空值表示由存储层按 task_success 推导。
func IsTaskStatus(status string) bool {
	switch status {
	case "", TaskStatusRunning, TaskStatusPassed, TaskStatusFailed, TaskStatusSkipped:
		return true
	default:
		return false
	}
}

// AcceptanceRun 表示一次 P5 MVP 验收。该结构只保存验收摘要和关联标识，不保存完整对话或工具输出。
type AcceptanceRun struct {
	ID            string
	Name          string
	Mode          string
	WorkspaceID   string
	ProjectID     string
	RepoID        string
	BaselineType  string
	CandidateType string
	Status        string
	StartedAt     time.Time
	EndedAt       time.Time
	SummaryJSON   string
	ReportPath    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AcceptanceTask 表示某个 MVP scenario 的一轮执行结果。
type AcceptanceTask struct {
	ID               string
	RunID            string
	ScenarioID       string
	Round            int
	AgentType        string
	Baseline         bool
	SessionID        string
	TaskID           string
	RetrievalTraceID string
	Status           string
	TaskSuccess      bool
	ExpectedJSON     string
	ObservedJSON     string
	FailureReason    string
	StartedAt        time.Time
	EndedAt          time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// MetricSample 是 P5 指标计算后的持久化样本。
type MetricSample struct {
	ID                string
	RunID             string
	ScenarioID        string
	TaskResultID      string
	AgentType         string
	MetricName        string
	MetricValue       float64
	Numerator         float64
	Denominator       float64
	Unit              string
	ThresholdValue    float64
	ThresholdOperator string
	Passed            bool
	SourceJSON        string
	CreatedAt         time.Time
}

// AgentCapability 保存某个 Agent 在一次 P5 验收中的真实捕获能力快照。
type AgentCapability struct {
	ID                     string
	RunID                  string
	AgentType              string
	AdapterName            string
	AdapterVersion         string
	CaptureLevel           int
	ConversationCapture    bool
	ToolCallCapture        bool
	ToolOutputCapture      bool
	FileEditCapture        bool
	SessionLifecycle       bool
	MemoryObserve          bool
	CapabilityCoverage     float64
	Completeness           float64
	DegradationReasonsJSON string
	CreatedAt              time.Time
}

type RunQuery struct {
	WorkspaceID string
	ProjectID   string
	RepoID      string
	Status      string
	Limit       int
}

type TaskQuery struct {
	RunID      string
	ScenarioID string
	AgentType  string
	Baseline   *bool
	Limit      int
}

type MetricQuery struct {
	RunID      string
	ScenarioID string
	AgentType  string
	MetricName string
	Limit      int
}

type CapabilityQuery struct {
	RunID     string
	AgentType string
	Limit     int
}

// Repository 定义 P5-A 验收模型需要的持久化能力。
type Repository interface {
	CreateRun(ctx context.Context, run AcceptanceRun) (AcceptanceRun, error)
	GetRun(ctx context.Context, runID string) (AcceptanceRun, error)
	UpdateRunStatus(ctx context.Context, run AcceptanceRun) error
	ListRuns(ctx context.Context, query RunQuery) ([]AcceptanceRun, error)
	RecordTask(ctx context.Context, task AcceptanceTask) (AcceptanceTask, error)
	ListAcceptanceTasks(ctx context.Context, query TaskQuery) ([]AcceptanceTask, error)
	UpsertMetricSamples(ctx context.Context, samples []MetricSample) ([]MetricSample, error)
	ListMetricSamples(ctx context.Context, query MetricQuery) ([]MetricSample, error)
	UpsertAgentCapability(ctx context.Context, capability AgentCapability) (AgentCapability, error)
	ListAgentCapabilities(ctx context.Context, query CapabilityQuery) ([]AgentCapability, error)
	ListRetrievalLatenciesByTraceIDs(ctx context.Context, traceIDs []string) ([]float64, error)
}

type ComputeMetricsRequest struct {
	RunID     string `json:"run_id"`
	Recompute bool   `json:"recompute,omitempty"`
}

type MetricDiagnostic struct {
	MetricName        string  `json:"metric_name"`
	ScenarioID        string  `json:"scenario_id,omitempty"`
	AgentType         string  `json:"agent_type,omitempty"`
	MetricValue       float64 `json:"metric_value"`
	ThresholdOperator string  `json:"threshold_operator,omitempty"`
	ThresholdValue    float64 `json:"threshold_value,omitempty"`
	Passed            bool    `json:"passed"`
	Unit              string  `json:"unit"`
}

type ComputeMetricsResponse struct {
	RequestID string             `json:"request_id,omitempty"`
	RunID     string             `json:"run_id"`
	Status    string             `json:"status"`
	Metrics   []MetricDiagnostic `json:"metrics"`
	Summary   MetricsSummary     `json:"summary"`
}

type MetricsSummary struct {
	MetricCount              int  `json:"metric_count"`
	PassedMetrics            int  `json:"passed_metrics"`
	FailedMetrics            int  `json:"failed_metrics"`
	EngineMVPPassed          bool `json:"engine_mvp_passed"`
	AgentCertificationPassed bool `json:"agent_certification_passed"`
}

type ReportRequest struct {
	RunID           string `json:"run_id"`
	Format          string `json:"format,omitempty"`
	IncludeFailures bool   `json:"include_failures,omitempty"`
}

type ReportResponse struct {
	RequestID  string         `json:"request_id,omitempty"`
	RunID      string         `json:"run_id"`
	Status     string         `json:"status"`
	ReportPath string         `json:"report_path,omitempty"`
	Summary    MetricsSummary `json:"summary"`
	Report     string         `json:"report,omitempty"`
}
