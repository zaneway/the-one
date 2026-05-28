//go:build sqlite_fts5

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/docindex"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retrieval"
)

func TestAppP4RetrievalGoldenSet(t *testing.T) {
	ctx := context.Background()
	app := newP4IntegrationApp(t, ctx)
	defer app.Close()

	decisionID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "项目认证链路暂不引入 Kafka，因为身份校验必须在请求内同步完成，避免一致性和排障复杂度。",
		Title:       "认证链路不引入 Kafka",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_retrieval_e2",
		ProjectID:   "project_a",
		SourceType:  "manual_review",
		Keywords:    []string{"Kafka", "认证", "同步校验"},
	})
	failureID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "历史失败：token expiry boundary error 的根因是过期时间边界使用了本地时间。",
		Title:       "token expiry boundary error",
		MemoryType:  memory.TypeFailure,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_retrieval_e2",
		ProjectID:   "project_a",
		SourceType:  "manual_review",
		Keywords:    []string{"token", "expiry", "boundary", "error"},
	})
	otherProjectID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "另一个项目使用 Kafka 处理订单异步事件。",
		Title:       "其他项目 Kafka 决策",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_retrieval_e2",
		ProjectID:   "project_b",
		SourceType:  "manual_review",
		Keywords:    []string{"Kafka", "订单"},
	})
	oldID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "旧结论：retrieval retrieval 只需要顺序裁剪 context。",
		Title:       "旧 context 结论",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_retrieval_e2",
		ProjectID:   "project_a",
		SourceType:  "manual_review",
		Keywords:    []string{"retrieval", "retrieval", "context", "顺序裁剪"},
	})
	newID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "新结论：retrieval retrieval context 必须使用多 bucket budget builder，替代顺序裁剪。",
		Title:       "新 context budget 结论",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_retrieval_e2",
		ProjectID:   "project_a",
		SourceType:  "manual_review",
		Keywords:    []string{"retrieval", "retrieval", "context", "bucket", "budget"},
	})
	if err := app.store.WriteMemoryRelation(ctx, memory.MemoryRelation{
		ID:           "rel_new_supersedes_old",
		SourceID:     newID,
		TargetID:     oldID,
		RelationType: retrieval.RelationTypeSupersedes,
		Weight:       1,
	}); err != nil {
		t.Fatalf("WriteMemoryRelation() error = %v", err)
	}

	cases := []struct {
		name      string
		query     string
		wantIDs   []string
		forbidIDs []string
	}{
		{name: "decision", query: "为什么认证链路没有使用 Kafka？", wantIDs: []string{decisionID}, forbidIDs: []string{otherProjectID}},
		{name: "failure", query: "token expiry boundary error 为什么又出现？", wantIDs: []string{failureID}},
		{name: "supersedes", query: "retrieval retrieval context 顺序裁剪还是 bucket budget？", wantIDs: []string{newID}, forbidIDs: []string{oldID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, toolErr := app.CallTool(ctx, "memory.search", memory.SearchRequest{
				Query:       tc.query,
				WorkspaceID: "ws_retrieval_e2",
				ProjectID:   "project_a",
				Scope:       []string{memory.ScopeProjectLocal},
				Limit:       10,
			})
			if toolErr != nil {
				t.Fatalf("memory.search error = %v", toolErr)
			}
			resp := raw.(memory.SearchResponse)
			for _, wantID := range tc.wantIDs {
				if !searchResultsContain(resp.Results, wantID) {
					t.Fatalf("case %s results = %+v, want %s", tc.name, resultIDs(resp.Results), wantID)
				}
			}
			for _, forbidID := range tc.forbidIDs {
				if searchResultsContain(resp.Results, forbidID) {
					t.Fatalf("case %s results = %+v, must not contain %s", tc.name, resultIDs(resp.Results), forbidID)
				}
			}
			if resp.Diagnostics.RetrievalTraceID == "" || !resp.Diagnostics.UsedFTS || resp.Diagnostics.UsedVector {
				t.Fatalf("case %s diagnostics = %+v, want trace + FTS vector-disabled path", tc.name, resp.Diagnostics)
			}
			if time.Duration(resp.Diagnostics.LatencyMS)*time.Millisecond > 100*time.Millisecond {
				t.Fatalf("case %s latency = %dms, want <=100ms", tc.name, resp.Diagnostics.LatencyMS)
			}
		})
	}
}

