package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/docindex"
	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/scoring"
)

const (
	defaultSearchLimit       = 10
	defaultContextBudget     = 1800
	defaultRelationLimit     = 20
	rawEventFallbackMaxItems = 5
	retrievedAccessEventType = "retrieved"
	injectedAccessEventType  = "injected"
)

// MemorySearcher 定义可复用的 FTS + metadata 检索能力。
// 设计边界：该接口只做候选召回，不负责 trace、access log 或上下文注入。
type MemorySearcher interface {
	// Search 执行 FTS + metadata 检索，返回兼容结果和基础诊断。
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

// RelationRepository 定义 relation expansion 所需的最小读取能力。
// 设计边界：只读取已持久化的 depth=1 强关系边，不在线生成关系。
type RelationRepository interface {
	// ListRelationExpansions 查询 seed memory 的一跳关系扩展。
	ListRelationExpansions(ctx context.Context, query RelationExpansionQuery) ([]RelationExpansion, error)
}

// CodeRefRepository 定义 code_ref 在线读取和状态写回能力。
// 查询必须按 memory_id 收敛，避免检索路径扫描完整 code_ref 表。
type CodeRefRepository interface {
	// ListCodeRefs 按 memory_id 查询已持久化 code_ref。
	ListCodeRefs(ctx context.Context, query memory.CodeRefQuery) ([]memory.CodeRef, error)

	// WriteCodeRef 写入解析后的 code_ref 状态、hash 和摘要。
	WriteCodeRef(ctx context.Context, ref memory.CodeRef) (memory.CodeRef, error)
}

// CodeIndexAdapter 定义 local_basic 解析器所需的最小接口。
// 该接口只解析已有 code_ref，不把调用图或结构事实写入 Memory。
type CodeIndexAdapter interface {
	// ResolveCodeRefs 对已有 code_ref 做 best-effort 文件/符号解析。
	ResolveCodeRefs(ctx context.Context, refs []memory.CodeRef) ([]memory.CodeRef, error)
}

// DocSnapshotRepository 定义 review strategy 所需的文档快照读写能力。
// 查询必须按 workspace_id + doc_path 收敛，避免 context 在线路径扫描文档历史。
type DocSnapshotRepository interface {
	// WriteDocSnapshot 写入当前 Markdown 文档快照。
	WriteDocSnapshot(ctx context.Context, snapshot docindex.DocumentSnapshot) (docindex.DocumentSnapshot, error)

	// ListDocSnapshots 查询指定文档的历史快照。
	ListDocSnapshots(ctx context.Context, query docindex.SnapshotQuery) ([]docindex.DocumentSnapshot, error)
}

// ReviewCheckpointRepository 定义使用 checkpoint target_hashes 的最小读取能力。
// Orchestrator 只读取结构化 hash 元数据，不读取或保存完整历史文档正文。
type ReviewCheckpointRepository interface {
	// GetReviewCheckpoint 按 review_checkpoint memory_id 读取 checkpoint 元数据。
	GetReviewCheckpoint(ctx context.Context, memoryID string) (memory.ReviewCheckpoint, bool, error)
}

// RawEventRepository 定义 raw_event 补充检索所需的最小读取能力。
// 仅用于命中不足时补充最近窗口内的高信号事件，不替代 memory_item 主检索路径。
type RawEventRepository interface {
	ListEvents(ctx context.Context, req capture.ListEventsRequest) ([]capture.RawEvent, error)
}

// MemoryOrchestrator 是面向 memory.Service 的在线检索编排器。
// 它复用现有 FTS + metadata repository，补齐 intent、score_breakdown、trace 和 access log；
// C2 仅启用持久化 relation depth=1 expansion；vector/code/doc 扩展仍不在本阶段执行。
type MemoryOrchestrator struct {
	cfg            config.Config
	memoryRepo     MemorySearcher
	traceRepo      TraceRepository
	accessLogRepo  AccessLogRepository
	relationRepo   RelationRepository
	codeRefRepo    CodeRefRepository
	codeIndex      CodeIndexAdapter
	docRepo        DocSnapshotRepository
	checkpointRepo ReviewCheckpointRepository
	rawEventRepo   RawEventRepository
	logger         *slog.Logger
}

// MemoryOrchestratorOption 配置在线检索编排器的可选依赖。
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

// WithCodeIndexAdapter 注入 Code Index Adapter。
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

// WithReviewCheckpointRepository 注入 review_checkpoint 读取能力。
// 为空时 Doc Index strategy 会降级为当前 snapshot 与历史 snapshot 对比。
func WithReviewCheckpointRepository(repo ReviewCheckpointRepository) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.checkpointRepo = repo
	}
}

