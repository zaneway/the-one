package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/mvp"
	"github.com/zaneway/the-one/internal/retrieval"
)

func TestAppRegistersMVPTools(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawRun, toolErr := app.CallTool(ctx, "memory.mvp.run.start", mvp.StartRunRequest{
		Name:        "P5-A app tool test",
		Mode:        mvp.RunModeSynthetic,
		WorkspaceID: "ws_p5",
		ProjectID:   "proj_p5",
		RepoID:      "repo_p5",
	})
	if toolErr != nil {
		t.Fatalf("memory.mvp.run.start error = %v", toolErr)
	}
	run := rawRun.(mvp.StartRunResponse)
	if run.RunID == "" || run.Status != mvp.RunStatusRunning {
		t.Fatalf("run response = %+v, want running run id", run)
	}

	rawTask, toolErr := app.CallTool(ctx, "memory.mvp.task.record", mvp.RecordTaskRequest{
		RunID:            run.RunID,
		ScenarioID:       "mvp_06_cross_agent_sharing",
		Round:            2,
		AgentType:        mvp.AgentCodex,
		SessionID:        "sess_p5",
		TaskID:           "task_p5",
		RetrievalTraceID: "rt_p5",
		TaskSuccess:      true,
		Expected:         []byte(`{"required_scope":"project_local"}`),
		Observed:         []byte(`{"cross_agent_recall_success_rate":1,"scope_error_count":0,"baseline_context_tokens":1000,"candidate_context_tokens":650,"wrong_memory_injected_count":0,"injected_memory_count":3}`),
	})
	if toolErr != nil {
		t.Fatalf("memory.mvp.task.record error = %v", toolErr)
	}
	task := rawTask.(mvp.RecordTaskResponse)
	if task.TaskResultID == "" || !task.Accepted {
		t.Fatalf("task response = %+v, want accepted task id", task)
	}
	if _, toolErr := app.CallTool(ctx, "memory.mvp.task.record", mvp.RecordTaskRequest{
		RunID:       "mvp_run_missing",
		ScenarioID:  "mvp_06_cross_agent_sharing",
		AgentType:   mvp.AgentCodex,
		TaskSuccess: true,
	}); toolErr == nil || toolErr.ErrorCode != "MVP_RUN_NOT_FOUND" {
		t.Fatalf("missing run task.record error = %+v, want MVP_RUN_NOT_FOUND", toolErr)
	}

	trace, err := app.store.CreateRetrievalTrace(ctx, retrieval.TraceRecord{
		ID:          "rt_p5",
		WorkspaceID: "ws_p5",
		ProjectID:   "proj_p5",
		RepoID:      "repo_p5",
		LatencyMS:   42,
		Status:      retrieval.TraceCompleted,
	})
	if err != nil {
		t.Fatalf("CreateRetrievalTrace() error = %v", err)
	}
	if trace.ID != "rt_p5" {
		t.Fatalf("trace id = %s, want rt_p5", trace.ID)
	}
	for _, agentType := range []string{mvp.AgentCodex, mvp.AgentClaudeCode, mvp.AgentCursor} {
		rawCapability, toolErr := app.CallTool(ctx, "memory.mvp.capability.record", fullCapabilityRequest(run.RunID, agentType))
		if toolErr != nil {
			t.Fatalf("memory.mvp.capability.record(%s) error = %v", agentType, toolErr)
		}
		capability := rawCapability.(mvp.RecordCapabilityResponse)
		if capability.CapabilityID == "" || !capability.Accepted || capability.CapabilityCoverage != 1 {
			t.Fatalf("capability response = %+v, want accepted full coverage", capability)
		}
	}

	rawMetrics, toolErr := app.CallTool(ctx, "memory.mvp.metrics.compute", mvp.ComputeMetricsRequest{RunID: run.RunID})
	if toolErr != nil {
		t.Fatalf("memory.mvp.metrics.compute error = %v", toolErr)
	}
	metrics := rawMetrics.(mvp.ComputeMetricsResponse)
	if metrics.RunID != run.RunID || len(metrics.Metrics) == 0 || !metrics.Summary.EngineMVPPassed || !metrics.Summary.AgentCertificationPassed {
		t.Fatalf("metrics response = %+v, want passing engine and agent metrics", metrics)
	}

	rawReport, toolErr := app.CallTool(ctx, "memory.mvp.report", mvp.ReportRequest{RunID: run.RunID, Format: "markdown"})
	if toolErr != nil {
		t.Fatalf("memory.mvp.report error = %v", toolErr)
	}
	report := rawReport.(mvp.ReportResponse)
	if report.RunID != run.RunID || report.Report == "" || !report.Summary.EngineMVPPassed {
		t.Fatalf("report response = %+v, want markdown report with passing engine summary", report)
	}
}

