package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/docindex"
	"github.com/zaneway/the-one/internal/idgen"
	"github.com/zaneway/the-one/internal/memory"
)

const (
	defaultSearchLimit       = 10
	defaultContextBudget     = 1800
	defaultRelationLimit     = 20
	retrievedAccessEventType = "retrieved"
	injectedAccessEventType  = "injected"
)

// MemorySearcher 定义 P4-C1 可复用的 P1 FTS + metadata 检索能力。
// 设计边界：该接口只做候选召回，不负责 trace、access log 或上下文注入。
type MemorySearcher interface {
	// Search 执行 FTS + metadata 检索，返回 P1 兼容结果和基础诊断。
	Search(ctx context.Context, req memory.SearchRequest) ([]memory.SearchResult, memory.SearchDiagnostics, error)
}

// TraceRepository 定义 retrieval_trace 的最小写入能力。
// trace 写入是诊断能力，失败时检索继续并通过 diagnostics 标记降级原因。
type TraceRepository interface {
	// CreateRetrievalTrace 创建 started 状态的检索 trace。
	CreateRetrievalTrace(ctx context.Context, record TraceRecord) (TraceRecord, error)

	// UpdateRetrievalTrace 更新 trace 完成状态、耗时和候选/注入数量。
	UpdateRetrievalTrace(ctx context.Context, record TraceRecord) error
}

// AccessLogRepository 定义 memory_access_log 的最小写入能力。
// access log 写入失败不影响在线检索响应，但必须通过日志和 diagnostics 暴露。
type AccessLogRepository interface {
	// WriteMemoryAccessLogs 批量写入 retrieved/injected 访问日志。
	WriteMemoryAccessLogs(ctx context.Context, records []AccessLogRecord) ([]AccessLogRecord, error)
}

// RelationRepository 定义 P4-C2 relation expansion 所需的最小读取能力。
// 设计边界：只读取已持久化的 depth=1 强关系边，不在线生成关系。
type RelationRepository interface {
	// ListRelationExpansions 查询 seed memory 的一跳关系扩展。
	ListRelationExpansions(ctx context.Context, query RelationExpansionQuery) ([]RelationExpansion, error)
}

// CodeRefRepository 定义 P4-C3 code_ref 在线读取和状态写回能力。
// 查询必须按 memory_id 收敛，避免检索路径扫描完整 code_ref 表。
type CodeRefRepository interface {
	// ListCodeRefs 按 memory_id 查询已持久化 code_ref。
	ListCodeRefs(ctx context.Context, query memory.CodeRefQuery) ([]memory.CodeRef, error)

	// WriteCodeRef 写入解析后的 code_ref 状态、hash 和摘要。
	WriteCodeRef(ctx context.Context, ref memory.CodeRef) (memory.CodeRef, error)
}

// CodeIndexAdapter 定义 P4-C3 local_basic 解析器所需的最小接口。
// 该接口只解析已有 code_ref，不把调用图或结构事实写入 Memory。
type CodeIndexAdapter interface {
	// ResolveCodeRefs 对已有 code_ref 做 best-effort 文件/符号解析。
	ResolveCodeRefs(ctx context.Context, refs []memory.CodeRef) ([]memory.CodeRef, error)
}

// DocSnapshotRepository 定义 P4-C4 review strategy 所需的文档快照读写能力。
// 查询必须按 workspace_id + doc_path 收敛，避免 context 在线路径扫描文档历史。
type DocSnapshotRepository interface {
	// WriteDocSnapshot 写入当前 Markdown 文档快照。
	WriteDocSnapshot(ctx context.Context, snapshot docindex.DocumentSnapshot) (docindex.DocumentSnapshot, error)

	// ListDocSnapshots 查询指定文档的历史快照。
	ListDocSnapshots(ctx context.Context, query docindex.SnapshotQuery) ([]docindex.DocumentSnapshot, error)
}

// MemoryOrchestrator 是 P4-C1 面向 memory.Service 的在线检索编排器。
// 它复用现有 FTS + metadata repository，补齐 intent、score_breakdown、trace 和 access log；
// C2 仅启用持久化 relation depth=1 expansion；vector/code/doc 扩展仍不在本阶段执行。
type MemoryOrchestrator struct {
	cfg           config.Config
	memoryRepo    MemorySearcher
	traceRepo     TraceRepository
	accessLogRepo AccessLogRepository
	relationRepo  RelationRepository
	codeRefRepo   CodeRefRepository
	codeIndex     CodeIndexAdapter
	docRepo       DocSnapshotRepository
	logger        *slog.Logger
}

// MemoryOrchestratorOption 配置 P4-C1 在线检索编排器的可选依赖。
type MemoryOrchestratorOption func(*MemoryOrchestrator)

// WithTraceRepository 注入 retrieval_trace repository。
// 为空时 Search/Context 仍可用，但 diagnostics 会标记 trace_unavailable。
func WithTraceRepository(repo TraceRepository) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.traceRepo = repo
	}
}

// WithAccessLogRepository 注入 memory_access_log repository。
// 为空时 Search/Context 仍可用，但命中结果会在 diagnostics 中标记 access_log_unavailable。
func WithAccessLogRepository(repo AccessLogRepository) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.accessLogRepo = repo
	}
}

// WithRelationRepository 注入 memory_relation repository。
// 为空时 Search/Context 仍回退到 FTS + metadata，并在有 seed 候选时标记 relation_unavailable。
func WithRelationRepository(repo RelationRepository) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.relationRepo = repo
	}
}

// WithCodeRefRepository 注入 code_ref repository。
// 为空时 code_task 仍可走 FTS/relation，但不会返回 code_refs。
func WithCodeRefRepository(repo CodeRefRepository) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.codeRefRepo = repo
	}
}

