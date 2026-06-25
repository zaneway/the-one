package retrieval

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/docindex"
	"github.com/zaneway/theone/internal/memory"
)

func TestMemoryOrchestratorSearchWritesTraceAndRetrievedLogs(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-decision",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "retrieval 需要接入检索 trace 和 access log",
			Score:      0.9,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
		{
			MemoryID:   "mem-procedure",
			MemoryType: memory.TypeProcedure,
			Scope:      memory.ScopeUserGlobal,
			Content:    "实现流程完成后需要运行 go test",
			Score:      0.6,
			Confidence: 0.7,
			State:      memory.StateStable,
			Tier:       memory.TierDurable,
		},
	}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	orchestrator := newTestOrchestrator(searcher, traceRepo, accessRepo)

	resp, err := orchestrator.Search(ctx, memory.SearchRequest{
		Query:       "retrieval trace access log",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		Scope:       []string{memory.ScopeProjectLocal, memory.ScopeUserGlobal},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if resp.RetrievalTraceID != "rt-test-1" || resp.Diagnostics.RetrievalTraceID != "rt-test-1" {
		t.Fatalf("trace id top=%q diagnostics=%q, want rt-test-1", resp.RetrievalTraceID, resp.Diagnostics.RetrievalTraceID)
	}
	if resp.Diagnostics.RetrievalMode != string(ModeFTSMetadata) || resp.Diagnostics.RetrievalIntent == "" || !resp.Diagnostics.UsedFTS {
		t.Fatalf("unexpected diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(resp.Results))
	}
	for _, result := range resp.Results {
		if result.ScoreBreakdown == nil {
			t.Fatalf("result %s missing score breakdown", result.MemoryID)
		}
		if result.Score <= 0 || len(result.WhyIncluded) == 0 {
			t.Fatalf("result %s missing retrieval score/reasons: %+v", result.MemoryID, result)
		}
	}
	if len(traceRepo.created) != 1 || len(traceRepo.updated) != 1 {
		t.Fatalf("trace writes created=%d updated=%d", len(traceRepo.created), len(traceRepo.updated))
	}
	updated := traceRepo.updated[0]
	if updated.Status != TraceCompleted || updated.CandidateCount != 2 || updated.InjectedCount != 0 {
		t.Fatalf("unexpected trace update: %+v", updated)
	}
	if len(accessRepo.records) != 2 {
		t.Fatalf("access logs len = %d, want 2", len(accessRepo.records))
	}
	for i, record := range accessRepo.records {
		if record.EventType != retrievedAccessEventType || record.UsedInContext {
			t.Fatalf("unexpected retrieved log %d: %+v", i, record)
		}
		if record.RetrievalTraceID != "rt-test-1" || record.Rank != i+1 || record.ScoreBreakdown.Final == 0 {
			t.Fatalf("unexpected retrieved log fields %d: %+v", i, record)
		}
	}
}

func TestMemoryOrchestratorUsesMemoryUpdatedAtForRecency(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{{
		MemoryID:   "mem-old",
		MemoryType: memory.TypeDecision,
		Scope:      memory.ScopeProjectLocal,
		Content:    "旧设计决策。",
		Score:      0.8,
		Confidence: 0.8,
		State:      memory.StateStable,
		Tier:       memory.TierLongTerm,
		UpdatedAt:  updatedAt,
	}}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	orchestrator := newTestOrchestrator(searcher, traceRepo, accessRepo)

	resp, err := orchestrator.Search(ctx, memory.SearchRequest{
		Query:       "旧设计决策",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		Scope:       []string{memory.ScopeProjectLocal},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].ScoreBreakdown == nil {
		t.Fatalf("results = %+v, want one result with score breakdown", resp.Results)
	}
	if resp.Results[0].ScoreBreakdown.Recency >= 1 {
		t.Fatalf("recency = %v, want real updated_at to avoid treating old memory as just updated", resp.Results[0].ScoreBreakdown.Recency)
	}
}

func TestMemoryOrchestratorSearchMergesVectorCandidates(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-fts",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "FTS 召回结果保留。",
			Score:      0.3,
			Confidence: 0.7,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
	}}
	vectorRepo := &fakeVectorRepo{results: []memory.SearchResult{
		{
			MemoryID:   "mem-vector",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "QKV 方案 C 通过 query embedding 做语义召回。",
			Score:      0.95,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
			WhyIncluded: []string{
				"vector_seed",
			},
		},
	}}
	embeddingProvider := &fakeQueryEmbeddingProvider{vector: []float32{1, 0}}
	cfg := config.Default()
	cfg.Embedding.Provider = "external"
	cfg.Embedding.Model = "embedding-test"
	cfg.Embedding.OnlineQueryEmbeddingEnabled = true
	orchestrator := NewMemoryOrchestrator(cfg, searcher,
		WithTraceRepository(&fakeTraceRepo{}),
		WithAccessLogRepository(&fakeAccessLogRepo{}),
		WithVectorRepository(vectorRepo),
		WithQueryEmbeddingProvider(embeddingProvider),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)

	resp, err := orchestrator.Search(ctx, memory.SearchRequest{
		Query:       "方案 C QKV 语义召回",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		Scope:       []string{memory.ScopeProjectLocal},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !resp.Diagnostics.UsedVector {
		t.Fatalf("UsedVector = false, want true: %+v", resp.Diagnostics)
	}
	if resp.Diagnostics.RetrievalMode != string(ModeFTSVectorRelation) {
		t.Fatalf("retrieval mode = %q, want %q", resp.Diagnostics.RetrievalMode, ModeFTSVectorRelation)
	}
	if len(embeddingProvider.calls) != 1 || embeddingProvider.calls[0] != "方案 C QKV 语义召回" {
		t.Fatalf("embedding provider calls = %+v", embeddingProvider.calls)
	}
	if len(vectorRepo.calls) != 1 || vectorRepo.calls[0].model != "embedding-test" {
		t.Fatalf("vector repo calls = %+v", vectorRepo.calls)
	}
	vectorResult := findSearchResult(resp.Results, "mem-vector")
	if vectorResult == nil {
		t.Fatalf("vector result missing: %+v", resp.Results)
	}
	if vectorResult.ScoreBreakdown == nil || vectorResult.ScoreBreakdown.Semantic <= 0 {
		t.Fatalf("vector result missing semantic score: %+v", vectorResult)
	}
	if !containsString(vectorResult.WhyIncluded, "vector_seed") {
		t.Fatalf("why_included = %+v, want vector_seed", vectorResult.WhyIncluded)
	}
}

func TestMemoryOrchestratorContextWritesInjectedLogs(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-constraint",
			MemoryType: memory.TypeConstraint,
			Scope:      memory.ScopeProjectLocal,
			Content:    "所有 retrieval 在线检索能力都必须保持当前版本不保存完整源码和完整输出的边界。",
			Score:      0.9,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
		{
			MemoryID:   "mem-pref",
			MemoryType: memory.TypePreference,
			Scope:      memory.ScopeUserGlobal,
			Content:    "用户偏好先给架构边界、风险和可落地实现路径。",
			Score:      0.8,
			Confidence: 0.9,
			State:      memory.StateStable,
			Tier:       memory.TierDurable,
		},
	}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	orchestrator := newTestOrchestrator(searcher, traceRepo, accessRepo)

	resp, err := orchestrator.Context(ctx, memory.ContextRequest{
		Task:        "继续完成 retrieval 流程",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		TokenBudget: 120,
	})
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}

	if resp.RetrievalTraceID != "rt-test-1" {
		t.Fatalf("trace id = %q, want rt-test-1", resp.RetrievalTraceID)
	}
	if len(resp.ContextPack.Memories) != 2 || len(resp.UsedMemoryIDs) != 2 {
		t.Fatalf("unexpected context memory count: memories=%d used=%d", len(resp.ContextPack.Memories), len(resp.UsedMemoryIDs))
	}
	if len(resp.ContextPack.Constraints) != 1 {
		t.Fatalf("constraints len = %d, want 1", len(resp.ContextPack.Constraints))
	}
	if resp.Diagnostics == nil || resp.Diagnostics.RetrievalMode != string(ModeFTSMetadata) || resp.Diagnostics.BudgetAllocation["memory_count"] != 2 {
		t.Fatalf("unexpected diagnostics: %+v", resp.Diagnostics)
	}
	if len(traceRepo.updated) != 1 {
		t.Fatalf("trace updates len = %d, want 1", len(traceRepo.updated))
	}
	updated := traceRepo.updated[0]
	if updated.Status != TraceCompleted || updated.CandidateCount != 2 || updated.InjectedCount != 2 {
		t.Fatalf("unexpected trace update: %+v", updated)
	}
	if got := countAccessEvents(accessRepo.records, retrievedAccessEventType); got != 2 {
		t.Fatalf("retrieved logs = %d, want 2", got)
	}
	if got := countAccessEvents(accessRepo.records, injectedAccessEventType); got != 2 {
		t.Fatalf("injected logs = %d, want 2", got)
	}
	for _, record := range accessRepo.records {
		if record.EventType == injectedAccessEventType && !record.UsedInContext {
			t.Fatalf("injected log not marked used_in_context: %+v", record)
		}
	}
}

func TestMemoryOrchestratorContextRequestBudgetOverridesConfiguredDefault(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-config-budget",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "memory.context 的预算应允许调用方按当前窗口覆盖默认配置。",
			Score:      0.9,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
	}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	cfg := config.Default()
	cfg.Retrieval.DefaultTokenBudget = 300
	orchestrator := NewMemoryOrchestrator(cfg, searcher,
		WithTraceRepository(traceRepo),
		WithAccessLogRepository(accessRepo),
		WithRelationRepository(&fakeRelationRepo{}),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)

	resp, err := orchestrator.Context(ctx, memory.ContextRequest{
		Task:        "检查 context 预算来源",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		TokenBudget: 60,
	})
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}

	if resp.Diagnostics == nil {
		t.Fatal("diagnostics = nil, want budget diagnostics")
	}
	if got := resp.Diagnostics.BudgetAllocation["total"]; got != 60 {
		t.Fatalf("budget total = %d, want request budget 60", got)
	}
	if len(resp.UsedMemoryIDs) != 1 || resp.UsedMemoryIDs[0] != "mem-config-budget" {
		t.Fatalf("used memory ids = %+v, want mem-config-budget", resp.UsedMemoryIDs)
	}
}

