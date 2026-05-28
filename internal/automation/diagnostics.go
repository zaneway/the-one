package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const diagnosticsLimitMax = 100

func (s *Service) ListJobs(ctx context.Context, req ListJobsRequest) (ListJobsResponse, error) {
	if req.WorkspaceID == "" && req.TargetID == "" {
		return ListJobsResponse{}, fmt.Errorf("VALIDATION_FAILED: workspace_id or target_id is required")
	}
	limit, diagnostics := normalizeDiagnosticsLimit(req.Limit)
	req.Limit = limit
	jobs, err := s.repo.ListJobs(ctx, req)
	if err != nil {
		return ListJobsResponse{}, err
	}
	resp := ListJobsResponse{Jobs: make([]JobDiagnostic, 0, len(jobs)), Diagnostics: diagnostics}
	for _, job := range jobs {
		resp.Jobs = append(resp.Jobs, jobDiagnostic(job, false))
	}
	return resp, nil
}

func (s *Service) GetJob(ctx context.Context, req GetJobRequest) (GetJobResponse, error) {
	if strings.TrimSpace(req.JobID) == "" {
		return GetJobResponse{}, fmt.Errorf("VALIDATION_FAILED: job_id is required")
	}
	job, err := s.repo.GetJob(ctx, req.JobID)
	if err != nil {
		return GetJobResponse{}, err
	}
	return GetJobResponse{Job: jobDiagnostic(job, true)}, nil
}

func (s *Service) ListCandidates(ctx context.Context, req ListCandidatesRequest) (ListCandidatesResponse, error) {
	if req.WorkspaceID == "" && (req.ProjectID != "" || req.RepoID != "") {
		return ListCandidatesResponse{}, fmt.Errorf("VALIDATION_FAILED: workspace_id is required when filtering candidates by project_id or repo_id")
	}
	limit, diagnostics := normalizeDiagnosticsLimit(req.Limit)
	req.Limit = limit
	candidates, err := s.repo.ListCandidates(ctx, req)
	if err != nil {
		return ListCandidatesResponse{}, err
	}
	resp := ListCandidatesResponse{Candidates: make([]CandidateDiagnostic, 0, len(candidates)), Diagnostics: diagnostics}
	for _, candidate := range candidates {
		resp.Candidates = append(resp.Candidates, candidateDiagnostic(candidate))
	}
	return resp, nil
}

func (s *Service) GetCandidate(ctx context.Context, req GetCandidateRequest) (GetCandidateResponse, error) {
	if strings.TrimSpace(req.CandidateID) == "" {
		return GetCandidateResponse{}, fmt.Errorf("VALIDATION_FAILED: candidate_id is required")
	}
	candidate, err := s.repo.GetCandidate(ctx, req.CandidateID)
	if err != nil {
		return GetCandidateResponse{}, err
	}
	return GetCandidateResponse{Candidate: candidateDiagnostic(candidate)}, nil
}

func (s *Service) Status(ctx context.Context) (AutomationStatusResponse, error) {
	pending, err := s.repo.ListJobs(ctx, ListJobsRequest{Status: JobStatusPending, Limit: diagnosticsLimitMax})
	if err != nil {
		return AutomationStatusResponse{}, err
	}
	running, err := s.repo.ListJobs(ctx, ListJobsRequest{Status: JobStatusRunning, Limit: diagnosticsLimitMax})
	if err != nil {
		return AutomationStatusResponse{}, err
	}
	failed, err := s.repo.ListJobs(ctx, ListJobsRequest{Status: JobStatusFailed, Limit: diagnosticsLimitMax})
	if err != nil {
		return AutomationStatusResponse{}, err
	}
	recent, err := s.repo.ListJobs(ctx, ListJobsRequest{Limit: 1})
	if err != nil {
		return AutomationStatusResponse{}, err
	}
	resp := AutomationStatusResponse{
		Provider:            s.cfg.Processor.Provider,
		PendingJobs:         len(pending),
		RunningJobs:         len(running),
		FailedJobs:          len(failed),
		RetentionJobEnabled: s.cfg.Retention.JobEnabled,
		TemporaryTTLDays:    s.cfg.Retention.TemporaryTTLDays,
		ShortTermTTLDays:    s.cfg.Retention.ShortTermTTLDays,
	}
	if len(recent) > 0 {
		resp.RecentJobUpdatedAt = recent[0].UpdatedAt.Format(time.RFC3339Nano)
	}
	if len(failed) > 0 {
		resp.RecentError = failed[0].LastError
	}
	if len(pending) == diagnosticsLimitMax || len(running) == diagnosticsLimitMax || len(failed) == diagnosticsLimitMax {
		resp.DiagnosticsLimitCapped = true
	}
	return resp, nil
}

func normalizeDiagnosticsLimit(limit int) (int, []string) {
	if limit <= 0 {
		return diagnosticsLimitMax, nil
	}
	if limit > diagnosticsLimitMax {
		return diagnosticsLimitMax, []string{"limit_truncated"}
	}
	return limit, nil
}

func jobDiagnostic(job AsyncJob, includePayload bool) JobDiagnostic {
	diag := JobDiagnostic{
		JobID:      job.ID,
		JobType:    job.JobType,
		TargetType: job.TargetType,
		TargetID:   job.TargetID,
		Status:     job.Status,
		RetryCount: job.RetryCount,
		MaxRetries: job.MaxRetries,
		LastError:  job.LastError,
		NextRunAt:  job.NextRunAt,
		CreatedAt:  job.CreatedAt,
		UpdatedAt:  job.UpdatedAt,
	}
	if includePayload && job.PayloadJSON != "" {
		diag.PayloadSummary = compactSummary(job.PayloadJSON, 160)
		diag.PayloadHash = shortSHA256(job.PayloadJSON)
	}
	return diag
}

func candidateDiagnostic(candidate MemoryCandidateRecord) CandidateDiagnostic {
	return CandidateDiagnostic{
		CandidateID:         candidate.ID,
		RawEventID:          candidate.RawEventID,
		EvidenceID:          candidate.EvidenceID,
		Provider:            candidate.Provider,
		MemoryType:          candidate.MemoryType,
		Scope:               candidate.Scope,
		WorkspaceID:         candidate.WorkspaceID,
		ProjectID:           candidate.ProjectID,
		RepoID:              candidate.RepoID,
		ContentSummary:      compactSummary(candidate.Content, 200),
		AdmissionScore:      candidate.AdmissionScore,
		AdmissionDecision:   candidate.AdmissionDecision,
		AdmissionReasonJSON: candidate.AdmissionReasonJSON,
		ResultingMemoryID:   candidate.ResultingMemoryID,
		Status:              candidate.Status,
		CreatedAt:           candidate.CreatedAt,
		UpdatedAt:           candidate.UpdatedAt,
	}
}

func compactSummary(value string, limit int) string {
	summary := strings.Join(strings.Fields(value), " ")
	if limit > 0 && len(summary) > limit {
		return summary[:limit] + "..."
	}
	return summary
}

func shortSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}
