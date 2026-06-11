package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegistryCallLogsStartAndCompletion(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	registry := NewRegistry(logger)
	registry.Register("memory.health", func(ctx context.Context, params json.RawMessage) (any, *Error) {
		return map[string]any{"ok": true}, nil
	})

	result, toolErr := registry.Call(context.Background(), "memory.health", map[string]any{})
	if toolErr != nil {
		t.Fatalf("Call() toolErr = %v", toolErr)
	}
	if result == nil {
		t.Fatal("Call() result = nil, want payload")
	}
	logs := buf.String()
	for _, want := range []string{"mcp tool call started", "mcp tool called", "memory.health", `"ok":true`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs = %s, want %q", logs, want)
		}
	}
}

func TestRegistryCallRecoversPanicAndLogsCompletion(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	registry := NewRegistry(logger)
	registry.Register("memory.panic", func(ctx context.Context, params json.RawMessage) (any, *Error) {
		panic("boom")
	})

	result, toolErr := registry.Call(context.Background(), "memory.panic", map[string]any{})
	if result != nil {
		t.Fatalf("Call() result = %#v, want nil after panic", result)
	}
	if toolErr == nil || toolErr.ErrorCode != "INTERNAL_ERROR" {
		t.Fatalf("Call() toolErr = %+v, want INTERNAL_ERROR", toolErr)
	}
	logs := buf.String()
	for _, want := range []string{"mcp tool call started", "mcp tool called", "memory.panic", `"ok":false`, "panic"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs = %s, want %q", logs, want)
		}
	}
}

func TestSDKServerLogsToolCallBoundary(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	registry := NewRegistry(logger)
	registry.RegisterTool(ToolSpec{
		Name:        "memory.health",
		Title:       "Health",
		InputSchema: RawObjectSchema(),
		Handler: func(ctx context.Context, params json.RawMessage) (any, *Error) {
			return map[string]any{"ok": true}, nil
		},
	})
	server := NewSDKServer(registry, "test", logger)
	t1, t2 := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), t1)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	defer serverSession.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      MCPToolName("memory.health"),
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned error result: %+v", result)
	}
	logs := buf.String()
	for _, want := range []string{"mcp sdk tool call received", "mcp sdk tool call completed", "memory.health", MCPToolName("memory.health")} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs = %s, want %q", logs, want)
		}
	}
}
