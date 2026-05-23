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

// Repository 捕获仓库接口
// 定义 P2-C1 observe service 依赖的持久化能力
type Repository interface {
	// UpsertSession 创建或更新会话
	// session.start事件时调用，创建新的agent_session
	UpsertSession(ctx context.Context, session AgentSession) (AgentSession, error)

	// EndSession 结束会话
	// session.end事件时调用，更新会话状态和结束时间
	EndSession(ctx context.Context, sessionID string, status string, endedAt time.Time, quality CaptureQuality) (AgentSession, error)

	// UpsertTask 创建或更新任务
	// task.start事件或default_task创建时调用
	UpsertTask(ctx context.Context, task AgentTask) (AgentTask, error)

	// EndTask 结束任务
	// task.result事件或session.end时调用，更新任务状态和结果
	EndTask(ctx context.Context, taskID string, status string, outcome string, endedAt time.Time) (AgentTask, error)

	// GetDefaultTask 获取默认任务
	// 查询session的default_task，用于任务绑定
	GetDefaultTask(ctx context.Context, sessionID string) (AgentTask, bool, error)

	// FindDuplicateEvent 查找重复事件
	// 按content_hash + session_id + event_type检测重复
	FindDuplicateEvent(ctx context.Context, dedup EventDedupKey) (RawEvent, bool, error)

	// InsertRawEvent 插入原始事件
	// append-only写入raw_event表
	InsertRawEvent(ctx context.Context, event RawEvent) error

	// ListSessions 列出会话
	// 按过滤条件查询会话列表，用于捕获诊断
	ListSessions(ctx context.Context, req ListSessionsRequest) ([]AgentSession, error)

	// ListTasks 列出任务
	// 按过滤条件查询任务列表，用于查看session/task边界
	ListTasks(ctx context.Context, req ListTasksRequest) ([]AgentTask, error)

	// ListEvents 列出事件
	// 按过滤条件查询事件列表，只返回摘要字段，不返回完整output或diff
	ListEvents(ctx context.Context, req ListEventsRequest) ([]RawEvent, error)

	// GetCaptureQuality 获取捕获质量
	// 查询指定session的capture capability和quality汇总
	GetCaptureQuality(ctx context.Context, sessionID string) (CaptureQualityReport, error)
}

// Service 捕获服务结构体
// 编排 P2 observe 写入链路
// 设计原则：P2 只落 raw_event，不创建 async_job 或 memory_item
type Service struct {
	cfg  config.Config // 配置信息
	repo Repository    // 仓库接口，负责持久化
}

// NewService 创建 capture service
// 后续 MCP handler 和 diagnostics service 复用该入口
func NewService(cfg config.Config, repo Repository) *Service {
	return &Service{cfg: cfg, repo: repo}
}

