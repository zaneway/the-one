package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/idgen"
)

const pipelineRawEventOnly = "raw_event_only"

// Repository 定义 P2-C1 observe service 依赖的持久化能力。
type Repository interface {
	UpsertSession(ctx context.Context, session AgentSession) (AgentSession, error)
	EndSession(ctx context.Context, sessionID string, status string, endedAt time.Time, quality CaptureQuality) (AgentSession, error)
	UpsertTask(ctx context.Context, task AgentTask) (AgentTask, error)
	EndTask(ctx context.Context, taskID string, status string, outcome string, endedAt time.Time) (AgentTask, error)
	GetDefaultTask(ctx context.Context, sessionID string) (AgentTask, bool, error)
	FindDuplicateEvent(ctx context.Context, dedup EventDedupKey) (RawEvent, bool, error)
	InsertRawEvent(ctx context.Context, event RawEvent) error
	ListSessions(ctx context.Context, req ListSessionsRequest) ([]AgentSession, error)
	ListTasks(ctx context.Context, req ListTasksRequest) ([]AgentTask, error)
	ListEvents(ctx context.Context, req ListEventsRequest) ([]RawEvent, error)
	GetCaptureQuality(ctx context.Context, sessionID string) (CaptureQualityReport, error)
}

// Service 编排 P2 observe 写入链路。P2 只落 raw_event，不创建 async_job 或 memory_item。
type Service struct {
	cfg  config.Config
	repo Repository
}

// NewService 创建 capture service，后续 MCP handler 和 diagnostics service 复用该入口。
func NewService(cfg config.Config, repo Repository) *Service {
	return &Service{cfg: cfg, repo: repo}
}

// Observe 执行 P2 memory.observe 的最小闭环：校验、最小化、session/task 解析、去重、写 raw_event 和更新 quality。
func (s *Service) Observe(ctx context.Context, req ObserveRequest) (ObserveResponse, error) {
	requestID, err := idgen.New("req")
	if err != nil {
		return ObserveResponse{}, err
	}
	if err := NormalizeObserve(s.cfg.Capture, &req); err != nil {
		return ObserveResponse{}, err
	}
	if err := CheckMinimizedObserve(s.cfg.Capture, req); err != nil {
		return ObserveResponse{}, err
	}
	if req.ContentHash == "" {
		contentHash, err := ComputeContentHash(req)
		if err != nil {
			return ObserveResponse{}, err
		}
		req.ContentHash = contentHash
	}
	occurredAt, err := normalizeOccurredAt(req.OccurredAt)
	if err != nil {
		return ObserveResponse{}, err
	}
	req.OccurredAt = occurredAt.Format(time.RFC3339Nano)
	captureLevel := CaptureLevel(req.CaptureCapabilities)

	session, hasSession, err := s.resolveSession(ctx, req, captureLevel)
	if err != nil {
		return ObserveResponse{}, err
	}
	if hasSession && req.SessionID == "" {
		req.SessionID = session.ID
	}
	task, hasTask, err := s.resolveTask(ctx, req, session, hasSession)
	if err != nil {
		return ObserveResponse{}, err
	}
	if hasTask {
		req.TaskID = task.ID
	}

	dedup := EventDedupKey{
		ContentHash:   req.ContentHash,
		SessionID:     req.SessionID,
		EventType:     req.EventType,
		SourceChannel: req.SourceChannel,
		WorkspaceID:   req.WorkspaceID,
		ProjectID:     req.ProjectID,
		RepoID:        req.RepoID,
	}
	if existing, ok, err := s.repo.FindDuplicateEvent(ctx, dedup); err != nil {
		return ObserveResponse{}, err
	} else if ok {
		if hasSession {
			_ = s.updateQuality(ctx, req, true)
		}
		return ObserveResponse{
			RequestID:    requestID,
			RawEventID:   existing.ID,
			SessionID:    existing.SessionID,
			TaskID:       existing.TaskID,
			Accepted:     true,
			Pipeline:     pipelineRawEventOnly,
			Deduped:      true,
			CaptureLevel: captureLevel,
		}, nil
	}

	event, err := buildRawEvent(req, occurredAt)
	if err != nil {
		return ObserveResponse{}, err
	}
	if err := s.repo.InsertRawEvent(ctx, event); err != nil {
		return ObserveResponse{}, err
	}
	if hasSession {
		if err := s.updateQuality(ctx, req, false); err != nil {
			return ObserveResponse{}, err
		}
	}
	if hasTask && req.EventType == EventTaskResult {
		status := terminalTaskStatus(req)
		if _, err := s.repo.EndTask(ctx, task.ID, status, taskOutcome(req), occurredAt); err != nil {
			return ObserveResponse{}, err
		}
	}
	if hasSession && req.EventType == EventSessionEnd {
		quality, err := s.loadQuality(ctx, session.ID)
		if err != nil {
			return ObserveResponse{}, err
		}
		if _, err := s.repo.EndSession(ctx, session.ID, terminalSessionStatus(req), occurredAt, quality); err != nil {
			return ObserveResponse{}, err
		}
	}
	return ObserveResponse{
		RequestID:    requestID,
		RawEventID:   event.ID,
		SessionID:    req.SessionID,
		TaskID:       req.TaskID,
		Accepted:     true,
		Pipeline:     pipelineRawEventOnly,
		Deduped:      false,
		CaptureLevel: captureLevel,
	}, nil
}