// WithCodeIndexAdapter 注入 P4-C3 Code Index Adapter。
// 为空时仅返回已持久化 code_ref，不做在线状态刷新。
func WithCodeIndexAdapter(adapter CodeIndexAdapter) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.codeIndex = adapter
	}
}

// WithDocSnapshotRepository 注入 doc_snapshot repository。
// 为空时架构复查仍返回普通 memory.context，但 diagnostics 会标记 doc_index_unavailable。
func WithDocSnapshotRepository(repo DocSnapshotRepository) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.docRepo = repo
	}
}

// WithLogger 注入结构化日志实例。
// 为空时使用 slog.Default，保证 trace/access log 降级仍有可观测日志。
func WithLogger(logger *slog.Logger) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.logger = logger
	}
}

// NewMemoryOrchestrator 创建 P4-C1 memory.Search/Context adapter。
// memoryRepo 必须提供 FTS + metadata 检索能力；trace/access log repository 可选注入。
func NewMemoryOrchestrator(cfg config.Config, memoryRepo MemorySearcher, opts ...MemoryOrchestratorOption) *MemoryOrchestrator {
	orchestrator := &MemoryOrchestrator{
		cfg:        cfg,
		memoryRepo: memoryRepo,
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(orchestrator)
		}
	}
	if orchestrator.logger == nil {
		orchestrator.logger = slog.Default()
	}
	return orchestrator
}

// Search 执行 P4-C1 在线检索编排。
// 流程：参数校验 -> 创建 trace -> FTS + metadata 召回 -> 可解释 rerank -> 写入 retrieved access log -> 更新 trace。
func (o *MemoryOrchestrator) Search(ctx context.Context, req memory.SearchRequest) (memory.SearchResponse, error) {
	startedAt := time.Now()
	if strings.TrimSpace(req.Query) == "" {
		return memory.SearchResponse{}, fmt.Errorf("VALIDATION_FAILED: query is required")
	}
	if err := memory.ValidateSearchScopes(req.Scope, req.WorkspaceID, req.ProjectID, req.RepoID, req.SessionID); err != nil {
		return memory.SearchResponse{}, err
	}
	o.normalizeSearchRequest(&req)

	internalReq := FromMemorySearchRequest(req)
	intent := DetectSearchIntent(internalReq)
	trace, tracePersisted, fallbackReasons := o.startTrace(ctx, traceInput{
		Query:       req.Query,
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		SessionID:   req.SessionID,
		Intent:      intent,
		Mode:        o.defaultMode(),
	})

	retrieved, err := o.retrieve(ctx, req, intent, 0)
	latencyMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		o.finishTrace(ctx, trace, tracePersisted, TraceFailed, 0, 0, latencyMS, fallbackReasons)
		return memory.SearchResponse{}, err
	}
	trace.Mode = retrieved.Mode
	trace.UsedRelation = retrieved.UsedRelation
	trace.UsedCodeIndex = retrieved.UsedCodeIndex
	fallbackReasons = appendFallbackReasons(fallbackReasons, repoFallbackReason(retrieved.Diagnostics.Fallback)...)
	fallbackReasons = appendFallbackReasons(fallbackReasons, retrieved.FallbackReasons...)
	if accessErr := o.writeAccessLogs(ctx, accessLogInput{
		TraceID:   trace.ID,
		SessionID: req.SessionID,
		Query:     req.Query,
		EventType: retrievedAccessEventType,
		Results:   retrieved.Results,
	}); accessErr != nil {
		o.logger.Warn("retrieval access log write failed", "event_type", retrievedAccessEventType, "trace_id", trace.ID, "error", accessErr)
		fallbackReasons = appendFallbackReasons(fallbackReasons, "access_log_unavailable")
	}
	o.finishTrace(ctx, trace, tracePersisted, traceStatus(fallbackReasons), len(retrieved.Results), 0, latencyMS, fallbackReasons)

	diag := retrieved.Diagnostics
	diag.RetrievalTraceID = trace.ID
	diag.LatencyMS = latencyMS
	diag.RetrievalIntent = string(intent)
	diag.RetrievalMode = string(retrieved.Mode)
	diag.UsedFTS = true
	diag.UsedVector = false
	diag.UsedRelation = retrieved.UsedRelation
	diag.UsedCodeIndex = retrieved.UsedCodeIndex
	diag.UsedDocIndex = false
	diag.FallbackReasons = fallbackReasons
	return memory.SearchResponse{Results: retrieved.Results, Diagnostics: diag}, nil
}

