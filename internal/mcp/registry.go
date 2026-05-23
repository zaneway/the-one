package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Error 是所有工具统一返回的错误结构，字段与 P0/P1 详细设计保持一致。
type Error struct {
	ErrorCode    string `json:"error_code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	FallbackHint string `json:"fallback_hint,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode, e.Message)
}

type Handler func(ctx context.Context, params json.RawMessage) (any, *Error)

// Registry 是 P0 的工具注册中心。P1 的 remember/search/context/review 会复用此入口。
type Registry struct {
	handlers map[string]Handler
	logger   *slog.Logger
}

func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
		logger:   logger,
	}
}

// Register 注册一个工具处理器。重复注册说明模块装配错误，直接 panic 便于启动期暴露。
func (r *Registry) Register(name string, handler Handler) {
	if _, exists := r.handlers[name]; exists {
		panic("duplicate mcp tool: " + name)
	}
	r.handlers[name] = handler
}

// Call 调用工具并记录基础耗时日志。日志不包含完整 params，避免泄露用户输入或工具输出。
func (r *Registry) Call(ctx context.Context, name string, params any) (any, *Error) {
	startedAt := time.Now()
	handler, ok := r.handlers[name]
	if !ok {
		return nil, &Error{
			ErrorCode:    "VALIDATION_FAILED",
			Message:      "unknown tool: " + name,
			Retryable:    false,
			FallbackHint: "check memoryd status and tool name",
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, &Error{
			ErrorCode: "VALIDATION_FAILED",
			Message:   "invalid params",
			Retryable: false,
		}
	}
	result, toolErr := handler(ctx, raw)
	r.logger.Info("mcp tool called",
		"tool", name,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"ok", toolErr == nil,
	)
	return result, toolErr
}
