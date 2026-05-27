package processor

import (
	"context"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

// Provider turns minimized capture events into explainable evidence and memory candidates.
// It must not write storage, decide final admission, or call external indexes.
type Provider interface {
	Name() string
	ExtractEvidence(ctx context.Context, input EvidenceInput) ([]EvidenceDraft, error)
	GenerateCandidates(ctx context.Context, input CandidateInput) ([]MemoryCandidate, error)
}

type CaptureQualitySnapshot struct {
	CaptureLevel                  int
	CapturedEventCount            int
	ToolResultCount               int
	FileEditCount                 int
	ConversationMessageCount      int
	ContentBoundaryRejectionCount int
}

type EvidenceInput struct {
	RawEvent       capture.RawEvent
	Session        capture.AgentSession
	Task           capture.AgentTask
	CaptureQuality CaptureQualitySnapshot
	RelatedEvents  []capture.RawEvent
	Now            time.Time
}

type EvidenceDraft struct {
	SourceType           string
	InterpretedStatement string
	Keywords             []string
	SalientSpans         []string
	SourceRef            map[string]any
	Confidence           float64
}

type CandidateInput struct {
	Evidence      memory.Evidence
	RawEvent      capture.RawEvent
	Session       capture.AgentSession
	Task          capture.AgentTask
	RelatedMemory []memory.MemoryItem
	Now           time.Time
}

type ReviewCheckpointDraft struct {
	CheckpointType    string
	ReviewIntent      []string
	TargetDocs        []map[string]any
	TargetSections    []map[string]any
	TargetHashes      []map[string]any
	Conclusion        string
	ConfirmedBaseline []string
	IgnoredItems      []string
	DeferredItems     []string
	OpenItems         []string
	NextReviewPolicy  map[string]any
}

type MemoryCandidate struct {
	CandidateID       string
	MemoryType        string
	Scope             string
	WorkspaceID       string
	UserID            string
	ProjectID         string
	RepoID            string
	SessionID         string
	TaskID            string
	SourceType        string
	Title             string
	Content           string
	Keywords          []string
	Entities          []string
	RetrievalCues     []string
	Tags              []string
	Confidence        float64
	Importance        float64
	EncodingDepth     int
	ReviewCheckpoint  *ReviewCheckpointDraft
	CandidateReason   []string
	SourceEvidenceIDs []string
}
