package retention

import "time"

const (
	ModeCleanupTemporary = "cleanup_temporary"
	ModeRecomputeScores  = "recompute_scores"
)

const (
	ActionArchive     = "archive"
	ActionUpdateScore = "update_score"
	ActionDelete      = "delete"
)

const (
	ReasonTemporaryExpired      = "temporary_expired"
	ReasonScoreRecomputed       = "score_recomputed"
	ReasonArchiveCandidate      = "archive_candidate"
	ReasonInvalidRetentionScore = "invalid_retention_score"
)

type RunRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	Mode        string `json:"mode"`
	DryRun      bool   `json:"dry_run"`
	Limit       int    `json:"limit,omitempty"`
}

type ActionItem struct {
	MemoryID       string  `json:"memory_id"`
	Action         string  `json:"action"`
	Reason         string  `json:"reason"`
	Tier           string  `json:"tier,omitempty"`
	RetentionScore float64 `json:"retention_score,omitempty"`
}

type RunResponse struct {
	Mode        string       `json:"mode"`
	DryRun      bool         `json:"dry_run"`
	Items       []ActionItem `json:"items"`
	Processed   int          `json:"processed"`
	Diagnostics []string     `json:"diagnostics,omitempty"`
}

// AccessFeedbackSummary 由 access log 聚合得到的访问与强化信号。
type AccessFeedbackSummary struct {
	EffectiveReinforcement float64
	ReinforcementCount     float64
	LastReinforcedAt       time.Time
	BaseActivation         float64
	BaseActivationNorm     float64
	NegativePenalty        float64
}

// RelationSignals 记忆关系图上的计数信号，用于 relation_factor 与 conflict_penalty。
type RelationSignals struct {
	SupportingCount         int
	ContradictingCount      int
	LinkedLongTermCount     int
	UnresolvedConflictCount int
	IsSuperseded            bool
}

// Input 是架构 §8.2 Retention Score 的完整计算输入。
type Input struct {
	State         string
	Tier          string
	MemoryType    string
	Scope         string
	SourceType    string
	Confidence    float64
	Importance    float64
	SourceQuality float64
	EncodingDepth int
	DecayRate     float64

	UserConfirmed bool
	Pinned        bool
	SupersedesID  string

	EffectiveReinforcement float64
	RetentionScore         float64
	Access                 AccessFeedbackSummary
	Relations              RelationSignals

	ValidUntil      time.Time
	LastValidatedAt time.Time
	LastAccessedAt  time.Time
	UpdatedAt       time.Time
	CreatedAt       time.Time
	Now             time.Time

	StaleCodeRefCount int
	TemporaryTTLDays  int
}

type MemoryRecord struct {
	ID                     string
	WorkspaceID            string
	ProjectID              string
	Scope                  string
	SourceType             string
	State                  string
	Tier                   string
	MemoryType             string
	Confidence             float64
	Importance             float64
	SourceQuality          float64
	EncodingDepth          int
	DecayRate              float64
	UserConfirmed          bool
	Pinned                 bool
	SupersedesID           string
	EffectiveReinforcement float64
	RetentionScore         float64
	ValidUntil             time.Time
	HasValidUntil          bool
	LastValidatedAt        time.Time
	HasLastValidatedAt     bool
	LastAccessedAt         time.Time
	HasLastAccessedAt      bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ListRequest struct {
	WorkspaceID      string
	ProjectID        string
	Limit            int
	TemporaryTTLDays int
	Now              time.Time
}

type ScoreUpdate struct {
	RetentionScore         float64
	Tier                   string
	State                  string
	StateTransitionReason  string
	EffectiveReinforcement float64
	ReinforcementCount     float64
	LastReinforcedAt       time.Time
	HasLastReinforcedAt    bool
	UpdatedAt              time.Time
}
