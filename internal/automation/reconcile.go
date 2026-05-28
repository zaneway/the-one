package automation

import (
	"context"
	"fmt"
	"strings"
)

// Reconcile 扫描遗漏的 raw_event 并补充入队。
// 当前支持的模式：orphan_raw_event — 查找没有 extract_evidence job 且没有 evidence 的 raw_event。
// 设计意图：修复因 worker 崩溃或入队失败导致的事件处理遗漏。
// dry_run 模式只返回遗漏列表，不实际入队。
func (s *Service) Reconcile(ctx context.Context, req ReconcileRequest) (ReconcileResponse, error) {
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return ReconcileResponse{}, fmt.Errorf("VALIDATION_FAILED: workspace_id is required")
	}
	mode := req.Mode
	if mode == "" {
		mode = ReconcileModeOrphanRawEvent
	}
	if mode != ReconcileModeOrphanRawEvent {
		return ReconcileResponse{}, fmt.Errorf("VALIDATION_FAILED: unsupported reconcile mode %q", mode)
	}
	resp := ReconcileResponse{
		Mode:   mode,
		DryRun: req.DryRun,
		Items:  []ReconcileItem{},
	}
	limit, diagnostics := normalizeDiagnosticsLimit(req.Limit)
	orphans, err := s.repo.ListOrphanRawEvents(ctx, OrphanRawEventRequest{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		Limit:       limit,
	})
	if err != nil {
		return ReconcileResponse{}, err
	}
	resp.Diagnostics = diagnostics
	for _, event := range orphans {
		resp.Items = append(resp.Items, ReconcileItem{
			RawEventID: event.ID,
			Reason:     ReconcileReasonMissingExtractJob,
		})
		if req.DryRun {
			continue
		}
		if err := s.EnqueueRawEvent(ctx, event); err != nil {
			return resp, err
		}
		resp.Enqueued++
	}
	return resp, nil
}
