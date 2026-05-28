package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/mvp"
)

func TestP5DMissingAgentCertificationFails(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawRun, toolErr := app.CallTool(ctx, "memory.mvp.run.start", mvp.StartRunRequest{
		Name:          "MVP missing agent certification",
		Mode:          mvp.RunModeMixed,
		WorkspaceID:   "ws_mvp_real",
		ProjectID:     "proj_the_one",
		RepoID:        "repo_the_one",
		BaselineType:  mvp.BaselineSummaryOnly,
		CandidateType: mvp.CandidateHybridMemory,
	})
	if toolErr != nil {
		t.Fatalf("memory.mvp.run.start error = %v", toolErr)
	}
	run := rawRun.(mvp.StartRunResponse)
	if _, err := app.store.UpsertAgentCapability(ctx, mvp.AgentCapability{
		RunID:               run.RunID,
		AgentType:           mvp.AgentCodex,
		CaptureLevel:        4,
		ConversationCapture: true,
		ToolCallCapture:     true,
		ToolOutputCapture:   true,
		FileEditCapture:     true,
		SessionLifecycle:    true,
		MemoryObserve:       true,
		Completeness:        0.95,
	}); err != nil {
		t.Fatalf("UpsertAgentCapability() error = %v", err)
	}

	rawMetrics, toolErr := app.CallTool(ctx, "memory.mvp.metrics.compute", mvp.ComputeMetricsRequest{RunID: run.RunID})
	if toolErr != nil {
		t.Fatalf("memory.mvp.metrics.compute error = %v", toolErr)
	}
	metrics := rawMetrics.(mvp.ComputeMetricsResponse)
	if metrics.Summary.AgentCertificationPassed {
		t.Fatalf("summary = %+v, want missing claude_code/cursor to fail certification", metrics.Summary)
	}
	missingFailures := 0
	for _, metric := range metrics.Metrics {
		if (metric.AgentType == mvp.AgentClaudeCode || metric.AgentType == mvp.AgentCursor) && !metric.Passed {
			missingFailures++
		}
	}
	if missingFailures != 4 {
		t.Fatalf("missing failure metrics = %d, want 4", missingFailures)
	}
}