func TestMemoryOrchestratorContextCanInjectMoreThanTwoWhenBudgetAllows(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-score-09",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "检查 context top two 时，最高分记忆应被注入。",
			Score:      0.9,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
		{
			MemoryID:   "mem-score-08",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "检查 context top two 时，第二高分记忆应被注入。",
			Score:      0.8,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
		{
			MemoryID:   "mem-score-07",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "检查 context top two 时，第三高分记忆预算允许也应被注入。",
			Score:      0.7,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
	}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	orchestrator := newTestOrchestrator(searcher, traceRepo, accessRepo)

	resp, err := orchestrator.Context(ctx, memory.ContextRequest{
		Task:        "检查 context top two",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		TokenBudget: 600,
	})
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}

	wantIDs := []string{"mem-score-09", "mem-score-08", "mem-score-07"}
	if len(resp.UsedMemoryIDs) != len(wantIDs) {
		t.Fatalf("used memory ids = %+v, want %+v", resp.UsedMemoryIDs, wantIDs)
	}
	for i, want := range wantIDs {
		if resp.UsedMemoryIDs[i] != want {
			t.Fatalf("used memory ids = %+v, want %+v", resp.UsedMemoryIDs, wantIDs)
		}
	}
	if resp.Diagnostics == nil || resp.Diagnostics.BudgetAllocation["memory_count"] != 3 {
		t.Fatalf("diagnostics = %+v, want memory_count 3", resp.Diagnostics)
	}
	if len(traceRepo.updated) != 1 || traceRepo.updated[0].CandidateCount != 3 || traceRepo.updated[0].InjectedCount != 3 {
		t.Fatalf("trace update = %+v, want candidate_count 3 and injected_count 3", traceRepo.updated)
	}
	if got := countAccessEvents(accessRepo.records, retrievedAccessEventType); got != 3 {
		t.Fatalf("retrieved logs = %d, want 3", got)
	}
	if got := countAccessEvents(accessRepo.records, injectedAccessEventType); got != 3 {
		t.Fatalf("injected logs = %d, want 3", got)
	}
}

