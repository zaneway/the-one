package app

import (
	"context"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/mcp"
)

func TestSDKServerListsAllRegisteredTools(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	clientSession, serverSession := connectSDKTestSession(t, ctx, app)
	defer clientSession.Close()
	defer serverSession.Close()

	list, err := clientSession.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(list.Tools) != 28 {
		t.Fatalf("tool count = %d, want 28", len(list.Tools))
	}
	seen := make(map[string]*mcpsdk.Tool, len(list.Tools))
	for _, tool := range list.Tools {
		seen[tool.Name] = tool
		if tool.Description == "" {
			t.Fatalf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %s has nil input schema", tool.Name)
		}
	}
	for _, name := range expectedMCPTools() {
		if _, ok := seen[name]; !ok {
			t.Fatalf("tool %s not listed", name)
		}
	}
}

func TestSDKServerCallsHealthTool(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	clientSession, serverSession := connectSDKTestSession(t, ctx, app)
	defer clientSession.Close()
	defer serverSession.Close()

	result, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      mcp.MCPToolName("memory.health"),
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(memory_health) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(memory_health) returned tool error: %+v", result)
	}
	content, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent = %T, want map[string]any", result.StructuredContent)
	}
	if content["ok"] != true {
		t.Fatalf("health ok = %v, want true", content["ok"])
	}
}

func connectSDKTestSession(t *testing.T, ctx context.Context, app *App) (*mcpsdk.ClientSession, *mcpsdk.ServerSession) {
	t.Helper()
	t1, t2 := mcpsdk.NewInMemoryTransports()
	server := mcp.NewSDKServer(app.registry, app.version, app.logger)
	serverSession, err := server.Connect(ctx, t1)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "theone-test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, t2, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("client.Connect() error = %v", err)
	}
	return clientSession, serverSession
}

func expectedMCPTools() []string {
	canonical := []string{
		"memory.health",
		"memory.status",
		"memory.retrieval.traces",
		"memory.retrieval.access_logs",
		"memory.code_refs",
		"memory.docindex.snapshots",
		"memory.docindex.diff",
		"memory.remember",
		"memory.search",
		"memory.context",
		"memory.review",
		"memory.jobs.list",
		"memory.jobs.get",
		"memory.candidates.list",
		"memory.candidates.get",
		"memory.automation.status",
		"memory.jobs.reconcile",
		"memory.retention.run",
		"memory.mvp.run.start",
		"memory.mvp.task.record",
		"memory.mvp.capability.record",
		"memory.mvp.metrics.compute",
		"memory.mvp.report",
		"memory.observe",
		"memory.capture.sessions",
		"memory.capture.tasks",
		"memory.capture.events",
		"memory.capture.quality",
	}
	out := make([]string, len(canonical))
	for i, name := range canonical {
		out[i] = mcp.MCPToolName(name)
	}
	return out
}