func TestAppMVPReportIncludesFailureDetails(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawRun, toolErr := app.CallTool(ctx, "memory.mvp.run.start", mvp.StartRunRequest{
		Name:        "P5 failure report",
		Mode:        mvp.RunModeSynthetic,
		WorkspaceID: "ws_p5_failure",
	})
	if toolErr != nil {
		t.Fatalf("memory.mvp.run.start error = %v", toolErr)
	}
	run := rawRun.(mvp.StartRunResponse)
	for _, agentType := range []string{mvp.AgentCodex, mvp.AgentClaudeCode, mvp.AgentCursor} {
		if _, toolErr := app.CallTool(ctx, "memory.mvp.capability.record", fullCapabilityRequest(run.RunID, agentType)); toolErr != nil {
			t.Fatalf("memory.mvp.capability.record(%s) error = %v", agentType, toolErr)
		}
	}
	if _, err := app.store.CreateRetrievalTrace(ctx, retrieval.TraceRecord{
		ID:          "rt_p5_failure",
		WorkspaceID: "ws_p5_failure",
		LatencyMS:   30,
		Status:      retrieval.TraceCompleted,
	}); err != nil {
		t.Fatalf("CreateRetrievalTrace() error = %v", err)
	}
	if _, toolErr := app.CallTool(ctx, "memory.mvp.task.record", mvp.RecordTaskRequest{
		RunID:            run.RunID,
		ScenarioID:       "mvp_01_task_continuation",
		Round:            1,
		AgentType:        mvp.AgentCodex,
		RetrievalTraceID: "rt_p5_failure",
		Status:           mvp.TaskStatusFailed,
		TaskSuccess:      false,
		Observed:         []byte(`{"repeated_explanation_count":0,"task_state_recall":1,"baseline_context_tokens":1000,"candidate_context_tokens":500}`),
		FailureReason:    "agent did not finish task",
	}); toolErr != nil {
		t.Fatalf("memory.mvp.task.record error = %v", toolErr)
	}
	if _, toolErr := app.CallTool(ctx, "memory.mvp.metrics.compute", mvp.ComputeMetricsRequest{RunID: run.RunID}); toolErr != nil {
		t.Fatalf("memory.mvp.metrics.compute error = %v", toolErr)
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
	if !strings.Contains(report.Report, "Failure Details") || !strings.Contains(report.Report, "agent did not finish task") {
		t.Fatalf("report = %q, want failure details", report.Report)
	}
}

func fullCapabilityRequest(runID, agentType string) mvp.RecordCapabilityRequest {
	return mvp.RecordCapabilityRequest{
		RunID:               runID,
		AgentType:           agentType,
		AdapterName:         "test",
		AdapterVersion:      "p5",
		CaptureLevel:        4,
		ConversationCapture: true,
		ToolCallCapture:     true,
		ToolOutputCapture:   true,
		FileEditCapture:     true,
		SessionLifecycle:    true,
		MemoryObserve:       true,
		Completeness:        0.95,
		DegradationReasons:  []byte(`[]`),
	}
}
