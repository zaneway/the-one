package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
)

func testConfigWithoutSuppress() config.Config {
	cfg := config.Default()
	cfg.Capture.SuppressRawEventTypes = []string{}
	return cfg
}

func TestServiceObserveSessionStartCreatesRawEventAndDefaultTask(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(config.Default(), repo)

	resp, err := service.Observe(context.Background(), ObserveRequest{
		SessionID:      "sess_codex_start_001",
		EventType:      EventSessionStart,
		SourceChannel:  SourceChannelAgentSession,
		WorkspaceID:    "ws",
		ProjectID:      "project_a",
		AgentType:      "codex",
		Actor:          ActorAdapter,
		ContentSummary: "【事件】会话生命周期：session.start\n【事实】开始实现 capture",
		CaptureCapabilities: CaptureCapabilities{
			SessionLifecycle:  true,
			ToolCallCapture:   true,
			ToolOutputCapture: true,
			MCPObserve:        true,
		},
		Session: &SessionInput{GoalSummary: "实现 capture"},
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if !resp.Accepted || resp.SessionID == "" || resp.TaskID == "" {
		t.Fatalf("response = %+v, want accepted with session and task ids", resp)
	}
	if resp.RawEventID != "" {
		t.Fatalf("raw_event_id = %q, want empty for suppressed session.start", resp.RawEventID)
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %d, want 0 for suppressed session.start", len(repo.events))
	}
	if !containsDiagnostic(resp.Diagnostics, "event_type_suppressed") {
		t.Fatalf("diagnostics = %+v, want event_type_suppressed", resp.Diagnostics)
	}
	task := repo.tasks[resp.TaskID]
	if task.TaskSummary != "default task" {
		t.Fatalf("default task summary = %q, want default task", task.TaskSummary)
	}
	report, err := repo.GetCaptureQuality(context.Background(), resp.SessionID)
	if err != nil {
		t.Fatalf("GetCaptureQuality() error = %v", err)
	}
	if !strings.Contains(report.CaptureQualityJSON, `"has_session_start":true`) {
		t.Fatalf("quality json = %s, want has_session_start", report.CaptureQualityJSON)
	}
}

func TestServiceObserveDedupesRawEvent(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(config.Default(), repo)
	start, err := service.Observe(context.Background(), ObserveRequest{
		SessionID:      "sess_codex_dedup_001",
		EventType:      EventSessionStart,
		SourceChannel:  SourceChannelAgentSession,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		ContentSummary: "【事件】会话生命周期：session.start",
		CaptureCapabilities: CaptureCapabilities{
			SessionLifecycle: true,
			MCPObserve:       true,
		},
	})
	if err != nil {
		t.Fatalf("Observe(session.start) error = %v", err)
	}

	req := ObserveRequest{
		SessionID:      start.SessionID,
		EventType:      EventTurnCompleted,
		SourceChannel:  SourceChannelAgentSession,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		Actor:          ActorAdapter,
		InputSummary:   "推进 dedup 测试",
		OutputSummary:  "已记录 dedup 测试",
		ContentSummary: "【结论/决策】dedup 测试应复用已有 raw_event。",
		ContentHash:    "sha256:same",
		CaptureCapabilities: CaptureCapabilities{
			SessionLifecycle: true,
			MCPObserve:       true,
		},
	}
	first, err := service.Observe(context.Background(), req)
	if err != nil {
		t.Fatalf("Observe(first) error = %v", err)
	}
	second, err := service.Observe(context.Background(), req)
	if err != nil {
		t.Fatalf("Observe(second) error = %v", err)
	}
	if first.RawEventID == "" || second.RawEventID != first.RawEventID || !second.Deduped {
		t.Fatalf("first=%+v second=%+v, want deduped existing event", first, second)
	}
}

func TestServiceObservePreservesRawPayloadMetadata(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(testConfigWithoutSuppress(), repo)

	resp, err := service.Observe(context.Background(), ObserveRequest{
		EventType:       EventToolResultSummary,
		SourceChannel:   SourceChannelMCPTool,
		WorkspaceID:     "ws",
		AgentType:       "codex",
		Actor:           ActorTool,
		ToolName:        "go test",
		OutputSummary:   "测试失败",
		ContentSummary:  "【事实】go test 失败，原始 payload 已脱敏保存。",
		RawPayloadJSON:  `{"argv":["go","test","./..."],"stderr":"panic: redacted token"}`,
		PayloadSchema:   "tool_result.v1",
		RawPayloadHash:  "sha256:raw-payload",
		RedactionState:  RedactionStateRedacted,
		RedactionPolicy: "theone.default.v1",
		TruncationPolicy: TruncationPolicy{
			Truncated:         true,
			OriginalSizeBytes: 8192,
			StoredSizeBytes:   2048,
			MaxSizeBytes:      2048,
			Reason:            "max_raw_payload_bytes",
		},
		CaptureCapabilities: CaptureCapabilities{
			ToolOutputCapture: true,
			MCPObserve:        true,
		},
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	event := repo.events[resp.RawEventID]
	if event.RawPayloadJSON != `{"argv":["go","test","./..."],"stderr":"panic: redacted token"}` ||
		event.PayloadSchema != "tool_result.v1" ||
		event.RawPayloadHash != "sha256:raw-payload" ||
		event.RedactionState != RedactionStateRedacted ||
		event.RedactionPolicy != "theone.default.v1" {
		t.Fatalf("raw payload metadata = %+v, want preserved payload metadata", event)
	}
	if !event.Truncation.Truncated || event.Truncation.OriginalSizeBytes != 8192 || event.Truncation.StoredSizeBytes != 2048 || event.Truncation.MaxSizeBytes != 2048 {
		t.Fatalf("truncation = %+v, want preserved truncation policy", event.Truncation)
	}
}

func TestServiceObserveDefaultsRawPayloadMetadataWithoutRedaction(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(config.Default(), repo)

	resp, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventUserDeclaration,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		Actor:          ActorUser,
		ContentSummary: "【事实】用户要求尽量保留 raw payload。",
		RawPayloadJSON: `{"message":"用户要求尽量保留 raw payload。","nested":{"ok":true}}`,
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	event := repo.events[resp.RawEventID]
	if event.RedactionState != RedactionStateRaw {
		t.Fatalf("redaction_state = %q, want raw", event.RedactionState)
	}
	if event.RawPayloadHash == "" || !strings.HasPrefix(event.RawPayloadHash, "sha256:") {
		t.Fatalf("raw_payload_hash = %q, want computed sha256", event.RawPayloadHash)
	}
	if event.Truncation.Truncated {
		t.Fatalf("truncation = %+v, want not truncated", event.Truncation)
	}
	if event.Truncation.OriginalSizeBytes != len([]byte(event.RawPayloadJSON)) ||
		event.Truncation.StoredSizeBytes != len([]byte(event.RawPayloadJSON)) {
		t.Fatalf("truncation = %+v, want raw payload byte sizes", event.Truncation)
	}
}

func TestServiceObserveRejectsInvalidRawPayloadJSON(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(config.Default(), repo)

	_, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventUserDeclaration,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		Actor:          ActorUser,
		ContentSummary: "【事实】invalid raw payload should be rejected.",
		RawPayloadJSON: `{"message":`,
	})
	if err == nil || !strings.Contains(err.Error(), "raw_payload_json must be valid JSON") {
		t.Fatalf("Observe() error = %v, want raw payload json validation", err)
	}
}

func TestServiceObserveEnqueuesAutomationForNewRawEventOnly(t *testing.T) {
	repo := newFakeRepository()
	enqueuer := &fakeJobEnqueuer{}
	service := NewServiceWithAutomation(config.Default(), repo, enqueuer)
	start, err := service.Observe(context.Background(), ObserveRequest{
		SessionID:      "sess_cursor_auto_001",
		EventType:      EventSessionStart,
		SourceChannel:  SourceChannelAgentSession,
		WorkspaceID:    "ws",
		AgentType:      "cursor",
		ContentSummary: "【事件】会话生命周期：session.start",
		CaptureCapabilities: CaptureCapabilities{
			SessionLifecycle: true,
			MCPObserve:       true,
		},
	})
	if err != nil {
		t.Fatalf("Observe(session.start) error = %v", err)
	}
	if len(enqueuer.rawEventIDs) != 0 {
		t.Fatalf("enqueued raw events after suppressed start = %+v, want none", enqueuer.rawEventIDs)
	}

	req := ObserveRequest{
		SessionID:      start.SessionID,
		EventType:      EventUserDeclaration,
		SourceChannel:  SourceChannelAgentSession,
		WorkspaceID:    "ws",
		AgentType:      "cursor",
		Actor:          ActorUser,
		ContentSummary: "【事实】以后推进 automation 时先写测试。",
		ContentHash:    "sha256:p3-c3",
		CaptureCapabilities: CaptureCapabilities{
			SessionLifecycle: true,
			MCPObserve:       true,
		},
	}
	first, err := service.Observe(context.Background(), req)
	if err != nil {
		t.Fatalf("Observe(first declaration) error = %v", err)
	}
	second, err := service.Observe(context.Background(), req)
	if err != nil {
		t.Fatalf("Observe(second declaration) error = %v", err)
	}
	if !second.Deduped {
		t.Fatalf("second response = %+v, want deduped", second)
	}
	if len(enqueuer.rawEventIDs) != 2 || enqueuer.rawEventIDs[0] != first.RawEventID || enqueuer.rawEventIDs[1] != first.RawEventID {
		t.Fatalf("enqueued raw events = %+v, want first declaration enqueued twice on dedup retry", enqueuer.rawEventIDs)
	}
}

func containsDiagnostic(diagnostics []string, want string) bool {
	for _, item := range diagnostics {
		if item == want {
			return true
		}
	}
	return false
}

func TestServiceObserveDoesNotCallSemanticEnhancerBeforeRawEventPersistence(t *testing.T) {
	repo := newFakeRepository()
	enhancer := &fakeSemanticEnhancer{
		output: SemanticEnhanceOutput{
			InputSummary:       "模型生成的输入摘要不应进入 raw_event。",
			OutputSummary:      "模型生成的输出摘要不应进入 raw_event。",
			ContentSummary:     "【事实】模型生成的语义简化不应进入 raw_event。",
			Keywords:           []string{"模型关键词"},
			SalientSpans:       []string{"模型生成片段"},
			SemanticEquivalent: true,
		},
	}
	service := NewService(config.Default(), repo, WithSemanticEnhancer(enhancer))

	resp, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventUserDeclaration,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		Actor:          ActorUser,
		InputSummary:   "用户输入强调 raw_event 必须先保存原始事实。",
		OutputSummary:  "Agent确认会把外部 AI 处理后置到 processor。",
		ContentSummary: "【事实】用户要求 raw_event 先保存原始事实。\n【约束】外部 AI 只能在 raw_event 落库后处理 evidence/candidate。",
		Keywords:       []string{"raw_event", "processor", "evidence"},
		SalientSpans:   []string{"raw_event 先保存原始事实"},
		ContentHash:    "sha256:pre-enhance-hash",
		RawPayloadJSON: `{"turn":{"user":"raw_event 必须先保存原始事实","assistant":"外部 AI 后置处理"}}`,
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if enhancer.called {
		t.Fatal("semantic enhancer was called before raw_event persistence")
	}
	event := repo.events[resp.RawEventID]
	if event.ContentSummary != "【事实】用户要求 raw_event 先保存原始事实。\n【约束】外部 AI 只能在 raw_event 落库后处理 evidence/candidate。" {
		t.Fatalf("content_summary = %q, want original observe summary", event.ContentSummary)
	}
	if event.ContentHash != "sha256:pre-enhance-hash" {
		t.Fatalf("content_hash = %q, want caller-provided hash preserved", event.ContentHash)
	}
	var keywords []string
	if err := json.Unmarshal([]byte(event.KeywordsJSON), &keywords); err != nil {
		t.Fatalf("decode keywords: %v", err)
	}
	if strings.Join(keywords, ",") != "raw_event,processor,evidence" {
		t.Fatalf("keywords = %+v, want original observe keywords", keywords)
	}
}

func TestServiceObservePersistsRawEventWhenConfiguredEnhancerWouldReject(t *testing.T) {
	repo := newFakeRepository()
	enhancer := &fakeSemanticEnhancer{
		output: SemanticEnhanceOutput{
			ContentSummary:     "【事实】模型输出了不同语义",
			SemanticEquivalent: false,
		},
	}
	service := NewService(config.Default(), repo, WithSemanticEnhancer(enhancer))

	resp, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventUserDeclaration,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		Actor:          ActorUser,
		ContentSummary: "【事实】用户要求保持 raw_event 原始事实完整。",
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if enhancer.called {
		t.Fatal("semantic enhancer was called before raw_event persistence")
	}
	if _, ok := repo.events[resp.RawEventID]; !ok {
		t.Fatalf("raw_event %q not persisted", resp.RawEventID)
	}
}

func TestServiceObserveRejectsForbiddenSourceRefsWithoutCallingSemanticEnhancer(t *testing.T) {
	repo := newFakeRepository()
	enhancer := &fakeSemanticEnhancer{
		output: SemanticEnhanceOutput{SemanticEquivalent: true},
	}
	service := NewService(config.Default(), repo, WithSemanticEnhancer(enhancer))

	_, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventToolResultSummary,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		ContentSummary: "【事件】工具执行结果\n【事实】测试失败",
		SourceRefs:     []SourceRef{{"full_output": "完整输出不能发送给外部模型"}},
	})
	if err == nil || !strings.Contains(err.Error(), "CONTENT_TOO_LARGE") {
		t.Fatalf("Observe() error = %v, want CONTENT_TOO_LARGE before semantic enhancer", err)
	}
	if enhancer.called {
		t.Fatal("semantic enhancer was called with forbidden source_refs")
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %d, want no raw_event", len(repo.events))
	}
}

