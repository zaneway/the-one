package retrieval

import (
	"context"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

// Orchestrator 定义 P4 检索编排器内部接口。
// 设计边界：编排器只负责召回、扩展、排序、上下文构造和 trace/access log，不写入新的长期记忆。
type Orchestrator interface {
	// Search 执行 P4 memory.search 检索流程，返回包含 trace、诊断和可解释分数的结果。
	Search(ctx context.Context, req SearchRequest) (*SearchResult, error)

	// BuildContext 执行 P4 memory.context 上下文构造流程，返回预算分配、注入记忆和诊断信息。
	BuildContext(ctx context.Context, req ContextRequest) (*ContextResult, error)
}

// RetrievalIntent 表示一次检索请求的业务意图。
type RetrievalIntent string

const (
	// IntentGeneralSearch 通用检索。
	IntentGeneralSearch RetrievalIntent = "general_search"

	// IntentTaskContinuation 任务连续性检索，优先召回 session/task 状态。
	IntentTaskContinuation RetrievalIntent = "task_continuation"

	// IntentArchitectureReview 架构或设计复查，优先 checkpoint 和文档变化。
	IntentArchitectureReview RetrievalIntent = "architecture_review"

	// IntentCodeTask 代码任务，允许使用 code_ref 和 Code Index。
	IntentCodeTask RetrievalIntent = "code_task"

	// IntentFailureRecall 失败经验召回，优先 failure/procedure。
	IntentFailureRecall RetrievalIntent = "failure_recall"

	// IntentUserPreference 用户偏好召回，优先 user_global preference。
	IntentUserPreference RetrievalIntent = "user_preference"
)

// RetrievalMode 表示一次检索实际采用的召回和扩展模式。
type RetrievalMode string

const (
	// ModeFTSOnly 只使用 FTS 召回。
	ModeFTSOnly RetrievalMode = "fts_only"

	// ModeFTSMetadata 使用 FTS + metadata filter。
	ModeFTSMetadata RetrievalMode = "fts_metadata"

	// ModeFTSRelation 使用 FTS + metadata + relation expansion。
	ModeFTSRelation RetrievalMode = "fts_relation"

	// ModeFTSVectorRelation 使用 FTS + vector + relation。
	ModeFTSVectorRelation RetrievalMode = "fts_vector_relation"

	// ModeCheckpointAware 使用 checkpoint/doc snapshot 辅助复查。
	ModeCheckpointAware RetrievalMode = "checkpoint_aware"

	// ModeCodeAware 使用 code_ref/Code Index 辅助召回。
	ModeCodeAware RetrievalMode = "code_aware"
)

// TraceStatus 表示检索 trace 生命周期状态。
type TraceStatus string

const (
	// TraceStarted trace 已创建但检索尚未完成。
	TraceStarted TraceStatus = "started"

	// TraceCompleted trace 正常完成。
	TraceCompleted TraceStatus = "completed"

	// TraceFailed trace 失败。
	TraceFailed TraceStatus = "failed"

	// TraceDegraded trace 完成但存在降级。
	TraceDegraded TraceStatus = "degraded"
)

// SearchRequest 是 P4 Orchestrator 内部 search DTO。
// 与 memory.SearchRequest 分离，是为了允许 P4 携带 intent/mode/trace 等内部字段，同时保持 MCP API 向后兼容。
type SearchRequest struct {
	// RequestID 请求 ID；可选，用于和上游调用链关联。
	RequestID string

	// Query 查询文本；来自 memory.SearchRequest.Query。
	Query string

	// Task 当前任务描述；search 场景可为空，context 场景会填充。
	Task string

	// WorkspaceID 工作空间 ID。
	WorkspaceID string

	// ProjectID 项目 ID。
	ProjectID string

	// RepoID 仓库 ID。
	RepoID string

	// SessionID 会话 ID。
	SessionID string

	// TaskID 任务 ID；P4 access log 可用。
	TaskID string

	// Scopes scope 过滤列表。
	Scopes []string

	// MemoryTypes 记忆类型过滤列表。
	MemoryTypes []string

	// Limit 结果数量限制。
	Limit int

	// IncludeArchived 是否包含归档记忆。
	IncludeArchived bool

	// IncludeEvidence 是否包含证据摘要。
	IncludeEvidence bool

	// IncludeCodeRefs 是否返回 code_ref。
	IncludeCodeRefs bool

	// IntentHint 上游显式意图；为空时由 D2 intent detector 识别。
	IntentHint RetrievalIntent

	// ModeHint 上游显式检索模式；为空时由 Orchestrator 按能力选择。
	ModeHint RetrievalMode
}

// ContextRequest 是 P4 Orchestrator 内部 context DTO。
type ContextRequest struct {
	// RequestID 请求 ID；可选，用于和上游调用链关联。
	RequestID string

	// Task 当前任务描述。
	Task string

	// WorkspaceID 工作空间 ID。
	WorkspaceID string

	// ProjectID 项目 ID。
	ProjectID string

	// RepoID 仓库 ID。
	RepoID string

	// SessionID 会话 ID。
	SessionID string

	// AgentType Agent 类型。
	AgentType string

	// TokenBudget 调用方请求的 token 预算。
	TokenBudget int

	// IncludeCodeRefs 是否返回 code_ref。
	IncludeCodeRefs bool

	// IncludeEvidenceSummary 是否返回证据摘要。
	IncludeEvidenceSummary bool

	// IntentHint 上游显式意图；为空时由 D2 intent detector 识别。
	IntentHint RetrievalIntent
}

// SearchResult 是 P4 Orchestrator 内部 search 结果。
type SearchResult struct {
	// RequestID 请求 ID。
	RequestID string

	// RetrievalTraceID 检索 trace ID。
	RetrievalTraceID string

	// Intent 检索意图。
	Intent RetrievalIntent

	// Mode 检索模式。
	Mode RetrievalMode

	// Items 排序后的结果项。
	Items []ResultItem

	// Diagnostics 检索诊断。
	Diagnostics Diagnostics
}

// ContextResult 是 P4 Orchestrator 内部 context 结果。
type ContextResult struct {
	// RequestID 请求 ID。
	RequestID string

	// RetrievalTraceID 检索 trace ID。
	RetrievalTraceID string

	// Intent 检索意图。
	Intent RetrievalIntent

	// Mode 检索模式。
	Mode RetrievalMode

	// ContextPack 可注入 Agent prompt 的上下文包。
	ContextPack memory.ContextPack

	// UsedMemoryIDs 实际注入上下文的 memory_id。
	UsedMemoryIDs []string

	// Diagnostics context 构造诊断。
	Diagnostics ContextDiagnostics

	// LatencyMS 构造延迟。
	LatencyMS int64
}

// ResultItem 是 P4 search 返回的单条记忆。
type ResultItem struct {
	// MemoryID 记忆 ID。
	MemoryID string

	// MemoryType 记忆类型。
	MemoryType string

	// Scope 作用域。
	Scope string

	// Title 标题。
	Title string

	// Content 记忆内容摘要。
	Content string

	// Score 最终排序分。
	Score float64

	// Confidence 置信度。
	Confidence float64

	// State 记忆状态。
	State string

	// Tier 记忆层级。
	Tier string

	// EvidenceRefs 证据引用。
	EvidenceRefs []string

	// ScoreBreakdown 分数拆解。
	ScoreBreakdown memory.ScoreBreakdown

	// InclusionReasons 注入或召回原因。
	InclusionReasons []string

	// CodeRefs 代码引用。
	CodeRefs []memory.CodeRef
}

// Candidate 是 P4 检索排序前后的候选模型。
// 它保留各分量分数，便于 D2/C1/C2 在不改变 API 的情况下实现 rerank、relation expansion 和降级诊断。
type Candidate struct {
	// Memory 记忆领域对象。
	Memory memory.MemoryItem

	// FTSScore FTS/BM25 召回分。
	FTSScore float64

	// SemanticScore 语义向量分。
	SemanticScore float64

	// TaskFit 任务匹配度。
	TaskFit float64

	// ScopeFit scope 匹配度。
	ScopeFit float64

	// RetentionScore 保留价值分。
	RetentionScore float64

	// RelationSupport 关系支持分。
	RelationSupport float64

	// SourceQuality 来源质量分。
	SourceQuality float64

	// RecencyFit 近期活跃分。
	RecencyFit float64

	// ConflictPenalty 冲突惩罚。
	ConflictPenalty float64

	// StalenessPenalty 过期或 code_ref 失效惩罚。
	StalenessPenalty float64

	// ContextCostPenalty 上下文成本惩罚。
	ContextCostPenalty float64

	// FinalScore 最终排序分。
	FinalScore float64

	// ScoreBreakdown 分数拆解。
	ScoreBreakdown memory.ScoreBreakdown

	// InclusionReasons 召回或注入原因。
	InclusionReasons []string

	// RelatedMemoryIDs 关系扩展命中的 memory_id。
	RelatedMemoryIDs []string

	// CodeRefs 关联代码引用。
	CodeRefs []memory.CodeRef
}

// Diagnostics 是 P4 search 诊断信息。
type Diagnostics struct {
	// RetrievalTraceID 检索 trace ID。
	RetrievalTraceID string

	// Intent 检索意图。
	Intent RetrievalIntent

	// Mode 检索模式。
	Mode RetrievalMode

	// UsedFTS 是否使用 FTS。
	UsedFTS bool

	// UsedVector 是否使用向量检索。
	UsedVector bool

	// UsedRelation 是否使用关系扩展。
	UsedRelation bool

	// UsedCodeIndex 是否使用 Code Index。
	UsedCodeIndex bool

	// UsedDocIndex 是否使用 Doc Index。
	UsedDocIndex bool

	// FallbackReasons 降级原因。
	FallbackReasons []string

	// CandidateCount 候选数量。
	CandidateCount int

	// InjectedCount 注入数量。
	InjectedCount int

	// LatencyMS 延迟毫秒。
	LatencyMS int64

	// Status trace 状态。
	Status TraceStatus
}

// ContextDiagnostics 是 P4 context 构造诊断。
type ContextDiagnostics struct {
	// Diagnostics 基础检索诊断。
	Diagnostics

	// BudgetAllocation 预算分配。
	BudgetAllocation BudgetAllocation
}

// BudgetAllocation 表示 P4 context builder 的预算分配结果。
type BudgetAllocation struct {
	// TotalTokens 总 token 预算。
	TotalTokens int

	// ReservedTokens summary/diagnostics 预留预算。
	ReservedTokens int

	// Buckets 各 bucket 预算。
	Buckets []BudgetBucket
}

// BudgetBucket 表示某一类上下文的预算约束。
type BudgetBucket struct {
	// Name bucket 名称。
	Name string

	// MaxItems 最大条数。
	MaxItems int

	// MaxTokensPerItem 单条最大 token。
	MaxTokensPerItem int

	// AllocatedTokens 已分配 token。
	AllocatedTokens int
}

// TraceRecord 是 retrieval_trace 的领域 DTO。
type TraceRecord struct {
	// ID trace ID。
	ID string

	// SessionID 会话 ID。
	SessionID string

	// TaskID 任务 ID。
	TaskID string

	// WorkspaceID 工作空间 ID。
	WorkspaceID string

	// ProjectID 项目 ID。
	ProjectID string

	// RepoID 仓库 ID。
	RepoID string

	// Query 查询文本摘要。
	Query string

	// Task 任务文本摘要。
	Task string

	// Intent 检索意图。
	Intent RetrievalIntent

	// Mode 检索模式。
	Mode RetrievalMode

	// UsedFTS 是否使用 FTS。
	UsedFTS bool

	// UsedVector 是否使用向量检索。
	UsedVector bool

	// UsedRelation 是否使用关系扩展。
	UsedRelation bool

	// UsedCodeIndex 是否使用 Code Index。
	UsedCodeIndex bool

	// UsedDocIndex 是否使用 Doc Index。
	UsedDocIndex bool

	// FallbackReason 降级原因 JSON 或短文本。
	FallbackReason string

	// CandidateCount 候选数量。
	CandidateCount int

	// InjectedCount 注入数量。
	InjectedCount int

	// LatencyMS 延迟毫秒。
	LatencyMS int64

	// Status 状态。
	Status TraceStatus

	// CreatedAt 创建时间。
	CreatedAt time.Time
}

// TraceQuery 是 retrieval_trace 诊断查询条件。
// 设计约束：列表查询必须带 WorkspaceID，避免诊断入口退化为全库扫描。
type TraceQuery struct {
	// WorkspaceID 工作空间 ID，列表查询必填。
	WorkspaceID string

	// ProjectID 项目 ID，可选过滤条件。
	ProjectID string

	// RepoID 仓库 ID，可选过滤条件。
	RepoID string

	// SessionID 会话 ID，可选过滤条件。
	SessionID string

	// TaskID 任务 ID，可选过滤条件。
	TaskID string

	// Status trace 状态，可选过滤条件。
	Status TraceStatus

	// Limit 返回数量限制。
	Limit int
}

// AccessLogRecord 是 memory_access_log 的领域 DTO。
type AccessLogRecord struct {
	// ID access log ID。
	ID string

	// MemoryID 记忆 ID。
	MemoryID string

	// SessionID 会话 ID。
	SessionID string

	// TaskID 任务 ID。
	TaskID string

	// RetrievalTraceID 检索 trace ID。
	RetrievalTraceID string

	// EventType 事件类型，如 retrieved/injected/user_rejected。
	EventType string

	// EventWeight 事件权重。
	EventWeight float64

	// SourceType 记忆来源类型。
	SourceType string

	// SourceQuality 来源质量分。
	SourceQuality float64

	// Query 查询文本摘要。
	Query string

	// Rank 排序位置。
	Rank int

	// Score 最终分数。
	Score float64

	// ScoreBreakdown 分数拆解。
	ScoreBreakdown memory.ScoreBreakdown

	// InclusionReasons 注入原因。
	InclusionReasons []string

	// UsedInContext 是否实际注入上下文。
	UsedInContext bool

	// Feedback 用户反馈。
	Feedback string

	// CreatedAt 创建时间。
	CreatedAt time.Time
}

// AccessLogQuery 是 memory_access_log 诊断查询条件。
// 设计约束：必须按 retrieval_trace_id 或 memory_id 查询，避免高增长表被无边界扫描。
type AccessLogQuery struct {
	// RetrievalTraceID 检索 trace ID。
	RetrievalTraceID string

	// MemoryID 记忆 ID。
	MemoryID string

	// TaskID 任务 ID，用于 task_success 等按任务聚合反馈。
	TaskID string

	// EventType access log 事件类型，可选过滤条件。
	EventType string

	// Limit 返回数量限制。
	Limit int
}

// FromMemorySearchRequest 将 P1/P4 对外 memory.search 请求转换为 retrieval 内部 DTO。
func FromMemorySearchRequest(req memory.SearchRequest) SearchRequest {
	return SearchRequest{
		Query:           req.Query,
		WorkspaceID:     req.WorkspaceID,
		ProjectID:       req.ProjectID,
		RepoID:          req.RepoID,
		SessionID:       req.SessionID,
		Scopes:          append([]string(nil), req.Scope...),
		MemoryTypes:     append([]string(nil), req.MemoryTypes...),
		Limit:           req.Limit,
		IncludeArchived: req.IncludeArchived,
		IncludeEvidence: req.IncludeEvidence,
		IncludeCodeRefs: req.IncludeCodeRefs,
	}
}

// FromMemoryContextRequest 将 P1/P4 对外 memory.context 请求转换为 retrieval 内部 DTO。
func FromMemoryContextRequest(req memory.ContextRequest) ContextRequest {
	return ContextRequest{
		Task:                   req.Task,
		WorkspaceID:            req.WorkspaceID,
		ProjectID:              req.ProjectID,
		RepoID:                 req.RepoID,
		SessionID:              req.SessionID,
		AgentType:              req.AgentType,
		TokenBudget:            req.TokenBudget,
		IncludeCodeRefs:        req.IncludeCodeRefs,
		IncludeEvidenceSummary: req.IncludeEvidenceSummary,
	}
}
