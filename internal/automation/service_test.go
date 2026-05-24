package automation_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/automation"
	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/processor"
	"github.com/zaneway/the-one/internal/storage/sqlite"
)

func TestServiceRunsEvidenceCandidateAdmissionChain(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; automation chain needs searchable automated memory")
	}

	now := time.Date(2026, 5, 24, 9, 30, 0, 0, time.UTC)
	session := capture.AgentSession{
		ID:           "sess_p3_c1",
		AgentType:    "cursor",
		WorkspaceID:  "ws",
		ProjectID:    "project_a",
		CaptureLevel: 3,
		StartedAt:    now,
		Status:       capture.StatusActive,
	}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{
		ID:          "task_p3_c1",
		SessionID:   session.ID,
		WorkspaceID: session.WorkspaceID,
		ProjectID:   session.ProjectID,
		TaskSummary: "推进 P3-C1 automation service",
		Status:      capture.StatusActive,
		StartedAt:   now,
	}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_p3_c1_preference",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "以后推进 P3 时先按详细设计拆分任务，再用测试验证。",
		ContentHash:    "sha256:p3-c1-preference",
		CreatedAt:      now,
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

	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{Status: automation.CandidateStatusAdmitted})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID == "" {
		t.Fatalf("admitted candidates = %+v, want one candidate with resulting memory", candidates)
	}

	written, err := store.Get(ctx, candidates[0].ResultingMemoryID)
	if err != nil {
		t.Fatalf("Get(resulting memory) error = %v", err)
	}
	if written.MemoryType != memory.TypePreference || written.State != memory.StateStable || !written.UserConfirmed {
		t.Fatalf("written memory = %+v, want stable user-confirmed preference", written)
	}
}

func TestServiceSkipsOrdinaryToolSuccessWithoutCandidate(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 24, 9, 40, 0, 0, time.UTC)
	session := capture.AgentSession{
		ID:           "sess_tool_success",
		AgentType:    "cursor",
		WorkspaceID:  "ws",
		ProjectID:    "project_a",
		CaptureLevel: 3,
		StartedAt:    now,
		Status:       capture.StatusActive,
	}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{
		ID:          "task_tool_success",
		SessionID:   session.ID,
		WorkspaceID: session.WorkspaceID,
		ProjectID:   session.ProjectID,
		TaskSummary: "运行测试",
		Status:      capture.StatusActive,
		StartedAt:   now,
	}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_tool_success",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventToolResultSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorTool,
		ToolName:       "go test",
		OutputSummary:  "ok github.com/zaneway/the-one/internal/automation",
		SourceRefsJSON: `[{"exit_code":0,"command_hash":"sha256:tool-success"}]`,
		ContentHash:    "sha256:tool-success",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)

	jobs, err := store.ListJobs(ctx, automation.ListJobsRequest{Status: automation.JobStatusPending})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("pending jobs = %+v, want none after ordinary successful tool output", jobs)
	}
	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none for ordinary successful tool output", candidates)
	}
}

func TestServiceSkipsEnqueueWhenProcessorDisabled(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Processor.EnableAutoProcessing = false
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, capture.RawEvent{ID: "evt_disabled"}); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	jobs, err := store.ListJobs(ctx, automation.ListJobsRequest{})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want none when auto processing disabled", jobs)
	}
}

func TestServiceSkipsEnqueueWhenProviderNone(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Processor.Provider = "none"
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, capture.RawEvent{ID: "evt_provider_none"}); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	jobs, err := store.ListJobs(ctx, automation.ListJobsRequest{})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want none when provider is none", jobs)
	}
}

func runNextJob(t *testing.T, ctx context.Context, store *sqlite.Store, service *automation.Service, wantType string, wantTarget string) {
	t.Helper()
	jobs, err := store.ClaimJobs(ctx, time.Now().UTC().Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ClaimJobs() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("claimed jobs = %+v, want exactly one", jobs)
	}
	if jobs[0].JobType != wantType {
		t.Fatalf("job type = %q, want %q", jobs[0].JobType, wantType)
	}
	if wantTarget != "" && jobs[0].TargetID != wantTarget {
		t.Fatalf("job target = %q, want %q", jobs[0].TargetID, wantTarget)
	}
	if err := service.RunJob(ctx, jobs[0]); err != nil {
		t.Fatalf("RunJob(%s) error = %v", jobs[0].JobType, err)
	}
	updated, err := store.GetJob(ctx, jobs[0].ID)
	if err != nil {
		t.Fatalf("GetJob(%s) error = %v", jobs[0].ID, err)
	}
	if updated.Status != automation.JobStatusSucceeded {
		t.Fatalf("job after RunJob = %+v, want succeeded", updated)
	}
}