func TestServiceObserveKeepsRawEventWhenAutomationEnqueueFails(t *testing.T) {
	repo := newFakeRepository()
	enqueuer := &fakeJobEnqueuer{err: errors.New("queue unavailable")}
	service := NewServiceWithAutomation(config.Default(), repo, enqueuer)

	resp, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventUserDeclaration,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "cursor",
		Actor:          ActorUser,
		ContentSummary: "【事实】以后推进 automation 时先写测试。",
		ContentHash:    "sha256:p3-c3-enqueue-failed",
		CaptureCapabilities: CaptureCapabilities{
			MCPObserve: true,
		},
	})
	if err != nil {
		t.Fatalf("Observe() error = %v, want accepted response with diagnostics", err)
	}
	if !resp.Accepted || resp.RawEventID == "" {
		t.Fatalf("response = %+v, want accepted with raw_event_id", resp)
	}
	if len(resp.Diagnostics) != 1 || resp.Diagnostics[0] != "automation_enqueue_failed" {
		t.Fatalf("diagnostics = %+v, want automation_enqueue_failed", resp.Diagnostics)
	}
	if len(repo.events) != 1 {
		t.Fatalf("events = %d, want raw_event kept after enqueue failure", len(repo.events))
	}
}

func TestServiceObserveRejectsFullOutput(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(config.Default(), repo)

	_, err := service.Observe(context.Background(), ObserveRequest{
		EventType:     EventToolResultSummary,
		SourceChannel: SourceChannelMCPTool,
		SourceRefs:    []SourceRef{{"full_output": "完整输出"}},
	})
	if err == nil || !strings.Contains(err.Error(), "CONTENT_TOO_LARGE") {
		t.Fatalf("Observe() error = %v, want CONTENT_TOO_LARGE", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %d, want 0", len(repo.events))
	}
}

func TestServiceObserveTracksContentBoundaryRejectionForSession(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(config.Default(), repo)
	start, err := service.Observe(context.Background(), ObserveRequest{
		SessionID:      "sess_cursor_boundary_001",
		EventType:      EventSessionStart,
		SourceChannel:  SourceChannelAgentSession,
		WorkspaceID:    "ws",
		AgentType:      "cursor",
		ContentSummary: "【事件】会话生命周期：session.start",
		CaptureCapabilities: CaptureCapabilities{
			SessionLifecycle: true,
			MCPObserve:       true,
		},
	})
	if err != nil {
		t.Fatalf("Observe(session.start) error = %v", err)
	}

	_, err = service.Observe(context.Background(), ObserveRequest{
		SessionID:      start.SessionID,
		EventType:      EventTurnCompleted,
		SourceChannel:  SourceChannelAgentSession,
		WorkspaceID:    "ws",
		AgentType:      "cursor",
		Actor:          ActorAdapter,
		ContentSummary: "【事实】boundary rejection test",
		SourceRefs:     []SourceRef{{"full_output": "完整输出"}},
		CaptureCapabilities: CaptureCapabilities{
			SessionLifecycle: true,
			MCPObserve:       true,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "CONTENT_TOO_LARGE") {
		t.Fatalf("Observe() error = %v, want CONTENT_TOO_LARGE", err)
	}
	report, err := repo.GetCaptureQuality(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("GetCaptureQuality() error = %v", err)
	}
	if !strings.Contains(report.CaptureQualityJSON, `"content_boundary_rejections":1`) {
		t.Fatalf("quality json = %s, want one content boundary rejection", report.CaptureQualityJSON)
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %d, want no raw_event after rejected turn.completed", len(repo.events))
	}
}

func TestServiceDiagnosticsDelegatesToRepository(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["sess_001"] = AgentSession{
		ID:           "sess_001",
		AgentType:    "cursor",
		WorkspaceID:  "ws",
		ProjectID:    "project_a",
		CaptureLevel: 2,
		Status:       StatusCompleted,
	}
	repo.tasks["task_001"] = AgentTask{
		ID:          "task_001",
		SessionID:   "sess_001",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		TaskSummary: "default task",
		Status:      StatusSucceeded,
	}
	repo.events["evt_001"] = RawEvent{
		ID:          "evt_001",
		SessionID:   "sess_001",
		TaskID:      "task_001",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		EventType:   EventToolResultSummary,
	}
	service := NewService(config.Default(), repo)

	sessions, err := service.ListSessions(context.Background(), ListSessionsRequest{WorkspaceID: "ws"})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].ID != "sess_001" {
		t.Fatalf("sessions = %+v, want sess_001", sessions)
	}
	tasks, err := service.ListTasks(context.Background(), ListTasksRequest{SessionID: "sess_001"})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks.Tasks) != 1 || tasks.Tasks[0].ID != "task_001" {
		t.Fatalf("tasks = %+v, want task_001", tasks)
	}
	events, err := service.ListEvents(context.Background(), ListEventsRequest{SessionID: "sess_001"})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events.Events) != 1 || events.Events[0].ID != "evt_001" {
		t.Fatalf("events = %+v, want evt_001", events)
	}
	quality, err := service.Quality(context.Background(), QualityRequest{SessionID: "sess_001"})
	if err != nil {
		t.Fatalf("Quality() error = %v", err)
	}
	if quality.Report.SessionID != "sess_001" || quality.Report.CaptureLevel != 2 {
		t.Fatalf("quality = %+v, want sess_001 level 2", quality)
	}
}