// Observe 执行 P2 memory.observe 的最小闭环
// 处理流程：
// 1. 生成请求ID
// 2. 校验并归一化请求参数
// 3. 检查内容边界（最小化检查）
// 4. 计算content_hash（如果未提供）
// 5. 解析occurred_at时间
// 6. 计算capture_level
// 7. 解析或创建session
// 8. 解析或创建task
// 9. 检测重复事件
// 10. 构建并写入raw_event
// 11. 更新session质量统计
// 12. 处理task.result和session.end生命周期事件
func (s *Service) Observe(ctx context.Context, req ObserveRequest) (ObserveResponse, error) {
	requestID, err := idgen.New("req")
	if err != nil {
		return ObserveResponse{}, err
	}
	if err := NormalizeObserve(s.cfg.Capture, &req); err != nil {
		return ObserveResponse{}, err
	}
	if err := CheckMinimizedObserve(s.cfg.Capture, req); err != nil {
		_ = s.recordContentBoundaryRejection(ctx, req)
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
		if hasTask && task.Status == StatusActive {
			if _, err := s.repo.EndTask(ctx, task.ID, sessionEndTaskStatus(req), taskOutcome(req), occurredAt); err != nil {
				return ObserveResponse{}, err
			}
		}
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

// ListSessions 返回符合过滤条件的 capture sessions
// 用于 P2-C2 捕获诊断，查看会话列表和捕获质量
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

// ListTasks 返回符合过滤条件的 capture tasks
// 用于查看 session/task 边界，了解任务划分情况
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

// ListEvents 返回最小化后的 raw_event 摘要
// 设计约束：不返回完整 output 或 diff，只返回摘要字段、hash和source refs
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

// Quality 返回指定 session 的 capture capability 和 quality 汇总
// 用于评估session的捕获能力和质量
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

// resolveSession 解析或创建会话
// 处理流程：
// 1. session.start事件且session_id为空时，生成新的session_id
// 2. session_id为空时返回false
// 3. 非session.start事件时，查询已有session
// 4. session.start事件时，创建或更新session
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

// resolveTask 解析或创建任务
// 处理流程：
// 1. 无session时返回false
// 2. 有task_id时，更新或返回已有任务
// 3. 有task.task_summary时，按session_id + normalized_task查找或创建任务
// 4. 无task信息时，查找或创建default_task
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

// taskFromRequest 从请求构建任务对象
// 将ObserveRequest中的任务信息转换为AgentTask结构
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

// updateQuality 更新会话捕获质量
// 每次事件处理后更新session的capture_quality_json
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

// recordContentBoundaryRejection 记录内容边界拒绝
// 当事件因内容过大被拒绝时，更新session的质量统计
func (s *Service) recordContentBoundaryRejection(ctx context.Context, req ObserveRequest) error {
	if req.SessionID == "" {
		return nil
	}
	report, err := s.repo.GetCaptureQuality(ctx, req.SessionID)
	if err != nil {
		return nil
	}
	quality, err := decodeQuality(report.CaptureQualityJSON)
	if err != nil {
		return err
	}
	quality = ApplyContentBoundaryRejection(quality)
	_, err = s.repo.UpsertSession(ctx, AgentSession{
		ID:                      req.SessionID,
		CaptureLevel:            report.CaptureLevel,
		CaptureCapabilitiesJSON: report.CaptureCapabilitiesJSON,
		CaptureQualityJSON:      mustJSONText(quality),
	})
	return err
}

// loadQuality 加载会话捕获质量
// 从数据库读取session的capture_quality_json并解析
func (s *Service) loadQuality(ctx context.Context, sessionID string) (CaptureQuality, error) {
	report, err := s.repo.GetCaptureQuality(ctx, sessionID)
	if err != nil {
		return CaptureQuality{}, err
	}
	return decodeQuality(report.CaptureQualityJSON)
}

// decodeQuality 解码捕获质量JSON
func decodeQuality(raw string) (CaptureQuality, error) {
	var quality CaptureQuality
	if raw == "" {
		return quality, nil
	}
	if err := json.Unmarshal([]byte(raw), &quality); err != nil {
		return CaptureQuality{}, fmt.Errorf("VALIDATION_FAILED: invalid capture_quality_json: %w", err)
	}
	return quality, nil
}

// buildRawEvent 构建原始事件
// 将ObserveRequest转换为持久化的RawEvent结构
// 将keywords、salient_spans、source_refs序列化为JSON
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

// normalizeOccurredAt 归一化发生时间
// 解析RFC3339或RFC3339Nano格式的时间字符串
// 为空时使用当前UTC时间
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

// sessionGoal 从请求中提取会话目标摘要
func sessionGoal(req ObserveRequest) string {
	if req.Session != nil {
		return req.Session.GoalSummary
	}
	return ""
}

// terminalTaskStatus 获取任务终态状态
// 根据请求中的task.status确定任务结束状态
func terminalTaskStatus(req ObserveRequest) string {
	if req.Task != nil && req.Task.Status != "" && req.Task.Status != StatusActive {
		return req.Task.Status
	}
	return StatusUnknown
}

// terminalSessionStatus 获取会话终态状态
// 根据请求中的session.status确定会话结束状态
func terminalSessionStatus(req ObserveRequest) string {
	if req.Session != nil && req.Session.Status != "" && req.Session.Status != StatusActive {
		return req.Session.Status
	}
	return StatusCompleted
}

// sessionEndTaskStatus 获取session结束时的任务状态
// 当session.end事件时，确定活跃任务的结束状态
func sessionEndTaskStatus(req ObserveRequest) string {
	if req.Task != nil && req.Task.Status != "" && req.Task.Status != StatusActive {
		return req.Task.Status
	}
	return StatusUnknown
}

// taskOutcome 从请求中提取任务结果摘要
func taskOutcome(req ObserveRequest) string {
	if req.Task == nil {
		return ""
	}
	return req.Task.OutcomeSummary
}

// jsonText 将值序列化为JSON文本
func jsonText(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: encode json: %w", err)
	}
	return string(data), nil
}

// mustJSONText 将值序列化为JSON文本，忽略错误
func mustJSONText(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
