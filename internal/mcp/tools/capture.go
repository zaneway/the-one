package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/idgen"
	"github.com/zaneway/the-one/internal/mcp"
)

// RegisterCaptureTools 注册 P2 事件捕获和捕获诊断工具到 MCP 注册表
// 注册的工具：
// - memory.observe：捕获 Agent 事件
// - memory.capture.sessions：查询会话列表
// - memory.capture.tasks：查询任务列表
// - memory.capture.events：查询事件列表
// - memory.capture.quality：查询捕获质量
// 设计说明：
// - 所有工具共享同一个 CaptureHandler 实例，复用 service 和 logger
// - 工具注册是 MCP 服务启动时的一次性操作
func RegisterCaptureTools(registry *mcp.Registry, service *capture.Service, logger *slog.Logger) {
	handler := &CaptureHandler{service: service, logger: logger}
	registry.Register("memory.observe", handler.Observe)
	registry.Register("memory.capture.sessions", handler.ListSessions)
	registry.Register("memory.capture.tasks", handler.ListTasks)
	registry.Register("memory.capture.events", handler.ListEvents)
	registry.Register("memory.capture.quality", handler.Quality)
}

// CaptureHandler P2 事件捕获工具的 MCP 处理器
// 职责：将 MCP JSON 参数适配到 capture service DTO，并处理错误转换
// 设计说明：
// - 持有 capture.Service 实例，委托业务逻辑给 service 层
// - 持有 logger 实例，记录工具调用的关键信息
// - 每个工具方法都遵循相同的模式：解析参数 → 调用 service → 记录日志 → 返回结果
type CaptureHandler struct {
	service *capture.Service
	logger  *slog.Logger
}

// Observe 处理 memory.observe 工具调用
// 功能：捕获 Agent 事件，写入 raw_event 表
// 处理流程：
// 1. 解析 MCP JSON 参数为 ObserveRequest
// 2. 委托给 capture.Service.Observe 执行事件捕获逻辑
// 3. 记录操作日志（raw_event_id、session_id、task_id、deduped、耗时）
// 4. 返回 ObserveResponse 或 MCP 错误
// 设计说明：
// - raw_event_id 是生成的事件唯一标识
// - session_id 和 task_id 是事件关联的会话和任务
// - deduped 表示事件是否被去重（重复事件不重复写入）
// - 耗时用于性能监控
func (h *CaptureHandler) Observe(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	startedAt := time.Now()
	var req capture.ObserveRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, captureMCPError(validationError("invalid observe params"))
	}
	resp, err := h.service.Observe(ctx, req)
	if err != nil {
		return nil, captureMCPError(toMCPError(err))
	}
	h.logger.Info("observe completed",
		"raw_event_id", resp.RawEventID,
		"session_id", resp.SessionID,
		"task_id", resp.TaskID,
		"deduped", resp.Deduped,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	return resp, nil
}

// ListSessions 处理 memory.capture.sessions 工具调用
// 功能：查询会话列表，用于捕获诊断和会话管理
// 处理流程：
// 1. 解析 MCP JSON 参数为 ListSessionsRequest
// 2. 委托给 capture.Service.ListSessions 执行查询逻辑
// 3. 记录操作日志（result_count）
// 4. 返回 ListSessionsResponse 或 MCP 错误
// 设计说明：
// - result_count 表示返回的会话数量
// - 支持按 agent_type、workspace_id 等条件过滤
// - 默认限制返回50条记录
func (h *CaptureHandler) ListSessions(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req capture.ListSessionsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, captureMCPError(validationError("invalid capture sessions params"))
	}
	resp, err := h.service.ListSessions(ctx, req)
	if err != nil {
		return nil, captureMCPError(toMCPError(err))
	}
	h.logger.Info("capture sessions listed", "result_count", len(resp.Sessions))
	return resp, nil
}

// ListTasks 处理 memory.capture.tasks 工具调用
// 功能：查询任务列表，用于查看 session/task 边界
// 处理流程：
// 1. 解析 MCP JSON 参数为 ListTasksRequest
// 2. 委托给 capture.Service.ListTasks 执行查询逻辑
// 3. 记录操作日志（result_count）
// 4. 返回 ListTasksResponse 或 MCP 错误
// 设计说明：
// - result_count 表示返回的任务数量
// - 支持按 session_id、status 等条件过滤
// - 默认限制返回50条记录
func (h *CaptureHandler) ListTasks(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req capture.ListTasksRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, captureMCPError(validationError("invalid capture tasks params"))
	}
	resp, err := h.service.ListTasks(ctx, req)
	if err != nil {
		return nil, captureMCPError(toMCPError(err))
	}
	h.logger.Info("capture tasks listed", "result_count", len(resp.Tasks))
	return resp, nil
}

// ListEvents 处理 memory.capture.events 工具调用
// 功能：查询事件列表，只返回摘要字段，不返回完整 output 或 diff
// 处理流程：
// 1. 解析 MCP JSON 参数为 ListEventsRequest
// 2. 委托给 capture.Service.ListEvents 执行查询逻辑
// 3. 记录操作日志（result_count）
// 4. 返回 ListEventsResponse 或 MCP 错误
// 设计说明：
// - result_count 表示返回的事件数量
// - 只返回摘要字段，不返回完整 output 或 diff，避免数据泄露
// - 支持按 session_id、event_type、source_channel 等条件过滤
// - 默认限制返回50条记录
func (h *CaptureHandler) ListEvents(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req capture.ListEventsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, captureMCPError(validationError("invalid capture events params"))
	}
	resp, err := h.service.ListEvents(ctx, req)
	if err != nil {
		return nil, captureMCPError(toMCPError(err))
	}
	h.logger.Info("capture events listed", "result_count", len(resp.Events))
	return resp, nil
}

// Quality 处理 memory.capture.quality 工具调用
// 功能：查询指定 session 的捕获能力（capability）和质量（quality）汇总
// 处理流程：
// 1. 解析 MCP JSON 参数为 QualityRequest
// 2. 委托给 capture.Service.Quality 执行查询逻辑
// 3. 记录操作日志（session_id、capture_level）
// 4. 返回 QualityResponse 或 MCP 错误
// 设计说明：
// - session_id 是查询的目标会话标识
// - capture_level 表示会话的捕获等级（Level1-Level4）
// - 用于评估 Adapter 的捕获能力和质量
func (h *CaptureHandler) Quality(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req capture.QualityRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, captureMCPError(validationError("invalid capture quality params"))
	}
	resp, err := h.service.Quality(ctx, req)
	if err != nil {
		return nil, captureMCPError(toMCPError(err))
	}
	h.logger.Info("capture quality loaded", "session_id", resp.Report.SessionID, "capture_level", resp.Report.CaptureLevel)
	return resp, nil
}

// captureMCPError 为 MCP 错误补充 request_id
// 处理逻辑：
// 1. 如果错误为 nil 或已有 request_id，直接返回
// 2. 否则生成新的 request_id 并补充到错误中
// 设计目的：
// - 确保所有返回给客户端的错误都有 request_id，便于问题追踪
// - request_id 格式为 "req_<snowflake_id>"
func captureMCPError(err *mcp.Error) *mcp.Error {
	if err == nil || err.RequestID != "" {
		return err
	}
	requestID, idErr := idgen.New("req")
	if idErr == nil {
		err.RequestID = requestID
	}
	return err
}