func TestMemoryOrchestratorSearchExpandsSupportRelation(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-seed",
			MemoryType: memory.TypeDecision,
			Scope:      memory.ScopeProjectLocal,
			Content:    "retrieval 需要 relation-aware rerank。",
			Score:      0.9,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
	}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	relationRepo := &fakeRelationRepo{expansions: []RelationExpansion{
		{
			SeedMemoryID: "mem-seed",
			Direction:    RelationDirectionOutgoing,
			RelationType: RelationTypeSupports,
			Weight:       1,
			RelatedMemory: memory.MemoryItem{
				ID:            "mem-related",
				MemoryType:    memory.TypeProcedure,
				Scope:         memory.ScopeProjectLocal,
				WorkspaceID:   "ws-1",
				ProjectID:     "prj-1",
				Content:       "relation expansion 命中后需要写入 access log 并参与排序。",
				Confidence:    0.8,
				Importance:    0.6,
				SourceQuality: 0.8,
				State:         memory.StateStable,
				Tier:          memory.TierLongTerm,
			},
		},
	}}
	orchestrator := NewMemoryOrchestrator(config.Default(), searcher,
		WithTraceRepository(traceRepo),
		WithAccessLogRepository(accessRepo),
		WithRelationRepository(relationRepo),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)

	resp, err := orchestrator.Search(ctx, memory.SearchRequest{
		Query:       "relation-aware rerank",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		Scope:       []string{memory.ScopeProjectLocal},
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if resp.Diagnostics.RetrievalMode != string(ModeFTSRelation) || !resp.Diagnostics.UsedRelation {
		t.Fatalf("diagnostics = %+v, want fts_relation with used_relation", resp.Diagnostics)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results len = %d, want seed + relation candidate", len(resp.Results))
	}
	related := findSearchResult(resp.Results, "mem-related")
	if related == nil {
		t.Fatalf("results = %+v, want relation-expanded candidate", resp.Results)
	}
	if related.ScoreBreakdown == nil || related.ScoreBreakdown.RelationSupport == 0 {
		t.Fatalf("related score breakdown = %+v, want relation support", related.ScoreBreakdown)
	}
	if !containsString(related.WhyIncluded, "relation_expansion") {
		t.Fatalf("related reasons = %+v, want relation_expansion", related.WhyIncluded)
	}
	if len(traceRepo.updated) != 1 || !traceRepo.updated[0].UsedRelation || traceRepo.updated[0].Mode != ModeFTSRelation {
		t.Fatalf("trace update = %+v, want relation mode", traceRepo.updated)
	}
	if len(accessRepo.records) != 2 {
		t.Fatalf("access logs len = %d, want seed + relation candidate", len(accessRepo.records))
	}
}

func TestMemoryOrchestratorSearchReturnsCodeRefsAndAppliesStaleness(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-code",
			MemoryType: memory.TypeFailure,
			Scope:      memory.ScopeRepoLocal,
			Content:    "Service.Search 曾经出现过检索状态回归。",
			Score:      0.9,
			Confidence: 0.8,
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
		},
	}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	codeRefRepo := &fakeCodeRefRepo{refs: map[string][]memory.CodeRef{
		"mem-code": {{
			ID:            "cr-code",
			MemoryID:      "mem-code",
			RepoID:        "repo",
			FilePath:      "internal/memory/service.go",
			Symbol:        "Service.Search",
			ResolveStatus: memory.CodeRefStatusUnresolved,
		}},
	}}
	codeIndex := &fakeCodeIndexAdapter{resolved: []memory.CodeRef{{
		ID:            "cr-code",
		MemoryID:      "mem-code",
		RepoID:        "repo",
		FilePath:      "internal/memory/service.go",
		Symbol:        "Service.Search",
		ContentHash:   "sha256:new",
		ResolveStatus: memory.CodeRefStatusStale,
		RefSummary:    "local_basic: file hash changed",
	}}}
	orchestrator := NewMemoryOrchestrator(config.Default(), searcher,
		WithTraceRepository(traceRepo),
		WithAccessLogRepository(accessRepo),
		WithRelationRepository(&fakeRelationRepo{}),
		WithCodeRefRepository(codeRefRepo),
		WithCodeIndexAdapter(codeIndex),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)

	resp, err := orchestrator.Search(ctx, memory.SearchRequest{
		Query:           "Service.Search 回归",
		WorkspaceID:     "ws-1",
		RepoID:          "repo",
		Scope:           []string{memory.ScopeRepoLocal},
		IncludeCodeRefs: true,
		Limit:           5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if resp.Diagnostics.RetrievalMode != string(ModeCodeAware) || !resp.Diagnostics.UsedCodeIndex {
		t.Fatalf("diagnostics = %+v, want code-aware mode", resp.Diagnostics)
	}
	if len(resp.Results) != 1 || len(resp.Results[0].CodeRefs) != 1 {
		t.Fatalf("results = %+v, want one code_ref", resp.Results)
	}
	if resp.Results[0].CodeRefs[0].ResolveStatus != memory.CodeRefStatusStale {
		t.Fatalf("code ref = %+v, want stale status", resp.Results[0].CodeRefs[0])
	}
	if resp.Results[0].ScoreBreakdown == nil || resp.Results[0].ScoreBreakdown.StalenessPenalty == 0 {
		t.Fatalf("score breakdown = %+v, want staleness penalty", resp.Results[0].ScoreBreakdown)
	}
	if !containsString(resp.Results[0].WhyIncluded, "code_ref_staleness") {
		t.Fatalf("why_included = %+v, want code_ref_staleness", resp.Results[0].WhyIncluded)
	}
	if len(codeRefRepo.written) != 1 || codeRefRepo.written[0].ResolveStatus != memory.CodeRefStatusStale {
		t.Fatalf("written refs = %+v, want resolved status writeback", codeRefRepo.written)
	}
}

func TestMemoryOrchestratorContextBuildsDocReviewStrategy(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "design.md"), []byte("# Design\n\n## Scope\nnew scope\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	searcher := &fakeMemorySearcher{results: []memory.SearchResult{
		{
			MemoryID:   "mem-checkpoint",
			MemoryType: memory.TypeReviewCheckpoint,
			Scope:      memory.ScopeProjectLocal,
			Content:    "上一轮设计复查已确认基础边界，忽略项继续沿用。",
			Score:      0.95,
			Confidence: 0.9,
			State:      memory.StateStable,
			Tier:       memory.TierDurable,
		},
		{
			MemoryID:   "mem-pref",
			MemoryType: memory.TypePreference,
			Scope:      memory.ScopeUserGlobal,
			Content:    "复查时先看变化章节，再看 checkpoint 未覆盖风险。",
			Score:      0.8,
			Confidence: 0.9,
			State:      memory.StateStable,
			Tier:       memory.TierDurable,
		},
	}}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	docRepo := &fakeDocSnapshotRepo{}
	checkpointRepo := &fakeReviewCheckpointRepo{checkpoints: map[string]memory.ReviewCheckpoint{
		"mem-checkpoint": {
			MemoryID: "mem-checkpoint",
			TargetHashesJSON: `[{
				"doc_path":"design.md",
				"content_hash":"sha256:old",
				"sections":[
					{"section_id":"design","heading_path":["Design"],"section_hash":"sha256:old-design"},
					{"section_id":"design/scope","heading_path":["Design","Scope"],"section_hash":"sha256:old-scope"}
				]
			}]`,
		},
	}}
	orchestrator := NewMemoryOrchestrator(config.Default(), searcher,
		WithTraceRepository(traceRepo),
		WithAccessLogRepository(accessRepo),
		WithRelationRepository(&fakeRelationRepo{}),
		WithDocSnapshotRepository(docRepo),
		WithReviewCheckpointRepository(checkpointRepo),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)

	resp, err := orchestrator.Context(ctx, memory.ContextRequest{
		Task:        "请架构评审 design.md 的变化风险",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		RepoID:      root,
		TokenBudget: 300,
	})
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}

	strategy := resp.ContextPack.ReviewStrategy
	if strategy == nil {
		t.Fatal("review strategy = nil, want doc-aware strategy")
	}
	if strategy.Mode != "changed_sections" || strategy.CheckpointID != "mem-checkpoint" || strategy.IgnoredItemsPolicy != "respect_checkpoint_ignored_items" {
		t.Fatalf("review strategy = %+v, want changed_sections with checkpoint policy", strategy)
	}
	if len(strategy.TargetDocs) != 1 || strategy.TargetDocs[0] != "design.md" {
		t.Fatalf("target docs = %+v, want design.md", strategy.TargetDocs)
	}
	if len(strategy.ChangedSections) == 0 {
		t.Fatalf("changed sections = nil, want section diff")
	}
	if resp.Diagnostics == nil || resp.Diagnostics.RetrievalMode != string(ModeCheckpointAware) || !resp.Diagnostics.UsedDocIndex {
		t.Fatalf("diagnostics = %+v, want checkpoint-aware doc index", resp.Diagnostics)
	}
	if len(traceRepo.updated) != 1 || !traceRepo.updated[0].UsedDocIndex || traceRepo.updated[0].Mode != ModeCheckpointAware {
		t.Fatalf("trace update = %+v, want doc index checkpoint-aware mode", traceRepo.updated)
	}
}

