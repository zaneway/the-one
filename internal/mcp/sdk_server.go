package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// SDKServer 把 theone 内部的 Registry 包装为官方 MCP Go SDK 的 Server，
// 让 tools/list、tools/call、initialize 等标准 MCP 协议由官方 SDK 直接处理。
// 设计要点：registry 是内部唯一调用入口，SDKServer 只负责把 ToolSpec 适配为 SDK 的
// Tool/Annotations，并把 handler 收到的 SDK 请求桥接到 registry.Call。
type SDKServer struct {
	// server 官方 MCP Go SDK 的 Server 实例，承担 initialize / list / call 等协议实现。
	server *mcpsdk.Server
	// logger 业务日志；协议流量本身不进 logger，避免污染 MCP 协议流。
	logger *slog.Logger
}

// NewSDKServer 创建 SDKServer 并把 Registry 中已注册的全部工具以 ToolSpec 形式注册到 SDK。
// 入参：
//   - registry：内部工具注册中心，持有所有已注册的 ToolSpec 与 Handler。
//   - version：实现版本号，会写入 MCP initialize 响应的 Implementation.Version。
//   - logger：业务日志输出；为 nil 时仅做协议处理。
//
// 返回：可立即通过 RunStdio 启动的 SDKServer。
func NewSDKServer(registry *Registry, version string, logger *slog.Logger) *SDKServer {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "theone", Version: version}, nil)
	s := &SDKServer{server: server, logger: logger}
	for _, spec := range registry.Tools() {
		s.addRegistryTool(registry, spec)
	}
	return s
}

// RunStdio 启动 stdio MCP 传输并阻塞直到客户端断开或 ctx 取消。
// 入参：ctx 用于控制服务生命周期，ctx 取消后会立即返回。
// 返回：客户端正常关闭（EOF + server is closing）时被识别为非错误，直接返回 nil；
//
//	其它错误原样返回给上层。
func (s *SDKServer) RunStdio(ctx context.Context) error {
	err := s.server.Run(ctx, &mcpsdk.StdioTransport{})
	if isNormalStdioDisconnect(err) {
		if s.logger != nil {
			s.logger.Info("mcp stdio client disconnected")
		}
		return nil
	}
	return err
}

// Connect 把 SDKServer 绑定到任意官方 SDK 支持的 Transport，返回可主动发送消息的 ServerSession。
// 主要用于 SDK Server 自定义传输（例如 HTTP/Streamable）以及测试场景。
func (s *SDKServer) Connect(ctx context.Context, transport mcpsdk.Transport) (*mcpsdk.ServerSession, error) {
	return s.server.Connect(ctx, transport, nil)
}

// addRegistryTool 把单个内部 ToolSpec 适配为官方 MCP SDK 的 Tool 并注册到 SDKServer。
// 主要工作：
//  1. 通过 MCPToolName 把内部 canonical 名转成对外暴露的 mcp_name（处理点号 -> 下划线）。
//  2. 填充 Tool 的 name/title/description/InputSchema/Annotations/OutputSchema。
//  3. 构造 SDK 处理函数：把 SDK 的 CallToolRequest 桥接到 registry.Call，并把结果/错误转成
//     SDK 的 CallToolResult（成功走 toolSuccessResult，失败走 toolErrorResult 并标记 IsError）。
//  4. 打点收到/完成/注册日志，方便线上追踪慢调用和工具装配情况。
func (s *SDKServer) addRegistryTool(registry *Registry, spec ToolSpec) {
	exposedName := MCPToolName(spec.Name)
	tool := &mcpsdk.Tool{
		Name:        exposedName,
		Title:       spec.Title,
		Description: spec.Description,
		InputSchema: spec.InputSchema,
		Annotations: &mcpsdk.ToolAnnotations{
			Title:           firstNonEmpty(spec.Title, spec.Name),
			ReadOnlyHint:    spec.ReadOnlyHint,
			DestructiveHint: spec.DestructiveHint,
			IdempotentHint:  spec.IdempotentHint,
			OpenWorldHint:   spec.OpenWorldHint,
		},
	}
	if len(spec.OutputSchema) > 0 {
		tool.OutputSchema = spec.OutputSchema
	}
	s.server.AddTool(tool, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		startedAt := time.Now()
		if s.logger != nil {
			s.logger.Info("mcp sdk tool call received",
				"tool", spec.Name,
				"mcp_name", exposedName,
			)
		}
		// SDK 在客户端不传 arguments 时不会构造空对象，统一补一个空 JSON 对象，
		// 避免下游 handler 对 nil RawMessage 走非预期分支。
		args := req.Params.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		result, toolErr := registry.Call(ctx, spec.Name, args)
		if s.logger != nil {
			s.logger.Info("mcp sdk tool call completed",
				"tool", spec.Name,
				"mcp_name", exposedName,
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"ok", toolErr == nil,
			)
		}
		if toolErr != nil {
			return toolErrorResult(toolErr), nil
		}
		return toolSuccessResult(result), nil
	})
	if s.logger != nil {
		s.logger.Info("mcp sdk tool registered", "tool", spec.Name, "mcp_name", exposedName)
	}
}

// toolSuccessResult 把 handler 的返回值包装为 SDK 的成功响应。
// 同时填充 text content 和 StructuredContent 两个字段，保证不支持结构化内容的老 Host
// 也能从 text 中解析 JSON；nil 结果统一回填 {"ok": true}，让 Host 始终拿到非空响应。
func toolSuccessResult(result any) *mcpsdk.CallToolResult {
	if result == nil {
		result = map[string]any{"ok": true}
	}
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: marshalToolText(result)}},
		StructuredContent: ensureJSONObject(result),
	}
}

// toolErrorResult 把内部 Error 转换为 SDK 的错误响应。
// 标记 IsError=true 让 Host 端按工具失败路径处理；StructuredContent 直接以 Error 结构体
// 作为对象返回，方便 Host 做结构化错误处理。
func toolErrorResult(toolErr *Error) *mcpsdk.CallToolResult {
	payload := map[string]any{"error": toolErr}
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("%s: %s", toolErr.ErrorCode, toolErr.Message)}},
		StructuredContent: payload,
		IsError:           true,
	}
}

// marshalToolText 将任意值序列化为 JSON 字符串作为 text content。
// 序列化失败时降级为 fmt.Sprintf("%v", value)，保证 text 字段始终可读。
func marshalToolText(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
}

// ensureJSONObject 保证 StructuredContent 一定是 JSON object。
// SDK 规范要求 StructuredContent 必须是对象，所以非对象值统一包装成 {"value": ...}。
func ensureJSONObject(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"value": fmt.Sprintf("%v", value)}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{"value": string(raw)}
	}
	if obj, ok := decoded.(map[string]any); ok {
		return obj
	}
	return map[string]any{"value": decoded}
}

// firstNonEmpty 返回第一个非空字符串；都为空时返回空串。
// 用于在 spec.Title 与 spec.Name 之间选择 ToolAnnotations.Title。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// isNormalStdioDisconnect 识别 stdio 模式下客户端正常关闭（EOF）造成的错误。
// RunStdio 用它把这种错误降级为成功返回，避免 stderr 上刷出无害的 disconnect 堆栈。
func isNormalStdioDisconnect(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "server is closing") && strings.Contains(message, "EOF")
}
