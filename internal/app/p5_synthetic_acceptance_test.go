package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/mvp"
	"github.com/zaneway/theone/internal/retrieval"
)

func TestP5CSyntheticMVPAcceptance(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawRun, toolErr := app.CallTool(ctx, "memory.mvp.run.start", mvp.StartRunRequest{
		Name:          "P5-C synthetic MVP acceptance",
		Mode:          mvp.RunModeSynthetic,
		WorkspaceID:   "ws_p5_synthetic",
		ProjectID:     "proj_p5_synthetic",
		RepoID:        "repo_p5_synthetic",
		BaselineType:  mvp.BaselineSummaryOnly,
		CandidateType: mvp.CandidateHybridMemory,
	})
	if toolErr != nil {
		t.Fatalf("memory.mvp.run.start error = %v", toolErr)
	}
	run := rawRun.(mvp.StartRunResponse)

	for _, capability := range []mvp.AgentCapability{
		fullSyntheticCapability(run.RunID, mvp.AgentCodex),
		fullSyntheticCapability(run.RunID, mvp.AgentClaudeCode),
		fullSyntheticCapability(run.RunID, mvp.AgentCursor),
	} {
		if _, err := app.store.UpsertAgentCapability(ctx, capability); err != nil {
			t.Fatalf("UpsertAgentCapability(%s) error = %v", capability.AgentType, err)
		}
	}

	fixtures := mvp.SyntheticScenarioFixtures()
	for i, fixture := range fixtures {
		traceID := mvp.SyntheticTraceID(i)
		if _, err := app.store.CreateRetrievalTrace(ctx, retrieval.TraceRecord{
			ID:          traceID,
			WorkspaceID: "ws_p5_synthetic",
			ProjectID:   "proj_p5_synthetic",
			RepoID:      "repo_p5_synthetic",
			LatencyMS:   fixture.LatencyMS,
			Status:      retrieval.TraceCompleted,
			UsedFTS:     true,
		}); err != nil {
			t.Fatalf("CreateRetrievalTrace(%s) error = %v", traceID, err)
		}
		expected, err := fixture.ExpectedJSON()
		if err != nil {
			t.Fatalf("ExpectedJSON(%s) error = %v", fixture.ScenarioID, err)
		}
		observed, err := fixture.ObservedJSON()
		if err != nil {
			t.Fatalf("ObservedJSON(%s) error = %v", fixture.ScenarioID, err)
		}
		rawTask, toolErr := app.CallTool(ctx, "memory.mvp.task.record", mvp.RecordTaskRequest{
			RunID:            run.RunID,
			ScenarioID:       fixture.ScenarioID,
			Round:            fixture.Round,
			AgentType:        fixture.AgentType,
			SessionID:        "sess_" + fixture.ScenarioID,
			TaskID:           "task_" + fixture.ScenarioID,
			RetrievalTraceID: traceID,
			TaskSuccess:      true,
			Expected:         expected,
			Observed:         observed,
		})
		if toolErr != nil {
			t.Fatalf("memory.mvp.task.record(%s) error = %v", fixture.ScenarioID, toolErr)
		}
		task := rawTask.(mvp.RecordTaskResponse)
		if task.TaskResultID == "" || !task.Accepted {
			t.Fatalf("task response = %+v, want accepted", task)
		}
	}

	rawMetrics, toolErr := app.CallTool(ctx, "memory.mvp.metrics.compute", mvp.ComputeMetricsRequest{RunID: run.RunID, Recompute: true})
	if toolErr != nil {
		t.Fatalf("memory.mvp.metrics.compute error = %v", toolErr)
	}
	metrics := rawMetrics.(mvp.ComputeMetricsResponse)
	if metrics.Status != mvp.RunStatusPassed || !metrics.Summary.EngineMVPPassed || !metrics.Summary.AgentCertificationPassed {
		t.Fatalf("metrics response = %+v, want passed engine and agent certification", metrics)
	}
	if metrics.Summary.FailedMetrics != 0 {
		t.Fatalf("failed metrics = %d, want 0", metrics.Summary.FailedMetrics)
	}

	rawReport, toolErr := app.CallTool(ctx, "memory.mvp.report", mvp.ReportRequest{
		RunID:           run.RunID,
		Format:          "markdown",
		IncludeFailures: true,
	})
	if toolErr != nil {
		t.Fatalf("memory.mvp.report error = %v", toolErr)
	}
	report := rawReport.(mvp.ReportResponse)
	if report.Report == "" || !strings.Contains(report.Report, "P5 MVP Acceptance Report") ||
		!strings.Contains(report.Report, "engine_mvp_passed: `true`") {
		t.Fatalf("report = %q, want markdown summary", report.Report)
	}
}

func fullSyntheticCapability(runID string, agentType string) mvp.AgentCapability {
	return mvp.AgentCapability{
		RunID:                  runID,
		AgentType:              agentType,
		AdapterName:            "synthetic",
		AdapterVersion:         "p5-c",
		CaptureLevel:           4,
		ConversationCapture:    true,
		ToolCallCapture:        true,
		ToolOutputCapture:      true,
		FileEditCapture:        true,
		SessionLifecycle:       true,
		MemoryObserve:          true,
		Completeness:           0.95,
		DegradationReasonsJSON: `[]`,
	}
}
