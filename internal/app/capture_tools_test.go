package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
)

func TestAppRegistersCaptureTools(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawStart, toolErr := app.CallTool(ctx, "memory.observe", capture.ObserveRequest{
		SessionID:      "sess_app_capture_tools_001",
		EventType:      capture.EventSessionStart,
		SourceChannel:  capture.SourceChannelAgentSession,
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		AgentType:      "cursor",
		Actor:          capture.ActorAdapter,
		ContentSummary: "【事件】会话生命周期：session.start\n【事实】验证 capture 工具注册",
		CaptureCapabilities: capture.CaptureCapabilities{
			SessionLifecycle: true,
			MCPObserve:       true,
		},
		Session: &capture.SessionInput{GoalSummary: "验证 capture"},
	})
	if toolErr != nil {
		t.Fatalf("memory.observe error = %v", toolErr)
	}
	start, ok := rawStart.(capture.ObserveResponse)
	if !ok || !start.Accepted || start.SessionID == "" || start.TaskID == "" {
		t.Fatalf("memory.observe response = %#v, want accepted capture response", rawStart)
	}

	rawSessions, toolErr := app.CallTool(ctx, "memory.capture.sessions", capture.ListSessionsRequest{WorkspaceID: "ws"})
	if toolErr != nil {
		t.Fatalf("memory.capture.sessions error = %v", toolErr)
	}
	sessions, ok := rawSessions.(capture.ListSessionsResponse)
	if !ok || len(sessions.Sessions) != 1 || sessions.Sessions[0].ID != start.SessionID {
		t.Fatalf("sessions response = %#v, want session %s", rawSessions, start.SessionID)
	}
	sessionsJSON, err := json.Marshal(sessions)
	if err != nil {
		t.Fatalf("json.Marshal(sessions) error = %v", err)
	}
	if !strings.Contains(string(sessionsJSON), "session_id") || strings.Contains(string(sessionsJSON), `"ID"`) {
		t.Fatalf("sessions json = %s, want snake_case fields", sessionsJSON)
	}

	rawTasks, toolErr := app.CallTool(ctx, "memory.capture.tasks", capture.ListTasksRequest{SessionID: start.SessionID})
	if toolErr != nil {
		t.Fatalf("memory.capture.tasks error = %v", toolErr)
	}
	tasks, ok := rawTasks.(capture.ListTasksResponse)
	if !ok || len(tasks.Tasks) != 1 || tasks.Tasks[0].ID != start.TaskID {
		t.Fatalf("tasks response = %#v, want task %s", rawTasks, start.TaskID)
	}

	rawEvents, toolErr := app.CallTool(ctx, "memory.capture.events", capture.ListEventsRequest{SessionID: start.SessionID})
	if toolErr != nil {
		t.Fatalf("memory.capture.events error = %v", toolErr)
	}
	events, ok := rawEvents.(capture.ListEventsResponse)
	if !ok || len(events.Events) != 1 || events.Events[0].ID != start.RawEventID {
		t.Fatalf("events response = %#v, want event %s", rawEvents, start.RawEventID)
	}
	rawFilteredEvents, toolErr := app.CallTool(ctx, "memory.capture.events", map[string]any{
		"session_id":  start.SessionID,
		"event_types": []string{capture.EventSessionStart, capture.EventToolResultSummary},
	})
	if toolErr != nil {
		t.Fatalf("memory.capture.events with event_types error = %v", toolErr)
	}
	filteredEvents, ok := rawFilteredEvents.(capture.ListEventsResponse)
	if !ok || len(filteredEvents.Events) != 1 || filteredEvents.Events[0].EventType != capture.EventSessionStart {
		t.Fatalf("filtered events response = %#v, want session.start event", rawFilteredEvents)
	}

	rawQuality, toolErr := app.CallTool(ctx, "memory.capture.quality", capture.QualityRequest{SessionID: start.SessionID})
	if toolErr != nil {
		t.Fatalf("memory.capture.quality error = %v", toolErr)
	}
	quality, ok := rawQuality.(capture.QualityResponse)
	if !ok || quality.Report.SessionID != start.SessionID || quality.Report.CaptureLevel != start.CaptureLevel {
		t.Fatalf("quality response = %#v, want session %s level %d", rawQuality, start.SessionID, start.CaptureLevel)
	}

	_, toolErr = app.CallTool(ctx, "memory.observe", map[string]any{
		"event_type":     capture.EventToolResultSummary,
		"source_channel": capture.SourceChannelAgentSession,
		"workspace_id":   "ws",
		"agent_type":     "cursor",
	})
	if toolErr == nil || toolErr.RequestID == "" || toolErr.ErrorCode != "SESSION_REQUIRED" {
		t.Fatalf("observe error = %+v, want SESSION_REQUIRED with request_id", toolErr)
	}
	if toolErr.FallbackHint == "" {
		t.Fatalf("observe error = %+v, want fallback_hint", toolErr)
	}
}

func TestEnsureCaptureSessionDoesNotResetExistingQuality(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	startReq := capture.ObserveRequest{
		SessionID:      "sess_app_ensure_quality",
		TaskID:         "task_app_ensure_quality",
		EventType:      capture.EventSessionStart,
		SourceChannel:  capture.SourceChannelAgentSession,
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		AgentType:      "cursor",
		Actor:          capture.ActorAdapter,
		ContentSummary: "【事件】会话生命周期：Cursor session start",
		CaptureCapabilities: capture.CaptureCapabilities{
			ConversationCapture: true,
			ToolCallCapture:     true,
			ToolOutputCapture:   true,
			FileEditCapture:     true,
			SessionLifecycle:    true,
			MCPObserve:          true,
		},
		Session: &capture.SessionInput{GoalSummary: "ensure quality"},
		Task:    &capture.TaskInput{TaskSummary: "task_app_ensure_quality", Status: capture.StatusActive},
	}
	if err := app.EnsureCaptureSession(ctx, startReq); err != nil {
		t.Fatalf("EnsureCaptureSession(start) error = %v", err)
	}

	rawFile, toolErr := app.CallTool(ctx, "memory.observe", capture.ObserveRequest{
		SessionID:      startReq.SessionID,
		TaskID:         startReq.TaskID,
		EventType:      capture.EventFileEditSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		AgentType:      "cursor",
		Actor:          capture.ActorAgent,
		ContentSummary: "【事实】更新 README",
		CaptureCapabilities: capture.CaptureCapabilities{
			FileEditCapture: true,
			MCPObserve:      true,
		},
	})
	if toolErr != nil {
		t.Fatalf("memory.observe file error = %v", toolErr)
	}
	if resp, ok := rawFile.(capture.ObserveResponse); !ok || !resp.Accepted {
		t.Fatalf("memory.observe file response = %#v", rawFile)
	}

	if err := app.EnsureCaptureSession(ctx, startReq); err != nil {
		t.Fatalf("EnsureCaptureSession(existing) error = %v", err)
	}
	report, err := app.store.GetCaptureQuality(ctx, startReq.SessionID)
	if err != nil {
		t.Fatalf("GetCaptureQuality() error = %v", err)
	}
	var quality capture.CaptureQuality
	if err := json.Unmarshal([]byte(report.CaptureQualityJSON), &quality); err != nil {
		t.Fatalf("decode quality: %v", err)
	}
	if quality.FileEditCount != 1 || quality.CapturedEventCount != 1 {
		t.Fatalf("quality = %+v, want file edit stats preserved", quality)
	}
}
