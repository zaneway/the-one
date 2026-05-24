package retention

import "time"

const (
	ModeCleanupTemporary = "cleanup_temporary"
	ModeRecomputeScores  = "recompute_scores"
)

const (
	ActionArchive     = "archive"
	ActionUpdateScore = "update_score"
)

const (
	ReasonTemporaryExpired = "temporary_expired"
	ReasonScoreRecomputed  = "score_recomputed"
	ReasonArchiveCandidate = "archive_candidate"
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

type Input struct {
	State                  string
	Tier                   string
	MemoryType             string
	Confidence             float64
	Importance             float64
	UserConfirmed          bool
	Pinned                 bool
	EffectiveReinforcement float64
	RetentionScore         float64
	ValidUntil             time.Time
	UpdatedAt              time.Time
	Now                    time.Time
}

type MemoryRecord struct {
	ID                     string
	WorkspaceID            string
	ProjectID              string
	State                  string
	Tier                   string
	MemoryType             string
	Confidence             float64
	Importance             float64
	UserConfirmed          bool
	Pinned                 bool
	EffectiveReinforcement float64
	RetentionScore         float64
	ValidUntil             time.Time
	HasValidUntil          bool
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
	RetentionScore float64
	Tier           string
	UpdatedAt      time.Time
}