// WithRawEventRepository 注入 raw_event 读取能力。
// 仅用于 memory.search/context 命中不足时补充最近 5 小时内的高信号事件。
func WithRawEventRepository(repo RawEventRepository) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.rawEventRepo = repo
	}
}

// WithLogger 注入结构化日志实例。
// 为空时使用 slog.Default，保证 trace/access log 降级仍有可观测日志。
func WithLogger(logger *slog.Logger) MemoryOrchestratorOption {
	return func(o *MemoryOrchestrator) {
		o.logger = logger
	}
}

// NewMemoryOrchestrator 创建 memory.Search/Context adapter。
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

// Search 执行在线检索编排。
// 流程：参数校验 -> 创建 trace -> FTS + metadata 召回 -> relation expansion -> code ref attach -> rerank -> 写入 retrieved access log -> 更新 trace。
// 设计约束：所有可选依赖（trace/access log/relation/code ref）降级时不影响核心检索响应，仅在 diagnostics 中标记 fallback reason。
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

	retrieved, err := o.retrieve(ctx, req, intent, 0, retrieveLogOpts{
		TraceID: trace.ID,
		Phase:   "search",
	})
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
	o.logger.Info("检索命中统计",
		"trace_id", trace.ID,
		"query_hash", shortHashForLog(req.Query),
		"retrieval_intent", string(intent),
		"retrieval_mode", string(retrieved.Mode),
		"candidate_count", len(retrieved.Results),
		"injected_count", 0,
		"candidate_hits", summarizeSearchResults(retrieved.Results),
		"fallback_reasons", fallbackReasons,
		"latency_ms", latencyMS,
	)

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
	return memory.SearchResponse{RetrievalTraceID: trace.ID, Results: retrieved.Results, Diagnostics: diag}, nil
}

// Context 执行上下文构造。
// 流程：参数校验 -> intent 检测 -> 创建 trace -> FTS 检索 + 补充检索（偏好/ checkpoint）-> 写 retrieved access log
// -> buildContextPack（按预算裁剪）-> attachReviewStrategy（Doc Index 策略）-> 写 injected access log -> 更新 trace。
// 与 Search 的区别：Context 会做两次补充检索（偏好和 review_checkpoint），并按 token budget 裁剪输出。
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

	retrieved, err := o.contextSearch(ctx, req, intent, trace.ID)
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
	o.logContextPackDiagnostics(trace.ID, shortHashForLog(req.Task), intent, retrieved.Results, budgetReport)
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
	o.logger.Info("注入命中统计",
		"trace_id", trace.ID,
		"task_hash", shortHashForLog(req.Task),
		"retrieval_intent", string(intent),
		"retrieval_mode", string(retrieved.Mode),
		"candidate_count", len(retrieved.Results),
		"injected_count", len(usedIDs),
		"dropped_count", len(budgetReport.Diagnostics.Dropped),
		"conversion_rate", safeRatio(len(usedIDs), len(retrieved.Results)),
		"candidate_hits", summarizeSearchResults(retrieved.Results),
		"injected_hits", summarizeContextPackHits(budgetReport.Diagnostics.Injected),
		"dropped_hits", summarizeContextPackHits(budgetReport.Diagnostics.Dropped),
		"fallback_reasons", fallbackReasons,
		"latency_ms", latencyMS,
	)
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