// Context 执行 P4-C1 上下文构造。
// 流程：参数校验 -> 单 trace 下执行 FTS 检索 -> 写 retrieved/injected access log -> 按预算裁剪上下文。
func (o *MemoryOrchestrator) Context(ctx context.Context, req memory.ContextRequest) (memory.ContextResponse, error) {
	startedAt := time.Now()
	if strings.TrimSpace(req.Task) == "" {
		return memory.ContextResponse{}, fmt.Errorf("VALIDATION_FAILED: task is required")
	}
	if req.TokenBudget <= 0 {
		req.TokenBudget = o.defaultTokenBudget()
	} else if defaultBudget := o.defaultTokenBudget(); defaultBudget > 0 && req.TokenBudget > defaultBudget {
		req.TokenBudget = defaultBudget
	}

	internalReq := FromMemoryContextRequest(req)
	intent := DetectContextIntent(internalReq)
	trace, tracePersisted, fallbackReasons := o.startTrace(ctx, traceInput{
		Query:       req.Task,
		Task:        req.Task,
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		SessionID:   req.SessionID,
		Intent:      intent,
		Mode:        o.defaultMode(),
	})

	retrieved, err := o.contextSearch(ctx, req, intent)
	latencyMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		o.finishTrace(ctx, trace, tracePersisted, TraceFailed, 0, 0, latencyMS, fallbackReasons)
		return memory.ContextResponse{}, err
	}
	trace.Mode = retrieved.Mode
	trace.UsedRelation = retrieved.UsedRelation
	trace.UsedCodeIndex = retrieved.UsedCodeIndex
	fallbackReasons = appendFallbackReasons(fallbackReasons, repoFallbackReason(retrieved.Diagnostics.Fallback)...)
	fallbackReasons = appendFallbackReasons(fallbackReasons, retrieved.FallbackReasons...)
	if accessErr := o.writeAccessLogs(ctx, accessLogInput{
		TraceID:   trace.ID,
		SessionID: req.SessionID,
		Query:     req.Task,
		EventType: retrievedAccessEventType,
		Results:   retrieved.Results,
	}); accessErr != nil {
		o.logger.Warn("retrieval access log write failed", "event_type", retrievedAccessEventType, "trace_id", trace.ID, "error", accessErr)
		fallbackReasons = appendFallbackReasons(fallbackReasons, "access_log_unavailable")
	}

	contextPack, usedIDs, budgetReport := buildContextPack(retrieved.Results, contextBuilderOptions{
		Intent:      intent,
		TokenBudget: req.TokenBudget,
	})
	usedDocIndex, docFallbackReasons := o.attachReviewStrategy(ctx, req, &contextPack)
	addDocChangedSectionBudget(&budgetReport, contextPack.ReviewStrategy)
	if usedDocIndex {
		trace.UsedDocIndex = true
		trace.Mode = ModeCheckpointAware
		retrieved.Mode = ModeCheckpointAware
	}
	fallbackReasons = appendFallbackReasons(fallbackReasons, docFallbackReasons...)
	if accessErr := o.writeAccessLogs(ctx, accessLogInput{
		TraceID:       trace.ID,
		SessionID:     req.SessionID,
		Query:         req.Task,
		EventType:     injectedAccessEventType,
		Results:       filterUsedResults(retrieved.Results, usedIDs),
		UsedInContext: true,
	}); accessErr != nil {
		o.logger.Warn("retrieval access log write failed", "event_type", injectedAccessEventType, "trace_id", trace.ID, "error", accessErr)
		fallbackReasons = appendFallbackReasons(fallbackReasons, "access_log_unavailable")
	}
	latencyMS = time.Since(startedAt).Milliseconds()
	o.finishTrace(ctx, trace, tracePersisted, traceStatus(fallbackReasons), len(retrieved.Results), len(usedIDs), latencyMS, fallbackReasons)

	return memory.ContextResponse{
		ContextPack:      contextPack,
		UsedMemoryIDs:    usedIDs,
		RetrievalTraceID: trace.ID,
		LatencyMS:        latencyMS,
		Diagnostics: &memory.ContextDiagnostics{
			RetrievalIntent:  string(intent),
			RetrievalMode:    string(retrieved.Mode),
			UsedDocIndex:     usedDocIndex,
			BudgetAllocation: contextBudgetMap(budgetReport),
			FallbackReasons:  fallbackReasons,
		},
	}, nil
}

func (o *MemoryOrchestrator) normalizeSearchRequest(req *memory.SearchRequest) {
	if req.Limit <= 0 {
		req.Limit = o.defaultLimit()
	}
}

func (o *MemoryOrchestrator) defaultLimit() int {
	if o.cfg.Retrieval.DefaultLimit > 0 {
		return o.cfg.Retrieval.DefaultLimit
	}
	return defaultSearchLimit
}

func (o *MemoryOrchestrator) defaultTokenBudget() int {
	if o.cfg.Retrieval.DefaultTokenBudget > 0 {
		return o.cfg.Retrieval.DefaultTokenBudget
	}
	return defaultContextBudget
}

func (o *MemoryOrchestrator) defaultMode() RetrievalMode {
	if o.relationRepo != nil {
		return ModeFTSRelation
	}
	return ModeFTSMetadata
}

type retrieveOutput struct {
	Results         []memory.SearchResult
	Diagnostics     memory.SearchDiagnostics
	Mode            RetrievalMode
	UsedRelation    bool
	UsedCodeIndex   bool
	FallbackReasons []string
}

func (o *MemoryOrchestrator) retrieve(ctx context.Context, req memory.SearchRequest, intent RetrievalIntent, tokenBudget int) (retrieveOutput, error) {
	if o.memoryRepo == nil {
		return retrieveOutput{}, fmt.Errorf("RETRIEVAL_UNAVAILABLE: memory search repository is required")
	}
	results, diag, err := o.memoryRepo.Search(ctx, req)
	if err != nil {
		return retrieveOutput{Diagnostics: diag, Mode: o.defaultMode()}, err
	}
	now := time.Now()
	candidates := make(map[string]Candidate, len(results))
	byID := make(map[string]memory.SearchResult, len(results))
	seedIDs := make([]string, 0, len(results))
	for _, result := range results {
		byID[result.MemoryID] = result
		seedIDs = append(seedIDs, result.MemoryID)
		candidates[result.MemoryID] = Candidate{
			Memory: memory.MemoryItem{
				ID:            result.MemoryID,
				Scope:         result.Scope,
				MemoryType:    result.MemoryType,
				Title:         result.Title,
				Content:       result.Content,
				State:         result.State,
				Confidence:    result.Confidence,
				Importance:    0.5,
				SourceQuality: 0.7,
				Tier:          result.Tier,
				UpdatedAt:     now,
			},
			FTSScore:         result.Score,
			InclusionReasons: append([]string(nil), result.WhyIncluded...),
			CodeRefs:         append([]memory.CodeRef(nil), result.CodeRefs...),
		}
	}
	usedRelation, relationFallback := o.expandRelations(ctx, req, candidates, byID, seedIDs)
	usedCodeIndex, codeFallback := o.attachCodeRefs(ctx, req, intent, candidates, byID)
	candidateList := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidateList = append(candidateList, candidate)
	}
	reranked := RerankCandidates(candidateList, RerankOptions{
		Query:         req.Query,
		Scopes:        req.Scope,
		Intent:        intent,
		VectorEnabled: false,
		TokenBudget:   tokenBudget,
		Now:           now,
	})
	if req.Limit > 0 && len(reranked) > req.Limit {
		reranked = reranked[:req.Limit]
	}
	out := make([]memory.SearchResult, 0, len(reranked))
	for _, candidate := range reranked {
		result, ok := byID[candidate.Memory.ID]
		if !ok {
			result = searchResultFromMemory(candidate.Memory)
		}
		result.Score = candidate.FinalScore
		breakdown := candidate.ScoreBreakdown
		result.ScoreBreakdown = &breakdown
		result.WhyIncluded = append([]string(nil), candidate.InclusionReasons...)
		result.CodeRefs = append([]memory.CodeRef(nil), candidate.CodeRefs...)
		out = append(out, result)
	}
	mode := ModeFTSMetadata
	if usedRelation {
		mode = ModeFTSRelation
	}
	if usedCodeIndex {
		mode = ModeCodeAware
	}
	return retrieveOutput{
		Results:         out,
		Diagnostics:     diag,
		Mode:            mode,
		UsedRelation:    usedRelation,
		UsedCodeIndex:   usedCodeIndex,
		FallbackReasons: appendFallbackReasons(relationFallback, codeFallback...),
	}, nil
}

