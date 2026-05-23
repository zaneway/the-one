package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/mcp"
	"github.com/zaneway/the-one/internal/memory"
)

// RegisterMemoryTools 注册 P1 手动记忆工具。
func RegisterMemoryTools(registry *mcp.Registry, service *memory.Service, logger *slog.Logger) {
	handler := &MemoryHandler{service: service, logger: logger}
	registry.Register("memory.remember", handler.Remember)
	registry.Register("memory.search", handler.Search)
	registry.Register("memory.context", handler.Context)
	registry.Register("memory.review", handler.Review)
}

// MemoryHandler 将 MCP JSON 参数适配到 memory service DTO。
type MemoryHandler struct {
	service *memory.Service
	logger  *slog.Logger
}

// Remember 处理 memory.remember。
func (h *MemoryHandler) Remember(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	startedAt := time.Now()
	var req memory.RememberRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid remember params")
	}
	resp, err := h.service.Remember(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("remember completed",
		"memory_id", resp.MemoryID,
		"memory_type", req.MemoryType,
		"scope", req.Scope,
		"state", resp.State,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	return resp, nil
}

// Search 处理 memory.search。
func (h *MemoryHandler) Search(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req memory.SearchRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid search params")
	}
	resp, err := h.service.Search(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("search completed",
		"query_hash", hashForLog(req.Query),
		"result_count", len(resp.Results),
		"duration_ms", resp.Diagnostics.LatencyMS,
	)
	return resp, nil
}

// Context 处理 memory.context。
func (h *MemoryHandler) Context(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req memory.ContextRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid context params")
	}
	resp, err := h.service.Context(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("context completed",
		"used_memory_count", len(resp.UsedMemoryIDs),
		"token_budget", req.TokenBudget,
		"duration_ms", resp.LatencyMS,
	)
	return resp, nil
}

// Review 处理 memory.review。
func (h *MemoryHandler) Review(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req memory.ReviewRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid review params")
	}
	resp, err := h.service.Review(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("review completed",
		"memory_id", resp.MemoryID,
		"action", req.Action,
		"state", resp.State,
	)
	return resp, nil
}

func validationError(message string) *mcp.Error {
	return &mcp.Error{ErrorCode: "VALIDATION_FAILED", Message: message, Retryable: false}
}

func toMCPError(err error) *mcp.Error {
	message := err.Error()
	code := "INTERNAL_ERROR"
	retryable := true
	if i := strings.Index(message, ":"); i > 0 {
		prefix := message[:i]
		switch prefix {
		case "VALIDATION_FAILED", "SCOPE_INVALID", "CONTENT_TOO_LARGE", "MEMORY_NOT_FOUND", "STATE_CONFLICT", "FTS_UNAVAILABLE":
			code = prefix
			retryable = false
		case "STORAGE_BUSY":
			code = prefix
			retryable = true
		}
	}
	return &mcp.Error{ErrorCode: code, Message: message, Retryable: retryable}
}

func hashForLog(value string) string {
	if value == "" {
		return ""
	}
	var sum uint32
	for _, r := range value {
		sum = sum*33 + uint32(r)
	}
	const alphabet = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = alphabet[sum&0xf]
		sum >>= 4
	}
	return string(out)
}
