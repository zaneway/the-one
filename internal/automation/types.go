package automation

import (
	"time"

	"github.com/zaneway/theone/internal/memory"
)

// ============================================================================
// 异步任务状态常量
// Worker 通过状态流转控制 job 的生命周期
// ============================================================================
const (
	JobStatusPending   = "pending"   // 等待领取
	JobStatusRunning   = "running"   // 执行中
	JobStatusSucceeded = "succeeded" // 执行成功
	JobStatusFailed    = "failed"    // 执行失败（达到最大重试次数）
	JobStatusCancelled = "cancelled" // 已取消
)

const (
	// JobTypeExtractEvidence 从 raw_event 抽取可解释 evidence。
	JobTypeExtractEvidence = "extract_evidence"
	// JobTypeGenerateMemoryCandidate 从 evidence 生成候选记忆。
	JobTypeGenerateMemoryCandidate = "generate_memory_candidate"
	// JobTypeComputeAdmission 对候选记忆执行准入控制。
	JobTypeComputeAdmission = "compute_admission"
	// JobTypeResolveCodeRef 从显式 payload 或 source_ref 生成/刷新 code_ref。
	JobTypeResolveCodeRef = "resolve_code_ref"
	// JobTypeRefreshCodeRefStatus 批量刷新已有 code_ref 的解析状态。
	JobTypeRefreshCodeRefStatus = "refresh_code_ref_status"
	// JobTypeBuildDocSnapshot 写入预计算的文档 snapshot 和 section metadata。
	JobTypeBuildDocSnapshot = "build_doc_snapshot"
	// JobTypeComputeEmbedding 写入预计算 memory embedding；provider=none 时安全跳过。
	JobTypeComputeEmbedding = "compute_embedding"
	// JobTypeCleanupAccessLog 清理低价值 memory_access_log 明细。
	JobTypeCleanupAccessLog = "cleanup_access_log"
)

const (
	// TargetTypeRawEvent 表示 job target 是 raw_event。
	TargetTypeRawEvent = "raw_event"
	// TargetTypeEvidence 表示 job target 是 evidence。
	TargetTypeEvidence = "evidence"
	// TargetTypeMemoryCandidate 表示 job target 是 memory_candidate。
	TargetTypeMemoryCandidate = "memory_candidate"
	// TargetTypeMemoryItem 表示 job target 是 memory_item。
	TargetTypeMemoryItem = "memory_item"
	// TargetTypeDocPath 表示 job target 是文档路径。
	TargetTypeDocPath = "doc_path"
	// TargetTypeWorkspace 表示 job target 是 workspace。
	TargetTypeWorkspace = "workspace"
	// TargetTypeRepo 表示 job target 是 repo。
	TargetTypeRepo = "repo"
	// TargetTypeCodeRef 表示 job target 是 code_ref。
	TargetTypeCodeRef = "code_ref"
)

// ============================================================================
// 候选记忆状态常量
// 记录候选记忆在 admission 管道中的状态流转
// ============================================================================
const (
	CandidateStatusGenerated = "generated" // Provider 已生成，等待 admission
	CandidateStatusAdmitted  = "admitted"  // admission 通过，已写入 memory_item
	CandidateStatusDropped   = "dropped"   // admission 拒绝，不写入长期记忆
	CandidateStatusMerged    = "merged"    // 与已有记忆合并（预留）
	CandidateStatusFailed    = "failed"    // 处理失败
)

