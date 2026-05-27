package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
)

func newCaptureTestStore(t *testing.T) *Store {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(context.Background(), cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}

func TestCaptureRepositorySessionTaskEventFlow(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	session, err := store.UpsertSession(ctx, capture.AgentSession{
		ID:                      "sess_001",
		AgentType:               "codex",
		WorkspaceID:             "ws",
		ProjectID:               "project_a",
		RepoID:                  "repo_a",
		CaptureLevel:            3,
		CaptureCapabilitiesJSON: `{"tool_call_capture":true,"tool_output_capture":true}`,
		CaptureQualityJSON:      `{"captured_event_count":0}`,
		GoalSummary:             "实现 P2-B2",
		Status:                  capture.StatusActive,
	})
	if err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if session.ID != "sess_001" || session.Status != capture.StatusActive {
		t.Fatalf("session = %+v, want active sess_001", session)
	}

	task, err := store.UpsertTask(ctx, capture.AgentTask{
		ID:          "task_001",
		SessionID:   session.ID,
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		RepoID:      "repo_a",
		TaskSummary: "default task",
		Status:      capture.StatusActive,
	})
	if err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	defaultTask, ok, err := store.GetDefaultTask(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetDefaultTask() error = %v", err)
	}
	if !ok || defaultTask.ID != task.ID {
		t.Fatalf("default task = %+v, ok=%v, want %s", defaultTask, ok, task.ID)
	}

	event := capture.RawEvent{
		ID:             "evt_001",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		RepoID:         "repo_a",
		AgentType:      "codex",
		EventType:      capture.EventToolResultSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     time.Date(2026, 5, 23, 20, 0, 0, 0, time.UTC),
		Actor:          capture.ActorTool,
		ToolName:       "go test",
		OutputSummary:  "测试通过",
		KeywordsJSON:   `["go test"]`,
		SourceRefsJSON: `[{"source_type":"tool_output","exit_code":0}]`,
		ContentHash:    "sha256:test",
		Sensitivity:    capture.SensitivityNormal,
		RetentionHint:  "short_term",
	}
	if err := store.InsertRawEvent(ctx, event); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}
	duplicate, found, err := store.FindDuplicateEvent(ctx, capture.EventDedupKey{
		ContentHash: "sha256:test",
		SessionID:   session.ID,
		EventType:   capture.EventToolResultSummary,
	})
	if err != nil {
		t.Fatalf("FindDuplicateEvent() error = %v", err)
	}
	if !found || duplicate.ID != event.ID {
		t.Fatalf("duplicate = %+v, found=%v, want %s", duplicate, found, event.ID)
	}

	events, err := store.ListEvents(ctx, capture.ListEventsRequest{SessionID: session.ID, EventType: capture.EventToolResultSummary})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("events = %+v, want evt_001", events)
	}
	tasks, err := store.ListTasks(ctx, capture.ListTasksRequest{SessionID: session.ID})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("tasks = %+v, want task_001", tasks)
	}
	sessions, err := store.ListSessions(ctx, capture.ListSessionsRequest{WorkspaceID: "ws", AgentType: "codex"})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("sessions = %+v, want sess_001", sessions)
	}
}

func TestCaptureRepositoryEndSessionAndTask(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	if _, err := store.UpsertSession(ctx, capture.AgentSession{
		ID:           "sess_002",
		AgentType:    "claude_code",
		WorkspaceID:  "ws",
		CaptureLevel: 2,
		Status:       capture.StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := store.UpsertTask(ctx, capture.AgentTask{
		ID:          "task_002",
		SessionID:   "sess_002",
		WorkspaceID: "ws",
		TaskSummary: "运行测试",
		Status:      capture.StatusActive,
	}); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}

	endedTask, err := store.EndTask(ctx, "task_002", capture.StatusSucceeded, "测试通过", time.Now().UTC())
	if err != nil {
		t.Fatalf("EndTask() error = %v", err)
	}
	if endedTask.Status != capture.StatusSucceeded || endedTask.OutcomeSummary != "测试通过" {
		t.Fatalf("ended task = %+v, want succeeded outcome", endedTask)
	}
	endedSession, err := store.EndSession(ctx, "sess_002", capture.StatusCompleted, time.Now().UTC(), capture.CaptureQuality{
		HasSessionEnd:      true,
		CapturedEventCount: 3,
	})
	if err != nil {
		t.Fatalf("EndSession() error = %v", err)
	}
	if endedSession.Status != capture.StatusCompleted || endedSession.EndedAt.IsZero() {
		t.Fatalf("ended session = %+v, want completed with ended_at", endedSession)
	}
	report, err := store.GetCaptureQuality(ctx, "sess_002")
	if err != nil {
		t.Fatalf("GetCaptureQuality() error = %v", err)
	}
	if report.CaptureLevel != 2 || report.CaptureQualityJSON == "" {
		t.Fatalf("quality report = %+v, want level 2 with json", report)
	}
}