type fakeRepository struct {
	sessions map[string]AgentSession
	tasks    map[string]AgentTask
	events   map[string]RawEvent
}

type fakeJobEnqueuer struct {
	rawEventIDs []string
	err         error
}

type fakeSemanticEnhancer struct {
	input  SemanticEnhanceInput
	output SemanticEnhanceOutput
	err    error
	called bool
}

func (e *fakeSemanticEnhancer) EnhanceObserve(_ context.Context, input SemanticEnhanceInput) (SemanticEnhanceOutput, error) {
	e.called = true
	e.input = input
	return e.output, e.err
}

func (e *fakeJobEnqueuer) EnqueueRawEvent(_ context.Context, event RawEvent) error {
	e.rawEventIDs = append(e.rawEventIDs, event.ID)
	return e.err
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		sessions: make(map[string]AgentSession),
		tasks:    make(map[string]AgentTask),
		events:   make(map[string]RawEvent),
	}
}

func (r *fakeRepository) UpsertSession(_ context.Context, session AgentSession) (AgentSession, error) {
	existing := r.sessions[session.ID]
	if !existing.StartedAt.IsZero() && session.StartedAt.IsZero() {
		session.StartedAt = existing.StartedAt
	}
	if session.WorkspaceID == "" {
		session.WorkspaceID = existing.WorkspaceID
	}
	if session.AgentType == "" {
		session.AgentType = existing.AgentType
	}
	if session.Status == "" {
		session.Status = existing.Status
	}
	if session.CaptureQualityJSON == "" {
		session.CaptureQualityJSON = existing.CaptureQualityJSON
	}
	if session.CaptureCapabilitiesJSON == "" {
		session.CaptureCapabilitiesJSON = existing.CaptureCapabilitiesJSON
	}
	r.sessions[session.ID] = session
	return session, nil
}

