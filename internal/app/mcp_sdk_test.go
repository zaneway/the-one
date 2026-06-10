package app

import (
	"context"
	"encoding/json"
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

func TestRegistryToolSpecsExposeRequiredIdentifiers(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	requiredByTool := map[string][]string{
		"memory.jobs.get":        {"job_id"},
		"memory.candidates.get":  {"candidate_id"},
		"memory.capture.quality": {"session_id"},
		"memory.mvp.report":      {"run_id"},
	}
	specs := make(map[string]mcp.ToolSpec, len(app.registry.Tools()))
	for _, spec := range app.registry.Tools() {
		specs[spec.Name] = spec
	}
	for toolName, wantFields := range requiredByTool {
		spec, ok := specs[toolName]
		if !ok {
			t.Fatalf("tool %s not registered", toolName)
		}
		required := schemaRequiredSet(t, spec.InputSchema)
		for _, field := range wantFields {
			if !required[field] {
				t.Fatalf("tool %s required = %+v, want %s", toolName, required, field)
			}
		}
	}
}

func TestRegistryMemoryObserveExposesRawPayloadFields(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	var observe mcp.ToolSpec
	for _, spec := range app.registry.Tools() {
		if spec.Name == "memory.observe" {
			observe = spec
			break
		}
	}
	if observe.Name == "" {
		t.Fatal("memory.observe not registered")
	}
	properties := schemaPropertiesSet(t, observe.InputSchema)
	for _, field := range []string{"raw_payload_json", "payload_schema", "raw_payload_hash", "redaction_state", "redaction_policy", "truncation"} {
		if !properties[field] {
			t.Fatalf("memory.observe properties = %+v, want %s", properties, field)
		}
	}
}

func schemaRequiredSet(t *testing.T, raw json.RawMessage) map[string]bool {
	t.Helper()
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	out := make(map[string]bool, len(schema.Required))
	for _, field := range schema.Required {
		out[field] = true
	}
	return out
}

func schemaPropertiesSet(t *testing.T, raw json.RawMessage) map[string]bool {
	t.Helper()
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	out := make(map[string]bool, len(schema.Properties))
	for field := range schema.Properties {
		out[field] = true
	}
	return out
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
