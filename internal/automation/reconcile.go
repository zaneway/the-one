package automation

import (
	"context"
	"fmt"
	"strings"
)

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
	if !s.cfg.Processor.EnableAutoProcessing || s.cfg.Processor.Provider == "none" {
		resp.Diagnostics = []string{"provider_disabled"}
		return resp, nil
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
