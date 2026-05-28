package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retention"
)

func TestAppRegistersAutomationDiagnosticsTools(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawObserved, toolErr := app.CallTool(ctx, "memory.observe", capture.ObserveRequest{
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelMCPTool,
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		AgentType:      "cursor",
		Actor:          capture.ActorUser,
		ContentSummary: "以后推进 automation 时先补 MCP 诊断测试。",
		ContentHash:    "sha256:p3-c4-diagnostics",
	})
	if toolErr != nil {
		t.Fatalf("memory.observe error = %v", toolErr)
	}
	observed := rawObserved.(capture.ObserveResponse)

	rawJobs, toolErr := app.CallTool(ctx, "memory.jobs.list", automation.ListJobsRequest{
		TargetID: observed.RawEventID,
		Limit:    200,
	})
	if toolErr != nil {
		t.Fatalf("memory.jobs.list error = %v", toolErr)
	}
	jobs := rawJobs.(automation.ListJobsResponse)
	if len(jobs.Jobs) != 1 || jobs.Jobs[0].TargetID != observed.RawEventID {
		t.Fatalf("jobs response = %#v, want extract job for raw_event", rawJobs)
	}
	if len(jobs.Diagnostics) != 1 || jobs.Diagnostics[0] != "limit_truncated" {
		t.Fatalf("jobs diagnostics = %+v, want limit_truncated", jobs.Diagnostics)
	}

	rawJob, toolErr := app.CallTool(ctx, "memory.jobs.get", automation.GetJobRequest{JobID: jobs.Jobs[0].JobID})
	if toolErr != nil {
		t.Fatalf("memory.jobs.get error = %v", toolErr)
	}
	job := rawJob.(automation.GetJobResponse)
	if job.Job.JobID != jobs.Jobs[0].JobID || job.Job.PayloadSummary != "" {
		t.Fatalf("job response = %#v, want matching job without full payload", rawJob)
	}
	jobJSON, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("json.Marshal(job) error = %v", err)
	}
	if strings.Contains(string(jobJSON), "payload_json") {
		t.Fatalf("job json = %s, must not expose payload_json", jobJSON)
	}

	if err := app.store.WriteCandidate(ctx, automation.MemoryCandidateRecord{
		ID:                    "cand_tool_diag",
		RawEventID:            observed.RawEventID,
		EvidenceID:            "ev_tool_diag",
		Provider:              "rule_based",
		MemoryType:            memory.TypePreference,
		Scope:                 memory.ScopeProjectLocal,
		WorkspaceID:           "ws",
		ProjectID:             "project_a",
		Content:               "推进 automation 时先补 MCP 诊断测试。",
		CandidateReasonJSON:   `["user_declaration"]`,
		AdmissionReasonJSON:   `["explicit_user_preference"]`,
		SourceEvidenceIDsJSON: `["ev_tool_diag"]`,
		Status:                automation.CandidateStatusDropped,
		DedupKey:              "cand:tool:diag",
	}); err != nil {
		t.Fatalf("WriteCandidate() error = %v", err)
	}
	rawCandidates, toolErr := app.CallTool(ctx, "memory.candidates.list", automation.ListCandidatesRequest{
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Status:      automation.CandidateStatusDropped,
	})
	if toolErr != nil {
		t.Fatalf("memory.candidates.list error = %v", toolErr)
	}
	candidates := rawCandidates.(automation.ListCandidatesResponse)
	if len(candidates.Candidates) != 1 || candidates.Candidates[0].CandidateID != "cand_tool_diag" {
		t.Fatalf("candidates response = %#v, want dropped candidate", rawCandidates)
	}
	if candidates.Candidates[0].ContentSummary == "" || strings.Contains(candidates.Candidates[0].ContentSummary, "\n") {
		t.Fatalf("candidate summary = %q, want compact content summary", candidates.Candidates[0].ContentSummary)
	}

	rawCandidate, toolErr := app.CallTool(ctx, "memory.candidates.get", automation.GetCandidateRequest{CandidateID: "cand_tool_diag"})
	if toolErr != nil {
		t.Fatalf("memory.candidates.get error = %v", toolErr)
	}
	candidate := rawCandidate.(automation.GetCandidateResponse)
	if candidate.Candidate.CandidateID != "cand_tool_diag" || candidate.Candidate.RawEventID != observed.RawEventID {
		t.Fatalf("candidate response = %#v, want candidate detail with raw_event id", rawCandidate)
	}

	rawStatus, toolErr := app.CallTool(ctx, "memory.automation.status", map[string]any{})
	if toolErr != nil {
		t.Fatalf("memory.automation.status error = %v", toolErr)
	}
	status := rawStatus.(automation.AutomationStatusResponse)
	if status.Provider != "rule_based" || status.PendingJobs < 1 {
		t.Fatalf("status response = %#v, want enabled rule_based with pending job", rawStatus)
	}
}

