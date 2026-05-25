//go:build sqlite_fts5

package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/automation"
	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retrieval"
)

// TestAppCrossPhaseObserveToSearchAndContext 验证 P2 observe 事件可经 P3 worker 写入 P1，并被 P4 search/context 召回。
func TestAppCrossPhaseObserveToSearchAndContext(t *testing.T) {
	ctx := context.Background()
	app := newP4IntegrationApp(t, ctx)
	defer app.Close()
	if !app.store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; cross-phase retrieval test needs searchable automated memory")
	}

	rawObserved, toolErr := app.CallTool(ctx, "memory.observe", capture.ObserveRequest{
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelMCPTool,
		WorkspaceID:    "ws_cross_phase",
		ProjectID:      "project_chain",
		AgentType:      "codex",
		Actor:          capture.ActorUser,
		ContentSummary: "项目要求 P4 联动验收必须覆盖 memory.observe、automation worker、admission、memory.search 和 memory.context 的端到端链路。",
		Keywords:       []string{"P4", "联动", "memory.observe", "worker", "admission", "memory.search", "memory.context"},
		ContentHash:    "sha256:cross-phase-observe-search-context",
	})
	if toolErr != nil {
		t.Fatalf("memory.observe error = %v", toolErr)
	}
	observed := rawObserved.(capture.ObserveResponse)
	if !observed.Accepted || observed.RawEventID == "" {
		t.Fatalf("observe response = %+v, want accepted raw_event", observed)
	}

	runAppWorkerUntilIdle(t, ctx, app)
	candidates, err := app.store.ListCandidates(ctx, automation.ListCandidatesRequest{
		RawEventID: observed.RawEventID,
		Status:     automation.CandidateStatusAdmitted,
	})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID == "" {
		t.Fatalf("admitted candidates = %+v, want one automated memory", candidates)
	}
	written, err := app.store.Get(ctx, candidates[0].ResultingMemoryID)
	if err != nil {
		t.Fatalf("Get(resulting memory) error = %v", err)
	}
	if written.MemoryType != memory.TypeRequirement || written.Scope != memory.ScopeProjectLocal {
		t.Fatalf("written memory = %+v, want project requirement from P3 admission", written)
	}

	rawSearch, toolErr := app.CallTool(ctx, "memory.search", memory.SearchRequest{
		Query:           "P4 联动 observe worker admission context",
		WorkspaceID:     "ws_cross_phase",
		ProjectID:       "project_chain",
		Scope:           []string{memory.ScopeProjectLocal},
		MemoryTypes:     []string{memory.TypeRequirement},
		IncludeEvidence: true,
		Limit:           5,
	})
	if toolErr != nil {
		t.Fatalf("memory.search error = %v", toolErr)
	}
	searchResp := rawSearch.(memory.SearchResponse)
	if !searchResultsContain(searchResp.Results, written.ID) || searchResp.Diagnostics.RetrievalTraceID == "" {
		t.Fatalf("search response = %+v, want P4 trace and automated memory %s", searchResp, written.ID)
	}
	if searchResp.Results[0].ScoreBreakdown == nil || len(searchResp.Results[0].EvidenceRefs) == 0 {
		t.Fatalf("search result = %+v, want P4 score breakdown and P3 evidence refs", searchResp.Results[0])
	}
	retrievedLogs, err := app.store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{
		RetrievalTraceID: searchResp.Diagnostics.RetrievalTraceID,
		EventType:        "retrieved",
		Limit:            5,
	})
	if err != nil {
		t.Fatalf("ListMemoryAccessLogs(retrieved) error = %v", err)
	}
	if len(retrievedLogs) == 0 {
		t.Fatalf("retrieved access logs = %+v, want P4 retrieval log", retrievedLogs)
	}

	rawContext, toolErr := app.CallTool(ctx, "memory.context", memory.ContextRequest{
		Task:                   "执行 P4 联动验收，检查 observe worker admission search context 是否端到端贯通",
		WorkspaceID:            "ws_cross_phase",
		ProjectID:              "project_chain",
		TokenBudget:            600,
		IncludeEvidenceSummary: true,
	})
	if toolErr != nil {
		t.Fatalf("memory.context error = %v", toolErr)
	}
	contextResp := rawContext.(memory.ContextResponse)
	if !stringSliceContains(contextResp.UsedMemoryIDs, written.ID) || contextResp.Diagnostics == nil {
		t.Fatalf("context response = %+v, want automated memory %s and P4 diagnostics", contextResp, written.ID)
	}
	injectedLogs, err := app.store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{
		RetrievalTraceID: contextResp.RetrievalTraceID,
		EventType:        "injected",
		Limit:            5,
	})
	if err != nil {
		t.Fatalf("ListMemoryAccessLogs(injected) error = %v", err)
	}
	if len(injectedLogs) == 0 {
		t.Fatalf("injected access logs = %+v, want P4 context injection log", injectedLogs)
	}
}

