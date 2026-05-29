package sqlite

import (
	"context"
	"testing"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/processor"
)

func TestCaptureServiceObserveWithSQLiteRepository(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	cfg := config.Default()
	service := capture.NewService(cfg, store)

	start, err := service.Observe(ctx, capture.ObserveRequest{
		SessionID:     "sess_sqlite_integration_001",
		EventType:     capture.EventSessionStart,
		SourceChannel: capture.SourceChannelAgentSession,
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		RepoID:        "repo_a",
		AgentType:     "codex",
		Actor:         capture.ActorAdapter,
		CaptureCapabilities: capture.CaptureCapabilities{
			SessionLifecycle:  true,
			ToolCallCapture:   true,
			ToolOutputCapture: true,
			MCPObserve:        true,
		},
		Session: &capture.SessionInput{GoalSummary: "sqlite observe integration"},
	})
	if err != nil {
		t.Fatalf("Observe(session.start) error = %v", err)
	}
	if start.SessionID == "" || start.TaskID == "" || start.RawEventID == "" {
		t.Fatalf("start response = %+v, want generated ids", start)
	}

	req := capture.ObserveRequest{
		SessionID:     start.SessionID,
		EventType:     capture.EventToolResultSummary,
		SourceChannel: capture.SourceChannelAgentSession,
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		RepoID:        "repo_a",
		AgentType:     "codex",
		Actor:         capture.ActorTool,
		ToolName:      "go test",
		OutputSummary: "测试通过",
		ContentHash:   "sha256:sqlite-service",
		CaptureCapabilities: capture.CaptureCapabilities{
			SessionLifecycle:  true,
			ToolCallCapture:   true,
			ToolOutputCapture: true,
			MCPObserve:        true,
		},
	}
	first, err := service.Observe(ctx, req)
	if err != nil {
		t.Fatalf("Observe(tool result first) error = %v", err)
	}
	second, err := service.Observe(ctx, req)
	if err != nil {
		t.Fatalf("Observe(tool result second) error = %v", err)
	}
	if !second.Deduped || second.RawEventID != first.RawEventID {
		t.Fatalf("first=%+v second=%+v, want deduped existing event", first, second)
	}
	report, err := store.GetCaptureQuality(ctx, start.SessionID)
	if err != nil {
		t.Fatalf("GetCaptureQuality() error = %v", err)
	}
	if report.CaptureQualityJSON == "" {
		t.Fatal("capture quality json empty, want updated quality")
	}
	sessions, err := store.ListSessions(ctx, capture.ListSessionsRequest{WorkspaceID: "ws", ProjectID: "project_a"})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].GoalSummary != "sqlite observe integration" {
		t.Fatalf("sessions = %+v, want preserved goal summary", sessions)
	}

	end, err := service.Observe(ctx, capture.ObserveRequest{
		SessionID:     start.SessionID,
		EventType:     capture.EventSessionEnd,
		SourceChannel: capture.SourceChannelAgentSession,
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		RepoID:        "repo_a",
		AgentType:     "codex",
		Actor:         capture.ActorAdapter,
		Session:       &capture.SessionInput{Status: capture.StatusCompleted},
		CaptureCapabilities: capture.CaptureCapabilities{
			SessionLifecycle:  true,
			ToolCallCapture:   true,
			ToolOutputCapture: true,
			MCPObserve:        true,
		},
	})
	if err != nil {
		t.Fatalf("Observe(session.end) error = %v", err)
	}
	if end.RawEventID == "" {
		t.Fatalf("session.end response = %+v, want raw event id", end)
	}
	tasks, err := store.ListTasks(ctx, capture.ListTasksRequest{SessionID: start.SessionID})
	if err != nil {
		t.Fatalf("ListTasks() after session.end error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != capture.StatusUnknown || tasks[0].EndedAt.IsZero() {
		t.Fatalf("tasks after session.end = %+v, want default task ended as unknown", tasks)
	}
}

func TestCaptureServiceObserveEnqueuesAutomationJobWithSQLiteRepository(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	cfg := config.Default()
	automationService := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	service := capture.NewServiceWithAutomation(cfg, store, automationService)

	resp, err := service.Observe(ctx, capture.ObserveRequest{
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelMCPTool,
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		AgentType:      "cursor",
		Actor:          capture.ActorUser,
		ContentSummary: "以后推进 automation 时先按详细设计拆分任务。",
		ContentHash:    "sha256:p3-c3-sqlite",
		CaptureCapabilities: capture.CaptureCapabilities{
			MCPObserve: true,
		},
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	jobs, err := store.ListJobs(ctx, automation.ListJobsRequest{
		JobType:  automation.JobTypeExtractEvidence,
		TargetID: resp.RawEventID,
	})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != automation.JobStatusPending || jobs[0].TargetType != automation.TargetTypeRawEvent {
		t.Fatalf("jobs = %+v, want one pending extract_evidence raw_event job", jobs)
	}
}