func TestAppReconcileOrphanRawEvents(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	now := time.Date(2026, 5, 24, 11, 20, 0, 0, time.UTC)
	orphan := capture.RawEvent{
		ID:             "evt_reconcile_orphan",
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelMCPTool,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "manual reconcile orphan raw_event",
		ContentHash:    "sha256:reconcile-orphan",
		CreatedAt:      now,
	}
	if err := app.store.InsertRawEvent(ctx, orphan); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	rawDryRun, toolErr := app.CallTool(ctx, "memory.jobs.reconcile", automation.ReconcileRequest{
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Mode:        automation.ReconcileModeOrphanRawEvent,
		DryRun:      true,
	})
	if toolErr != nil {
		t.Fatalf("memory.jobs.reconcile dry_run error = %v", toolErr)
	}
	dryRun := rawDryRun.(automation.ReconcileResponse)
	if len(dryRun.Items) != 1 || dryRun.Items[0].RawEventID != orphan.ID || dryRun.Enqueued != 0 {
		t.Fatalf("dry_run response = %#v, want orphan item without enqueue", rawDryRun)
	}

	rawApply, toolErr := app.CallTool(ctx, "memory.jobs.reconcile", automation.ReconcileRequest{
		WorkspaceID: "ws",
		Mode:        automation.ReconcileModeOrphanRawEvent,
		DryRun:      false,
	})
	if toolErr != nil {
		t.Fatalf("memory.jobs.reconcile apply error = %v", toolErr)
	}
	apply := rawApply.(automation.ReconcileResponse)
	if apply.Enqueued != 1 {
		t.Fatalf("apply response = %#v, want enqueued=1", rawApply)
	}

	jobs, err := app.store.ListJobs(ctx, automation.ListJobsRequest{TargetID: orphan.ID})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobType != automation.JobTypeExtractEvidence {
		t.Fatalf("jobs = %+v, want extract_evidence after reconcile", jobs)
	}
}

func TestAppRetentionRunCleanupTemporary(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()
	if !app.store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; retention cleanup test needs automated memory write")
	}

	now := time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC)
	evidence := memory.Evidence{
		ID:                   "ev_app_temp",
		RawEventID:           "evt_app_temp",
		SourceType:           "tool_result",
		InterpretedStatement: "临时状态。",
	}
	if err := app.store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:          "mem_app_temp",
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		MemoryType:  memory.TypeTemporaryState,
		Content:     "应用层 temporary retention 测试。",
		State:       memory.StateProvisional,
		Tier:        memory.TierTemporary,
		CreatedAt:   now.Add(-10 * 24 * time.Hour),
		UpdatedAt:   now.Add(-10 * 24 * time.Hour),
	}
	if _, err := app.store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}

	rawDryRun, toolErr := app.CallTool(ctx, "memory.retention.run", retention.RunRequest{
		Mode:   retention.ModeCleanupTemporary,
		DryRun: true,
	})
	if toolErr != nil {
		t.Fatalf("memory.retention.run dry_run error = %v", toolErr)
	}
	dryRun := rawDryRun.(retention.RunResponse)
	if len(dryRun.Items) != 1 || dryRun.Processed != 0 {
		t.Fatalf("dry_run = %#v, want one candidate without processing", rawDryRun)
	}

	rawApply, toolErr := app.CallTool(ctx, "memory.retention.run", retention.RunRequest{
		Mode:   retention.ModeCleanupTemporary,
		DryRun: false,
	})
	if toolErr != nil {
		t.Fatalf("memory.retention.run apply error = %v", toolErr)
	}
	apply := rawApply.(retention.RunResponse)
	if apply.Processed != 1 {
		t.Fatalf("apply = %#v, want processed=1", rawApply)
	}
}

func TestAutomationDiagnosticsRequireScopedListRequests(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	_, toolErr := app.CallTool(ctx, "memory.jobs.list", automation.ListJobsRequest{})
	if toolErr == nil || toolErr.ErrorCode != "VALIDATION_FAILED" {
		t.Fatalf("memory.jobs.list error = %+v, want VALIDATION_FAILED", toolErr)
	}

	_, toolErr = app.CallTool(ctx, "memory.candidates.list", automation.ListCandidatesRequest{ProjectID: "project_a"})
	if toolErr == nil || toolErr.ErrorCode != "VALIDATION_FAILED" {
		t.Fatalf("memory.candidates.list error = %+v, want VALIDATION_FAILED", toolErr)
	}
}