// TestAppCrossPhaseUserCorrectionOverwritesAndSearches 验证用户纠正事件会原地覆盖旧记忆并同步 P4 检索索引。
func TestAppCrossPhaseUserCorrectionOverwritesAndSearches(t *testing.T) {
	ctx := context.Background()
	app := newP4IntegrationApp(t, ctx)
	defer app.Close()
	if !app.store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; correction overwrite test needs FTS sync")
	}

	oldID := rememberP4IntegrationMemory(t, ctx, app, memory.RememberRequest{
		Content:     "当前链路数据库使用 MySQL。",
		Title:       "链路数据库事实",
		MemoryType:  memory.TypeProjectFact,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_cross_phase_correction",
		ProjectID:   "project_chain",
		SourceType:  "manual_review",
		Keywords:    []string{"数据库", "MySQL"},
	})
	rawObserved, toolErr := app.CallTool(ctx, "memory.observe", capture.ObserveRequest{
		EventType:      capture.EventUserCorrection,
		SourceChannel:  capture.SourceChannelMCPTool,
		WorkspaceID:    "ws_cross_phase_correction",
		ProjectID:      "project_chain",
		AgentType:      "codex",
		Actor:          capture.ActorUser,
		ContentSummary: "纠正：当前链路数据库使用 PostgreSQL。",
		Keywords:       []string{"数据库", "PostgreSQL", "纠正"},
		SourceRefs: []capture.SourceRef{{
			"target_memory_id":    oldID,
			"target_memory_type":  memory.TypeProjectFact,
			"target_memory_scope": memory.ScopeProjectLocal,
		}},
		ContentHash: "sha256:cross-phase-correction-postgresql",
	})
	if toolErr != nil {
		t.Fatalf("memory.observe correction error = %v", toolErr)
	}
	observed := rawObserved.(capture.ObserveResponse)

	runAppWorkerUntilIdle(t, ctx, app)
	candidates, err := app.store.ListCandidates(ctx, automation.ListCandidatesRequest{
		RawEventID: observed.RawEventID,
		Status:     automation.CandidateStatusAdmitted,
	})
	if err != nil {
		t.Fatalf("ListCandidates(correction) error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID != oldID {
		t.Fatalf("correction candidates = %+v, want overwrite target %s", candidates, oldID)
	}
	updated, err := app.store.Get(ctx, oldID)
	if err != nil {
		t.Fatalf("Get(updated memory) error = %v", err)
	}
	if !strings.Contains(updated.Content, "PostgreSQL") || strings.Contains(updated.Content, "MySQL") || updated.Version != 2 {
		t.Fatalf("updated memory = %+v, want PostgreSQL correction with version increment", updated)
	}

	rawSearch, toolErr := app.CallTool(ctx, "memory.search", memory.SearchRequest{
		Query:       "PostgreSQL",
		WorkspaceID: "ws_cross_phase_correction",
		ProjectID:   "project_chain",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeProjectFact},
		Limit:       5,
	})
	if toolErr != nil {
		t.Fatalf("memory.search PostgreSQL error = %v", toolErr)
	}
	searchResp := rawSearch.(memory.SearchResponse)
	if !searchResultsContain(searchResp.Results, oldID) || searchResp.Diagnostics.RetrievalTraceID == "" {
		t.Fatalf("search response = %+v, want corrected memory %s through P4", searchResp, oldID)
	}

	rawOldSearch, toolErr := app.CallTool(ctx, "memory.search", memory.SearchRequest{
		Query:       "MySQL",
		WorkspaceID: "ws_cross_phase_correction",
		ProjectID:   "project_chain",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeProjectFact},
		Limit:       5,
	})
	if toolErr != nil {
		t.Fatalf("memory.search MySQL error = %v", toolErr)
	}
	oldSearchResp := rawOldSearch.(memory.SearchResponse)
	if searchResultsContain(oldSearchResp.Results, oldID) {
		t.Fatalf("old search response = %+v, must not retrieve overwritten MySQL content", oldSearchResp)
	}
}

