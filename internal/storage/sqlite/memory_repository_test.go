//go:build sqlite_fts5

package sqlite

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
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
	return store, memory.NewService(cfg, store, memory.WithRememberAdmissionDecider(allowRememberAdmission{}))
}

type allowRememberAdmission struct{}

func (allowRememberAdmission) DecideRemember(_ context.Context, req memory.RememberRequest) (memory.RememberAdmissionDecision, error) {
	return memory.RememberAdmissionDecision{
		Allowed:        true,
		Decision:       "test_allow",
		InitialState:   initialTestMemoryState(req.SourceType),
		InitialTier:    initialTestMemoryTier(req.Pinned),
		UserConfirmed:  req.SourceType == "user_declared" || req.Pinned,
		RetentionScore: 0.7,
		DecayRate:      0.8,
		ReasonCodes:    []string{"test_allow"},
	}, nil
}

func initialTestMemoryState(sourceType string) string {
	if sourceType == "user_declared" || sourceType == "user_confirmed" {
		return memory.StateStable
	}
	return memory.StatePendingReview
}

func initialTestMemoryTier(pinned bool) string {
	if pinned {
		return memory.TierDurable
	}
	return memory.TierLongTerm
}

func TestRememberAndEditEnqueueMemoryEmbedding(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	enqueuer := &fakeEmbeddingJobEnqueuer{}
	svc := memory.NewService(cfg, store,
		memory.WithRememberAdmissionDecider(allowRememberAdmission{}),
		memory.WithEmbeddingJobEnqueuer(enqueuer),
	)

	rememberResp, err := svc.Remember(ctx, memory.RememberRequest{
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_embedding",
		MemoryType:  memory.TypeDecision,
		SourceType:  "user_declared",
		Title:       "embedding lifecycle",
		Content:     "memory embedding K 应在 memory_item 写入后异步生成。",
		Confidence:  0.9,
		Importance:  0.8,
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户要求补齐 memory_embedding(K) 生成逻辑。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if len(enqueuer.memoryIDs) != 1 || enqueuer.memoryIDs[0] != rememberResp.MemoryID {
		t.Fatalf("embedding enqueue after remember = %+v, want %s", enqueuer.memoryIDs, rememberResp.MemoryID)
	}

	if _, err := svc.Review(ctx, memory.ReviewRequest{
		Action:      "edit",
		MemoryID:    rememberResp.MemoryID,
		Reviewer:    "test",
		Feedback:    "update K",
		EditContent: "memory embedding K 应在 memory_item 编辑后重新异步生成。",
	}); err != nil {
		t.Fatalf("Review(edit) error = %v", err)
	}
	if len(enqueuer.memoryIDs) != 2 || enqueuer.memoryIDs[1] != rememberResp.MemoryID {
		t.Fatalf("embedding enqueue after edit = %+v, want second %s", enqueuer.memoryIDs, rememberResp.MemoryID)
	}
}

func TestApproveEnqueuesMemoryEmbedding(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	enqueuer := &fakeEmbeddingJobEnqueuer{}
	svc := memory.NewService(cfg, store,
		memory.WithRememberAdmissionDecider(allowRememberAdmission{}),
		memory.WithEmbeddingJobEnqueuer(enqueuer),
	)

	rememberResp, err := svc.Remember(ctx, memory.RememberRequest{
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_embedding_approve",
		MemoryType:  memory.TypeProjectFact,
		SourceType:  "agent_summary",
		Title:       "embedding approve lifecycle",
		Content:     "pending_review memory 在审批为 stable 后需要重新排队生成 K。",
		Confidence:  0.8,
		Importance:  0.7,
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "自动候选进入 pending_review。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if len(enqueuer.memoryIDs) != 1 || enqueuer.memoryIDs[0] != rememberResp.MemoryID {
		t.Fatalf("embedding enqueue after pending remember = %+v, want %s", enqueuer.memoryIDs, rememberResp.MemoryID)
	}
	if _, err := svc.Review(ctx, memory.ReviewRequest{
		Action:   "approve",
		MemoryID: rememberResp.MemoryID,
		Reviewer: "test",
		Feedback: "confirm",
	}); err != nil {
		t.Fatalf("Review(approve) error = %v", err)
	}
	if len(enqueuer.memoryIDs) != 2 || enqueuer.memoryIDs[1] != rememberResp.MemoryID {
		t.Fatalf("embedding enqueue after approve = %+v, want second %s", enqueuer.memoryIDs, rememberResp.MemoryID)
	}
}

type fakeEmbeddingJobEnqueuer struct {
	memoryIDs []string
}

func (f *fakeEmbeddingJobEnqueuer) EnqueueMemoryEmbedding(ctx context.Context, memoryID string) error {
	f.memoryIDs = append(f.memoryIDs, memoryID)
	return nil
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
	provenance, found, err := store.GetMemoryProvenance(ctx, rememberResp.MemoryID)
	if err != nil {
		t.Fatalf("GetMemoryProvenance() error = %v", err)
	}
	if !found || provenance.HookPhase != "manual_observe" || provenance.DerivationStage != "memory_remember" {
		t.Fatalf("provenance = %+v found=%v, want manual remember provenance", provenance, found)
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
	deletedWithArchived, err := svc.Search(ctx, memory.SearchRequest{
		Query:           "Kafka",
		WorkspaceID:     "ws",
		ProjectID:       "project_a",
		Scope:           []string{memory.ScopeProjectLocal},
		MemoryTypes:     []string{memory.TypeDecision},
		IncludeArchived: true,
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("Search(include archived after delete) error = %v", err)
	}
	if len(deletedWithArchived.Results) != 0 {
		t.Fatalf("deleted include_archived results = %d, want 0", len(deletedWithArchived.Results))
	}
}

func TestP1RememberDedupesSameContentInSameScope(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	req := memory.RememberRequest{
		Content:    "用户偏好技术方案先分析架构边界、风险和工程落地，再给实现步骤。",
		Title:      "用户偏好：先架构分析再实现",
		MemoryType: memory.TypePreference,
		Scope:      memory.ScopeUserGlobal,
		SourceType: "user_declared",
		Keywords:   []string{"用户偏好", "架构", "风险", "工程落地"},
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户明确要求技术方案先分析架构边界、风险和工程落地。",
		},
	}

	first, err := svc.Remember(ctx, req)
	if err != nil {
		t.Fatalf("Remember(first) error = %v", err)
	}
	second, err := svc.Remember(ctx, req)
	if err != nil {
		t.Fatalf("Remember(second) error = %v", err)
	}
	if !second.Deduped {
		t.Fatal("second remember deduped = false, want true")
	}
	if second.MemoryID != first.MemoryID {
		t.Fatalf("second memory_id = %q, want existing %q", second.MemoryID, first.MemoryID)
	}
}

func TestP1RememberDedupesProjectLocalByScopeIdentityNotProvenance(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	req := memory.RememberRequest{
		Content:     "项目级记忆去重必须按 workspace/project/content 判断，repo/session/task 仅作为来源上下文。",
		Title:       "项目级记忆去重",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_dedup",
		ProjectID:   "project_dedup",
		RepoID:      "repo_source_a",
		SessionID:   "sess_source_a",
		TaskID:      "task_source_a",
		SourceType:  "agent_summary",
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "项目级记忆的来源字段不应改变 scope 去重语义。",
		},
	}

	first, err := svc.Remember(ctx, req)
	if err != nil {
		t.Fatalf("Remember(first) error = %v", err)
	}
	req.RepoID = "repo_source_b"
	req.SessionID = "sess_source_b"
	req.TaskID = "task_source_b"
	second, err := svc.Remember(ctx, req)
	if err != nil {
		t.Fatalf("Remember(second) error = %v", err)
	}
	if !second.Deduped || second.MemoryID != first.MemoryID {
		t.Fatalf("second = %+v, want deduped existing %s", second, first.MemoryID)
	}
}

func TestP1SearchIsolatesProjectLocalByWorkspace(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	if _, err := svc.Remember(ctx, memory.RememberRequest{
		Content:     "workspace A 的项目决策是暂不引入 Kafka。",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "workspace_a",
		ProjectID:   "project_same",
		SourceType:  "manual_review",
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "workspace A 的项目暂不引入 Kafka。",
		},
	}); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	resp, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "Kafka",
		WorkspaceID: "workspace_b",
		ProjectID:   "project_same",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("workspace_b results = %d, want 0", len(resp.Results))
	}
}

func TestP1SearchValidatesRequestedScopeFields(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	_, err := svc.Search(ctx, memory.SearchRequest{
		Query: "Kafka",
		Scope: []string{memory.ScopeProjectLocal},
		Limit: 10,
	})
	if err == nil {
		t.Fatal("Search() error = nil, want SCOPE_INVALID")
	}
	if got := err.Error(); !strings.HasPrefix(got, "SCOPE_INVALID") {
		t.Fatalf("Search() error = %v, want SCOPE_INVALID", err)
	}
}

func TestP1ReviewEditRejectsOversizedContent(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	resp, err := svc.Remember(ctx, memory.RememberRequest{
		Content:    "用户偏好先分析架构边界。",
		MemoryType: memory.TypePreference,
		Scope:      memory.ScopeUserGlobal,
		SourceType: "user_declared",
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户明确要求先分析架构边界。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	_, err = svc.Review(ctx, memory.ReviewRequest{
		Action:      "edit",
		MemoryID:    resp.MemoryID,
		EditContent: strings.Repeat("x", config.Default().Memory.MaxContentChars+1),
	})
	if err == nil {
		t.Fatal("Review(edit oversized) error = nil, want CONTENT_TOO_LARGE")
	}
	if got := err.Error(); !strings.HasPrefix(got, "CONTENT_TOO_LARGE") {
		t.Fatalf("Review(edit oversized) error = %v, want CONTENT_TOO_LARGE", err)
	}
}

func TestP1SearchAndContextReturnRetrievalTraceID(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	if _, err := svc.Remember(ctx, memory.RememberRequest{
		Content:    "用户偏好技术方案先分析架构边界、风险和工程落地，再给实现步骤。",
		MemoryType: memory.TypePreference,
		Scope:      memory.ScopeUserGlobal,
		SourceType: "user_declared",
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户明确要求技术方案先分析架构边界、风险和工程落地。",
		},
	}); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	searchResp, err := svc.Search(ctx, memory.SearchRequest{
		Query: "架构 风险",
		Scope: []string{memory.ScopeUserGlobal},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if searchResp.Diagnostics.RetrievalTraceID == "" {
		t.Fatal("search retrieval_trace_id is empty")
	}

	contextResp, err := svc.Context(ctx, memory.ContextRequest{
		Task:        "设计任务调度模块",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		TokenBudget: 1200,
	})
	if err != nil {
		t.Fatalf("Context() error = %v", err)
	}
	if contextResp.RetrievalTraceID == "" {
		t.Fatal("context retrieval_trace_id is empty")
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
	var checkpointText string
	for _, item := range resp.ContextPack.Memories {
		if item.Type == memory.TypeReviewCheckpoint {
			checkpointText = item.Compressed
			break
		}
	}
	for _, want := range []string{
		"The One 长期记忆系统总体架构设计.md",
		"baseline_frozen",
		"Code Index 与 Memory 分层",
		"major_logic_gap_only",
	} {
		if !strings.Contains(checkpointText, want) {
			t.Fatalf("checkpoint context = %q, want to contain %q", checkpointText, want)
		}
	}
}

func TestMemoryKeyProjectionSearchesCompactRetrievalCueWhenFTSMisses(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	rememberResp, err := svc.Remember(ctx, memory.RememberRequest{
		Content:       "检索层采用多路投影，主记忆内容不直接拆分。",
		Title:         "QKV 投影层",
		MemoryType:    memory.TypeDecision,
		Scope:         memory.ScopeProjectLocal,
		WorkspaceID:   "ws",
		ProjectID:     "project_qkv",
		SourceType:    "manual_review",
		RetrievalCues: []string{"QKV Retrieval Projection"},
		Keywords:      []string{"QKV", "检索投影"},
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "方案 B 决定用 QKV Retrieval Projection 作为检索投影层。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	var keyCount int
	if err := store.db.QueryRowContext(ctx,
		"select count(*) from memory_key where memory_id = ?",
		rememberResp.MemoryID,
	).Scan(&keyCount); err != nil {
		t.Fatalf("query memory_key count error = %v", err)
	}
	if keyCount == 0 {
		t.Fatal("memory_key count = 0, want key projection rows")
	}

	resp, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "qkvretrievalprojection",
		WorkspaceID: "ws",
		ProjectID:   "project_qkv",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want compact key fallback hit", len(resp.Results))
	}
	if resp.Results[0].MemoryID != rememberResp.MemoryID {
		t.Fatalf("memory_id = %q, want %q", resp.Results[0].MemoryID, rememberResp.MemoryID)
	}
	if !containsString(resp.Results[0].WhyIncluded, "memory_key_fallback") {
		t.Fatalf("why_included = %#v, want memory_key_fallback", resp.Results[0].WhyIncluded)
	}
	if resp.Diagnostics.Fallback != "memory_key" {
		t.Fatalf("fallback = %q, want memory_key", resp.Diagnostics.Fallback)
	}
}

func TestMemoryKeyFallbackRanksHigherWeightFirst(t *testing.T) {
	ctx := context.Background()
	store, _ := newP1TestService(t)
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range []struct {
		id     string
		weight float64
	}{
		{id: "mem_key_low", weight: 0.2},
		{id: "mem_key_high", weight: 1.0},
	} {
		if _, err := store.db.ExecContext(ctx, `insert into memory_item(
			id, scope, workspace_id, project_id, memory_type, content, search_text,
			state, confidence, importance, decay_rate, tier, created_at, updated_at
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.id, memory.ScopeProjectLocal, "ws", "project_rank", memory.TypeDecision,
			"排序测试内容。", "unrelated text",
			memory.StateStable, 0.7, 0.5, 0.8, memory.TierLongTerm, now, now,
		); err != nil {
			t.Fatalf("insert memory_item(%s) error = %v", item.id, err)
		}
		if _, err := store.db.ExecContext(ctx, `insert into memory_key(
			key_id, memory_id, key_type, key_text, key_hash, weight,
			scope, memory_type, state, tier, created_at, updated_at
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.id+":key", item.id, "test", "sharedcompactkey", item.id+":hash", item.weight,
			memory.ScopeProjectLocal, memory.TypeDecision, memory.StateStable, memory.TierLongTerm, now, now,
		); err != nil {
			t.Fatalf("insert memory_key(%s) error = %v", item.id, err)
		}
	}

	results, _, err := store.searchByMemoryKey(ctx, memory.SearchRequest{
		Query:       "sharedcompactkey",
		WorkspaceID: "ws",
		ProjectID:   "project_rank",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	}, 10)
	if err != nil {
		t.Fatalf("searchByMemoryKey() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 key hits", results)
	}
	if results[0].MemoryID != "mem_key_high" {
		t.Fatalf("top result = %q, want mem_key_high; results=%+v", results[0].MemoryID, results)
	}
}

func TestMemoryKeyProjectionFollowsEditArchiveAndDeleteLifecycle(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	rememberResp, err := svc.Remember(ctx, memory.RememberRequest{
		Content:       "初始检索投影关键词是 QKV Retrieval Projection。",
		Title:         "检索投影生命周期",
		MemoryType:    memory.TypeProcedure,
		Scope:         memory.ScopeProjectLocal,
		WorkspaceID:   "ws",
		ProjectID:     "project_lifecycle",
		SourceType:    "manual_review",
		RetrievalCues: []string{"QKV Retrieval Projection"},
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "测试 key 投影跟随记忆生命周期变化。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	if _, err := svc.Review(ctx, memory.ReviewRequest{
		Action:      "edit",
		MemoryID:    rememberResp.MemoryID,
		EditContent: "更新后的检索投影关键词是 MCP Prompt Cache。",
	}); err != nil {
		t.Fatalf("Review(edit) error = %v", err)
	}

	editedHit, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "mcppromptcache",
		WorkspaceID: "ws",
		ProjectID:   "project_lifecycle",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeProcedure},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(edited key) error = %v", err)
	}
	if len(editedHit.Results) != 1 {
		t.Fatalf("edited key results = %d, want 1", len(editedHit.Results))
	}

	oldKeyHit, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "qkvretrievalprojection",
		WorkspaceID: "ws",
		ProjectID:   "project_lifecycle",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeProcedure},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(old key) error = %v", err)
	}
	if len(oldKeyHit.Results) != 0 {
		t.Fatalf("old key results = %d, want 0 after edit", len(oldKeyHit.Results))
	}

	if _, err := svc.Review(ctx, memory.ReviewRequest{
		Action:   "archive",
		MemoryID: rememberResp.MemoryID,
	}); err != nil {
		t.Fatalf("Review(archive) error = %v", err)
	}
	archivedHit, err := svc.Search(ctx, memory.SearchRequest{
		Query:       "mcppromptcache",
		WorkspaceID: "ws",
		ProjectID:   "project_lifecycle",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeProcedure},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(archived key) error = %v", err)
	}
	if len(archivedHit.Results) != 0 {
		t.Fatalf("archived key results = %d, want 0", len(archivedHit.Results))
	}

	if _, err := svc.Review(ctx, memory.ReviewRequest{
		Action:   "delete",
		MemoryID: rememberResp.MemoryID,
	}); err != nil {
		t.Fatalf("Review(delete) error = %v", err)
	}
	var keyCount int
	if err := store.db.QueryRowContext(ctx,
		"select count(*) from memory_key where memory_id = ?",
		rememberResp.MemoryID,
	).Scan(&keyCount); err != nil {
		t.Fatalf("query memory_key count after delete error = %v", err)
	}
	if keyCount != 0 {
		t.Fatalf("memory_key count after delete = %d, want 0", keyCount)
	}
}

func TestArchiveDeletesMemoryEmbedding(t *testing.T) {
	ctx := context.Background()
	store, svc := newP1TestService(t)
	defer store.Close()

	rememberResp, err := svc.Remember(ctx, memory.RememberRequest{
		Content:     "归档时 memory_embedding 派生索引必须同步清理。",
		Title:       "embedding archive lifecycle",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_embedding_archive",
		SourceType:  "user_declared",
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "测试归档清理 embedding。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if err := store.UpsertMemoryEmbedding(ctx, rememberResp.MemoryID, "embedding-test", []float32{1, 0}); err != nil {
		t.Fatalf("UpsertMemoryEmbedding() error = %v", err)
	}
	if _, err := svc.Review(ctx, memory.ReviewRequest{
		Action:   "archive",
		MemoryID: rememberResp.MemoryID,
	}); err != nil {
		t.Fatalf("Review(archive) error = %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "select count(*) from memory_embedding where memory_id = ?", rememberResp.MemoryID).Scan(&count); err != nil {
		t.Fatalf("query memory_embedding count error = %v", err)
	}
	if count != 0 {
		t.Fatalf("memory_embedding count = %d, want 0 after archive", count)
	}
}
