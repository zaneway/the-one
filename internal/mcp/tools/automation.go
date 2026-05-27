package tools

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/mcp"
	"github.com/zaneway/theone/internal/retention"
)

// RegisterAutomationTools 注册 P3 自动记忆诊断工具到 MCP 注册表。
func RegisterAutomationTools(registry *mcp.Registry, service *automation.Service, logger *slog.Logger) {
	handler := &AutomationHandler{service: service, logger: logger}
	registry.RegisterTool(automationListJobsSpec(handler.ListJobs))
	registry.RegisterTool(automationGetJobSpec(handler.GetJob))
	registry.RegisterTool(automationListCandidatesSpec(handler.ListCandidates))
	registry.RegisterTool(automationGetCandidateSpec(handler.GetCandidate))
	registry.RegisterTool(automationStatusSpec(handler.Status))
	registry.RegisterTool(automationReconcileSpec(handler.Reconcile))
	registry.RegisterTool(retentionRunSpec(handler.RunRetention))
}

type AutomationHandler struct {
	service *automation.Service
	logger  *slog.Logger
}

// ListJobs 处理 memory.jobs.list 工具调用。
// 功能：查询异步任务列表，支持按状态、类型、目标和 scope 过滤。
// 用于 P3 诊断：查看 extract_evidence、generate_candidate、admit_memory 等任务的执行情况。
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

// GetJob 处理 memory.jobs.get 工具调用。
// 功能：获取单个异步任务的详细信息，包括重试次数、错误信息、payload 摘要等。
// 用于 P3 诊断：排查单个 job 的执行状态和失败原因。
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

// ListCandidates 处理 memory.candidates.list 工具调用。
// 功能：查询候选记忆列表，支持按状态、类型、provider 和 scope 过滤。
// 用于 P3 诊断：查看 generate_memory_candidate 的输出和 admission 决策结果。
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

// GetCandidate 处理 memory.candidates.get 工具调用。
// 功能：获取单个候选记忆的详细信息，包括 admission 评分、决策原因和结果 memory_id。
// 用于 P3 诊断：排查为什么某条候选被 admit 或 dropped。
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

// Status 处理 memory.automation.status 工具调用。
// 功能：返回 P3 自动处理管道的整体状态，包括 pending/running/failed/succeeded 任务数量。
// 用于监控：了解自动记忆处理管道的健康状况。
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

// Reconcile 处理 memory.jobs.reconcile 工具调用。
// 功能：扫描没有 extract_evidence job 且没有 evidence 的 raw_event，为遗漏的事件补充入队。
// 设计说明：用于修复因 worker 崩溃或入队失败导致的事件处理遗漏。
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

// RunRetention 处理 memory.retention.run 工具调用。
// 功能：手动触发保留任务，包括临时记忆清理和保留分数重算。
// 支持 dry_run 模式：只返回将要处理的记忆列表，不实际执行。
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