func (o *MemoryOrchestrator) contextSearch(ctx context.Context, req memory.ContextRequest, intent RetrievalIntent) (retrieveOutput, error) {
	searchTypes := []string{memory.TypeConstraint, memory.TypeDecision, memory.TypeFailure, memory.TypePreference, memory.TypeProjectFact, memory.TypeProcedure, memory.TypeTemporaryState}
	if isArchitectureReviewTask(req.Task) {
		searchTypes = append([]string{memory.TypeReviewCheckpoint}, searchTypes...)
	}
	main, err := o.retrieve(ctx, memory.SearchRequest{
		Query:           req.Task,
		WorkspaceID:     req.WorkspaceID,
		ProjectID:       req.ProjectID,
		RepoID:          req.RepoID,
		SessionID:       req.SessionID,
		Scope:           contextSearchScopes(req),
		MemoryTypes:     searchTypes,
		Limit:           o.defaultLimit(),
		IncludeArchived: false,
		IncludeEvidence: req.IncludeEvidenceSummary,
		IncludeCodeRefs: req.IncludeCodeRefs,
	}, intent, req.TokenBudget)
	if err != nil {
		return main, err
	}
	out := main
	if !containsMemoryType(out.Results, memory.TypePreference) {
		preferences, prefErr := o.retrieve(ctx, memory.SearchRequest{
			Query:           "用户偏好 架构 风险 工程落地 preference",
			WorkspaceID:     req.WorkspaceID,
			ProjectID:       req.ProjectID,
			RepoID:          req.RepoID,
			SessionID:       req.SessionID,
			Scope:           []string{memory.ScopeUserGlobal},
			MemoryTypes:     []string{memory.TypePreference},
			Limit:           3,
			IncludeArchived: false,
			IncludeEvidence: req.IncludeEvidenceSummary,
			IncludeCodeRefs: req.IncludeCodeRefs,
		}, IntentUserPreference, req.TokenBudget)
		if prefErr == nil {
			out.Results = appendMissingResults(out.Results, preferences.Results)
			out.Diagnostics.FTSHits += preferences.Diagnostics.FTSHits
			out.Diagnostics.FilteredCount += preferences.Diagnostics.FilteredCount
			out.UsedRelation = out.UsedRelation || preferences.UsedRelation
			out.UsedCodeIndex = out.UsedCodeIndex || preferences.UsedCodeIndex
			out.FallbackReasons = appendFallbackReasons(out.FallbackReasons, preferences.FallbackReasons...)
			if preferences.UsedCodeIndex {
				out.Mode = ModeCodeAware
			} else if preferences.UsedRelation {
				out.Mode = ModeFTSRelation
			}
		} else {
			o.logger.Warn("preference supplemental search failed", "error", prefErr)
		}
	}
	if isArchitectureReviewTask(req.Task) && !containsMemoryType(out.Results, memory.TypeReviewCheckpoint) {
		scopes := checkpointSearchScopes(req)
		if len(scopes) > 0 {
			checkpoints, checkpointErr := o.retrieve(ctx, memory.SearchRequest{
				Query:           "设计复查 架构评审 文档完整性 review_checkpoint",
				WorkspaceID:     req.WorkspaceID,
				ProjectID:       req.ProjectID,
				RepoID:          req.RepoID,
				SessionID:       req.SessionID,
				Scope:           scopes,
				MemoryTypes:     []string{memory.TypeReviewCheckpoint},
				Limit:           3,
				IncludeArchived: false,
				IncludeEvidence: req.IncludeEvidenceSummary,
				IncludeCodeRefs: req.IncludeCodeRefs,
			}, IntentArchitectureReview, req.TokenBudget)
			if checkpointErr == nil {
				out.Results = appendMissingResults(checkpoints.Results, out.Results)
				out.Diagnostics.FTSHits += checkpoints.Diagnostics.FTSHits
				out.Diagnostics.FilteredCount += checkpoints.Diagnostics.FilteredCount
				out.UsedRelation = out.UsedRelation || checkpoints.UsedRelation
				out.UsedCodeIndex = out.UsedCodeIndex || checkpoints.UsedCodeIndex
				out.FallbackReasons = appendFallbackReasons(out.FallbackReasons, checkpoints.FallbackReasons...)
				if checkpoints.UsedCodeIndex {
					out.Mode = ModeCodeAware
				} else if checkpoints.UsedRelation {
					out.Mode = ModeFTSRelation
				}
			} else {
				o.logger.Warn("checkpoint supplemental search failed", "error", checkpointErr)
			}
		}
	}
	return out, nil
}