type retrieveLogOpts struct {
	TraceID string
	Phase   string
}

// retrieve 执行核心检索流程：FTS 召回 -> relation expansion -> code ref attach -> rerank -> filter -> limit。
// 该方法被 Search 和 Context 共用，tokenBudget 仅在 Context 路径下非零。
func (o *MemoryOrchestrator) retrieve(ctx context.Context, req memory.SearchRequest, intent RetrievalIntent, tokenBudget int, logOpts retrieveLogOpts) (retrieveOutput, error) {
	if o.memoryRepo == nil {
		return retrieveOutput{}, fmt.Errorf("RETRIEVAL_UNAVAILABLE: memory search repository is required")
	}
	phase := strings.TrimSpace(logOpts.Phase)
	if phase == "" {
		phase = "retrieve"
	}
	results, diag, err := o.memoryRepo.Search(ctx, req)
	if err != nil {
		return retrieveOutput{Diagnostics: diag, Mode: o.defaultMode()}, err
	}
	o.logRetrievalStage("fts_seed", phase, logOpts.TraceID, req, results)
	now := time.Now()
	candidates := make(map[string]Candidate, len(results))
	byID := make(map[string]memory.SearchResult, len(results))
	seedIDs := make([]string, 0, len(results))
	seedIDSet := make(map[string]bool, len(results))
	for _, result := range results {
		byID[result.MemoryID] = result
		seedIDs = append(seedIDs, result.MemoryID)
		seedIDSet[result.MemoryID] = true
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
	if usedRelation {
		relationAdded := make([]memory.SearchResult, 0)
		for id, result := range byID {
			if !seedIDSet[id] {
				relationAdded = append(relationAdded, result)
			}
		}
		if len(relationAdded) > 0 {
			o.logRetrievalStage("relation_expanded", phase, logOpts.TraceID, req, relationAdded)
		}
	}
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
	beforeSuperseded := append([]Candidate(nil), reranked...)
	reranked = filterSupersededCandidates(reranked)
	if len(beforeSuperseded) > len(reranked) {
		supersededIDs := make(map[string]bool)
		afterIDs := make(map[string]bool, len(reranked))
		for _, candidate := range reranked {
			afterIDs[candidate.Memory.ID] = true
		}
		for _, candidate := range beforeSuperseded {
			if !afterIDs[candidate.Memory.ID] {
				supersededIDs[candidate.Memory.ID] = true
			}
		}
		o.logRetrievalStage("superseded_dropped", phase, logOpts.TraceID, req, filterResultsByIDs(searchResultsFromCandidates(beforeSuperseded), supersededIDs, true))
	}
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
	rawEventFallbackReasons := []string{}
	if len(out) < req.Limit {
		rawEventResults, fallbackReason := o.retrieveFromRawEvents(ctx, req, req.Limit-len(out))
		if len(rawEventResults) > 0 {
			out = appendMissingResults(out, rawEventResults)
			o.logRetrievalStage("raw_event_fallback", phase, logOpts.TraceID, req, rawEventResults, "fallback_reason", fallbackReason)
		}
		if fallbackReason != "" {
			rawEventFallbackReasons = append(rawEventFallbackReasons, fallbackReason)
		}
	}
	o.logRetrievalStage("final_candidates", phase, logOpts.TraceID, req, out)
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
		FallbackReasons: appendFallbackReasons(appendFallbackReasons(relationFallback, codeFallback...), rawEventFallbackReasons...),
	}, nil
}

