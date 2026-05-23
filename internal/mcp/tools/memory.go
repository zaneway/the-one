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

// RegisterMemoryTools 注册 P1 手动记忆工具到 MCP 注册表
// 注册的工具：
// - memory.remember：写入或更新记忆
// - memory.search：检索相关记忆
// - memory.context：构建上下文包
// - memory.review：审查记忆状态
// 设计说明：
// - 所有工具共享同一个 MemoryHandler 实例，复用 service 和 logger
// - 工具注册是 MCP 服务启动时的一次性操作
func RegisterMemoryTools(registry *mcp.Registry, service *memory.Service, logger *slog.Logger) {
	handler := &MemoryHandler{service: service, logger: logger}
	registry.Register("memory.remember", handler.Remember)
	registry.Register("memory.search", handler.Search)
	registry.Register("memory.context", handler.Context)
	registry.Register("memory.review", handler.Review)
}

// MemoryHandler P1 手动记忆工具的 MCP 处理器
// 职责：将 MCP JSON 参数适配到 memory service DTO，并处理错误转换
// 设计说明：
// - 持有 memory.Service 实例，委托业务逻辑给 service 层
// - 持有 logger 实例，记录工具调用的关键信息
// - 每个工具方法都遵循相同的模式：解析参数 → 调用 service → 记录日志 → 返回结果
type MemoryHandler struct {
	service *memory.Service
	logger  *slog.Logger
}

// Remember 处理 memory.remember 工具调用
// 功能：写入或更新一条记忆
// 处理流程：
// 1. 解析 MCP JSON 参数为 RememberRequest
// 2. 委托给 memory.Service.Remember 执行业务逻辑
// 3. 记录操作日志（memory_id、memory_type、scope、state、耗时）
// 4. 返回 RememberResponse 或 MCP 错误
// 设计说明：
// - 记录耗时用于性能监控
// - 错误通过 toMCPError 转换为 MCP 标准错误格式
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

// Search 处理 memory.search 工具调用
// 功能：检索与查询相关的记忆
// 处理流程：
// 1. 解析 MCP JSON 参数为 SearchRequest
// 2. 委托给 memory.Service.Search 执行检索逻辑
// 3. 记录操作日志（query_hash、result_count、耗时）
// 4. 返回 SearchResponse 或 MCP 错误
// 设计说明：
// - query_hash 用于日志脱敏，避免记录完整的查询内容
// - result_count 用于监控检索效果
// - 耗时来自 Diagnostics.LatencyMS，包含检索和排序的总时间
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

// Context 处理 memory.context 工具调用
// 功能：构建上下文包，用于注入到 Agent 的系统提示中
// 处理流程：
// 1. 解析 MCP JSON 参数为 ContextRequest
// 2. 委托给 memory.Service.Context 执行上下文构建逻辑
// 3. 记录操作日志（used_memory_count、token_budget、耗时）
// 4. 返回 ContextPack 或 MCP 错误
// 设计说明：
// - used_memory_count 表示实际使用的记忆数量，用于监控上下文构建效果
// - token_budget 表示请求的 token 预算，用于评估资源使用
// - 耗时来自响应的 LatencyMS 字段
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

// Review 处理 memory.review 工具调用
// 功能：审查记忆状态，支持确认、修正、归档、删除等操作
// 处理流程：
// 1. 解析 MCP JSON 参数为 ReviewRequest
// 2. 委托给 memory.Service.Review 执行审查逻辑
// 3. 记录操作日志（memory_id、action、state）
// 4. 返回 ReviewResponse 或 MCP 错误
// 设计说明：
// - action 表示审查操作类型（confirm、correct、archive、delete）
// - state 表示审查后的记忆状态
// - Review 是记忆生命周期管理的关键环节
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

// validationError 创建参数校验错误
// 错误码：VALIDATION_FAILED
// 可重试：否（参数错误需要客户端修正）
func validationError(message string) *mcp.Error {
	return &mcp.Error{ErrorCode: "VALIDATION_FAILED", Message: message, Retryable: false}
}

// toMCPError 将业务错误转换为 MCP 标准错误格式
// 错误码提取规则：
// - 从错误消息中提取 ":" 前的前缀作为错误码
// - 已知的业务错误码（VALIDATION_FAILED、SCOPE_INVALID 等）设置为不可重试
// - STORAGE_BUSY 错误码设置为可重试（等待 SQLite 写锁释放）
// - 其他未知错误使用 INTERNAL_ERROR 错误码，设置为可重试
// 设计说明：
// - 错误码前缀是业务层定义的错误分类约定
// - FallbackHint 提供错误恢复建议，帮助客户端处理错误
func toMCPError(err error) *mcp.Error {
	message := err.Error()
	code := "INTERNAL_ERROR"
	retryable := true
	// 从错误消息中提取错误码前缀
	if i := strings.Index(message, ":"); i > 0 {
		prefix := message[:i]
		switch prefix {
		// 业务错误码，不可重试
		case "VALIDATION_FAILED", "SCOPE_INVALID", "CONTENT_TOO_LARGE", "MEMORY_NOT_FOUND", "STATE_CONFLICT", "FTS_UNAVAILABLE", "SESSION_REQUIRED", "TASK_INVALID", "CAPTURE_UNSUPPORTED", "SESSION_NOT_FOUND", "TASK_NOT_FOUND":
			code = prefix
			retryable = false
		// 存储繁忙错误码，可重试
		case "STORAGE_BUSY":
			code = prefix
			retryable = true
		}
	}
	return &mcp.Error{ErrorCode: code, Message: message, Retryable: retryable, FallbackHint: fallbackHint(code)}
}

// fallbackHint 根据错误码提供错误恢复建议
// 设计目的：帮助客户端理解错误原因并采取正确的恢复措施
// 返回值：人类可读的恢复建议字符串
func fallbackHint(code string) string {
	switch code {
	case "SESSION_REQUIRED":
		return "send session.start first and reuse the returned session_id"
	case "CONTENT_TOO_LARGE":
		return "send summarized content with salient_spans and content_hash"
	case "SCOPE_INVALID":
		return "check scope required fields before retrying"
	case "FTS_UNAVAILABLE":
		return "restart memoryd with sqlite fts5 support or use status diagnostics"
	case "STORAGE_BUSY":
		return "retry after the current SQLite write completes"
	default:
		return "check request fields and memoryd diagnostics"
	}
}

// hashForLog 为日志生成字符串的短哈希值
// 算法：DJB2 哈希算法，输出8位十六进制字符串
// 设计目的：
// - 用于日志脱敏，避免记录完整的查询内容
// - 相同输入始终生成相同的哈希值，便于日志关联分析
// - 8位十六进制提供足够的区分度，同时保持日志简洁
func hashForLog(value string) string {
	if value == "" {
		return ""
	}
	// DJB2 哈希算法
	var sum uint32
	for _, r := range value {
		sum = sum*33 + uint32(r)
	}
	// 转换为8位十六进制字符串
	const alphabet = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = alphabet[sum&0xf]
		sum >>= 4
	}
	return string(out)
}
