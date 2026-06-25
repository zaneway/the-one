package dream

import (
	"context"
	"time"
)

const (
	RouteProject   = "project"
	RouteKnowledge = "knowledge"
	RouteThinking  = "thinking"
	RouteSkills    = "skills"
	RouteInbox     = "inbox"
	RouteArchive   = "archive"

	NoteModeMemory       = "memory"
	NoteModeConsolidated = "consolidated"
	NoteModeMOC          = "moc"
)

type Config struct {
	Enabled   bool            `yaml:"enabled" json:"enabled"`
	Vault     VaultConfig     `yaml:"vault" json:"vault"`
	Scheduler SchedulerConfig `yaml:"scheduler" json:"scheduler"`
	Curation  CurationConfig  `yaml:"curation" json:"curation"`
}

type VaultConfig struct {
	Root           string            `yaml:"root" json:"root"`
	SystemDir      string            `yaml:"system_dir" json:"system_dir"`
	Directories    DirectoryConfig   `yaml:"directories" json:"directories"`
	MemoryTypeDirs map[string]string `yaml:"memory_type_dirs" json:"memory_type_dirs"`
	TopicDirs      map[string]string `yaml:"topic_dirs" json:"topic_dirs"`
	UserNotesDir   string            `yaml:"user_notes_dir" json:"user_notes_dir"`
}

type DirectoryConfig struct {
	Inbox     string `yaml:"inbox" json:"inbox"`
	Projects  string `yaml:"projects" json:"projects"`
	Knowledge string `yaml:"knowledge" json:"knowledge"`
	Thinking  string `yaml:"thinking" json:"thinking"`
	Skills    string `yaml:"skills" json:"skills"`
	MOC       string `yaml:"moc" json:"moc"`
	Archive   string `yaml:"archive" json:"archive"`
}

type SchedulerConfig struct {
	Enabled               bool    `yaml:"enabled" json:"enabled"`
	IntervalMS            int     `yaml:"interval_ms" json:"interval_ms"`
	InitialDelayMS        int     `yaml:"initial_delay_ms" json:"initial_delay_ms"`
	JitterRatio           float64 `yaml:"jitter_ratio" json:"jitter_ratio"`
	MaxRunDurationMS      int     `yaml:"max_run_duration_ms" json:"max_run_duration_ms"`
	SkipIfPreviousRunning bool    `yaml:"skip_if_previous_running" json:"skip_if_previous_running"`
}

type CurationConfig struct {
	Enabled                bool `yaml:"enabled" json:"enabled"`
	MaxInputMemories       int  `yaml:"max_input_memories" json:"max_input_memories"`
	MaxInputChars          int  `yaml:"max_input_chars" json:"max_input_chars"`
	TimeoutMS              int  `yaml:"timeout_ms" json:"timeout_ms"`
	MinGroupSize           int  `yaml:"min_group_size" json:"min_group_size"`
	RequireSourceMemoryIDs bool `yaml:"require_source_memory_ids" json:"require_source_memory_ids"`
	FallbackRules          bool `yaml:"fallback_to_rule_export" json:"fallback_to_rule_export"`
}

type RunRequest struct {
	DryRun      bool   `json:"dry_run,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type RunResponse struct {
	DryRun      bool       `json:"dry_run"`
	Planned     int        `json:"planned"`
	Written     int        `json:"written"`
	Skipped     int        `json:"skipped"`
	Diagnostics []string   `json:"diagnostics,omitempty"`
	Items       []PlanItem `json:"items"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     time.Time  `json:"ended_at"`
}

type PlanItem struct {
	ProjectionID string   `json:"projection_id"`
	MemoryID     string   `json:"memory_id,omitempty"`
	SourceIDs    []string `json:"source_memory_ids,omitempty"`
	Mode         string   `json:"mode"`
	Path         string   `json:"path"`
	Action       string   `json:"action"`
	RouteKey     string   `json:"route_key"`
}

type ListRequest struct {
	WorkspaceID string
	ProjectID   string
	RepoID      string
	Limit       int
}

type MemoryRecord struct {
	ID           string
	Scope        string
	WorkspaceID  string
	ProjectID    string
	RepoID       string
	MemoryType   string
	Title        string
	Content      string
	KeywordsJSON string
	EntitiesJSON string
	TagsJSON     string
	State        string
	Tier         string
	Version      int
	Confidence   float64
	Importance   float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RelationRecord struct {
	SourceID     string
	TargetID     string
	RelationType string
	Weight       float64
	UpdatedAt    time.Time
}

type Repository interface {
	ListMemoriesForDream(ctx context.Context, req ListRequest) ([]MemoryRecord, error)
	ListRelationsForDream(ctx context.Context, req ListRequest) ([]RelationRecord, error)
}

type Curator interface {
	Curate(ctx context.Context, input CurationInput) (CurationResult, error)
}

type CuratorFunc func(ctx context.Context, input CurationInput) (CurationResult, error)

func (f CuratorFunc) Curate(ctx context.Context, input CurationInput) (CurationResult, error) {
	return f(ctx, input)
}

type CurationInput struct {
	Memories  []MemoryRecord
	Relations []RelationRecord
	Config    CurationConfig
}

type CurationResult struct {
	Groups []CurationGroup
}

type CurationGroup struct {
	ProjectionID     string              `json:"projection_id"`
	TopicKey         string              `json:"topic_key"`
	Title            string              `json:"title"`
	Summary          string              `json:"summary"`
	SourceMemoryIDs  []string            `json:"source_memory_ids"`
	SourceMap        map[string][]string `json:"source_map"`
	RouteCategory    string              `json:"route_category"`
	RouteSubject     string              `json:"route_subject"`
	MemoryTypeBucket string              `json:"memory_type_bucket"`
}
