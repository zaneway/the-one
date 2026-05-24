package tools

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/zaneway/the-one/internal/automation"
	"github.com/zaneway/the-one/internal/mcp"
	"github.com/zaneway/the-one/internal/retention"
)

// RegisterAutomationTools 注册 P3 自动记忆诊断工具到 MCP 注册表。
func RegisterAutomationTools(registry *mcp.Registry, service *automation.Service, logger *slog.Logger) {
	handler := &AutomationHandler{service: service, logger: logger}
	registry.Register("memory.jobs.list", handler.ListJobs)
	registry.Register("memory.jobs.get", handler.GetJob)
	registry.Register("memory.candidates.list", handler.ListCandidates)
	registry.Register("memory.candidates.get", handler.GetCandidate)
	registry.Register("memory.automation.status", handler.Status)
	registry.Register("memory.jobs.reconcile", handler.Reconcile)
	registry.Register("memory.retention.run", handler.RunRetention)
}

type AutomationHandler struct {
	service *automation.Service
	logger  *slog.Logger
}

func (h *AutomationHandler) ListJobs(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req automation.ListJobsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid jobs list params")
	}
	resp, err := h.service.ListJobs(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("automation jobs listed", "result_count", len(resp.Jobs))
	return resp, nil
}

func (h *AutomationHandler) GetJob(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req automation.GetJobRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid jobs get params")
	}
	resp, err := h.service.GetJob(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("automation job loaded", "job_id", resp.Job.JobID, "status", resp.Job.Status)
	return resp, nil
}

func (h *AutomationHandler) ListCandidates(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req automation.ListCandidatesRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid candidates list params")
	}
	resp, err := h.service.ListCandidates(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("automation candidates listed", "result_count", len(resp.Candidates))
	return resp, nil
}

func (h *AutomationHandler) GetCandidate(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req automation.GetCandidateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid candidates get params")
	}
	resp, err := h.service.GetCandidate(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("automation candidate loaded", "candidate_id", resp.Candidate.CandidateID, "status", resp.Candidate.Status)
	return resp, nil
}

func (h *AutomationHandler) Status(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, validationError("invalid automation status params")
		}
	}
	resp, err := h.service.Status(ctx)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("automation status loaded",
		"pending_jobs", resp.PendingJobs,
		"running_jobs", resp.RunningJobs,
		"failed_jobs", resp.FailedJobs,
	)
	return resp, nil
}

func (h *AutomationHandler) Reconcile(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req automation.ReconcileRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid jobs reconcile params")
	}
	resp, err := h.service.Reconcile(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("automation reconcile completed",
		"mode", resp.Mode,
		"dry_run", resp.DryRun,
		"item_count", len(resp.Items),
		"enqueued", resp.Enqueued,
	)
	return resp, nil
}

func (h *AutomationHandler) RunRetention(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req retention.RunRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid retention run params")
	}
	resp, err := h.service.RunRetention(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("retention run completed",
		"mode", resp.Mode,
		"dry_run", resp.DryRun,
		"processed", resp.Processed,
		"item_count", len(resp.Items),
	)
	return resp, nil
}
