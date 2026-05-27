package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/memory"
)

func TestAsyncRepositoryJobLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	job, deduped, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:         "job_001",
		JobType:    "extract_evidence",
		TargetType: "raw_event",
		TargetID:   "evt_001",
		DedupKey:   "extract:evt_001",
		NextRunAt:  now,
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("EnqueueJob() error = %v", err)
	}
	if deduped {
		t.Fatal("deduped = true, want false")
	}
	if job.Status != automation.JobStatusPending || job.Priority != 5 || job.MaxRetries != 3 {
		t.Fatalf("job defaults = %+v, want pending priority 5 max retries 3", job)
	}

	again, deduped, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:         "job_001_duplicate",
		JobType:    "extract_evidence",
		TargetType: "raw_event",
		TargetID:   "evt_001",
		DedupKey:   "extract:evt_001",
	})
	if err != nil {
		t.Fatalf("EnqueueJob duplicate error = %v", err)
	}
	if !deduped || again.ID != job.ID {
		t.Fatalf("duplicate = %+v deduped=%v, want existing job_001", again, deduped)
	}

	claimed, err := store.ClaimJobs(ctx, now.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("ClaimJobs() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != job.ID || claimed[0].Status != automation.JobStatusRunning {
		t.Fatalf("claimed = %+v, want running job_001", claimed)
	}

	retryAt := now.Add(2 * time.Second)
	if err := store.MarkJobRetry(ctx, job.ID, 1, retryAt, "STORAGE_BUSY", now.Add(time.Second)); err != nil {
		t.Fatalf("MarkJobRetry() error = %v", err)
	}
	retried, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() after retry error = %v", err)
	}
	if retried.Status != automation.JobStatusPending || retried.RetryCount != 1 || retried.LastError != "STORAGE_BUSY" {
		t.Fatalf("retried = %+v, want pending retry_count=1 last_error", retried)
	}

	claimed, err = store.ClaimJobs(ctx, retryAt.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("ClaimJobs second error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed second count = %d, want 1", len(claimed))
	}
	if err := store.MarkJobSucceeded(ctx, job.ID, `{"evidence_count":1}`, retryAt.Add(2*time.Second)); err != nil {
		t.Fatalf("MarkJobSucceeded() error = %v", err)
	}
	succeeded, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() after success error = %v", err)
	}
	if succeeded.Status != automation.JobStatusSucceeded || succeeded.PayloadJSON == "" || succeeded.LastError != "" {
		t.Fatalf("succeeded = %+v, want succeeded with payload and no last_error", succeeded)
	}
}

func TestAsyncRepositoryClaimOrderAndListJobs(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	base := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	for _, job := range []automation.AsyncJob{
		{ID: "job_low", JobType: "compute_admission", TargetType: "memory_candidate", TargetID: "cand_1", Priority: 9, NextRunAt: base, CreatedAt: base.Add(-time.Minute)},
		{ID: "job_high", JobType: "extract_evidence", TargetType: "raw_event", TargetID: "evt_1", Priority: 1, NextRunAt: base, CreatedAt: base},
		{ID: "job_future", JobType: "extract_evidence", TargetType: "raw_event", TargetID: "evt_2", Priority: 1, NextRunAt: base.Add(time.Hour), CreatedAt: base},
	} {
		if _, _, err := store.EnqueueJob(ctx, job); err != nil {
			t.Fatalf("EnqueueJob(%s) error = %v", job.ID, err)
		}
	}
	claimed, err := store.ClaimJobs(ctx, base.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("ClaimJobs() error = %v", err)
	}
	if len(claimed) != 2 || claimed[0].ID != "job_high" || claimed[1].ID != "job_low" {
		t.Fatalf("claimed order = %+v, want high then low", claimed)
	}
	pending, err := store.ListJobs(ctx, automation.ListJobsRequest{Status: automation.JobStatusPending})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "job_future" {
		t.Fatalf("pending = %+v, want future job", pending)
	}
}

func TestAsyncRepositoryJobFailedAndNotFound(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	_, _, err := store.EnqueueJob(ctx, automation.AsyncJob{ID: "job_failed", JobType: "extract_evidence", TargetType: "raw_event", TargetID: "evt"})
	if err != nil {
		t.Fatalf("EnqueueJob() error = %v", err)
	}
	if err := store.MarkJobFailed(ctx, "job_failed", "VALIDATION_FAILED", time.Now().UTC()); err != nil {
		t.Fatalf("MarkJobFailed() error = %v", err)
	}
	failed, err := store.GetJob(ctx, "job_failed")
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if failed.Status != automation.JobStatusFailed || failed.LastError != "VALIDATION_FAILED" {
		t.Fatalf("failed = %+v, want failed with last_error", failed)
	}
	if err := store.MarkJobFailed(ctx, "missing", "error", time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "JOB_NOT_FOUND") {
		t.Fatalf("missing MarkJobFailed error = %v, want JOB_NOT_FOUND", err)
	}
}

func TestAsyncRepositoryCandidateLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	candidate := automation.MemoryCandidateRecord{
		ID:                    "cand_001",
		RawEventID:            "evt_001",
		EvidenceID:            "ev_001",
		Provider:              "rule_based",
		MemoryType:            memory.TypeDecision,
		Scope:                 memory.ScopeProjectLocal,
		WorkspaceID:           "ws",
		ProjectID:             "project_a",
		Title:                 "decision: P3 rule_based",
		Content:               "P3 只实现 rule_based Provider。",
		KeywordsJSON:          jsonArrayText("P3", "rule_based"),
		SourceEvidenceIDsJSON: jsonArrayText("ev_001"),
		CandidateReasonJSON:   jsonArrayText("architecture_decision"),
		DedupKey:              "cand:evt_001:ev_001",
	}
	if err := store.WriteCandidate(ctx, candidate); err != nil {
		t.Fatalf("WriteCandidate() error = %v", err)
	}
	if err := store.WriteCandidate(ctx, automation.MemoryCandidateRecord{
		ID:         "cand_duplicate",
		Provider:   "rule_based",
		MemoryType: memory.TypeDecision,
		Scope:      memory.ScopeProjectLocal,
		Content:    "duplicate",
		DedupKey:   candidate.DedupKey,
	}); err != nil {
		t.Fatalf("WriteCandidate duplicate error = %v", err)
	}

	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{Status: automation.CandidateStatusGenerated, MemoryType: memory.TypeDecision})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != candidate.ID {
		t.Fatalf("candidates = %+v, want cand_001 only", candidates)
	}

	admission := automation.AdmissionResult{
		Decision:       automation.DecisionWritePendingReview,
		AdmissionScore: 0.78,
		ReasonCodes:    []string{"architecture_decision", "high_impact_requires_review"},
	}
	if err := store.UpdateCandidateAdmission(ctx, candidate.ID, admission, automation.CandidateStatusAdmitted, "mem_001"); err != nil {
		t.Fatalf("UpdateCandidateAdmission() error = %v", err)
	}
	updated, err := store.GetCandidate(ctx, candidate.ID)
	if err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	}
	if updated.AdmissionDecision != automation.DecisionWritePendingReview || updated.Status != automation.CandidateStatusAdmitted || updated.ResultingMemoryID != "mem_001" {
		t.Fatalf("updated = %+v, want admitted pending review mem_001", updated)
	}
	var reasons []string
	if err := json.Unmarshal([]byte(updated.AdmissionReasonJSON), &reasons); err != nil {
		t.Fatalf("admission reason json invalid: %v", err)
	}
	if len(reasons) != 2 || reasons[0] != "architecture_decision" {
		t.Fatalf("reasons = %+v, want architecture_decision first", reasons)
	}
}

func TestAsyncRepositoryCandidateFiltersAndNotFound(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	for _, candidate := range []automation.MemoryCandidateRecord{
		{ID: "cand_a", Provider: "rule_based", MemoryType: memory.TypeFailure, Scope: memory.ScopeRepoLocal, WorkspaceID: "ws", RepoID: "repo_a", Content: "failure a", Status: automation.CandidateStatusGenerated},
		{ID: "cand_b", Provider: "rule_based", MemoryType: memory.TypeFailure, Scope: memory.ScopeRepoLocal, WorkspaceID: "ws", RepoID: "repo_b", Content: "failure b", Status: automation.CandidateStatusDropped},
	} {
		if err := store.WriteCandidate(ctx, candidate); err != nil {
			t.Fatalf("WriteCandidate(%s) error = %v", candidate.ID, err)
		}
	}
	items, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{MemoryType: memory.TypeFailure, RepoID: "repo_b"})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "cand_b" {
		t.Fatalf("items = %+v, want cand_b", items)
	}
	if _, err := store.GetCandidate(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "CANDIDATE_NOT_FOUND") {
		t.Fatalf("GetCandidate missing error = %v, want CANDIDATE_NOT_FOUND", err)
	}
	if err := store.UpdateCandidateAdmission(ctx, "missing", automation.AdmissionResult{}, automation.CandidateStatusFailed, ""); err == nil || !strings.Contains(err.Error(), "CANDIDATE_NOT_FOUND") {
		t.Fatalf("UpdateCandidateAdmission missing error = %v, want CANDIDATE_NOT_FOUND", err)
	}
}

func jsonArrayText(values ...string) string {
	data, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(data)
}