// ListSessions 返回符合过滤条件的 capture sessions，用于 P2-C2 捕获诊断。
func (s *Service) ListSessions(ctx context.Context, req ListSessionsRequest) (ListSessionsResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 50
	}
	sessions, err := s.repo.ListSessions(ctx, req)
	if err != nil {
		return ListSessionsResponse{}, err
	}
	return ListSessionsResponse{Sessions: sessions}, nil
}

// ListTasks 返回符合过滤条件的 capture tasks，用于查看 session/task 边界。
func (s *Service) ListTasks(ctx context.Context, req ListTasksRequest) (ListTasksResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 50
	}
	tasks, err := s.repo.ListTasks(ctx, req)
	if err != nil {
		return ListTasksResponse{}, err
	}
	return ListTasksResponse{Tasks: tasks}, nil
}

// ListEvents 返回最小化后的 raw_event 摘要，不返回完整 output 或 diff。
func (s *Service) ListEvents(ctx context.Context, req ListEventsRequest) (ListEventsResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 50
	}
	events, err := s.repo.ListEvents(ctx, req)
	if err != nil {
		return ListEventsResponse{}, err
	}
	return ListEventsResponse{Events: events}, nil
}

// Quality 返回指定 session 的 capture capability 和 quality 汇总。
func (s *Service) Quality(ctx context.Context, req QualityRequest) (QualityResponse, error) {
	if req.SessionID == "" {
		return QualityResponse{}, fmt.Errorf("VALIDATION_FAILED: session_id is required")
	}
	report, err := s.repo.GetCaptureQuality(ctx, req.SessionID)
	if err != nil {
		return QualityResponse{}, err
	}
	return QualityResponse{Report: report}, nil
}

func (s *Service) resolveSession(ctx context.Context, req ObserveRequest, captureLevel int) (AgentSession, bool, error) {
	if req.EventType == EventSessionStart && req.SessionID == "" {
		sessionID, err := idgen.New("sess")
		if err != nil {
			return AgentSession{}, false, err
		}
		req.SessionID = sessionID
	}
	if req.SessionID == "" {
		return AgentSession{}, false, nil
	}
	if req.EventType != EventSessionStart {
		report, err := s.repo.GetCaptureQuality(ctx, req.SessionID)
		if err != nil {
			return AgentSession{}, false, err
		}
		return AgentSession{ID: report.SessionID, CaptureLevel: report.CaptureLevel}, true, nil
	}
	capabilitiesJSON, err := jsonText(req.CaptureCapabilities)
	if err != nil {
		return AgentSession{}, false, err
	}
	qualityJSON, err := jsonText(CaptureQuality{MissingCapabilities: MissingCapabilities(req.CaptureCapabilities)})
	if err != nil {
		return AgentSession{}, false, err
	}
	session := AgentSession{
		ID:                      req.SessionID,
		AgentType:               req.AgentType,
		WorkspaceID:             req.WorkspaceID,
		ProjectID:               req.ProjectID,
		RepoID:                  req.RepoID,
		CaptureLevel:            captureLevel,
		CaptureCapabilitiesJSON: capabilitiesJSON,
		CaptureQualityJSON:      qualityJSON,
		GoalSummary:             sessionGoal(req),
		Status:                  StatusActive,
	}
	if req.Session != nil && req.Session.Status != "" {
		session.Status = req.Session.Status
	}
	stored, err := s.repo.UpsertSession(ctx, session)
	return stored, err == nil, err
}

func (s *Service) resolveTask(ctx context.Context, req ObserveRequest, session AgentSession, hasSession bool) (AgentTask, bool, error) {
	if !hasSession {
		return AgentTask{}, false, nil
	}
	if req.TaskID != "" {
		if req.Task != nil {
			task, err := s.repo.UpsertTask(ctx, taskFromRequest(req, session, req.TaskID, req.Task.TaskSummary))
			return task, err == nil, err
		}
		return AgentTask{ID: req.TaskID, SessionID: session.ID}, true, nil
	}
	if req.Task != nil && req.Task.TaskSummary != "" {
		tasks, err := s.repo.ListTasks(ctx, ListTasksRequest{SessionID: session.ID})
		if err != nil {
			return AgentTask{}, false, err
		}
		normalized := NormalizeTaskSummary(req.Task.TaskSummary)
		for _, task := range tasks {
			if NormalizeTaskSummary(task.TaskSummary) == normalized {
				return task, true, nil
			}
		}
		taskID, err := idgen.New("task")
		if err != nil {
			return AgentTask{}, false, err
		}
		task, err := s.repo.UpsertTask(ctx, taskFromRequest(req, session, taskID, normalized))
		return task, err == nil, err
	}
	task, ok, err := s.repo.GetDefaultTask(ctx, session.ID)
	if err != nil || ok {
		return task, ok, err
	}
	taskID, err := idgen.New("task")
	if err != nil {
		return AgentTask{}, false, err
	}
	task, err = s.repo.UpsertTask(ctx, taskFromRequest(req, session, taskID, "default task"))
	return task, err == nil, err
}

