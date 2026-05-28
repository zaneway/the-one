package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Error 是所有工具统一返回的错误结构。
type Error struct {
	RequestID    string `json:"request_id,omitempty"`
	ErrorCode    string `json:"error_code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	FallbackHint string `json:"fallback_hint,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorCode, e.Message)
}

type Handler func(ctx context.Context, params json.RawMessage) (any, *Error)

// ToolSpec 描述一个可暴露给 MCP Host 的工具。
// Registry 仍然是内部调用边界；ToolSpec 只增加标准 MCP tools/list 所需的元数据。
type ToolSpec struct {
	Name            string
	Title           string
	Description     string
	InputSchema     json.RawMessage
	OutputSchema    json.RawMessage
	ReadOnlyHint    bool
	DestructiveHint *bool
	IdempotentHint  bool
	OpenWorldHint   *bool
	Handler         Handler
}

// Registry 是工具注册中心。remember/search/context/review 会复用此入口。
type Registry struct {
	handlers map[string]Handler
	tools    map[string]ToolSpec
	order    []string
	logger   *slog.Logger
}

func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
		tools:    make(map[string]ToolSpec),
		logger:   logger,
	}
}

// Register 注册一个工具处理器。重复注册说明模块装配错误，直接 panic 便于启动期暴露。
func (r *Registry) Register(name string, handler Handler) {
	r.RegisterTool(ToolSpec{
		Name:        name,
		InputSchema: RawObjectSchema(),
		Handler:     handler,
	})
}

// RegisterTool 注册一个带 MCP 元数据的工具处理器。
// InputSchema 必须是 JSON Schema object；为空时退化为任意 object，避免 tools/list 暴露非法 schema。
func (r *Registry) RegisterTool(spec ToolSpec) {
	name := spec.Name
	if _, exists := r.handlers[name]; exists {
		panic("duplicate mcp tool: " + name)
	}
	if len(spec.InputSchema) == 0 {
		spec.InputSchema = RawObjectSchema()
	}
	r.handlers[name] = spec.Handler
	r.tools[name] = spec
	r.order = append(r.order, name)
}

// Tools 返回按注册顺序排列的工具元数据快照，供官方 MCP SDK 注册 tools/list。
func (r *Registry) Tools() []ToolSpec {
	out := make([]ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// Call 调用工具并记录基础耗时日志。
// 处理流程：查找 handler -> 序列化 params -> 调用 handler -> 记录耗时日志。
// 安全设计：日志只记录 tool 名称和耗时，不包含完整 params，避免泄露用户输入。
// 错误处理：未知工具返回 VALIDATION_FAILED 错误码。
func (r *Registry) Call(ctx context.Context, name string, params any) (any, *Error) {
	startedAt := time.Now()
	// 查找注册的 handler，未知工具返回 VALIDATION_FAILED
	handler, ok := r.handlers[name]
	if !ok {
		return nil, &Error{
			ErrorCode:    "VALIDATION_FAILED",
			Message:      "unknown tool: " + name,
			Retryable:    false,
			FallbackHint: "check theone status and tool name",
		}
	}
	// 将 params 统一序列化为 json.RawMessage，handler 内部再按需反序列化
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, &Error{
			ErrorCode: "VALIDATION_FAILED",
			Message:   "invalid params",
			Retryable: false,
		}
	}
	result, toolErr := handler(ctx, raw)
	// 日志只记录 tool 名称和耗时，不包含完整 params，避免泄露用户输入
	r.logger.Info("mcp tool called",
		"tool", name,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"ok", toolErr == nil,
	)
	return result, toolErr
}
