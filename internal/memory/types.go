package memory

import (
	"encoding/json"
	"time"
)

const (
	ScopeUserGlobal   = "user_global"
	ScopeProjectLocal = "project_local"
	ScopeRepoLocal    = "repo_local"
	ScopeSession      = "session"

	TypePreference       = "preference"
	TypeDecision         = "decision"
	TypeConstraint       = "constraint"
	TypeFailure          = "failure"
	TypeProjectFact      = "project_fact"
	TypeProcedure        = "procedure"
	TypeTemporaryState   = "temporary_state"
	TypeReviewCheckpoint = "review_checkpoint"

	StateProvisional   = "provisional"
	StatePendingReview = "pending_review"
	StateStable        = "stable"
	StateArchived      = "archived"
	StateDeleted       = "deleted"

	TierTemporary = "temporary"
	TierLongTerm  = "long_term"
	TierDurable   = "durable"
)

// EvidenceInput 是 memory.remember 显式证据输入。P1 只保存解释后语句和关键片段。
type EvidenceInput struct {
	InterpretedStatement string         `json:"interpreted_statement"`
	Keywords             []string       `json:"keywords"`
	SalientSpans         []string       `json:"salient_spans"`
	SourceRef            map[string]any `json:"source_ref"`
}

// ReviewCheckpointInput 是 P1 手动复查 checkpoint 输入，不允许保存完整文档正文。
type ReviewCheckpointInput struct {
	CheckpointType    string           `json:"checkpoint_type"`
	ReviewIntent      []string         `json:"review_intent"`
	TargetDocs        []map[string]any `json:"target_docs"`
	TargetSections    []map[string]any `json:"target_sections"`
	TargetHashes      []map[string]any `json:"target_hashes"`
	Conclusion        string           `json:"conclusion"`
	ConfirmedBaseline []string         `json:"confirmed_baseline"`
	IgnoredItems      []string         `json:"ignored_items"`
	DeferredItems     []string         `json:"deferred_items"`
	OpenItems         []string         `json:"open_items"`
	NextReviewPolicy  map[string]any   `json:"next_review_policy"`
}

// RememberRequest 是 memory.remember 请求结构。
type RememberRequest struct {
	Content          string                 `json:"content"`
	Title            string                 `json:"title"`
	MemoryType       string                 `json:"memory_type"`
	Scope            string                 `json:"scope"`
	WorkspaceID      string                 `json:"workspace_id"`
	UserID           string                 `json:"user_id"`
	ProjectID        string                 `json:"project_id"`
	RepoID           string                 `json:"repo_id"`
	SessionID        string                 `json:"session_id"`
	TaskID           string                 `json:"task_id"`
	SourceType       string                 `json:"source_type"`
	Importance       float64                `json:"importance"`
	Confidence       float64                `json:"confidence"`
	Pinned           bool                   `json:"pinned"`
	Tags             []string               `json:"tags"`
	Keywords         []string               `json:"keywords"`
	Entities         []string               `json:"entities"`
	RetrievalCues    []string               `json:"retrieval_cues"`
	ReviewCheckpoint *ReviewCheckpointInput `json:"review_checkpoint"`
	Evidence         EvidenceInput          `json:"evidence"`
}

type RememberResponse struct {
	MemoryID string `json:"memory_id"`
	State    string `json:"state"`
	Tier     string `json:"tier"`
	Deduped  bool   `json:"deduped"`
}

// SearchRequest 是 memory.search 请求结构。
type SearchRequest struct {
	Query           string   `json:"query"`
	WorkspaceID     string   `json:"workspace_id"`
	ProjectID       string   `json:"project_id"`
	RepoID          string   `json:"repo_id"`
	SessionID       string   `json:"session_id"`
	Scope           []string `json:"scope"`
	MemoryTypes     []string `json:"memory_types"`
	Limit           int      `json:"limit"`
	IncludeArchived bool     `json:"include_archived"`
	IncludeEvidence bool     `json:"include_evidence"`
}