func TestMemoryOrchestratorSearchFallbackToRawEvents(t *testing.T) {
	ctx := context.Background()
	searcher := &fakeMemorySearcher{results: nil}
	traceRepo := &fakeTraceRepo{}
	accessRepo := &fakeAccessLogRepo{}
	rawRepo := &fakeRawEventRepo{events: []capture.RawEvent{
		{
			ID:             "evt-correction",
			EventType:      capture.EventUserCorrection,
			WorkspaceID:    "ws-1",
			ProjectID:      "prj-1",
			ContentSummary: "用户纠正：记忆检索缺少 raw_event 回退，需要增加最近5小时窗口。",
			KeywordsJSON:   `["raw_event","回退"]`,
			OccurredAt:     time.Now().Add(-20 * time.Minute),
		},
		{
			ID:             "evt-old",
			EventType:      capture.EventAgentDecision,
			WorkspaceID:    "ws-1",
			ProjectID:      "prj-1",
			ContentSummary: "过期事件，不应被召回。",
			OccurredAt:     time.Now().Add(-6 * time.Hour),
		},
	}}
	orchestrator := NewMemoryOrchestrator(config.Default(), searcher,
		WithTraceRepository(traceRepo),
		WithAccessLogRepository(accessRepo),
		WithRelationRepository(&fakeRelationRepo{}),
		WithRawEventRepository(rawRepo),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)

	resp, err := orchestrator.Search(ctx, memory.SearchRequest{
		Query:       "raw_event 回退",
		WorkspaceID: "ws-1",
		ProjectID:   "prj-1",
		Scope:       []string{memory.ScopeProjectLocal},
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results len = %d, want 1 fallback raw_event", len(resp.Results))
	}
	if resp.Results[0].MemoryID != "rawevt:evt-correction" {
		t.Fatalf("fallback memory id = %q, want rawevt:evt-correction", resp.Results[0].MemoryID)
	}
	if resp.Results[0].Score <= 0.9 {
		t.Fatalf("fallback score = %f, want > 0.9", resp.Results[0].Score)
	}
	if !containsString(resp.Diagnostics.FallbackReasons, "raw_event_fallback") {
		t.Fatalf("fallback reasons = %+v, want raw_event_fallback", resp.Diagnostics.FallbackReasons)
	}
}

func newTestOrchestrator(searcher *fakeMemorySearcher, traceRepo *fakeTraceRepo, accessRepo *fakeAccessLogRepo) *MemoryOrchestrator {
	return NewMemoryOrchestrator(config.Default(), searcher,
		WithTraceRepository(traceRepo),
		WithAccessLogRepository(accessRepo),
		WithRelationRepository(&fakeRelationRepo{}),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
}

func findSearchResult(results []memory.SearchResult, memoryID string) *memory.SearchResult {
	for i := range results {
		if results[i].MemoryID == memoryID {
			return &results[i]
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type fakeMemorySearcher struct {
	results []memory.SearchResult
	calls   []memory.SearchRequest
}

func (f *fakeMemorySearcher) Search(ctx context.Context, req memory.SearchRequest) ([]memory.SearchResult, memory.SearchDiagnostics, error) {
	f.calls = append(f.calls, req)
	results := append([]memory.SearchResult(nil), f.results...)
	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}
	return results, memory.SearchDiagnostics{
		FTSHits:       len(results),
		FilteredCount: 0,
		Fallback:      "fts_metadata",
	}, nil
}

type fakeTraceRepo struct {
	created []TraceRecord
	updated []TraceRecord
}

func (f *fakeTraceRepo) CreateRetrievalTrace(ctx context.Context, record TraceRecord) (TraceRecord, error) {
	if record.ID == "" {
		record.ID = "rt-test-1"
	}
	f.created = append(f.created, record)
	return record, nil
}

func (f *fakeTraceRepo) UpdateRetrievalTrace(ctx context.Context, record TraceRecord) error {
	f.updated = append(f.updated, record)
	return nil
}

type fakeAccessLogRepo struct {
	records []AccessLogRecord
}

func (f *fakeAccessLogRepo) WriteMemoryAccessLogs(ctx context.Context, records []AccessLogRecord) ([]AccessLogRecord, error) {
	f.records = append(f.records, records...)
	return records, nil
}

type fakeRelationRepo struct {
	expansions []RelationExpansion
}

func (f *fakeRelationRepo) ListRelationExpansions(ctx context.Context, query RelationExpansionQuery) ([]RelationExpansion, error) {
	return append([]RelationExpansion(nil), f.expansions...), nil
}

type fakeVectorRepo struct {
	results []memory.SearchResult
	calls   []fakeVectorSearchCall
}

type fakeVectorSearchCall struct {
	req    memory.SearchRequest
	model  string
	vector []float32
	limit  int
}

func (f *fakeVectorRepo) SearchVector(ctx context.Context, req memory.SearchRequest, model string, queryVector []float32, limit int) ([]memory.SearchResult, error) {
	f.calls = append(f.calls, fakeVectorSearchCall{
		req:    req,
		model:  model,
		vector: append([]float32(nil), queryVector...),
		limit:  limit,
	})
	return append([]memory.SearchResult(nil), f.results...), nil
}

type fakeQueryEmbeddingProvider struct {
	vector []float32
	calls  []string
}

func (f *fakeQueryEmbeddingProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	f.calls = append(f.calls, query)
	return append([]float32(nil), f.vector...), nil
}

type fakeCodeRefRepo struct {
	refs    map[string][]memory.CodeRef
	written []memory.CodeRef
}

func (f *fakeCodeRefRepo) ListCodeRefs(ctx context.Context, query memory.CodeRefQuery) ([]memory.CodeRef, error) {
	return append([]memory.CodeRef(nil), f.refs[query.MemoryID]...), nil
}

func (f *fakeCodeRefRepo) WriteCodeRef(ctx context.Context, ref memory.CodeRef) (memory.CodeRef, error) {
	f.written = append(f.written, ref)
	f.refs[ref.MemoryID] = []memory.CodeRef{ref}
	return ref, nil
}

type fakeCodeIndexAdapter struct {
	resolved []memory.CodeRef
}

func (f *fakeCodeIndexAdapter) ResolveCodeRefs(ctx context.Context, refs []memory.CodeRef) ([]memory.CodeRef, error) {
	return append([]memory.CodeRef(nil), f.resolved...), nil
}

type fakeDocSnapshotRepo struct {
	snapshots []docindex.DocumentSnapshot
}

func (f *fakeDocSnapshotRepo) WriteDocSnapshot(ctx context.Context, snapshot docindex.DocumentSnapshot) (docindex.DocumentSnapshot, error) {
	if snapshot.ID == "" {
		snapshot.ID = "doc-current"
	}
	f.snapshots = append([]docindex.DocumentSnapshot{snapshot}, f.snapshots...)
	return snapshot, nil
}

func (f *fakeDocSnapshotRepo) ListDocSnapshots(ctx context.Context, query docindex.SnapshotQuery) ([]docindex.DocumentSnapshot, error) {
	out := make([]docindex.DocumentSnapshot, 0, len(f.snapshots))
	for _, snapshot := range f.snapshots {
		if snapshot.WorkspaceID == query.WorkspaceID && snapshot.Path == query.Path {
			out = append(out, snapshot)
		}
	}
	return out, nil
}

type fakeReviewCheckpointRepo struct {
	checkpoints map[string]memory.ReviewCheckpoint
}

type fakeRawEventRepo struct {
	events []capture.RawEvent
}

func (f *fakeRawEventRepo) ListEvents(ctx context.Context, req capture.ListEventsRequest) ([]capture.RawEvent, error) {
	return append([]capture.RawEvent(nil), f.events...), nil
}

func (f *fakeReviewCheckpointRepo) GetReviewCheckpoint(ctx context.Context, memoryID string) (memory.ReviewCheckpoint, bool, error) {
	checkpoint, ok := f.checkpoints[memoryID]
	return checkpoint, ok, nil
}

func countAccessEvents(records []AccessLogRecord, eventType string) int {
	count := 0
	for _, record := range records {
		if record.EventType == eventType {
			count++
		}
	}
	return count
}
