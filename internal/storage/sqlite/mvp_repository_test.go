package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/mvp"
)

func TestP5AMVPRunRepositoryCreateUpdateList(t *testing.T) {
	ctx := context.Background()
	store := openP5ATestStore(t)
	defer store.Close()

	run, err := store.CreateRun(ctx, mvp.AcceptanceRun{
		Name:        "P5-A synthetic acceptance",
		WorkspaceID: "ws_p5",
		ProjectID:   "proj_p5",
		RepoID:      "repo_p5",
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if run.ID == "" || run.Mode != mvp.RunModeSynthetic || run.Status != mvp.RunStatusRunning ||
		run.BaselineType != mvp.BaselineSummaryOnly || run.CandidateType != mvp.CandidateHybridMemory {
		t.Fatalf("created run = %+v, want defaults", run)
	}
	loaded, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if loaded.ID != run.ID || loaded.WorkspaceID != "ws_p5" {
		t.Fatalf("loaded run = %+v, want created run", loaded)
	}

	if err := store.UpdateRunStatus(ctx, mvp.AcceptanceRun{
		ID:          run.ID,
		Status:      mvp.RunStatusPartial,
		SummaryJSON: `{"engine_mvp_passed":true}`,
		ReportPath:  "reports/mvp/run.md",
	}); err != nil {
		t.Fatalf("UpdateRunStatus() error = %v", err)
	}

	runs, err := store.ListRuns(ctx, mvp.RunQuery{WorkspaceID: "ws_p5", ProjectID: "proj_p5", Status: mvp.RunStatusPartial})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.ID != run.ID || got.ReportPath != "reports/mvp/run.md" || got.SummaryJSON == "" || got.EndedAt.IsZero() {
		t.Fatalf("listed run = %+v, want updated summary/report/end time", got)
	}
	if _, err := store.ListRuns(ctx, mvp.RunQuery{}); err == nil {
		t.Fatal("ListRuns() without workspace error = nil, want validation error")
	}
}

func TestP5BRetrievalLatencyLookup(t *testing.T) {
	ctx := context.Background()
	store := openP5ATestStore(t)
	defer store.Close()

	now := time.Now().UTC()
	for i, item := range []struct {
		id      string
		latency int
	}{
		{id: "rt_p5_1", latency: 20},
		{id: "rt_p5_2", latency: 80},
	} {
		if _, err := store.db.ExecContext(ctx, `insert into retrieval_trace(
			id, workspace_id, latency_ms, status, created_at
		) values (?, ?, ?, ?, ?)`, item.id, "ws_p5", item.latency, "completed", now.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("insert retrieval_trace %s: %v", item.id, err)
		}
	}
	latencies, err := store.ListRetrievalLatenciesByTraceIDs(ctx, []string{"rt_p5_1", "rt_p5_2", "rt_p5_1", ""})
	if err != nil {
		t.Fatalf("ListRetrievalLatenciesByTraceIDs() error = %v", err)
	}
	if len(latencies) != 2 || latencies[0] != 20 || latencies[1] != 80 {
		t.Fatalf("latencies = %+v, want [20 80]", latencies)
	}
}

func TestP5AAcceptanceTaskRepositoryRecordAndList(t *testing.T) {
	ctx := context.Background()
	store := openP5ATestStore(t)
	defer store.Close()

	run := seedP5ARun(t, ctx, store)
	task, err := store.RecordTask(ctx, mvp.AcceptanceTask{
		RunID:            run.ID,
		ScenarioID:       "mvp_03_decision_recall",
		Round:            2,
		AgentType:        mvp.AgentCodex,
		SessionID:        "sess_p5",
		TaskID:           "task_p5",
		RetrievalTraceID: "rt_p5",
		TaskSuccess:      true,
		ExpectedJSON:     `{"memory_types":["decision"]}`,
		ObservedJSON:     `{"injected_memory_count":2}`,
	})
	if err != nil {
		t.Fatalf("RecordTask() error = %v", err)
	}
	if task.ID == "" || task.Status != mvp.TaskStatusPassed {
		t.Fatalf("recorded task = %+v, want generated id and passed status", task)
	}

	tasks, err := store.ListAcceptanceTasks(ctx, mvp.TaskQuery{RunID: run.ID, ScenarioID: "mvp_03_decision_recall"})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	if tasks[0].RetrievalTraceID != "rt_p5" || tasks[0].ExpectedJSON == "" || !tasks[0].TaskSuccess {
		t.Fatalf("listed task = %+v, want trace/expected/success", tasks[0])
	}
	if _, err := store.ListAcceptanceTasks(ctx, mvp.TaskQuery{}); err == nil {
		t.Fatal("ListAcceptanceTasks() without run_id error = nil, want validation error")
	}
}

func TestP5AMetricRepositoryUpsertAndList(t *testing.T) {
	ctx := context.Background()
	store := openP5ATestStore(t)
	defer store.Close()

	run := seedP5ARun(t, ctx, store)
	written, err := store.UpsertMetricSamples(ctx, []mvp.MetricSample{
		mvp.TokenSavings(run.ID, "mvp_01_task_continuation", 1000, 650, 0.30),
		mvp.WrongMemoryInjectionRate(run.ID, "mvp_03_decision_recall", 1, 40),
	})
	if err != nil {
		t.Fatalf("UpsertMetricSamples() error = %v", err)
	}
	if len(written) != 2 || written[0].ID == "" || written[1].ID == "" {
		t.Fatalf("written metrics = %+v, want generated ids", written)
	}

	replacement := mvp.TokenSavings(run.ID, "mvp_01_task_continuation", 1000, 500, 0.30)
	if _, err := store.UpsertMetricSamples(ctx, []mvp.MetricSample{replacement}); err != nil {
		t.Fatalf("UpsertMetricSamples() replacement error = %v", err)
	}

	metrics, err := store.ListMetricSamples(ctx, mvp.MetricQuery{
		RunID:      run.ID,
		ScenarioID: "mvp_01_task_continuation",
		MetricName: mvp.MetricTokenSavings,
	})
	if err != nil {
		t.Fatalf("ListMetricSamples() error = %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("metric count = %d, want replacement to keep one row", len(metrics))
	}
	if metrics[0].MetricValue != 0.5 || !metrics[0].Passed {
		t.Fatalf("listed metric = %+v, want replacement value 0.5 passed", metrics[0])
	}
	if _, err := store.ListMetricSamples(ctx, mvp.MetricQuery{}); err == nil {
		t.Fatal("ListMetricSamples() without run_id error = nil, want validation error")
	}
}

func TestP5AAgentCapabilityRepositoryUpsertAndList(t *testing.T) {
	ctx := context.Background()
	store := openP5ATestStore(t)
	defer store.Close()

	run := seedP5ARun(t, ctx, store)
	capability, err := store.UpsertAgentCapability(ctx, mvp.AgentCapability{
		RunID:                  run.ID,
		AgentType:              mvp.AgentClaudeCode,
		AdapterName:            "hooks",
		AdapterVersion:         "test",
		CaptureLevel:           4,
		ConversationCapture:    true,
		ToolCallCapture:        true,
		ToolOutputCapture:      true,
		FileEditCapture:        true,
		SessionLifecycle:       true,
		MemoryObserve:          true,
		Completeness:           0.92,
		DegradationReasonsJSON: `[]`,
	})
	if err != nil {
		t.Fatalf("UpsertAgentCapability() error = %v", err)
	}
	if capability.ID == "" || capability.CapabilityCoverage != 1 || capability.Completeness != 0.92 {
		t.Fatalf("capability = %+v, want generated id and full coverage", capability)
	}

	updated, err := store.UpsertAgentCapability(ctx, mvp.AgentCapability{
		RunID:                  run.ID,
		AgentType:              mvp.AgentClaudeCode,
		AdapterName:            "hooks",
		CaptureLevel:           3,
		ConversationCapture:    true,
		ToolCallCapture:        true,
		ToolOutputCapture:      false,
		FileEditCapture:        true,
		SessionLifecycle:       true,
		MemoryObserve:          true,
		Completeness:           0.81,
		DegradationReasonsJSON: `["tool_output_unavailable"]`,
	})
	if err != nil {
		t.Fatalf("UpsertAgentCapability() update error = %v", err)
	}
	if updated.CapabilityCoverage != 5.0/6.0 || updated.CaptureLevel != 3 {
		t.Fatalf("updated capability = %+v, want 5/6 coverage and capture level 3", updated)
	}

	capabilities, err := store.ListAgentCapabilities(ctx, mvp.CapabilityQuery{RunID: run.ID})
	if err != nil {
		t.Fatalf("ListAgentCapabilities() error = %v", err)
	}
	if len(capabilities) != 1 || capabilities[0].DegradationReasonsJSON == "" {
		t.Fatalf("capabilities = %+v, want one updated capability with degradation reasons", capabilities)
	}
	if _, err := store.ListAgentCapabilities(ctx, mvp.CapabilityQuery{}); err == nil {
		t.Fatal("ListAgentCapabilities() without run_id error = nil, want validation error")
	}
}

func openP5ATestStore(t *testing.T) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(context.Background(), cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}

func seedP5ARun(t *testing.T, ctx context.Context, store *Store) mvp.AcceptanceRun {
	t.Helper()
	run, err := store.CreateRun(ctx, mvp.AcceptanceRun{
		Name:        "P5-A seed",
		Mode:        mvp.RunModeSynthetic,
		WorkspaceID: "ws_p5",
		ProjectID:   "proj_p5",
		RepoID:      "repo_p5",
		StartedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateRun() seed error = %v", err)
	}
	return run
}
