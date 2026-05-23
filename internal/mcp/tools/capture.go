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

// RegisterCaptureTools 注册 P2 事件捕获和捕获诊断工具。
func RegisterCaptureTools(registry *mcp.Registry, service *capture.Service, logger *slog.Logger) {
	handler := &CaptureHandler{service: service, logger: logger}
	registry.Register("memory.observe", handler.Observe)
	registry.Register("memory.capture.sessions", handler.ListSessions)
	registry.Register("memory.capture.tasks", handler.ListTasks)
	registry.Register("memory.capture.events", handler.ListEvents)
	registry.Register("memory.capture.quality", handler.Quality)
}

// CaptureHandler 将 MCP JSON 参数适配到 capture service DTO。
type CaptureHandler struct {
	service *capture.Service
	logger  *slog.Logger
}

// Observe 处理 memory.observe。
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

// ListSessions 处理 memory.capture.sessions。
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

// ListTasks 处理 memory.capture.tasks。
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

// ListEvents 处理 memory.capture.events。
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

// Quality 处理 memory.capture.quality。
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