type SearchResult struct {
	MemoryID     string   `json:"memory_id"`
	MemoryType   string   `json:"memory_type"`
	Scope        string   `json:"scope"`
	Title        string   `json:"title,omitempty"`
	Content      string   `json:"content"`
	Score        float64  `json:"score"`
	Confidence   float64  `json:"confidence"`
	State        string   `json:"state"`
	Tier         string   `json:"tier"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

type SearchDiagnostics struct {
	FTSHits       int    `json:"fts_hits"`
	FilteredCount int    `json:"filtered_count"`
	LatencyMS     int64  `json:"latency_ms"`
	Fallback      string `json:"fallback"`
}

type SearchResponse struct {
	Results     []SearchResult    `json:"results"`
	Diagnostics SearchDiagnostics `json:"diagnostics"`
}

// ContextRequest 是 memory.context 请求结构。
type ContextRequest struct {
	Task                   string `json:"task"`
	WorkspaceID            string `json:"workspace_id"`
	ProjectID              string `json:"project_id"`
	RepoID                 string `json:"repo_id"`
	SessionID              string `json:"session_id"`
	AgentType              string `json:"agent_type"`
	TokenBudget            int    `json:"token_budget"`
	IncludeCodeRefs        bool   `json:"include_code_refs"`
	IncludeEvidenceSummary bool   `json:"include_evidence_summary"`
}

type ContextPack struct {
	Summary     string          `json:"summary"`
	Memories    []ContextMemory `json:"memories"`
	Constraints []string        `json:"constraints"`
	CodeRefs    []any           `json:"code_refs"`
}

type ContextMemory struct {
	MemoryID    string   `json:"memory_id"`
	Type        string   `json:"type"`
	Compressed  string   `json:"compressed"`
	WhyIncluded []string `json:"why_included"`
}

type ContextResponse struct {
	ContextPack   ContextPack `json:"context_pack"`
	UsedMemoryIDs []string    `json:"used_memory_ids"`
	LatencyMS     int64       `json:"latency_ms"`
}

// ReviewRequest 是 memory.review 请求结构，支持 list/approve/reject/edit/archive/delete。
type ReviewRequest struct {
	Action      string `json:"action"`
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id"`
	State       string `json:"state"`
	Limit       int    `json:"limit"`
	MemoryID    string `json:"memory_id"`
	EditContent string `json:"edit_content"`
	Feedback    string `json:"feedback"`
	Reviewer    string `json:"reviewer"`
}

type ReviewResponse struct {
	MemoryID      string       `json:"memory_id,omitempty"`
	State         string       `json:"state,omitempty"`
	UserConfirmed bool         `json:"user_confirmed"`
	Results       []MemoryItem `json:"results,omitempty"`
}

// MemoryItem 是 P1 服务层使用的记忆聚合结构。
type MemoryItem struct {
	ID                string    `json:"memory_id"`
	Scope             string    `json:"scope"`
	WorkspaceID       string    `json:"workspace_id,omitempty"`
	UserID            string    `json:"user_id,omitempty"`
	ProjectID         string    `json:"project_id,omitempty"`
	RepoID            string    `json:"repo_id,omitempty"`
	SessionID         string    `json:"session_id,omitempty"`
	TaskID            string    `json:"task_id,omitempty"`
	MemoryType        string    `json:"memory_type"`
	SourceType        string    `json:"source_type,omitempty"`
	SourceQuality     float64   `json:"source_quality"`
	Title             string    `json:"title,omitempty"`
	Content           string    `json:"content"`
	NormalizedContent string    `json:"normalized_content,omitempty"`
	SearchText        string    `json:"-"`
	KeywordsJSON      string    `json:"keywords_json,omitempty"`
	EntitiesJSON      string    `json:"entities_json,omitempty"`
	RetrievalCuesJSON string    `json:"retrieval_cues_json,omitempty"`
	TagsJSON          string    `json:"tags_json,omitempty"`
	State             string    `json:"state"`
	Confidence        float64   `json:"confidence"`
	Importance        float64   `json:"importance"`
	EncodingDepth     int       `json:"encoding_depth"`
	DecayRate         float64   `json:"decay_rate"`
	RetentionScore    float64   `json:"retention_score"`
	Tier              string    `json:"tier"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Pinned            bool      `json:"pinned"`
	UserConfirmed     bool      `json:"user_confirmed"`
	Version           int       `json:"version"`
	SupersedesID      string    `json:"supersedes_id,omitempty"`
	EvidenceRefs      []string  `json:"evidence_refs,omitempty"`
}

// Evidence 是 repository 层持久化证据的结构。
type Evidence struct {
	ID                   string
	SourceType           string
	InterpretedStatement string
	KeywordsJSON         string
	SalientSpansJSON     string
	SourceRefJSON        string
	Confidence           float64
	CreatedAt            time.Time
}

// ReviewCheckpoint 是 repository 层持久化复查 checkpoint 的结构。
type ReviewCheckpoint struct {
	ID                    string
	MemoryID              string
	WorkspaceID           string
	ProjectID             string
	RepoID                string
	SessionID             string
	TaskID                string
	CheckpointType        string
	ReviewIntentJSON      string
	TargetDocsJSON        string
	TargetSectionsJSON    string
	TargetHashesJSON      string
	Conclusion            string
	ConfirmedBaselineJSON string
	IgnoredItemsJSON      string
	DeferredItemsJSON     string
	OpenItemsJSON         string
	NextReviewPolicyJSON  string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func toJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