func (o *MemoryOrchestrator) retrieveFromRawEvents(ctx context.Context, req memory.SearchRequest, limit int) ([]memory.SearchResult, string) {
	if limit <= 0 {
		return nil, ""
	}
	if o.rawEventRepo == nil {
		return nil, ""
	}
	if req.WorkspaceID == "" {
		return nil, "raw_event_scope_unavailable"
	}
	rawEvents, err := o.rawEventRepo.ListEvents(ctx, capture.ListEventsRequest{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		SessionID:   req.SessionID,
		EventTypes: []string{
			capture.EventUserCorrection,
			capture.EventUserDeclaration,
			capture.EventAgentDecision,
		},
		Limit: maxInt(rawEventFallbackMaxItems*4, limit*4),
	})
	if err != nil {
		o.logger.Warn("raw_event fallback query failed", "error", err)
		return nil, "raw_event_query_failed"
	}
	now := time.Now()
	results := make([]memory.SearchResult, 0, limit)
	for _, event := range rawEvents {
		if len(results) >= limit {
			break
		}
		if !scoring.WithinRawEventWindow(event.OccurredAt, now, scoring.RawEventPolicy{WindowHours: scoring.DefaultRawEventWindowHours}) {
			continue
		}
		score := scoring.ScoreRawEvent(scoring.RawEventInput{
			EventType:      event.EventType,
			OccurredAt:     event.OccurredAt,
			ContentSummary: event.ContentSummary,
			InputSummary:   event.InputSummary,
			OutputSummary:  event.OutputSummary,
			KeywordsJSON:   event.KeywordsJSON,
			SourceRefsJSON: event.SourceRefsJSON,
			Query:          req.Query,
			Now:            now,
		})
		if score <= 0 {
			continue
		}
		results = append(results, memory.SearchResult{
			MemoryID:   "rawevt:" + event.ID,
			MemoryType: event.EventType,
			Scope:      scopedRawEvent(event),
			Title:      "raw_event fallback",
			Content:    firstNonEmpty(event.ContentSummary, event.OutputSummary, event.InputSummary),
			Score:      score,
			Confidence: score,
			State:      memory.StateProvisional,
			Tier:       memory.TierTemporary,
			WhyIncluded: []string{
				"raw_event_fallback",
				"recent_window",
			},
			ScoreBreakdown: &memory.ScoreBreakdown{
				TaskFit: score,
				Final:   score,
			},
		})
	}
	if len(results) == 0 {
		return nil, ""
	}
	return results, "raw_event_fallback"
}

func scopedRawEvent(event capture.RawEvent) string {
	switch {
	case event.SessionID != "":
		return memory.ScopeSession
	case event.ProjectID != "":
		return memory.ScopeProjectLocal
	case event.RepoID != "":
		return memory.ScopeRepoLocal
	default:
		return memory.ScopeUserGlobal
	}
}

// contextSearch 执行 Context 专用的多轮检索策略。
// 流程：
//  1. 主检索：按 searchTypes（10 种记忆类型）+ 作用域检索
//  2. 偏好补充：如果主结果不含 TypePreference，额外检索用户全局偏好（最多 3 条）
//  3. Checkpoint 补充：如果是架构复查任务且不含 TypeReviewCheckpoint，额外检索 checkpoint（最多 3 条）
//
// 设计意图：Context 场景需要更完整的记忆覆盖，偏好和 checkpoint 是高频遗漏项。
func (o *MemoryOrchestrator) contextSearch(ctx context.Context, req memory.ContextRequest, intent RetrievalIntent, traceID string) (retrieveOutput, error) {
	searchTypes := []string{
		memory.TypeRequirement,
		memory.TypeConstraint,
		memory.TypeDecision,
		memory.TypeAssumption,
		memory.TypeOpenIssue,
		memory.TypeFailure,
		memory.TypePreference,
		memory.TypeProjectFact,
		memory.TypeProcedure,
		memory.TypeTemporaryState,
	}
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
	}, intent, req.TokenBudget, retrieveLogOpts{TraceID: traceID, Phase: "context_main"})
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
		}, IntentUserPreference, req.TokenBudget, retrieveLogOpts{TraceID: traceID, Phase: "context_preference"})
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
			}, IntentArchitectureReview, req.TokenBudget, retrieveLogOpts{TraceID: traceID, Phase: "context_checkpoint"})
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