func taskFromRequest(req ObserveRequest, session AgentSession, taskID string, summary string) AgentTask {
	status := StatusActive
	outcome := ""
	if req.Task != nil {
		status = req.Task.Status
		outcome = req.Task.OutcomeSummary
	}
	return AgentTask{
		ID:             taskID,
		SessionID:      session.ID,
		WorkspaceID:    firstNonEmpty(req.WorkspaceID, session.WorkspaceID),
		ProjectID:      firstNonEmpty(req.ProjectID, session.ProjectID),
		RepoID:         firstNonEmpty(req.RepoID, session.RepoID),
		TaskSummary:    summary,
		Status:         status,
		OutcomeSummary: outcome,
	}
}

func (s *Service) updateQuality(ctx context.Context, req ObserveRequest, deduped bool) error {
	quality, err := s.loadQuality(ctx, req.SessionID)
	if err != nil {
		return err
	}
	quality = ApplyAcceptedEvent(quality, req, deduped)
	session := AgentSession{
		ID:                      req.SessionID,
		AgentType:               req.AgentType,
		WorkspaceID:             req.WorkspaceID,
		ProjectID:               req.ProjectID,
		RepoID:                  req.RepoID,
		CaptureLevel:            CaptureLevel(req.CaptureCapabilities),
		CaptureCapabilitiesJSON: mustJSONText(req.CaptureCapabilities),
		CaptureQualityJSON:      mustJSONText(quality),
		Status:                  StatusActive,
	}
	_, err = s.repo.UpsertSession(ctx, session)
	return err
}

func (s *Service) loadQuality(ctx context.Context, sessionID string) (CaptureQuality, error) {
	report, err := s.repo.GetCaptureQuality(ctx, sessionID)
	if err != nil {
		return CaptureQuality{}, err
	}
	var quality CaptureQuality
	if report.CaptureQualityJSON == "" {
		return quality, nil
	}
	if err := json.Unmarshal([]byte(report.CaptureQualityJSON), &quality); err != nil {
		return CaptureQuality{}, fmt.Errorf("VALIDATION_FAILED: invalid capture_quality_json: %w", err)
	}
	return quality, nil
}

func buildRawEvent(req ObserveRequest, occurredAt time.Time) (RawEvent, error) {
	eventID, err := idgen.New("evt")
	if err != nil {
		return RawEvent{}, err
	}
	keywordsJSON, err := jsonText(req.Keywords)
	if err != nil {
		return RawEvent{}, err
	}
	spansJSON, err := jsonText(req.SalientSpans)
	if err != nil {
		return RawEvent{}, err
	}
	refsJSON, err := jsonText(req.SourceRefs)
	if err != nil {
		return RawEvent{}, err
	}
	return RawEvent{
		ID:               eventID,
		SessionID:        req.SessionID,
		TaskID:           req.TaskID,
		WorkspaceID:      req.WorkspaceID,
		ProjectID:        req.ProjectID,
		RepoID:           req.RepoID,
		AgentType:        req.AgentType,
		EventType:        req.EventType,
		SourceChannel:    req.SourceChannel,
		OccurredAt:       occurredAt,
		Actor:            req.Actor,
		ToolName:         req.ToolName,
		InputSummary:     req.InputSummary,
		OutputSummary:    req.OutputSummary,
		ContentSummary:   req.ContentSummary,
		KeywordsJSON:     keywordsJSON,
		SalientSpansJSON: spansJSON,
		SourceRefsJSON:   refsJSON,
		ContentHash:      req.ContentHash,
		Sensitivity:      req.Sensitivity,
		RetentionHint:    req.RetentionHint,
	}, nil
}

func normalizeOccurredAt(value string) (time.Time, error) {
	if value == "" {
		return time.Now().UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.UTC(), nil
	}
	parsed, err = time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("VALIDATION_FAILED: occurred_at must be RFC3339: %w", err)
	}
	return parsed.UTC(), nil
}

func sessionGoal(req ObserveRequest) string {
	if req.Session != nil {
		return req.Session.GoalSummary
	}
	return ""
}

func terminalTaskStatus(req ObserveRequest) string {
	if req.Task != nil && req.Task.Status != "" && req.Task.Status != StatusActive {
		return req.Task.Status
	}
	return StatusUnknown
}

func terminalSessionStatus(req ObserveRequest) string {
	if req.Session != nil && req.Session.Status != "" && req.Session.Status != StatusActive {
		return req.Session.Status
	}
	return StatusCompleted
}

func taskOutcome(req ObserveRequest) string {
	if req.Task == nil {
		return ""
	}
	return req.Task.OutcomeSummary
}

func jsonText(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: encode json: %w", err)
	}
	return string(data), nil
}

func mustJSONText(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
