package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/diagnostics"
	"github.com/zaneway/theone/internal/docindex"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retrieval"
)

func TestAppRegistersP4DiagnosticsTools(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	now := time.Date(2026, 5, 25, 9, 30, 0, 0, time.UTC)
	trace, err := app.store.CreateRetrievalTrace(ctx, retrieval.TraceRecord{
		ID:             "rt_diag",
		SessionID:      "sess_diag",
		TaskID:         "task_diag",
		WorkspaceID:    "ws_diag",
		ProjectID:      "project_diag",
		RepoID:         "repo_diag",
		Query:          "retrieval diagnostics trace query summary",
		Intent:         retrieval.IntentArchitectureReview,
		Mode:           retrieval.ModeCheckpointAware,
		UsedFTS:        true,
		UsedRelation:   true,
		UsedCodeIndex:  true,
		UsedDocIndex:   true,
		CandidateCount: 3,
		InjectedCount:  2,
		LatencyMS:      12,
		Status:         retrieval.TraceCompleted,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("CreateRetrievalTrace() error = %v", err)
	}
	if _, err := app.store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:               "mal_diag",
		MemoryID:         "mem_diag",
		SessionID:        "sess_diag",
		TaskID:           "task_diag",
		RetrievalTraceID: trace.ID,
		EventType:        "injected",
		Query:            "retrieval diagnostics trace query summary",
		Rank:             1,
		Score:            0.91,
		ScoreBreakdown:   memory.ScoreBreakdown{Final: 0.91},
		InclusionReasons: []string{"task_match", "review_checkpoint"},
		UsedInContext:    true,
		CreatedAt:        now,
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog() error = %v", err)
	}
	if _, err := app.store.WriteCodeRef(ctx, memory.CodeRef{
		ID:            "cr_diag",
		MemoryID:      "mem_diag",
		RepoID:        "repo_diag",
		FilePath:      "internal/retrieval/context_builder.go",
		Symbol:        "buildContextPack",
		ContentHash:   "sha256:code",
		RefSummary:    "local_basic: resolved internal/retrieval/context_builder.go buildContextPack line=42",
		ResolveStatus: memory.CodeRefStatusResolved,
	}); err != nil {
		t.Fatalf("WriteCodeRef() error = %v", err)
	}
	baseSnapshot, err := app.store.WriteDocSnapshot(ctx, docindex.DocumentSnapshot{
		ID:          "doc_base",
		WorkspaceID: "ws_diag",
		ProjectID:   "project_diag",
		RepoID:      "repo_diag",
		Path:        "doc/p4.md",
		ContentHash: "sha256:base",
		CreatedAt:   now.Add(-time.Minute),
		Sections: []docindex.DocumentSection{
			{SectionID: "overview", HeadingPath: []string{"Overview"}, StartLine: 1, EndLine: 10, ContentHash: "sha256:overview-old", Summary: "Overview"},
			{SectionID: "removed", HeadingPath: []string{"Removed"}, StartLine: 11, EndLine: 20, ContentHash: "sha256:removed", Summary: "Removed"},
		},
	})
	if err != nil {
		t.Fatalf("WriteDocSnapshot(base) error = %v", err)
	}
	currentSnapshot, err := app.store.WriteDocSnapshot(ctx, docindex.DocumentSnapshot{
		ID:          "doc_current",
		WorkspaceID: "ws_diag",
		ProjectID:   "project_diag",
		RepoID:      "repo_diag",
		Path:        "doc/p4.md",
		ContentHash: "sha256:current",
		CreatedAt:   now,
		Sections: []docindex.DocumentSection{
			{SectionID: "overview", HeadingPath: []string{"Overview"}, StartLine: 1, EndLine: 12, ContentHash: "sha256:overview-new", Summary: "Overview"},
			{SectionID: "added", HeadingPath: []string{"Added"}, StartLine: 13, EndLine: 30, ContentHash: "sha256:added", Summary: "Added"},
		},
	})
	if err != nil {
		t.Fatalf("WriteDocSnapshot(current) error = %v", err)
	}

	rawTraces, toolErr := app.CallTool(ctx, "memory.retrieval.traces", diagnostics.RetrievalTracesRequest{WorkspaceID: "ws_diag", Limit: 200})
	if toolErr != nil {
		t.Fatalf("memory.retrieval.traces error = %v", toolErr)
	}
	traces := rawTraces.(diagnostics.RetrievalTracesResponse)
	if len(traces.Traces) != 1 || traces.Traces[0].TraceID != trace.ID || !traces.Traces[0].UsedDocIndex {
		t.Fatalf("traces response = %#v, want trace diagnostics", rawTraces)
	}
	if len(traces.Diagnostics) != 1 || traces.Diagnostics[0] != "limit_truncated" {
		t.Fatalf("trace diagnostics = %+v, want limit_truncated", traces.Diagnostics)
	}

	rawLogs, toolErr := app.CallTool(ctx, "memory.retrieval.access_logs", diagnostics.RetrievalAccessLogsRequest{RetrievalTraceID: trace.ID})
	if toolErr != nil {
		t.Fatalf("memory.retrieval.access_logs error = %v", toolErr)
	}
	logs := rawLogs.(diagnostics.RetrievalAccessLogsResponse)
	if len(logs.AccessLogs) != 1 || logs.AccessLogs[0].AccessLogID != "mal_diag" || logs.AccessLogs[0].ScoreBreakdown.Final == 0 {
		t.Fatalf("access logs response = %#v, want access log diagnostics", rawLogs)
	}

	rawRefs, toolErr := app.CallTool(ctx, "memory.code_refs", diagnostics.CodeRefsRequest{MemoryID: "mem_diag"})
	if toolErr != nil {
		t.Fatalf("memory.code_refs error = %v", toolErr)
	}
	refs := rawRefs.(diagnostics.CodeRefsResponse)
	if len(refs.CodeRefs) != 1 || refs.CodeRefs[0].ID != "cr_diag" || refs.CodeRefs[0].ResolveStatus != memory.CodeRefStatusResolved {
		t.Fatalf("code refs response = %#v, want code ref diagnostics", rawRefs)
	}

	rawSnapshots, toolErr := app.CallTool(ctx, "memory.docindex.snapshots", diagnostics.DocSnapshotsRequest{
		WorkspaceID:     "ws_diag",
		ProjectID:       "project_diag",
		RepoID:          "repo_diag",
		Path:            "doc/p4.md",
		IncludeSections: true,
	})
	if toolErr != nil {
		t.Fatalf("memory.docindex.snapshots error = %v", toolErr)
	}
	snapshots := rawSnapshots.(diagnostics.DocSnapshotsResponse)
	if len(snapshots.Snapshots) != 2 || snapshots.Snapshots[0].ID != currentSnapshot.ID || len(snapshots.Snapshots[0].Sections) != 2 {
		t.Fatalf("snapshots response = %#v, want current snapshot with sections", rawSnapshots)
	}

	rawDiff, toolErr := app.CallTool(ctx, "memory.docindex.diff", diagnostics.DocDiffRequest{
		WorkspaceID:    "ws_diag",
		ProjectID:      "project_diag",
		RepoID:         "repo_diag",
		Path:           "doc/p4.md",
		BaseSnapshotID: baseSnapshot.ID,
	})
	if toolErr != nil {
		t.Fatalf("memory.docindex.diff error = %v", toolErr)
	}
	diff := rawDiff.(diagnostics.DocDiffResponse)
	if diff.CurrentSnapshotID != currentSnapshot.ID || diff.BaseSnapshotID != baseSnapshot.ID || !diff.DocChanged || len(diff.ChangedSections) != 3 {
		t.Fatalf("diff response = %#v, want modified/added/removed sections", rawDiff)
	}
}