// attachReviewStrategy 为架构复查任务附加 Doc Index 策略。
// 流程：
//  1. 从 task 中提取 .md 文件路径
//  2. 加载 review checkpoint（如有）获取 target_hashes
//  3. 构建当前文档 snapshot 并持久化
//  4. 对比历史 snapshot 或 checkpoint hash，确定 review mode：
//     - checkpoint_only：文档未变更，只需复查 checkpoint 标记的项目
//     - changed_sections：仅部分 section 变更（≤6 个），只复查变更部分
//     - full_document：大量变更或无法对比，全文复查
func (o *MemoryOrchestrator) attachReviewStrategy(ctx context.Context, req memory.ContextRequest, pack *memory.ContextPack) (bool, []string) {
	if pack == nil || !isArchitectureReviewTask(req.Task) {
		return false, nil
	}
	docPaths := extractMarkdownDocPaths(req.Task, 3)
	if len(docPaths) == 0 {
		return false, nil
	}
	if !o.cfg.DocIndex.Enabled || !o.cfg.Retrieval.EnableDocIndex {
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
	checkpoint, checkpointFound := o.loadReviewCheckpoint(ctx, checkpointID)
	if checkpointID != "" && !checkpointFound {
		fallbackReasons = appendFallbackReasons(fallbackReasons, "checkpoint_hash_unavailable")
	}
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
		mode, changedSections := reviewModeForDoc(written, snapshots, checkpoint)
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

func (o *MemoryOrchestrator) loadReviewCheckpoint(ctx context.Context, checkpointID string) (memory.ReviewCheckpoint, bool) {
	if checkpointID == "" || o.checkpointRepo == nil {
		return memory.ReviewCheckpoint{}, false
	}
	checkpoint, found, err := o.checkpointRepo.GetReviewCheckpoint(ctx, checkpointID)
	if err != nil {
		o.logger.Warn("review checkpoint lookup failed", "memory_id", checkpointID, "error", err)
		return memory.ReviewCheckpoint{}, false
	}
	return checkpoint, found
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

// reviewModeForDoc 确定单个文档的复查模式。
// 决策逻辑：
//  1. 优先对比 checkpoint target_hashes（如有）→ hash 一致则 checkpoint_only
//  2. 回退到对比历史 snapshot → hash 一致则 checkpoint_only
//  3. 有变更 section 且 ≤6 个 → changed_sections
//  4. 其余情况 → full_document
func reviewModeForDoc(current docindex.DocumentSnapshot, snapshots []docindex.DocumentSnapshot, checkpoint memory.ReviewCheckpoint) (string, []string) {
	if target, ok := checkpointTargetHashForDoc(checkpoint, current.Path); ok {
		mode, changedSections := reviewModeAgainstCheckpoint(current, target)
		if mode != "full_document" || len(target.Sections) > 0 || len(snapshots) == 0 {
			return mode, changedSections
		}
	}
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

type checkpointTargetHash struct {
	DocPath     string
	ContentHash string
	Sections    []checkpointSectionHash
}

type checkpointSectionHash struct {
	SectionID   string
	HeadingPath []string
	ContentHash string
}

func checkpointTargetHashForDoc(checkpoint memory.ReviewCheckpoint, docPath string) (checkpointTargetHash, bool) {
	docPath = normalizeDocPathForCompare(docPath)
	if docPath == "" {
		return checkpointTargetHash{}, false
	}
	for _, target := range parseCheckpointTargets(checkpoint.TargetHashesJSON) {
		if normalizeDocPathForCompare(target.DocPath) == docPath {
			return target, true
		}
	}
	for _, target := range parseCheckpointTargets(checkpoint.TargetDocsJSON) {
		if normalizeDocPathForCompare(target.DocPath) == docPath && target.ContentHash != "" {
			return target, true
		}
	}
	return checkpointTargetHash{}, false
}

func parseCheckpointTargets(raw string) []checkpointTargetHash {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		var single map[string]any
		if singleErr := json.Unmarshal([]byte(raw), &single); singleErr != nil {
			return nil
		}
		list = []map[string]any{single}
	}
	out := make([]checkpointTargetHash, 0, len(list))
	for _, item := range list {
		target := checkpointTargetHash{
			DocPath:     firstStringFromMap(item, "doc_path", "path"),
			ContentHash: firstStringFromMap(item, "content_hash", "hash"),
			Sections:    parseCheckpointSections(item["sections"]),
		}
		if target.DocPath == "" && target.ContentHash == "" && len(target.Sections) == 0 {
			continue
		}
		out = append(out, target)
	}
	return out
}

func parseCheckpointSections(raw any) []checkpointSectionHash {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]checkpointSectionHash, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		section := checkpointSectionHash{
			SectionID:   firstStringFromMap(item, "section_id"),
			HeadingPath: stringSliceFromAny(item["heading_path"]),
			ContentHash: firstStringFromMap(item, "section_hash", "content_hash"),
		}
		if section.SectionID == "" && section.ContentHash == "" {
			continue
		}
		out = append(out, section)
	}
	return out
}

func reviewModeAgainstCheckpoint(current docindex.DocumentSnapshot, target checkpointTargetHash) (string, []string) {
	if target.ContentHash != "" && target.ContentHash == current.ContentHash {
		return "checkpoint_only", nil
	}
	if len(target.Sections) == 0 || len(current.Sections) == 0 {
		return "full_document", nil
	}
	changed := changedSectionsFromCheckpoint(current, target, 20)
	if len(changed) == 0 && target.ContentHash == "" {
		return "checkpoint_only", nil
	}
	if len(changed) > 0 && len(changed) <= 6 {
		return "changed_sections", changed
	}
	return "full_document", nil
}

func changedSectionsFromCheckpoint(current docindex.DocumentSnapshot, target checkpointTargetHash, limit int) []string {
	targetByID := make(map[string]checkpointSectionHash, len(target.Sections))
	for _, section := range target.Sections {
		if section.SectionID == "" {
			continue
		}
		targetByID[section.SectionID] = section
	}
	currentByID := make(map[string]bool, len(current.Sections))
	changed := make([]string, 0)
	for _, section := range current.Sections {
		currentByID[section.SectionID] = true
		expected, ok := targetByID[section.SectionID]
		if !ok || expected.ContentHash != section.ContentHash {
			changed = append(changed, formatChangedSection(current.Path, section))
		}
		if limit > 0 && len(changed) >= limit {
			return changed
		}
	}
	for _, section := range target.Sections {
		if section.SectionID == "" || currentByID[section.SectionID] {
			continue
		}
		changed = append(changed, current.Path+"#"+checkpointSectionLabel(section))
		if limit > 0 && len(changed) >= limit {
			return changed
		}
	}
	return changed
}

func checkpointSectionLabel(section checkpointSectionHash) string {
	if len(section.HeadingPath) > 0 {
		return strings.Join(section.HeadingPath, " > ")
	}
	return section.SectionID
}

func firstStringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringSliceFromAny(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(text))
	}
	return out
}

