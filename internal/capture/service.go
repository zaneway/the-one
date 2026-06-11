package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/idgen"
)

const pipelineRawEventOnly = "raw_event_only"

// Repository 捕获仓库接口
// 定义 observe service 依赖的持久化能力
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

// JobEnqueuer 是自动记忆链路对 capture service 暴露的最小入队接口。
// capture 只负责在 raw_event 成功写入后通知自动处理层，不直接执行 evidence/candidate 逻辑。
type JobEnqueuer interface {
	EnqueueRawEvent(ctx context.Context, event RawEvent) error
}

// TaskResultRecorder 在 task.result 成功时写入 task_success 访问反馈（可选，由 automation 实现）。
type TaskResultRecorder interface {
	RecordTaskSuccessFeedback(ctx context.Context, taskID, sessionID string) error
}

// Service 捕获服务结构体
// 编排 observe 写入链路
// 设计原则：capture 只落 raw_event；通过可选 enqueuer 触发后续 async_job。
type Service struct {
	cfg      config.Config // 配置信息
	repo     Repository    // 仓库接口，负责持久化
	enqueuer JobEnqueuer   // 自动处理入队器；为空时保持 raw_event-only 行为
	logger   *slog.Logger  // 结构化日志
}

// NewService 创建 capture service
// 后续 MCP handler 和 diagnostics service 复用该入口
func NewService(cfg config.Config, repo Repository, opts ...ServiceOption) *Service {
	return newService(cfg, repo, nil, opts...)
}

// NewServiceWithAutomation 创建带自动处理入队能力的 capture service。
// 该构造函数用于自动入队场景；调用方继续使用 NewService 即可保持原行为。
func NewServiceWithAutomation(cfg config.Config, repo Repository, enqueuer JobEnqueuer, opts ...ServiceOption) *Service {
	return newService(cfg, repo, enqueuer, opts...)
}

type ServiceOption func(*Service)

// WithSemanticEnhancer 保留为兼容旧调用方的 no-op。
// capture 主链路必须先保存 raw_event；AI 处理由 processor 在后续阶段执行。
func WithSemanticEnhancer(enhancer SemanticEnhancer) ServiceOption {
	return func(s *Service) {
		_ = enhancer
	}
}

func newService(cfg config.Config, repo Repository, enqueuer JobEnqueuer, opts ...ServiceOption) *Service {
	service := &Service{cfg: cfg, repo: repo, enqueuer: enqueuer, logger: slog.Default()}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service
}