// TestAppCrossPhaseReviewCheckpointObservedAndRetrievable 验证 P3 自动生成的 review checkpoint 可被 P4 检索。
func TestAppCrossPhaseReviewCheckpointObservedAndRetrievable(t *testing.T) {
	ctx := context.Background()
	app := newP4IntegrationApp(t, ctx)
	defer app.Close()
	if !app.store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; checkpoint retrieval test needs searchable automated memory")
	}

	rawObserved, toolErr := app.CallTool(ctx, "memory.observe", capture.ObserveRequest{
		EventType:      capture.EventTaskResult,
		SourceChannel:  capture.SourceChannelMCPTool,
		WorkspaceID:    "ws_cross_phase_checkpoint",
		ProjectID:      "project_chain",
		AgentType:      "codex",
		Actor:          capture.ActorAgent,
		ContentSummary: "P4 详细设计复查完成，已确认 P1、P2、P3、P4 联动验收需要保留 checkpoint。",
		Keywords:       []string{"P4", "详细设计", "复查", "checkpoint", "联动验收"},
		SourceRefs: []capture.SourceRef{{
			"checkpoint_type": "implementation_design_review",
			"review_intent":   []string{"cross_phase_linkage", "acceptance_regression"},
			"target_docs":     []map[string]any{{"path": "doc/The One 长期记忆系统 P4 详细设计.md", "content_hash": "sha256:p4-design"}},
			"target_hashes":   []map[string]any{{"path": "doc/The One 长期记忆系统 P4 详细设计.md", "content_hash": "sha256:p4-design"}},
			"conclusion":      "supplemented",
		}},
		ContentHash: "sha256:cross-phase-review-checkpoint",
	})
	if toolErr != nil {
		t.Fatalf("memory.observe checkpoint error = %v", toolErr)
	}
	observed := rawObserved.(capture.ObserveResponse)

	runAppWorkerUntilIdle(t, ctx, app)
	candidates, err := app.store.ListCandidates(ctx, automation.ListCandidatesRequest{
		RawEventID: observed.RawEventID,
		MemoryType: memory.TypeReviewCheckpoint,
		Status:     automation.CandidateStatusAdmitted,
	})
	if err != nil {
		t.Fatalf("ListCandidates(checkpoint) error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID == "" {
		t.Fatalf("checkpoint candidates = %+v, want admitted checkpoint memory", candidates)
	}
	checkpoint, found, err := app.store.GetReviewCheckpoint(ctx, candidates[0].ResultingMemoryID)
	if err != nil {
		t.Fatalf("GetReviewCheckpoint() error = %v", err)
	}
	if !found || checkpoint.Conclusion != "supplemented" || checkpoint.TargetHashesJSON == "" {
		t.Fatalf("checkpoint = %+v found=%v, want persisted target hashes", checkpoint, found)
	}

	rawSearch, toolErr := app.CallTool(ctx, "memory.search", memory.SearchRequest{
		Query:       "P4 详细设计 checkpoint 联动验收",
		WorkspaceID: "ws_cross_phase_checkpoint",
		ProjectID:   "project_chain",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeReviewCheckpoint},
		Limit:       5,
	})
	if toolErr != nil {
		t.Fatalf("memory.search checkpoint error = %v", toolErr)
	}
	searchResp := rawSearch.(memory.SearchResponse)
	if !searchResultsContain(searchResp.Results, candidates[0].ResultingMemoryID) || searchResp.Diagnostics.RetrievalTraceID == "" {
		t.Fatalf("checkpoint search response = %+v, want P4 retrieval of observed checkpoint", searchResp)
	}
}

func runAppWorkerUntilIdle(t *testing.T, ctx context.Context, app *App) {
	t.Helper()
	if app.worker == nil {
		t.Fatal("app worker is nil")
	}
	base := time.Now().UTC()
	for i := 0; i < 10; i++ {
		result, err := app.worker.RunOnce(ctx, base.Add(time.Duration(i+1)*time.Hour))
		if err != nil {
			t.Fatalf("RunOnce(%d) error = %v", i, err)
		}
		if result.Failed > 0 || result.Retried > 0 {
			jobs, listErr := app.store.ListJobs(ctx, automation.ListJobsRequest{})
			if listErr != nil {
				t.Fatalf("RunOnce(%d) result = %+v and ListJobs error = %v", i, result, listErr)
			}
			t.Fatalf("RunOnce(%d) result = %+v, jobs = %+v", i, result, jobs)
		}
		if result.Claimed == 0 {
			return
		}
	}
	jobs, err := app.store.ListJobs(ctx, automation.ListJobsRequest{})
	if err != nil {
		t.Fatalf("ListJobs() after worker loop error = %v", err)
	}
	t.Fatalf("worker did not become idle, jobs = %+v", jobs)
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