func normalizeDocPathForCompare(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(cleaned)
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

// expandRelations 对 seed 候选执行一跳关系扩展。
// 扩展策略：查询 seed memory 的 depth=1 强关系边（supports/supersedes/superseded_by/contradicts），
// 将 related memory 合并到候选集，并调整 seed 的 RelationSupport/StalenessPenalty/ConflictPenalty。
// 设计边界：只读取已持久化的关系边，不在线生成关系。
func (o *MemoryOrchestrator) expandRelations(ctx context.Context, req memory.SearchRequest, candidates map[string]Candidate, byID map[string]memory.SearchResult, seedIDs []string) (bool, []string) {
	if len(seedIDs) == 0 {
		return false, nil
	}
	if !o.cfg.Retrieval.EnableRelationExpansion {
		return false, []string{"relation_disabled"}
	}
	if o.relationRepo == nil {
		return false, []string{"relation_unavailable"}
	}
	limit := o.cfg.Retrieval.MaxRelationExpansion
	if limit <= 0 {
		limit = defaultRelationLimit
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
		Limit:           limit,
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

// attachCodeRefs 为候选记忆附加 code_ref 信息。
// 流程：
//  1. 按 memory_id 查询已持久化的 code_ref
//  2. 如果 CodeIndexAdapter 可用，对 code_ref 做在线文件/符号解析并写回状态
//  3. 根据解析状态计算 codeRefStalenessPenalty（stale=0.2, missing/ambiguous=0.5, unresolved=0.1）
//
// 设计边界：只在 IncludeCodeRefs=true 或 intent=code_task 时执行。
func (o *MemoryOrchestrator) attachCodeRefs(ctx context.Context, req memory.SearchRequest, intent RetrievalIntent, candidates map[string]Candidate, byID map[string]memory.SearchResult) (bool, []string) {
	if !req.IncludeCodeRefs && intent != IntentCodeTask {
		return false, nil
	}
	if !o.cfg.Retrieval.EnableCodeRefResolution || o.cfg.CodeIndex.Provider == "none" {
		return false, []string{"code_index_disabled"}
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

// applyRelationExpansion 将单条关系扩展应用到候选集。
// 不同关系类型的处理策略：
//   - supports：seed 的 RelationSupport 提升，related memory 以 0.75 权重加入候选
//   - supersedes（正向）：seed 的 RelationSupport 大幅提升（0.9），related 以 0.95 权重加入
//   - supersedes（反向/被取代）：seed 的 StalenessPenalty 提升（0.75），标记为过时
//   - supersedes_by（正向）：同 supersedes 反向处理
//   - supersedes_by（反向/取代方）：seed 的 RelationSupport 大幅提升（0.9）
//   - contradicts：seed 和 related 的 ConflictPenalty 均提升（0.65）
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

// filterSupersededCandidates 过滤被取代的候选记忆。
// 如果存在 supersedes_relation 标记的候选（取代方），则移除被取代方（superseded_relation）。
// 设计意图：同一对记忆的取代关系只保留较新的一方。
func filterSupersededCandidates(candidates []Candidate) []Candidate {
	hasReplacement := false
	for _, candidate := range candidates {
		if containsReason(candidate.InclusionReasons, "supersedes_relation") {
			hasReplacement = true
			break
		}
	}
	if !hasReplacement {
		return candidates
	}
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if containsReason(candidate.InclusionReasons, "superseded_relation") {
			continue
		}
		out = append(out, candidate)
	}
	return out
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

// startTrace 创建检索 trace 记录。
// 设计约束：trace 写入失败时返回本地生成的 trace ID 和 fallback reason，不阻塞检索流程。
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
		CreatedAt:   time.Now(),
	}
	if !o.cfg.Retrieval.EnableTrace {
		trace.ID = newTraceID(o.logger)
		return trace, false, []string{"trace_disabled"}
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

// writeAccessLogs 写入 memory_access_log（retrieved 或 injected 类型）。
// 设计约束：access log 写入失败不影响检索响应，但必须通过 logger 和 diagnostics 暴露。
func (o *MemoryOrchestrator) writeAccessLogs(ctx context.Context, input accessLogInput) error {
	if len(input.Results) == 0 {
		return nil
	}
	if !o.cfg.Retrieval.EnableAccessLog {
		return fmt.Errorf("access_log_disabled")
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
			CreatedAt:        time.Now(),
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

func containsReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func shortHashForLog(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var sum uint32
	for _, r := range value {
		sum = sum*33 + uint32(r)
	}
	return fmt.Sprintf("%08x", sum)
}

func safeRatio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