func (o *MemoryOrchestrator) attachReviewStrategy(ctx context.Context, req memory.ContextRequest, pack *memory.ContextPack) (bool, []string) {
	if pack == nil || !isArchitectureReviewTask(req.Task) {
		return false, nil
	}
	docPaths := extractMarkdownDocPaths(req.Task, 3)
	if len(docPaths) == 0 {
		return false, nil
	}
	if !o.cfg.DocIndex.Enabled {
		return false, []string{"doc_index_disabled"}
	}
	if o.docRepo == nil {
		return false, []string{"doc_index_unavailable"}
	}
	checkpointID := firstReviewCheckpointID(pack.Memories)
	strategy := memory.ReviewStrategy{
		Mode:               "checkpoint_only",
		CheckpointID:       checkpointID,
		TargetDocs:         docPaths,
		IgnoredItemsPolicy: ignoredItemsPolicy(checkpointID),
	}
	fallbackReasons := []string{}
	used := false
	for _, docPath := range docPaths {
		current, err := docindex.BuildMarkdownSnapshot(docindex.MarkdownBuildOptions{
			WorkspaceID:         req.WorkspaceID,
			ProjectID:           req.ProjectID,
			RepoID:              req.RepoID,
			Path:                docPath,
			MaxDocSizeKB:        o.cfg.DocIndex.MaxDocSizeKB,
			MaxSections:         o.cfg.DocIndex.MaxSections,
			StoreSectionSummary: o.cfg.DocIndex.StoreSectionSummary,
		})
		if err != nil {
			o.logger.Warn("doc snapshot build failed", "doc_path", docPath, "error", err)
			fallbackReasons = appendFallbackReasons(fallbackReasons, "doc_snapshot_failed")
			continue
		}
		written, err := o.docRepo.WriteDocSnapshot(ctx, current)
		if err != nil {
			o.logger.Warn("doc snapshot write failed", "doc_path", docPath, "error", err)
			fallbackReasons = appendFallbackReasons(fallbackReasons, "doc_snapshot_write_failed")
			continue
		}
		snapshots, err := o.docRepo.ListDocSnapshots(ctx, docindex.SnapshotQuery{
			WorkspaceID:     req.WorkspaceID,
			ProjectID:       req.ProjectID,
			RepoID:          req.RepoID,
			Path:            written.Path,
			IncludeSections: true,
			Limit:           o.cfg.DocIndex.MaxSnapshotsPerDoc,
		})
		if err != nil {
			o.logger.Warn("doc snapshot lookup failed", "doc_path", docPath, "error", err)
			fallbackReasons = appendFallbackReasons(fallbackReasons, "doc_snapshot_lookup_failed")
			continue
		}
		used = true
		mode, changedSections := reviewModeForDoc(written, snapshots)
		strategy.Mode = strongerReviewMode(strategy.Mode, mode)
		strategy.ChangedSections = appendFallbackReasons(strategy.ChangedSections, changedSections...)
	}
	if !used {
		return false, fallbackReasons
	}
	if strategy.Mode != "changed_sections" {
		strategy.ChangedSections = nil
	}
	pack.ReviewStrategy = &strategy
	return true, fallbackReasons
}

func extractMarkdownDocPaths(task string, limit int) []string {
	task = strings.ReplaceAll(task, "\n", " ")
	rawTokens := strings.Fields(task)
	out := make([]string, 0, limit)
	seen := map[string]bool{}
	for _, token := range rawTokens {
		token = strings.Trim(token, "`'\"“”‘’()[]{}<>，。；;：:,")
		lower := strings.ToLower(token)
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
			continue
		}
		cleaned := filepath.Clean(filepath.FromSlash(token))
		if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			continue
		}
		cleaned = filepath.ToSlash(cleaned)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstReviewCheckpointID(memories []memory.ContextMemory) string {
	for _, item := range memories {
		if item.Type == memory.TypeReviewCheckpoint {
			return item.MemoryID
		}
	}
	return ""
}

func ignoredItemsPolicy(checkpointID string) string {
	if checkpointID == "" {
		return "none"
	}
	return "respect_checkpoint_ignored_items"
}

func reviewModeForDoc(current docindex.DocumentSnapshot, snapshots []docindex.DocumentSnapshot) (string, []string) {
	previous, found := previousDocSnapshot(current, snapshots)
	if !found {
		return "full_document", nil
	}
	if previous.ContentHash == current.ContentHash {
		return "checkpoint_only", nil
	}
	changed := changedDocSections(current, previous, 20)
	if len(changed) > 0 && len(changed) <= 6 {
		return "changed_sections", changed
	}
	return "full_document", nil
}

func previousDocSnapshot(current docindex.DocumentSnapshot, snapshots []docindex.DocumentSnapshot) (docindex.DocumentSnapshot, bool) {
	for _, snapshot := range snapshots {
		if snapshot.ID != "" && current.ID != "" && snapshot.ID == current.ID {
			continue
		}
		return snapshot, true
	}
	return docindex.DocumentSnapshot{}, false
}

func changedDocSections(current, previous docindex.DocumentSnapshot, limit int) []string {
	if len(current.Sections) == 0 || len(previous.Sections) == 0 {
		return nil
	}
	previousByID := make(map[string]docindex.DocumentSection, len(previous.Sections))
	for _, section := range previous.Sections {
		previousByID[section.SectionID] = section
	}
	currentByID := make(map[string]docindex.DocumentSection, len(current.Sections))
	changed := make([]string, 0)
	for _, section := range current.Sections {
		currentByID[section.SectionID] = section
		prev, ok := previousByID[section.SectionID]
		if !ok || prev.ContentHash != section.ContentHash {
			changed = append(changed, formatChangedSection(current.Path, section))
		}
		if limit > 0 && len(changed) >= limit {
			return changed
		}
	}
	for _, section := range previous.Sections {
		if _, ok := currentByID[section.SectionID]; ok {
			continue
		}
		changed = append(changed, formatChangedSection(current.Path, section))
		if limit > 0 && len(changed) >= limit {
			return changed
		}
	}
	return changed
}