func (r *fakeRepository) EndSession(_ context.Context, sessionID string, status string, endedAt time.Time, quality CaptureQuality) (AgentSession, error) {
	session, ok := r.sessions[sessionID]
	if !ok {
		return AgentSession{}, fmt.Errorf("SESSION_NOT_FOUND: %s", sessionID)
	}
	session.Status = status
	session.EndedAt = endedAt
	session.CaptureQualityJSON = mustJSONText(quality)
	r.sessions[sessionID] = session
	return session, nil
}

func (r *fakeRepository) UpsertTask(_ context.Context, task AgentTask) (AgentTask, error) {
	r.tasks[task.ID] = task
	return task, nil
}

func (r *fakeRepository) EndTask(_ context.Context, taskID string, status string, outcome string, endedAt time.Time) (AgentTask, error) {
	task, ok := r.tasks[taskID]
	if !ok {
		return AgentTask{}, fmt.Errorf("TASK_NOT_FOUND: %s", taskID)
	}
	task.Status = status
	task.OutcomeSummary = outcome
	task.EndedAt = endedAt
	r.tasks[taskID] = task
	return task, nil
}

func (r *fakeRepository) GetDefaultTask(_ context.Context, sessionID string) (AgentTask, bool, error) {
	for _, task := range r.tasks {
		if task.SessionID == sessionID && task.TaskSummary == "default task" {
			return task, true, nil
		}
	}
	return AgentTask{}, false, nil
}

