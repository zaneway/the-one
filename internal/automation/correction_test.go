package automation_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
	"github.com/zaneway/theone/internal/storage/sqlite"
)

func TestServiceSupersedesConflictingMemoryWithoutCorrectionTarget(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable")
	}

	now := time.Date(2026, 5, 24, 11, 0, 0, 0, time.UTC)
	oldEvidence := memory.Evidence{ID: "ev_old_cache", RawEventID: "evt_old_cache", SourceType: "agent_summary", InterpretedStatement: "缓存使用 Redis。", Confidence: 0.8}
	if err := store.WriteEvidence(ctx, oldEvidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item: memory.MemoryItem{
			ID: "mem_old_cache", Scope: memory.ScopeProjectLocal, WorkspaceID: "ws", ProjectID: "project_a",
			MemoryType: memory.TypeProjectFact, SourceType: "agent_summary", Title: "缓存", Content: "缓存使用 Redis。",
			State: memory.StateStable, Confidence: 0.8, Importance: 0.7, EncodingDepth: 2, DecayRate: 0.4,
			Tier: memory.TierLongTerm, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
		},
		EvidenceIDs: []string{oldEvidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory(old) error = %v", err)
	}

	session := capture.AgentSession{ID: "sess_supersede", AgentType: "cursor", WorkspaceID: "ws", ProjectID: "project_a", CaptureLevel: 3, StartedAt: now, Status: capture.StatusActive}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{ID: "task_supersede", SessionID: session.ID, WorkspaceID: session.WorkspaceID, ProjectID: session.ProjectID, TaskSummary: "纠正缓存", Status: capture.StatusActive, StartedAt: now}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID: "evt_supersede_cache", SessionID: session.ID, TaskID: task.ID, WorkspaceID: session.WorkspaceID,
		ProjectID: session.ProjectID, AgentType: session.AgentType, EventType: capture.EventUserCorrection,
		SourceChannel: capture.SourceChannelAgentSession, OccurredAt: now, Actor: capture.ActorUser,
		ContentSummary: "纠正：缓存改为使用 Memcached，不再使用 Redis。",
		ContentHash:    "sha256:supersede-cache", CreatedAt: now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)
	runNextJob(t, ctx, store, service, automation.JobTypeGenerateMemoryCandidate, "")
	runNextJob(t, ctx, store, service, automation.JobTypeComputeAdmission, "")

	archived, err := store.Get(ctx, "mem_old_cache")
	if err != nil {
		t.Fatalf("Get(old memory) error = %v", err)
	}
	if archived.State != memory.StateArchived {
		t.Fatalf("old memory state = %q, want archived", archived.State)
	}

	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{RawEventID: rawEvent.ID, Status: automation.CandidateStatusAdmitted})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID == "" || candidates[0].ResultingMemoryID == "mem_old_cache" {
		t.Fatalf("candidates = %+v, want new admitted memory id", candidates)
	}
	newMemory, err := store.Get(ctx, candidates[0].ResultingMemoryID)
	if err != nil {
		t.Fatalf("Get(new memory) error = %v", err)
	}
	if !strings.Contains(newMemory.Content, "Memcached") {
		t.Fatalf("new memory content = %q, want Memcached", newMemory.Content)
	}
	if newMemory.SupersedesID != "mem_old_cache" {
		t.Fatalf("supersedes_id = %q, want mem_old_cache", newMemory.SupersedesID)
	}
}
