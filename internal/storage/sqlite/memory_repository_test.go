//go:build sqlite_fts5

package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/memory"
)

func newP1TestService(t *testing.T) (*Store, *memory.Service) {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(context.Background(), cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !store.Status().Capabilities.FTS5 {
		t.Fatal("FTS5 capability false, sqlite_fts5 test requires true")
	}
	return store, memory.NewService(cfg, store)
}

func TestP1RememberSearchArchiveDelete(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	rememberResp, err := svc.Remember(ctx, memory.RememberRequest{
		Content:     "当前异步需求不足，历史决策是暂不引入 Kafka，避免过早复杂化。",
		Title:       "暂不引入 Kafka",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		SourceType:  "manual_review",
		Keywords:    []string{"Kafka", "架构决策"},
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户和 Agent 讨论后决定项目 A 暂不引入 Kafka。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if rememberResp.State != memory.StatePendingReview {
		t.Fatalf("state = %q, want pending_review", rememberResp.State)
	}

	hitResp, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "为什么没有 Kafka",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(project_a) error = %v", err)
	}
	if len(hitResp.Results) != 1 {
		t.Fatalf("project_a results = %d, want 1", len(hitResp.Results))
	}

	missResp, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "为什么没有 Kafka",
		WorkspaceID: "ws",
		ProjectID:   "project_b",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(project_b) error = %v", err)
	}
	if len(missResp.Results) != 0 {
		t.Fatalf("project_b results = %d, want 0", len(missResp.Results))
	}

	if _, err := svc.Review(ctx, memory.ReviewRequest{Action: "archive", MemoryID: rememberResp.MemoryID, Feedback: "过期"}); err != nil {
		t.Fatalf("Review(archive) error = %v", err)
	}
	archivedSearch, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "Kafka",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(after archive) error = %v", err)
	}
	if len(archivedSearch.Results) != 0 {
		t.Fatalf("archived default results = %d, want 0", len(archivedSearch.Results))
	}

	if _, err := svc.Review(ctx, memory.ReviewRequest{Action: "delete", MemoryID: rememberResp.MemoryID, Feedback: "误写入"}); err != nil {
		t.Fatalf("Review(delete) error = %v", err)
	}
	deletedSearch, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "Kafka",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(after delete) error = %v", err)
	}
	if len(deletedSearch.Results) != 0 {
		t.Fatalf("deleted results = %d, want 0", len(deletedSearch.Results))
	}
}

func TestP1ContextIncludesUserPreferenceAndCheckpoint(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	if _, err := svc.Remember(ctx, memory.RememberRequest{
		Content:    "用户偏好技术方案先分析架构边界、风险和工程落地，再给实现步骤。",
		Title:      "用户偏好：先架构分析再实现",
		MemoryType: memory.TypePreference,
		Scope:      memory.ScopeUserGlobal,
		SourceType: "user_declared",
		Keywords:   []string{"用户偏好", "架构", "风险", "工程落地"},
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户明确要求技术方案先分析架构边界、风险和工程落地。",
		},
	}); err != nil {
		t.Fatalf("Remember(preference) error = %v", err)
	}

	if _, err := svc.Remember(ctx, memory.RememberRequest{
		Content:     "总体架构设计已冻结；后续设计复查只关注重大逻辑缺失。",
		Title:       "总体架构设计复查 checkpoint",
		MemoryType:  memory.TypeReviewCheckpoint,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		SourceType:  "manual_review",
		Keywords:    []string{"设计复查", "架构评审", "checkpoint"},
		ReviewCheckpoint: &memory.ReviewCheckpointInput{
			CheckpointType: "architecture_design_review",
			ReviewIntent:   []string{"logic_completeness"},
			TargetDocs: []map[string]any{{
				"path":         "The One 长期记忆系统总体架构设计.md",
				"doc_role":     "architecture_baseline",
				"content_hash": "sha256:test",
			}},
			Conclusion:        "baseline_frozen",
			ConfirmedBaseline: []string{"Code Index 与 Memory 分层"},
			NextReviewPolicy: map[string]any{
				"focus": "major_logic_gap_only",
			},
		},
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "总体架构设计已冻结。",
		},
	}); err != nil {
		t.Fatalf("Remember(checkpoint) error = %v", err)
	}

	resp, err := svc.Context(ctx, memory.ContextRequest{
		Task:        "请再次进行设计复查，检查文档完整性",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		TokenBudget: 1200,
	})
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}
	types := map[string]bool{}
	for _, item := range resp.ContextPack.Memories {
		types[item.Type] = true
	}
	if !types[memory.TypePreference] {
		t.Fatalf("context missing user preference: %#v", resp.ContextPack.Memories)
	}
	if !types[memory.TypeReviewCheckpoint] {
		t.Fatalf("context missing review checkpoint: %#v", resp.ContextPack.Memories)
	}
}