func (r *fakeRepository) FindDuplicateEvent(_ context.Context, dedup EventDedupKey) (RawEvent, bool, error) {
	for _, event := range r.events {
		if event.ContentHash == dedup.ContentHash && event.EventType == dedup.EventType {
			if dedup.SessionID != "" && event.SessionID == dedup.SessionID {
				return event, true, nil
			}
			if dedup.SessionID == "" && event.SourceChannel == dedup.SourceChannel && event.WorkspaceID == dedup.WorkspaceID && event.ProjectID == dedup.ProjectID && event.RepoID == dedup.RepoID {
				return event, true, nil
			}
		}
	}
	return RawEvent{}, false, nil
}

func (r *fakeRepository) InsertRawEvent(_ context.Context, event RawEvent) error {
	r.events[event.ID] = event
	return nil
}

func (r *fakeRepository) ListSessions(_ context.Context, req ListSessionsRequest) ([]AgentSession, error) {
	sessions := make([]AgentSession, 0)
	for _, session := range r.sessions {
		if req.WorkspaceID != "" && session.WorkspaceID != req.WorkspaceID {
			continue
		}
		if req.ProjectID != "" && session.ProjectID != req.ProjectID {
			continue
		}
		if req.AgentType != "" && session.AgentType != req.AgentType {
			continue
		}
		if req.Status != "" && session.Status != req.Status {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (r *fakeRepository) ListTasks(_ context.Context, req ListTasksRequest) ([]AgentTask, error) {
	tasks := make([]AgentTask, 0)
	for _, task := range r.tasks {
		if req.SessionID == "" || task.SessionID == req.SessionID {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (r *fakeRepository) ListEvents(_ context.Context, req ListEventsRequest) ([]RawEvent, error) {
	events := make([]RawEvent, 0)
	for _, event := range r.events {
		if req.SessionID != "" && event.SessionID != req.SessionID {
			continue
		}
		if req.TaskID != "" && event.TaskID != req.TaskID {
			continue
		}
		if req.EventType != "" && event.EventType != req.EventType {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *fakeRepository) GetCaptureQuality(_ context.Context, sessionID string) (CaptureQualityReport, error) {
	session, ok := r.sessions[sessionID]
	if !ok {
		return CaptureQualityReport{}, fmt.Errorf("SESSION_NOT_FOUND: %s", sessionID)
	}
	return CaptureQualityReport{
		SessionID:               session.ID,
		CaptureLevel:            session.CaptureLevel,
		CaptureCapabilitiesJSON: session.CaptureCapabilitiesJSON,
		CaptureQualityJSON:      session.CaptureQualityJSON,
	}, nil
}