func TestAppP4ContextIntegratesCodeDocBudgetAndDiagnostics(t *testing.T) {
	ctx := context.Background()
	app := newP4IntegrationApp(t, ctx)
	defer app.Close()

	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "service.go"), []byte(`package demo

func ValidateToken(token string) bool {
	return token != ""
}
`), 0o644); err != nil {
		t.Fatalf("write service.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "design.md"), []byte("# Design\n\n## Scope\nnew changed section\n"), 0o644); err != nil {
		t.Fatalf("write design.md: %v", err)
	}
	codeMemoryID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "ValidateToken 位于 service.go，用于认证链路 token 校验。",
		Title:       "ValidateToken code ref",
		MemoryType:  memory.TypeProcedure,
		Scope:       memory.ScopeRepoLocal,
		WorkspaceID: "ws_retrieval_e2_ctx",
		RepoID:      repoRoot,
		SourceType:  "manual_review",
		Keywords:    []string{"ValidateToken", "service.go", "token"},
	})
	if _, err := app.store.WriteCodeRef(ctx, memory.CodeRef{
		ID:            "cr_validate_token_e2",
		MemoryID:      codeMemoryID,
		RepoID:        repoRoot,
		FilePath:      "service.go",
		Symbol:        "ValidateToken",
		ResolveStatus: memory.CodeRefStatusUnresolved,
	}); err != nil {
		t.Fatalf("WriteCodeRef() error = %v", err)
	}
	checkpointID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "上一轮架构评审已确认 design.md 的基础边界，本轮重点看变化章节。",
		Title:       "design.md checkpoint",
		MemoryType:  memory.TypeReviewCheckpoint,
		Scope:       memory.ScopeRepoLocal,
		WorkspaceID: "ws_retrieval_e2_ctx",
		RepoID:      repoRoot,
		SourceType:  "manual_review",
		Keywords:    []string{"架构评审", "design.md", "checkpoint"},
		ReviewCheckpoint: &memory.ReviewCheckpointInput{
			CheckpointType: "implementation_design_review",
			ReviewIntent:   []string{"logic_consistency"},
			TargetDocs:     []map[string]any{{"path": "design.md", "content_hash": "sha256:old"}},
			Conclusion:     "baseline_frozen",
			IgnoredItems:   []string{"已确认不做 vector 强依赖"},
		},
	})
	if _, err := app.store.WriteDocSnapshot(ctx, docindex.DocumentSnapshot{
		ID:          "doc_e2_previous",
		WorkspaceID: "ws_retrieval_e2_ctx",
		RepoID:      repoRoot,
		Path:        "design.md",
		ContentHash: "sha256:previous",
		CreatedAt:   time.Now().Add(-time.Minute),
		Sections: []docindex.DocumentSection{
			{SectionID: "design", HeadingPath: []string{"Design"}, StartLine: 1, EndLine: 2, ContentHash: "sha256:design-old", Summary: "Design"},
			{SectionID: "design/scope", HeadingPath: []string{"Design", "Scope"}, StartLine: 3, EndLine: 4, ContentHash: "sha256:scope-old", Summary: "Design > Scope"},
		},
	}); err != nil {
		t.Fatalf("WriteDocSnapshot(previous) error = %v", err)
	}

	raw, toolErr := app.CallTool(ctx, "memory.context", memory.ContextRequest{
		Task:            "架构评审 design.md 并检查 service.go ValidateToken 的代码引用",
		WorkspaceID:     "ws_retrieval_e2_ctx",
		RepoID:          repoRoot,
		TokenBudget:     600,
		IncludeCodeRefs: true,
	})
	if toolErr != nil {
		t.Fatalf("memory.context error = %v", toolErr)
	}
	resp := raw.(memory.ContextResponse)
	if resp.ContextPack.ReviewStrategy == nil || resp.ContextPack.ReviewStrategy.Mode != "changed_sections" {
		t.Fatalf("review strategy = %+v, want changed_sections", resp.ContextPack.ReviewStrategy)
	}
	if resp.ContextPack.ReviewStrategy.CheckpointID != checkpointID {
		t.Fatalf("checkpoint id = %q, want %q", resp.ContextPack.ReviewStrategy.CheckpointID, checkpointID)
	}
	if len(resp.ContextPack.CodeRefs) == 0 || resp.ContextPack.CodeRefs[0].ResolveStatus != memory.CodeRefStatusResolved {
		t.Fatalf("code refs = %+v, want resolved code ref in context", resp.ContextPack.CodeRefs)
	}
	if resp.Diagnostics == nil || !resp.Diagnostics.UsedDocIndex || resp.Diagnostics.RetrievalMode != string(retrieval.ModeCheckpointAware) {
		t.Fatalf("context diagnostics = %+v, want checkpoint-aware doc diagnostics", resp.Diagnostics)
	}
	if resp.Diagnostics.BudgetAllocation["bucket_review_checkpoint_items"] == 0 ||
		resp.Diagnostics.BudgetAllocation["bucket_code_refs_items"] == 0 ||
		resp.Diagnostics.BudgetAllocation["bucket_doc_changed_sections_items"] == 0 {
		t.Fatalf("budget allocation = %+v, want review/code/doc buckets used", resp.Diagnostics.BudgetAllocation)
	}
	logs, err := app.store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{RetrievalTraceID: resp.RetrievalTraceID, EventType: "injected"})
	if err != nil {
		t.Fatalf("ListMemoryAccessLogs(injected) error = %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("injected access logs = 0, want context injection logs")
	}
}

func newP4IntegrationApp(t *testing.T, ctx context.Context) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return app
}

func rememberP4IntegrationMemory(t *testing.T, ctx context.Context, app *App, req memory.RememberRequest) string {
	t.Helper()
	raw, toolErr := app.CallTool(ctx, "memory.remember", req)
	if toolErr != nil {
		t.Fatalf("memory.remember(%s) error = %v", req.Title, toolErr)
	}
	resp := raw.(memory.RememberResponse)
	if resp.MemoryID == "" {
		t.Fatalf("remember response = %+v, want memory id", resp)
	}
	return resp.MemoryID
}

func searchResultsContain(results []memory.SearchResult, memoryID string) bool {
	for _, result := range results {
		if result.MemoryID == memoryID {
			return true
		}
	}
	return false
}

func resultIDs(results []memory.SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.MemoryID)
	}
	return ids
}
