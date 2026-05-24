//go:build sqlite_fts5

package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retrieval"
)

func TestAppMemorySearchUsesP4RetrievalOrchestrator(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawRemember, toolErr := app.CallTool(ctx, "memory.remember", memory.RememberRequest{
		Content:     "P4-C1 阶段需要让 memory.search 走 Retrieval Orchestrator，并写入 retrieval trace 和 access log。",
		Title:       "P4-C1 检索编排接入",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		SourceType:  "manual_review",
		Keywords:    []string{"P4-C1", "Retrieval", "trace", "access log"},
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "P4-C1 验证 memory.search 已接入检索编排和访问日志。",
		},
	})
	if toolErr != nil {
		t.Fatalf("memory.remember error = %v", toolErr)
	}
	if rememberResp, ok := rawRemember.(memory.RememberResponse); !ok || rememberResp.MemoryID == "" {
		t.Fatalf("remember response = %#v, want memory id", rawRemember)
	}

	rawSearch, toolErr := app.CallTool(ctx, "memory.search", memory.SearchRequest{
		Query:       "Retrieval trace access log",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       5,
	})
	if toolErr != nil {
		t.Fatalf("memory.search error = %v", toolErr)
	}
	searchResp, ok := rawSearch.(memory.SearchResponse)
	if !ok {
		t.Fatalf("search response = %#v, want memory.SearchResponse", rawSearch)
	}
	if len(searchResp.Results) != 1 || searchResp.Results[0].ScoreBreakdown == nil {
		t.Fatalf("search response missing P4 score breakdown: %#v", searchResp)
	}
	if searchResp.Diagnostics.RetrievalTraceID == "" || searchResp.Diagnostics.RetrievalMode != string(retrieval.ModeFTSMetadata) {
		t.Fatalf("search diagnostics missing P4 fields: %+v", searchResp.Diagnostics)
	}

	traces, err := app.store.ListRetrievalTraces(ctx, retrieval.TraceQuery{WorkspaceID: "ws", Limit: 5})
	if err != nil {
		t.Fatalf("ListRetrievalTraces() error = %v", err)
	}
	if len(traces) != 1 || traces[0].ID != searchResp.Diagnostics.RetrievalTraceID || traces[0].CandidateCount != 1 {
		t.Fatalf("traces = %+v, want search trace with one candidate", traces)
	}
	accessLogs, err := app.store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{
		RetrievalTraceID: searchResp.Diagnostics.RetrievalTraceID,
		EventType:        "retrieved",
		Limit:            5,
	})
	if err != nil {
		t.Fatalf("ListMemoryAccessLogs() error = %v", err)
	}
	if len(accessLogs) != 1 || accessLogs[0].EventType != "retrieved" || accessLogs[0].ScoreBreakdown.Final == 0 {
		t.Fatalf("access logs = %+v, want retrieved log with score breakdown", accessLogs)
	}
}