func TestAppStatusReportsP4Capabilities(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	raw, toolErr := app.CallTool(ctx, "memory.status", diagnostics.StatusRequest{IncludeConfig: true})
	if toolErr != nil {
		t.Fatalf("memory.status error = %v", toolErr)
	}
	status := raw.(diagnostics.StatusResponse)
	if !status.CodeIndex.Enabled || status.CodeIndex.Provider != "local_basic" || status.CodeIndex.Capabilities.CallGraph {
		t.Fatalf("code index status = %+v, want local_basic enabled without call graph", status.CodeIndex)
	}
	if status.Embedding.Provider != "none" || status.Embedding.QueryCacheSize != 256 ||
		!status.Embedding.OnlineQueryEmbeddingEnabled || !status.Embedding.MemoryEmbeddingEnabled {
		t.Fatalf("embedding status = %+v, want none with embedding switches enabled", status.Embedding)
	}
	if status.Vector.Backend != "none" || status.Vector.Available {
		t.Fatalf("vector status = %+v, want disabled vector index", status.Vector)
	}
	if status.Config["codeindex_provider"] != "local_basic" || status.Config["vector_index_backend"] != "none" {
		t.Fatalf("status config = %+v, want retrieval config summary", status.Config)
	}
}

func TestAppHealthChecksOpenAIProviderWhenConfigured(t *testing.T) {
	ctx := context.Background()
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		responseFormat, ok := request["response_format"].(map[string]any)
		if !ok || responseFormat["type"] != "json_object" {
			t.Fatalf("response_format = %#v, want json_object", request["response_format"])
		}
		messages, ok := request["messages"].([]any)
		if !ok || len(messages) < 2 {
			t.Fatalf("messages = %#v, want system and user messages", request["messages"])
		}
		userMessage, _ := messages[1].(map[string]any)
		userContent, _ := userMessage["content"].(string)
		if !strings.Contains(userContent, "health_check") {
			t.Fatalf("user content = %#v, want health check payload", userContent)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAIHealthResponseJSON(`{"ok":true}`)))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Processor.Provider = "openai"
	cfg.Processor.OpenAI.APIKey = "test-key"
	cfg.Processor.OpenAI.BaseURL = server.URL
	cfg.Processor.OpenAI.Model = "gpt-5-mini"
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	raw, toolErr := app.CallTool(ctx, "memory.health", nil)
	if toolErr != nil {
		t.Fatalf("memory.health error = %v", toolErr)
	}
	health := raw.(diagnostics.HealthResponse)
	if !health.OK || health.AI == nil || !health.AI.Supported || !health.AI.OK {
		t.Fatalf("health = %+v, want ok health with AI availability", health)
	}
	if health.AI.Provider != "openai" || health.AI.Model != "gpt-5-mini" {
		t.Fatalf("ai health = %+v, want openai model", health.AI)
	}
	if requestCount != 1 {
		t.Fatalf("requestCount = %d, want one provider health request", requestCount)
	}
}

func TestAppP4DiagnosticsRejectsUnboundedTraceQuery(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	_, toolErr := app.CallTool(ctx, "memory.retrieval.traces", diagnostics.RetrievalTracesRequest{})
	if toolErr == nil || toolErr.ErrorCode != "VALIDATION_FAILED" {
		t.Fatalf("toolErr = %+v, want validation failure for missing workspace_id", toolErr)
	}
}

func openAIHealthResponseJSON(outputText string) string {
	escaped, _ := json.Marshal(outputText)
	return `{
		"id": "resp_health",
		"object": "chat.completion",
		"created": 1781000000,
		"model": "gpt-5-mini",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": ` + string(escaped) + `
			},
			"finish_reason": "stop"
		}]
	}`
}