// Observe 执行 memory.observe 的最小闭环。
// 完整处理链路：
//  1. 生成请求 ID（用于日志关联）
//  2. 归一化：去空白、校验 event_type/source_channel/actor 合法性、设置默认值
//  3. 内容边界检查：摘要长度、关键词数量、source_refs 完整性（禁止 full_text/full_output/full_diff）
//  4. 计算 content_hash（未提供时自动计算，基于摘要索引字段和 raw_payload_hash 的 SHA256）
//  5. 归一化 occurred_at（RFC3339 格式，空值使用当前 UTC 时间）
//  6. 计算 capture_level（基于 Adapter 声明的能力，范围 1-4）
//  7. 解析或创建 session（session.start 自动生成新 session_id）
//  8. 解析或创建 task（按 task_summary 归一化后查找，无则创建 default_task）
//  9. 幂等检测：按 content_hash + session_id + event_type 去重
//  10. 构建并写入 raw_event（append-only 事实层）
//  11. 更新 session 质量统计（accepted/deduped 计数）
//  12. 生命周期处理：task.result -> EndTask，session.End -> EndTask + EndSession
//  13. 可选：通知自动处理入队（enqueuer 不为空时）
//
// 设计约束：capture 先保存 raw_event，不调用外部 AI；evidence/candidate/memory_item 由后续 processor 处理。
func (s *Service) Observe(ctx context.Context, req ObserveRequest) (ObserveResponse, error) {
	// Step 1: 生成请求 ID，用于日志关联和问题追踪
	requestID, err := idgen.New("req")
	if err != nil {
		s.logger.Error("observe request id generation failed", "error", err)
		return ObserveResponse{}, err
	}
	// Step 2: 归一化——去空白、校验 event_type/source_channel/actor 合法性、设置默认值
	if err := NormalizeObserve(s.cfg.Capture, &req); err != nil {
		s.logger.Error("observe normalize failed", "request_id", requestID, "error", err)
		return ObserveResponse{}, err
	}
	s.logger.Info("observe started",
		"request_id", requestID,
		"event_type", req.EventType,
		"source_channel", req.SourceChannel,
		"session_id", req.SessionID,
		"agent_type", req.AgentType,
	)
	// Step 3: 内容边界检查——摘要长度、关键词数量、salient_span 数量；禁止 full_text/full_output/full_diff
	if err := CheckMinimizedObserve(s.cfg.Capture, req); err != nil {
		s.logger.Error("observe content boundary check failed",
			"request_id", requestID,
			"event_type", req.EventType,
			"error", err,
		)
		// 内容超界时记录拒绝计数到 session 质量统计，然后返回错误
		_ = s.recordContentBoundaryRejection(ctx, req)
		return ObserveResponse{}, err
	}
	// Step 4: 计算 content_hash——未提供时自动计算，基于摘要索引字段和 raw_payload_hash 的 SHA256
	if req.ContentHash == "" {
		contentHash, err := ComputeContentHash(req)
		if err != nil {
			return ObserveResponse{}, err
		}
		req.ContentHash = contentHash
	}
	// Step 5: 归一化 occurred_at——RFC3339 格式，空值使用当前 UTC 时间
	occurredAt, err := normalizeOccurredAt(req.OccurredAt)
	if err != nil {
		return ObserveResponse{}, err
	}
	req.OccurredAt = occurredAt.Format(time.RFC3339Nano)
	// Step 6: 计算 capture_level——基于 Adapter 声明的能力（Level1-4），用于评估捕获质量
	captureLevel := CaptureLevel(req.CaptureCapabilities)

	// Step 7: 解析或创建 session——session.start 自动生成新 session_id，其他事件验证 session 存在
	session, hasSession, err := s.resolveSession(ctx, req, captureLevel)
	if err != nil {
		return ObserveResponse{}, err
	}
	if hasSession && req.SessionID == "" {
		req.SessionID = session.ID
	}
	// Step 8: 解析或创建 task——按 task_summary 归一化后查找，无则创建 default_task
	task, hasTask, err := s.resolveTask(ctx, req, session, hasSession)
	if err != nil {
		return ObserveResponse{}, err
	}
	if hasTask {
		req.TaskID = task.ID
	}

	// Step 9: 幂等检测——按 content_hash + session_id + event_type + source_channel + workspace/project/repo 去重
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
		s.logger.Error("observe duplicate check failed", "request_id", requestID, "error", err)
		return ObserveResponse{}, err
	} else if ok {
		s.logger.Info("observe deduped",
			"request_id", requestID,
			"existing_event_id", existing.ID,
			"event_type", req.EventType,
		)
		// 重复事件：更新 session 质量统计（deduped 计数+1），返回已存在的事件 ID
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

	// Step 10: 构建并写入 raw_event——append-only 事实层，将关键词/salient_spans/source_refs 序列化为 JSON
	event, err := buildRawEvent(req, occurredAt)
	if err != nil {
		s.logger.Error("observe build raw event failed", "request_id", requestID, "error", err)
		return ObserveResponse{}, err
	}
	if err := s.repo.InsertRawEvent(ctx, event); err != nil {
		s.logger.Error("observe insert raw event failed",
			"request_id", requestID,
			"event_id", event.ID,
			"event_type", req.EventType,
			"error", err,
		)
		return ObserveResponse{}, err
	}
	diagnostics := make([]string, 0, 2)
	// Step 11: 更新 session 质量统计（accepted 计数+1，更新 capture_level 和 capabilities）
	if hasSession {
		if err := s.updateQuality(ctx, req, false); err != nil {
			return ObserveResponse{}, err
		}
	}
	// Step 12: 生命周期处理——task.result 事件结束任务，session.end 事件结束活跃任务和会话
	if hasTask && req.EventType == EventTaskResult {
		status := terminalTaskStatus(req)
		if _, err := s.repo.EndTask(ctx, task.ID, status, taskOutcome(req), occurredAt); err != nil {
			return ObserveResponse{}, err
		}
		if status == StatusSucceeded && hasSession {
			if recorder, ok := s.enqueuer.(TaskResultRecorder); ok {
				if err := recorder.RecordTaskSuccessFeedback(ctx, task.ID, session.ID); err != nil {
					diagnostics = append(diagnostics, "task_success_feedback_failed")
				}
			}
		}
	}
	if hasSession && req.EventType == EventSessionEnd {
		// session.end 时，先结束所有活跃任务，再加载最终质量统计并关闭会话
		if hasTask && task.Status == StatusActive {
			endStatus := sessionEndTaskStatus(req)
			if _, err := s.repo.EndTask(ctx, task.ID, endStatus, taskOutcome(req), occurredAt); err != nil {
				return ObserveResponse{}, err
			}
			if endStatus == StatusCompleted || endStatus == StatusSucceeded {
				if recorder, ok := s.enqueuer.(TaskResultRecorder); ok {
					if err := recorder.RecordTaskSuccessFeedback(ctx, task.ID, session.ID); err != nil {
						diagnostics = append(diagnostics, "task_success_feedback_failed")
					}
				}
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
	// Step 13: 可选通知自动处理入队——enqueuer 不为空时将 raw_event 推入异步处理管道
	if s.enqueuer != nil {
		if err := s.enqueuer.EnqueueRawEvent(ctx, event); err != nil {
			// 入队失败不阻塞主流程，记录诊断信息返回给调用方
			s.logger.Error("observe automation enqueue failed",
				"request_id", requestID,
				"event_id", event.ID,
				"error", err,
			)
			diagnostics = append(diagnostics, "automation_enqueue_failed")
		} else {
			s.logger.Debug("observe automation enqueued",
				"request_id", requestID,
				"event_id", event.ID,
			)
		}
	}
	s.logger.Info("observe succeeded",
		"request_id", requestID,
		"event_id", event.ID,
		"event_type", req.EventType,
		"session_id", req.SessionID,
		"task_id", req.TaskID,
		"capture_level", captureLevel,
	)
	return ObserveResponse{
		RequestID:    requestID,
		RawEventID:   event.ID,
		SessionID:    req.SessionID,
		TaskID:       req.TaskID,
		Accepted:     true,
		Pipeline:     pipelineRawEventOnly,
		Deduped:      false,
		CaptureLevel: captureLevel,
		Diagnostics:  diagnostics,
	}, nil
}

// ListSessions 返回符合过滤条件的 capture sessions
// 用于捕获诊断，查看会话列表和捕获质量
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

// ListEvents 返回 raw_event 事实层事件
// 设计约束：raw payload 是否完整由 redaction/truncation 元数据表达，不直接提升为 memory_item
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

// resolveSession 解析或创建 Agent 会话。
// 决策逻辑：
//   - session.start 且 session_id 为空 -> 生成新 session_id（首次创建）
//   - session_id 为空 -> 返回 false（非会话级事件，如手动 CLI 写入）
//   - 非 session.start -> 查询已有 session 的 capture quality（验证 session 存在）
//   - session.start -> 构建 session 对象并 upsert（INSERT OR UPDATE）
//
// session.start 时会初始化 capture_capabilities 和 capture_quality_json。
func (s *Service) resolveSession(ctx context.Context, req ObserveRequest, captureLevel int) (AgentSession, bool, error) {
	// 场景 1: session.start 必须携带 session_id（由 SessionBinder / Driver 绑定，禁止服务端 idgen）
	if req.EventType == EventSessionStart && req.SessionID == "" {
		return AgentSession{}, false, fmt.Errorf("SESSION_REQUIRED: session.start requires session_id")
	}
	// 场景 2: 无 session_id -> 返回 false（非会话级事件，如手动 CLI 写入）
	if req.SessionID == "" {
		return AgentSession{}, false, nil
	}
	// 场景 3: 非 session.start -> 查询已有 session 的 capture quality（验证 session 存在）
	if req.EventType != EventSessionStart {
		report, err := s.repo.GetCaptureQuality(ctx, req.SessionID)
		if err != nil {
			return AgentSession{}, false, err
		}
		return AgentSession{ID: report.SessionID, CaptureLevel: report.CaptureLevel}, true, nil
	}
	// 场景 4: session.start -> 构建 session 对象并 upsert（INSERT OR UPDATE）
	// 初始化 capture_capabilities 和 capture_quality_json（含 missing_capabilities）
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

// resolveTask 解析或创建 Agent 任务。
// 决策逻辑（按优先级）：
//   - 无 session -> 返回 false（任务必须绑定 session）
//   - 有 task_id 且有 task 信息 -> upsert 任务（显式指定任务 ID）
//   - 有 task_id 无 task 信息 -> 返回已有任务（只做关联）
//   - 有 task.task_summary -> 按 session_id + normalized_task_summary 查找，不存在则创建
//   - 无任何 task 信息 -> 查找或创建 default_task（每个 session 最多一个 default_task）
//
// 任务归一化：task_summary 去空白、合并连续空格，用于任务查找匹配。
func (s *Service) resolveTask(ctx context.Context, req ObserveRequest, session AgentSession, hasSession bool) (AgentTask, bool, error) {
	// 无 session -> 任务必须绑定 session，直接返回
	if !hasSession {
		return AgentTask{}, false, nil
	}
	// 优先级 1: 显式指定 task_id -> 有 task 信息则 upsert，否则只做关联
	if req.TaskID != "" {
		if req.Task != nil {
			task, err := s.repo.UpsertTask(ctx, taskFromRequest(req, session, req.TaskID, req.Task.TaskSummary))
			return task, err == nil, err
		}
		return AgentTask{ID: req.TaskID, SessionID: session.ID}, true, nil
	}
	// 优先级 2: 有 task.task_summary -> 按归一化后的 summary 查找，不存在则创建
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
	// 优先级 3: 无任何 task 信息 -> 查找或创建 default_task（每个 session 最多一个）
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
	// 加载当前 session 的 quality 统计（accepted/deduped/event_type 计数等）
	quality, err := s.loadQuality(ctx, req.SessionID)
	if err != nil {
		return err
	}
	// 根据事件类型更新对应的计数器；deduped=true 时只更新 deduped 计数
	quality = ApplyAcceptedEvent(quality, req, deduped)
	// 通过 upsert 更新 session 的 quality 和 capabilities（INSERT OR UPDATE 语义）
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
	// 将数组字段序列化为 JSON 字符串用于 SQLite 存储
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
	// 构建 raw_event 对象——append-only 事实层，所有字段均来自归一化后的请求
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
		RawPayloadJSON:   req.RawPayloadJSON,
		PayloadSchema:    req.PayloadSchema,
		RawPayloadHash:   req.RawPayloadHash,
		RedactionState:   req.RedactionState,
		RedactionPolicy:  req.RedactionPolicy,
		Truncation:       req.TruncationPolicy,
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
		return time.Now(), nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.In(time.Local), nil
	}
	parsed, err = time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("VALIDATION_FAILED: occurred_at must be RFC3339: %w", err)
	}
	return parsed.In(time.Local), nil
}

// sessionGoal 从请求中提取会话目标摘要。
// 入参：ObserveRequest。
// 返回：req.Session.GoalSummary；Session 为 nil 时返回空串。
// 设计约束：不裁剪空白，由调用方在写入数据库前统一 trim。
func sessionGoal(req ObserveRequest) string {
	if req.Session != nil {
		return req.Session.GoalSummary
	}
	return ""
}

// terminalTaskStatus 获取任务终态状态。
// 入参：ObserveRequest。
// 返回：req.Task.Status（当任务非活跃时）；活跃或未指定时回退 StatusUnknown。
// 设计约束：仅在 task 真正结束才返回非 active 状态，确保 lifecycle 与 status 字段一致。
func terminalTaskStatus(req ObserveRequest) string {
	if req.Task != nil && req.Task.Status != "" && req.Task.Status != StatusActive {
		return req.Task.Status
	}
	return StatusUnknown
}

// terminalSessionStatus 获取会话终态状态。
// 入参：ObserveRequest。
// 返回：req.Session.Status（当非 active 时）；活跃或未指定时回退 StatusCompleted，
// 让"未显式声明结束"被视作正常完成的兜底。
func terminalSessionStatus(req ObserveRequest) string {
	if req.Session != nil && req.Session.Status != "" && req.Session.Status != StatusActive {
		return req.Session.Status
	}
	return StatusCompleted
}

// sessionEndTaskStatus 获取 session.end 事件触发时，活跃任务的结束状态。
// 入参：ObserveRequest。
// 返回：任务自带的非 active status；活跃任务或缺失 task 时回退 StatusUnknown。
// 设计约束：与 terminalTaskStatus 行为类似，但专用于"会话级"终结而非任务自身。
func sessionEndTaskStatus(req ObserveRequest) string {
	if req.Task != nil && req.Task.Status != "" && req.Task.Status != StatusActive {
		return req.Task.Status
	}
	return StatusUnknown
}

// taskOutcome 从请求中提取任务结果摘要。
// 入参：ObserveRequest。
// 返回：req.Task.OutcomeSummary；Task 为 nil 时返回空串。
// 设计约束：仅做字段透传，不做语言规范化。
func taskOutcome(req ObserveRequest) string {
	if req.Task == nil {
		return ""
	}
	return req.Task.OutcomeSummary
}

// jsonText 将任意值序列化为 JSON 字符串，失败时返回 VALIDATION_FAILED 错误。
// 入参：value。
// 返回：序列化结果与错误。
// 设计约束：与 internal/app 下的同名函数职责一致；不同包独立实现以避免循环依赖。
func jsonText(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: encode json: %w", err)
	}
	return string(data), nil
}

// mustJSONText 将任意值序列化为 JSON 字符串，忽略错误（失败时返回 "null"）。
// 用于字段为可选结构、写失败不影响主流程的场景，避免在 hot path 增加错误处理。
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