func formatChangedSection(docPath string, section docindex.DocumentSection) string {
	heading := strings.Join(section.HeadingPath, " > ")
	if heading == "" {
		heading = section.SectionID
	}
	return docPath + "#" + heading
}

func strongerReviewMode(current, next string) string {
	rank := map[string]int{
		"checkpoint_only":  1,
		"changed_sections": 2,
		"full_document":    3,
	}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func (o *MemoryOrchestrator) expandRelations(ctx context.Context, req memory.SearchRequest, candidates map[string]Candidate, byID map[string]memory.SearchResult, seedIDs []string) (bool, []string) {
	if len(seedIDs) == 0 {
		return false, nil
	}
	if o.relationRepo == nil {
		return false, []string{"relation_unavailable"}
	}
	expansions, err := o.relationRepo.ListRelationExpansions(ctx, RelationExpansionQuery{
		SeedMemoryIDs:   seedIDs,
		WorkspaceID:     req.WorkspaceID,
		ProjectID:       req.ProjectID,
		RepoID:          req.RepoID,
		SessionID:       req.SessionID,
		Scopes:          append([]string(nil), req.Scope...),
		IncludeArchived: req.IncludeArchived,
		RelationTypes:   DefaultRelationTypes(),
		Limit:           defaultRelationLimit,
	})
	if err != nil {
		o.logger.Warn("relation expansion failed", "error", err)
		return false, []string{"relation_failed"}
	}
	if len(expansions) == 0 {
		return false, nil
	}
	used := false
	for _, expansion := range expansions {
		if applyRelationExpansion(candidates, byID, expansion) {
			used = true
		}
	}
	return used, nil
}

func (o *MemoryOrchestrator) attachCodeRefs(ctx context.Context, req memory.SearchRequest, intent RetrievalIntent, candidates map[string]Candidate, byID map[string]memory.SearchResult) (bool, []string) {
	if !req.IncludeCodeRefs && intent != IntentCodeTask {
		return false, nil
	}
	if len(candidates) == 0 {
		return false, nil
	}
	if o.codeRefRepo == nil {
		return false, []string{"code_ref_repository_unavailable"}
	}
	used := false
	fallbackReasons := []string{}
	for id, candidate := range candidates {
		refs, err := o.codeRefRepo.ListCodeRefs(ctx, memory.CodeRefQuery{MemoryID: id, Limit: codeRefLimit(o.cfg.CodeIndex.MaxResolveRefs)})
		if err != nil {
			o.logger.Warn("code_ref lookup failed", "memory_id", id, "error", err)
			fallbackReasons = appendFallbackReasons(fallbackReasons, "code_ref_lookup_failed")
			continue
		}
		if len(refs) == 0 {
			continue
		}
		used = true
		if o.codeIndex != nil {
			resolved, err := o.codeIndex.ResolveCodeRefs(ctx, refs)
			if err != nil {
				o.logger.Warn("code_ref resolve failed", "memory_id", id, "error", err)
				fallbackReasons = appendFallbackReasons(fallbackReasons, "code_index_failed")
			} else {
				refs = resolved
				for i, ref := range refs {
					written, writeErr := o.codeRefRepo.WriteCodeRef(ctx, ref)
					if writeErr != nil {
						o.logger.Warn("code_ref resolve status write failed", "code_ref_id", ref.ID, "error", writeErr)
						fallbackReasons = appendFallbackReasons(fallbackReasons, "code_ref_write_failed")
						continue
					}
					refs[i] = written
				}
			}
		} else {
			fallbackReasons = appendFallbackReasons(fallbackReasons, "code_index_unavailable")
		}
		candidate.CodeRefs = refs
		candidate.InclusionReasons = mergeReasons(candidate.InclusionReasons, "code_ref")
		candidate.StalenessPenalty = maxFloat(candidate.StalenessPenalty, codeRefStalenessPenalty(refs))
		if candidate.StalenessPenalty > 0 {
			candidate.InclusionReasons = mergeReasons(candidate.InclusionReasons, "code_ref_staleness")
		}
		candidates[id] = candidate
		result := byID[id]
		result.CodeRefs = refs
		byID[id] = result
	}
	return used, fallbackReasons
}

func applyRelationExpansion(candidates map[string]Candidate, byID map[string]memory.SearchResult, expansion RelationExpansion) bool {
	seed, ok := candidates[expansion.SeedMemoryID]
	if !ok {
		return false
	}
	weight := clampRelationWeight(expansion.Weight)
	relatedID := expansion.RelatedMemory.ID
	used := true
	switch expansion.RelationType {
	case RelationTypeSupports:
		seed.RelationSupport = maxFloat(seed.RelationSupport, 0.6*weight)
		seed.RelatedMemoryIDs = appendUniqueString(seed.RelatedMemoryIDs, relatedID)
		seed.InclusionReasons = mergeReasons(seed.InclusionReasons, "relation_support")
		candidates[expansion.SeedMemoryID] = seed
		mergeRelatedCandidate(candidates, byID, expansion.RelatedMemory, 0.75*weight, "relation_expansion", "supported_relation")
	case RelationTypeSupersedes:
		if expansion.Direction == RelationDirectionIncoming {
			seed.StalenessPenalty = maxFloat(seed.StalenessPenalty, 0.75*weight)
			seed.RelatedMemoryIDs = appendUniqueString(seed.RelatedMemoryIDs, relatedID)
			seed.InclusionReasons = mergeReasons(seed.InclusionReasons, "superseded_relation")
			candidates[expansion.SeedMemoryID] = seed
			mergeRelatedCandidate(candidates, byID, expansion.RelatedMemory, 0.95*weight, "relation_expansion", "supersedes_relation")
			return used
		}
		seed.RelationSupport = maxFloat(seed.RelationSupport, 0.9*weight)
		seed.RelatedMemoryIDs = appendUniqueString(seed.RelatedMemoryIDs, relatedID)
		seed.InclusionReasons = mergeReasons(seed.InclusionReasons, "supersedes_relation")
		candidates[expansion.SeedMemoryID] = seed
	case RelationTypeSupersededBy:
		if expansion.Direction == RelationDirectionOutgoing {
			seed.StalenessPenalty = maxFloat(seed.StalenessPenalty, 0.75*weight)
			seed.RelatedMemoryIDs = appendUniqueString(seed.RelatedMemoryIDs, relatedID)
			seed.InclusionReasons = mergeReasons(seed.InclusionReasons, "superseded_relation")
			candidates[expansion.SeedMemoryID] = seed
			mergeRelatedCandidate(candidates, byID, expansion.RelatedMemory, 0.95*weight, "relation_expansion", "supersedes_relation")
			return used
		}
		seed.RelationSupport = maxFloat(seed.RelationSupport, 0.9*weight)
		seed.RelatedMemoryIDs = appendUniqueString(seed.RelatedMemoryIDs, relatedID)
		seed.InclusionReasons = mergeReasons(seed.InclusionReasons, "supersedes_relation")
		candidates[expansion.SeedMemoryID] = seed
	case RelationTypeContradicts:
		seed.ConflictPenalty = maxFloat(seed.ConflictPenalty, 0.65*weight)
		seed.RelatedMemoryIDs = appendUniqueString(seed.RelatedMemoryIDs, relatedID)
		seed.InclusionReasons = mergeReasons(seed.InclusionReasons, "contradiction_relation")
		candidates[expansion.SeedMemoryID] = seed
		if related, exists := candidates[relatedID]; exists {
			related.ConflictPenalty = maxFloat(related.ConflictPenalty, 0.65*weight)
			related.RelatedMemoryIDs = appendUniqueString(related.RelatedMemoryIDs, expansion.SeedMemoryID)
			related.InclusionReasons = mergeReasons(related.InclusionReasons, "contradiction_relation")
			candidates[relatedID] = related
		}
	default:
		used = false
	}
	return used
}

func mergeRelatedCandidate(candidates map[string]Candidate, byID map[string]memory.SearchResult, item memory.MemoryItem, relationSupport float64, reasons ...string) {
	if item.ID == "" {
		return
	}
	candidate, exists := candidates[item.ID]
	if !exists {
		candidate = Candidate{
			Memory:           item,
			RelationSupport:  clampRelationWeight(relationSupport),
			InclusionReasons: mergeReasons(nil, reasons...),
		}
		candidates[item.ID] = candidate
		byID[item.ID] = searchResultFromMemory(item)
		return
	}
	candidate.RelationSupport = maxFloat(candidate.RelationSupport, clampRelationWeight(relationSupport))
	candidate.InclusionReasons = mergeReasons(candidate.InclusionReasons, reasons...)
	candidates[item.ID] = candidate
}

func searchResultFromMemory(item memory.MemoryItem) memory.SearchResult {
	return memory.SearchResult{
		MemoryID:   item.ID,
		MemoryType: item.MemoryType,
		Scope:      item.Scope,
		Title:      item.Title,
		Content:    item.Content,
		Confidence: item.Confidence,
		State:      item.State,
		Tier:       item.Tier,
		CodeRefs:   []memory.CodeRef{},
	}
}

type traceInput struct {
	Query       string
	Task        string
	WorkspaceID string
	ProjectID   string
	RepoID      string
	SessionID   string
	Intent      RetrievalIntent
	Mode        RetrievalMode
}

func (o *MemoryOrchestrator) startTrace(ctx context.Context, input traceInput) (TraceRecord, bool, []string) {
	trace := TraceRecord{
		SessionID:   input.SessionID,
		WorkspaceID: input.WorkspaceID,
		ProjectID:   input.ProjectID,
		RepoID:      input.RepoID,
		Query:       input.Query,
		Task:        input.Task,
		Intent:      input.Intent,
		Mode:        input.Mode,
		UsedFTS:     true,
		Status:      TraceStarted,
		CreatedAt:   time.Now().UTC(),
	}
	if o.traceRepo == nil {
		trace.ID = newTraceID(o.logger)
		return trace, false, []string{"trace_unavailable"}
	}
	created, err := o.traceRepo.CreateRetrievalTrace(ctx, trace)
	if err != nil {
		o.logger.Warn("retrieval trace create failed", "error", err)
		trace.ID = newTraceID(o.logger)
		return trace, false, []string{"trace_unavailable"}
	}
	return created, true, nil
}

func (o *MemoryOrchestrator) finishTrace(ctx context.Context, trace TraceRecord, persisted bool, status TraceStatus, candidateCount, injectedCount int, latencyMS int64, fallbackReasons []string) {
	if !persisted || o.traceRepo == nil {
		return
	}
	trace.Status = status
	trace.CandidateCount = candidateCount
	trace.InjectedCount = injectedCount
	trace.LatencyMS = latencyMS
	trace.FallbackReason = encodeFallbackReasons(fallbackReasons)
	if err := o.traceRepo.UpdateRetrievalTrace(ctx, trace); err != nil {
		o.logger.Warn("retrieval trace update failed", "trace_id", trace.ID, "error", err)
	}
}

type accessLogInput struct {
	TraceID       string
	SessionID     string
	Query         string
	EventType     string
	Results       []memory.SearchResult
	UsedInContext bool
}

func (o *MemoryOrchestrator) writeAccessLogs(ctx context.Context, input accessLogInput) error {
	if len(input.Results) == 0 {
		return nil
	}
	if o.accessLogRepo == nil {
		return fmt.Errorf("access log repository is unavailable")
	}
	records := make([]AccessLogRecord, 0, len(input.Results))
	for i, result := range input.Results {
		breakdown := memory.ScoreBreakdown{}
		if result.ScoreBreakdown != nil {
			breakdown = *result.ScoreBreakdown
		}
		records = append(records, AccessLogRecord{
			MemoryID:         result.MemoryID,
			SessionID:        input.SessionID,
			RetrievalTraceID: input.TraceID,
			EventType:        input.EventType,
			Query:            input.Query,
			Rank:             i + 1,
			Score:            result.Score,
			ScoreBreakdown:   breakdown,
			InclusionReasons: append([]string(nil), result.WhyIncluded...),
			UsedInContext:    input.UsedInContext,
			CreatedAt:        time.Now().UTC(),
		})
	}
	_, err := o.accessLogRepo.WriteMemoryAccessLogs(ctx, records)
	return err
}

func appendCodeRefs(existing []memory.CodeRef, additions []memory.CodeRef, limit int) []memory.CodeRef {
	if len(additions) == 0 || (limit > 0 && len(existing) >= limit) {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(additions))
	for _, ref := range existing {
		seen[ref.ID] = true
	}
	for _, ref := range additions {
		if ref.ID != "" && seen[ref.ID] {
			continue
		}
		existing = append(existing, ref)
		if ref.ID != "" {
			seen[ref.ID] = true
		}
		if limit > 0 && len(existing) >= limit {
			break
		}
	}
	return existing
}

func contextSearchScopes(req memory.ContextRequest) []string {
	scopes := []string{memory.ScopeUserGlobal}
	if req.WorkspaceID != "" && req.ProjectID != "" {
		scopes = append([]string{memory.ScopeProjectLocal}, scopes...)
	}
	if req.WorkspaceID != "" && req.RepoID != "" {
		scopes = append(scopes, memory.ScopeRepoLocal)
	}
	if req.WorkspaceID != "" && req.SessionID != "" {
		scopes = append(scopes, memory.ScopeSession)
	}
	return scopes
}

func checkpointSearchScopes(req memory.ContextRequest) []string {
	scopes := make([]string, 0, 2)
	if req.WorkspaceID != "" && req.ProjectID != "" {
		scopes = append(scopes, memory.ScopeProjectLocal)
	}
	if req.WorkspaceID != "" && req.RepoID != "" {
		scopes = append(scopes, memory.ScopeRepoLocal)
	}
	return scopes
}

func isArchitectureReviewTask(task string) bool {
	task = strings.ToLower(task)
	return strings.Contains(task, "设计复查") ||
		strings.Contains(task, "架构评审") ||
		strings.Contains(task, "文档完整性") ||
		strings.Contains(task, "review")
}

func containsMemoryType(results []memory.SearchResult, memoryType string) bool {
	for _, result := range results {
		if result.MemoryType == memoryType {
			return true
		}
	}
	return false
}

func appendMissingResults(base []memory.SearchResult, additions []memory.SearchResult) []memory.SearchResult {
	seen := make(map[string]bool, len(base)+len(additions))
	out := make([]memory.SearchResult, 0, len(base)+len(additions))
	for _, result := range base {
		if result.MemoryID == "" || seen[result.MemoryID] {
			continue
		}
		seen[result.MemoryID] = true
		out = append(out, result)
	}
	for _, result := range additions {
		if result.MemoryID == "" || seen[result.MemoryID] {
			continue
		}
		seen[result.MemoryID] = true
		out = append(out, result)
	}
	return out
}

func filterUsedResults(results []memory.SearchResult, usedIDs []string) []memory.SearchResult {
	used := make(map[string]bool, len(usedIDs))
	for _, id := range usedIDs {
		used[id] = true
	}
	out := make([]memory.SearchResult, 0, len(usedIDs))
	for _, result := range results {
		if used[result.MemoryID] {
			out = append(out, result)
		}
	}
	return out
}

func repoFallbackReason(fallback string) []string {
	switch fallback {
	case "", "fts_metadata":
		return nil
	case "metadata_like":
		return []string{"metadata_like_fallback"}
	default:
		return []string{fallback}
	}
}

func appendFallbackReasons(existing []string, additions ...string) []string {
	seen := make(map[string]bool, len(existing)+len(additions))
	out := make([]string, 0, len(existing)+len(additions))
	for _, reason := range append(existing, additions...) {
		if reason == "" || seen[reason] {
			continue
		}
		seen[reason] = true
		out = append(out, reason)
	}
	return out
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func clampRelationWeight(value float64) float64 {
	if value <= 0 {
		return 1.0
	}
	if value > 1 {
		return 1.0
	}
	return value
}

func codeRefLimit(limit int) int {
	if limit <= 0 {
		return 30
	}
	if limit > 30 {
		return 30
	}
	return limit
}

func codeRefStalenessPenalty(refs []memory.CodeRef) float64 {
	penalty := 0.0
	for _, ref := range refs {
		switch ref.ResolveStatus {
		case memory.CodeRefStatusStale:
			penalty = maxFloat(penalty, 0.2)
		case memory.CodeRefStatusMissing, memory.CodeRefStatusAmbiguous:
			penalty = maxFloat(penalty, 0.5)
		case memory.CodeRefStatusUnresolved:
			penalty = maxFloat(penalty, 0.1)
		}
	}
	return penalty
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func traceStatus(fallbackReasons []string) TraceStatus {
	if len(fallbackReasons) > 0 {
		return TraceDegraded
	}
	return TraceCompleted
}

func encodeFallbackReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	raw, err := json.Marshal(reasons)
	if err != nil {
		return strings.Join(reasons, ",")
	}
	return string(raw)
}

func newTraceID(logger *slog.Logger) string {
	id, err := idgen.New("rt")
	if err != nil {
		if logger != nil {
			logger.Warn("retrieval trace id generation failed", "error", err)
		}
		return fmt.Sprintf("rt_%d", time.Now().UnixNano())
	}
	return id
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