// AsyncJob 表示 P3 异步处理队列中的一条任务记录。
// Worker 通过 status、next_run_at 和 retry_count 控制本地重试，不在事务内执行 Provider 逻辑。
// 管道流转：extract_evidence → generate_memory_candidate → compute_admission。
// 每个 job 通过 TargetType + TargetID 关联处理对象，DedupKey 保证幂等入队。
type AsyncJob struct {
	ID          string
	JobType     string
	TargetType  string
	TargetID    string
	Status      string
	Priority    int
	RetryCount  int
	MaxRetries  int
	NextRunAt   time.Time
	LastError   string
	DedupKey    string
	PayloadJSON string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListJobsRequest 是异步任务诊断查询条件。
type ListJobsRequest struct {
	Status      string `json:"status,omitempty"`
	JobType     string `json:"job_type,omitempty"`
	TargetType  string `json:"target_type,omitempty"`
	TargetID    string `json:"target_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// RelatedEventsRequest 用于 Provider 抽取时读取同 session/task 的近邻事件。
type RelatedEventsRequest struct {
	SessionID string
	TaskID    string
	Limit     int
}

// MemoryCandidateRecord 保存 Provider 生成的候选记忆诊断记录。
// 它不是稳定长期记忆，只有 Admission 通过后才会关联 resulting_memory_id。
type MemoryCandidateRecord struct {
	ID                    string
	RawEventID            string
	EvidenceID            string
	Provider              string
	MemoryType            string
	Scope                 string
	WorkspaceID           string
	UserID                string
	ProjectID             string
	RepoID                string
	SessionID             string
	TaskID                string
	Title                 string
	Content               string
	KeywordsJSON          string
	EntitiesJSON          string
	RetrievalCuesJSON     string
	TagsJSON              string
	SourceEvidenceIDsJSON string
	ReviewCheckpointJSON  string
	Confidence            float64
	Importance            float64
	EncodingDepth         int
	CandidateReasonJSON   string
	AdmissionScore        float64
	AdmissionDecision     string
	AdmissionReasonJSON   string
	ResultingMemoryID     string
	Status                string
	DedupKey              string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ListCandidatesRequest 是候选记忆诊断查询条件。
type ListCandidatesRequest struct {
	Status      string `json:"status,omitempty"`
	MemoryType  string `json:"memory_type,omitempty"`
	Provider    string `json:"provider,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	RawEventID  string `json:"raw_event_id,omitempty"`
	EvidenceID  string `json:"evidence_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type GetJobRequest struct {
	JobID string `json:"job_id"`
}

type JobDiagnostic struct {
	JobID          string    `json:"job_id"`
	JobType        string    `json:"job_type"`
	TargetType     string    `json:"target_type"`
	TargetID       string    `json:"target_id"`
	Status         string    `json:"status"`
	RetryCount     int       `json:"retry_count"`
	MaxRetries     int       `json:"max_retries"`
	LastError      string    `json:"last_error,omitempty"`
	NextRunAt      time.Time `json:"next_run_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	PayloadSummary string    `json:"payload_summary,omitempty"`
	PayloadHash    string    `json:"payload_hash,omitempty"`
}

type ListJobsResponse struct {
	Jobs        []JobDiagnostic `json:"jobs"`
	Diagnostics []string        `json:"diagnostics,omitempty"`
}

type GetJobResponse struct {
	Job JobDiagnostic `json:"job"`
}

type GetCandidateRequest struct {
	CandidateID string `json:"candidate_id"`
}

type CandidateDiagnostic struct {
	CandidateID         string    `json:"candidate_id"`
	RawEventID          string    `json:"raw_event_id,omitempty"`
	EvidenceID          string    `json:"evidence_id,omitempty"`
	Provider            string    `json:"provider"`
	MemoryType          string    `json:"memory_type"`
	Scope               string    `json:"scope"`
	WorkspaceID         string    `json:"workspace_id,omitempty"`
	ProjectID           string    `json:"project_id,omitempty"`
	RepoID              string    `json:"repo_id,omitempty"`
	ContentSummary      string    `json:"content_summary"`
	AdmissionScore      float64   `json:"admission_score,omitempty"`
	AdmissionDecision   string    `json:"admission_decision,omitempty"`
	AdmissionReasonJSON string    `json:"admission_reason_json,omitempty"`
	ResultingMemoryID   string    `json:"resulting_memory_id,omitempty"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ListCandidatesResponse struct {
	Candidates  []CandidateDiagnostic `json:"candidates"`
	Diagnostics []string              `json:"diagnostics,omitempty"`
}

type GetCandidateResponse struct {
	Candidate CandidateDiagnostic `json:"candidate"`
}

const ReconcileModeOrphanRawEvent = "orphan_raw_event"

const ReconcileReasonMissingExtractJob = "missing_extract_evidence_job"

// OrphanRawEventRequest 查询没有 extract_evidence job 且没有 evidence 的 raw_event。
type OrphanRawEventRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// ReconcileRequest 是 memory.jobs.reconcile 的请求结构。
type ReconcileRequest struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	Mode        string `json:"mode"`
	DryRun      bool   `json:"dry_run"`
	Limit       int    `json:"limit,omitempty"`
}

type ReconcileItem struct {
	RawEventID string `json:"raw_event_id"`
	Reason     string `json:"reason"`
}

type ReconcileResponse struct {
	Mode        string          `json:"mode"`
	DryRun      bool            `json:"dry_run"`
	Items       []ReconcileItem `json:"items"`
	Enqueued    int             `json:"enqueued"`
	Diagnostics []string        `json:"diagnostics,omitempty"`
}

type AutomationStatusResponse struct {
	WorkerEnabled          bool   `json:"worker_enabled"`
	Provider               string `json:"provider"`
	EnableAutoProcessing   bool   `json:"enable_auto_processing"`
	PendingJobs            int    `json:"pending_jobs"`
	RunningJobs            int    `json:"running_jobs"`
	FailedJobs             int    `json:"failed_jobs"`
	RecentJobUpdatedAt     string `json:"recent_job_updated_at,omitempty"`
	RecentError            string `json:"recent_error,omitempty"`
	RetentionJobEnabled    bool   `json:"retention_job_enabled"`
	TemporaryTTLDays       int    `json:"temporary_ttl_days"`
	ShortTermTTLDays       int    `json:"short_term_ttl_days"`
	DiagnosticsLimitCapped bool   `json:"diagnostics_limit_capped,omitempty"`
}

// EvidenceDraftKey 表示自动 evidence 的幂等键。
type EvidenceDraftKey struct {
	RawEventID           string
	SourceType           string
	InterpretedStatement string
}

// AutomatedMemoryWrite 描述一次自动写入 memory_item 所需的最小事务输入。
type AutomatedMemoryWrite struct {
	Item             memory.MemoryItem
	EvidenceIDs      []string
	EvidenceRelation string
	ReviewCheckpoint *memory.ReviewCheckpoint
}

// AutomatedMemoryCorrection 描述用户纠正命中旧 memory 后的原地覆盖写入。
// P3 采用覆盖语义：保留旧 memory_id，更新内容和检索字段，并追加新 evidence/review 轨迹。
type AutomatedMemoryCorrection struct {
	TargetMemoryID   string
	Item             memory.MemoryItem
	EvidenceIDs      []string
	EvidenceRelation string
	ReviewFeedback   string
}

// RelatedMemoryRequest 用于 Admission 前查找同 scope 的相关记忆。
type RelatedMemoryRequest struct {
	WorkspaceID string
	ProjectID   string
	RepoID      string
	Scope       string
	MemoryType  string
	Query       string
	Limit       int
}

// CorrectionTargetRequest 用于把 user.correction source_ref 定位到旧 memory。
type CorrectionTargetRequest struct {
	TargetMemoryID string
	TargetEventID  string
}
