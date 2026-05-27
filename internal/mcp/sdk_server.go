package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SDKServer struct {
	server *mcpsdk.Server
	logger *slog.Logger
}

func NewSDKServer(registry *Registry, version string, logger *slog.Logger) *SDKServer {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "theone", Version: version}, nil)
	s := &SDKServer{server: server, logger: logger}
	for _, spec := range registry.Tools() {
		s.addRegistryTool(registry, spec)
	}
	return s
}

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

func (s *SDKServer) Connect(ctx context.Context, transport mcpsdk.Transport) (*mcpsdk.ServerSession, error) {
	return s.server.Connect(ctx, transport, nil)
}

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
		args := req.Params.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		result, toolErr := registry.Call(ctx, spec.Name, args)
		if toolErr != nil {
			return toolErrorResult(toolErr), nil
		}
		return toolSuccessResult(result), nil
	})
	if s.logger != nil {
		s.logger.Info("mcp sdk tool registered", "tool", spec.Name, "mcp_name", exposedName)
	}
}

func toolSuccessResult(result any) *mcpsdk.CallToolResult {
	if result == nil {
		result = map[string]any{"ok": true}
	}
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: marshalToolText(result)}},
		StructuredContent: ensureJSONObject(result),
	}
}

func toolErrorResult(toolErr *Error) *mcpsdk.CallToolResult {
	payload := map[string]any{"error": toolErr}
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("%s: %s", toolErr.ErrorCode, toolErr.Message)}},
		StructuredContent: payload,
		IsError:           true,
	}
}

func marshalToolText(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
}

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isNormalStdioDisconnect(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "server is closing") && strings.Contains(message, "EOF")
}
