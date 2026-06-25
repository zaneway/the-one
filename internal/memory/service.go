package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/ingest"
)

// Repository 记忆仓库接口
// 定义 Memory CRUD 所需的事务能力
// 设计约束：SQLite 实现必须保证多表写入原子性
type Repository interface {
	// FindDuplicate 查找重复记忆
	// 按scope、content、memory_type检测是否存在重复记忆
	FindDuplicate(ctx context.Context, item MemoryItem) (MemoryItem, bool, error)

	// Remember 写入记忆
	// 同事务写入memory_item、evidence、memory_evidence_link和FTS
	Remember(ctx context.Context, item MemoryItem, evidence Evidence, checkpoint *ReviewCheckpoint) error

	// Search 检索记忆
	// 执行FTS + metadata检索，返回排序后的结果和诊断信息
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, SearchDiagnostics, error)

	// Get 获取单个记忆
	// 按memoryID获取记忆详情
	Get(ctx context.Context, memoryID string) (MemoryItem, error)

	// GetReviewCheckpoint 获取复查检查点
	// 按memoryID获取关联的review_checkpoint
	GetReviewCheckpoint(ctx context.Context, memoryID string) (ReviewCheckpoint, bool, error)

	// ListReview 列出待审核记忆
	// 查询pending_review状态的记忆列表
	ListReview(ctx context.Context, req ReviewRequest) ([]MemoryItem, error)

	// Approve 批准记忆
	// 将记忆状态从pending_review转为stable，记录用户确认
	Approve(ctx context.Context, memoryID, reviewer, feedback string) (MemoryItem, error)

	// RejectOrArchive 拒绝或归档记忆
	// 将记忆状态转为archived或deleted
	RejectOrArchive(ctx context.Context, memoryID, action, reviewer, feedback string) (MemoryItem, error)

	// Edit 编辑记忆
	// 更新记忆内容、版本号和search_text
	Edit(ctx context.Context, memoryID, editContent, reviewer, feedback, searchText string) (MemoryItem, error)

	// Delete 删除记忆
	// 将记忆状态转为deleted，写入tombstone，删除FTS条目
	Delete(ctx context.Context, memoryID, reviewer, feedback string) (MemoryItem, error)
}

// Service 记忆服务结构体
// 编排手动记忆写入、检索、上下文构建和 review 流转
type Service struct {
	cfg               config.Config            // 配置信息
	repo              Repository               // 仓库接口，负责持久化
	orchestrator      RetrievalOrchestrator    // 检索编排器；为空时保持 FTS 路径
	accessFeedback    AccessFeedbackWriter     // 质量闭环：review 等写入 access log
	rememberAdmission RememberAdmissionDecider // 显式 remember 准入；运行时必须注入
	embeddingJobs     EmbeddingJobEnqueuer     // memory_embedding(K) 异步生成入口
	logger            *slog.Logger             // 结构化日志
}

// RetrievalOrchestrator 定义 memory.Service 可选接入的检索编排接口。
// 设计约束：该接口使用 memory 包现有 DTO，避免 memory 反向依赖 internal/retrieval 造成包循环；
// 可以在 retrieval 包中实现 adapter，把 retrieval 内部 DTO 转换为 memory 对外响应。
type RetrievalOrchestrator interface {
	// Search 执行检索编排，返回向后兼容的 memory.search 响应。
	Search(ctx context.Context, req SearchRequest) (SearchResponse, error)

	// Context 执行上下文构造，返回向后兼容的 memory.context 响应。
	Context(ctx context.Context, req ContextRequest) (ContextResponse, error)
}

// EmbeddingJobEnqueuer 定义 memory_item 写入/变更后触发 K 派生索引生成的能力。
// memory.Service 只负责通知，不直接调用外部 embedding 模型。
type EmbeddingJobEnqueuer interface {
	EnqueueMemoryEmbedding(ctx context.Context, memoryID string) error
}

// ServiceOption 配置 Memory Service 的可选能力。
type ServiceOption func(*Service)

// WithRetrievalOrchestrator 注入检索编排器。
// 为空时不改变默认行为；非空时 Search/Context 委托给编排器，Remember/Review 仍由 memory.Service 处理。
func WithRetrievalOrchestrator(orchestrator RetrievalOrchestrator) ServiceOption {
	return func(s *Service) {
		s.orchestrator = orchestrator
	}
}

// WithAccessFeedbackWriter 注入 access log 写入能力，用于 review 反馈进入 retention 闭环。
func WithAccessFeedbackWriter(writer AccessFeedbackWriter) ServiceOption {
	return func(s *Service) {
		s.accessFeedback = writer
	}
}

// WithRememberAdmissionDecider 注入 remember 准入决策器（与 compute_admission 同一规则集）。
func WithRememberAdmissionDecider(decider RememberAdmissionDecider) ServiceOption {
	return func(s *Service) {
		s.rememberAdmission = decider
	}
}

// WithEmbeddingJobEnqueuer 注入 memory_embedding(K) 异步生成入口。
// 为空时 Remember/Review 保持原行为；非空时写入或编辑成功后 best-effort 入队。
func WithEmbeddingJobEnqueuer(enqueuer EmbeddingJobEnqueuer) ServiceOption {
	return func(s *Service) {
		s.embeddingJobs = enqueuer
	}
}

// WithLogger 注入结构化日志实例。
func WithLogger(logger *slog.Logger) ServiceOption {
	return func(s *Service) {
		s.logger = logger
	}
}

// NewService 创建 Memory 服务。
// 默认只启用 FTS + metadata 检索路径；通过 WithRetrievalOrchestrator 可接入检索编排器。
func NewService(cfg config.Config, repo Repository, opts ...ServiceOption) *Service {
	service := &Service{cfg: cfg, repo: repo, logger: slog.Default()}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service
}

// Remember 实现显式写入闭环。
// 完整流程：归一化 -> 内容边界检查 -> 幂等检测 -> 准入决策 -> 构建 memory/evidence/checkpoint -> 事务写入。
// 准入：必须经过 RememberAdmissionDecider（与 AdmissionController 同一规则），未通过则拒绝持久化。
//
// 幂等检测：按 scope + type + content + 所有隔离 ID 匹配，命中则返回已有记忆。
func (s *Service) Remember(ctx context.Context, req RememberRequest) (RememberResponse, error) {
	if s.rememberAdmission == nil {
		s.logger.Error("remember admission decider not configured")
		return RememberResponse{}, fmt.Errorf("ADMISSION_REQUIRED: remember admission decider is not configured")
	}
	// Step 1: 归一化请求参数，填充默认值（user_id、workspace_id、source_type、confidence、importance）
	if err := NormalizeRemember(s.cfg.Memory, &req); err != nil {
		s.logger.Error("remember normalize failed", "error", err)
		return RememberResponse{}, err
	}
	s.logger.Info("remember started",
		"memory_type", req.MemoryType,
		"scope", req.Scope,
		"source_type", req.SourceType,
		"content_len", len([]rune(req.Content)),
	)
	// Step 2: 将 review_checkpoint 序列化为 JSON，用于后续内容边界检查
	checkpointRaw := ""
	if req.ReviewCheckpoint != nil {
		raw, err := toJSON(req.ReviewCheckpoint)
		if err != nil {
			return RememberResponse{}, fmt.Errorf("VALIDATION_FAILED: invalid review_checkpoint: %w", err)
		}
		checkpointRaw = raw
	}
	// Step 3: 内容最小化硬边界检查，超界直接拒绝（content <= 4000字、evidence <= 1200字、keywords <= 30个等）
	if err := ingest.CheckMinimizedContent(s.cfg.Memory, ingest.MinimizationInput{
		Content:               req.Content,
		EvidenceStatement:     req.Evidence.InterpretedStatement,
		Keywords:              req.Keywords,
		SalientSpans:          req.Evidence.SalientSpans,
		ReviewCheckpointJSONs: []string{checkpointRaw},
	}); err != nil {
		return RememberResponse{}, err
	}
	// review_checkpoint 类型必须携带 checkpoint 结构化数据
	if req.MemoryType == TypeReviewCheckpoint && req.ReviewCheckpoint == nil {
		return RememberResponse{}, fmt.Errorf("VALIDATION_FAILED: review_checkpoint is required")
	}
	probe := MemoryItem{
		Scope:       req.Scope,
		WorkspaceID: req.WorkspaceID,
		UserID:      req.UserID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		SessionID:   req.SessionID,
		TaskID:      req.TaskID,
		MemoryType:  req.MemoryType,
		Content:     req.Content,
	}
	// Step 5: 幂等检测——按 scope + type + content + 所有隔离 ID 匹配已有记忆
	if existing, ok, err := s.repo.FindDuplicate(ctx, probe); err != nil {
		s.logger.Error("remember duplicate check failed", "error", err)
		return RememberResponse{}, err
	} else if ok {
		s.logger.Info("remember deduped",
			"memory_id", existing.ID,
			"existing_state", existing.State,
			"existing_tier", existing.Tier,
		)
		return RememberResponse{MemoryID: existing.ID, State: existing.State, Tier: existing.Tier, Deduped: true}, nil
	}
	admission, err := s.rememberAdmission.DecideRemember(ctx, req)
	if err != nil {
		s.logger.Error("remember admission decision failed", "error", err)
		return RememberResponse{}, err
	}
	if !admission.Allowed {
		s.logger.Info("remember admission rejected",
			"decision", admission.Decision,
			"reason_codes", admission.ReasonCodes,
		)
		return RememberResponse{}, fmt.Errorf("ADMISSION_REJECTED: decision=%s reasons=%v", admission.Decision, admission.ReasonCodes)
	}
	state := admission.InitialState
	tier := admission.InitialTier
	// Step 6: 生成 memory_id 和 evidence_id（随机 ID，避免本地并发写入时时间序列冲突）
	memoryID, err := idgen.New("mem")
	if err != nil {
		return RememberResponse{}, err
	}
	evidenceID, err := idgen.New("ev")
	if err != nil {
		return RememberResponse{}, err
	}
	now := time.Now()
	// Step 7: 将数组字段序列化为 JSON 字符串，用于 SQLite 存储
	keywordsJSON, err := toJSON(req.Keywords)
	if err != nil {
		return RememberResponse{}, err
	}
	tagsJSON, err := toJSON(req.Tags)
	if err != nil {
		return RememberResponse{}, err
	}
	entitiesJSON, err := toJSON(req.Entities)
	if err != nil {
		return RememberResponse{}, err
	}
	retrievalCuesJSON, err := toJSON(req.RetrievalCues)
	if err != nil {
		return RememberResponse{}, err
	}
	evidenceKeywordsJSON, err := toJSON(req.Evidence.Keywords)
	if err != nil {
		return RememberResponse{}, err
	}
	salientSpansJSON, err := toJSON(req.Evidence.SalientSpans)
	if err != nil {
		return RememberResponse{}, err
	}
	sourceRefJSON, err := toJSON(req.Evidence.SourceRef)
	if err != nil {
		return RememberResponse{}, err
	}
	// Step 8: 构建 FTS 索引文档（title + content + keywords + tags + retrieval_cues + entities 拼接）
	searchText := ingest.BuildSearchText(ingest.SearchTextInput{
		Title:             req.Title,
		Content:           req.Content,
		NormalizedContent: req.Content,
		Keywords:          req.Keywords,
		Tags:              req.Tags,
		RetrievalCues:     req.RetrievalCues,
		Entities:          req.Entities,
	})
	// Step 9: 构建 memory_item 领域对象，设置所有默认值
	item := MemoryItem{
		ID:                memoryID,
		Scope:             req.Scope,
		WorkspaceID:       req.WorkspaceID,
		UserID:            req.UserID,
		ProjectID:         req.ProjectID,
		RepoID:            req.RepoID,
		SessionID:         req.SessionID,
		TaskID:            req.TaskID,
		MemoryType:        req.MemoryType,
		SourceType:        req.SourceType,
		SourceQuality:     sourceQuality(req.SourceType),
		Title:             req.Title,
		Content:           req.Content,
		NormalizedContent: req.Content,
		SearchText:        searchText,
		KeywordsJSON:      keywordsJSON,
		EntitiesJSON:      entitiesJSON,
		RetrievalCuesJSON: retrievalCuesJSON,
		TagsJSON:          tagsJSON,
		State:             state,
		Confidence:        req.Confidence,
		Importance:        req.Importance,
		EncodingDepth:     2, // 语义摘要级别（0=原始指针, 1=表层摘要, 2=语义摘要, 3=实体关系, 4=策略抽象）
		DecayRate:         firstPositiveFloat(admission.DecayRate, defaultDecayRate(req.MemoryType)),
		RetentionScore:    admission.RetentionScore,
		Tier:              tier,
		CreatedAt:         now,
		UpdatedAt:         now,
		Pinned:            req.Pinned,
		UserConfirmed:     admission.UserConfirmed,
		Version:           1,
	}
	// Step 10: 构建 evidence 对象，interpreted_statement 为空时降级为 content
	evidence := Evidence{
		ID:                   evidenceID,
		SourceType:           req.SourceType,
		InterpretedStatement: firstNonEmpty(req.Evidence.InterpretedStatement, req.Content),
		KeywordsJSON:         evidenceKeywordsJSON,
		SalientSpansJSON:     salientSpansJSON,
		SourceRefJSON:        sourceRefJSON,
		Confidence:           req.Confidence,
		CreatedAt:            now,
	}
	// Step 11: 构建可选的 review_checkpoint（设计复查检查点）
	var checkpoint *ReviewCheckpoint
	if req.ReviewCheckpoint != nil {
		checkpoint, err = buildCheckpoint(memoryID, item, *req.ReviewCheckpoint, now)
		if err != nil {
			return RememberResponse{}, err
		}
	}
	// Step 12: 事务写入 memory_item + evidence + memory_evidence_link + FTS 索引
	if err := s.repo.Remember(ctx, item, evidence, checkpoint); err != nil {
		s.logger.Error("remember write failed",
			"memory_id", memoryID,
			"memory_type", req.MemoryType,
			"scope", req.Scope,
			"error", err,
		)
		return RememberResponse{}, err
	}
	s.logger.Info("remember succeeded",
		"memory_id", memoryID,
		"memory_type", req.MemoryType,
		"scope", req.Scope,
		"state", state,
		"tier", tier,
		"source_type", req.SourceType,
	)
	s.enqueueEmbeddingBestEffort(ctx, memoryID, "remember")
	return RememberResponse{MemoryID: memoryID, State: state, Tier: tier, Deduped: false}, nil
}

func (s *Service) enqueueEmbeddingBestEffort(ctx context.Context, memoryID, reason string) {
	if s.embeddingJobs == nil || memoryID == "" {
		return
	}
	if err := s.embeddingJobs.EnqueueMemoryEmbedding(ctx, memoryID); err != nil {
		s.logger.Warn("enqueue memory embedding failed", "memory_id", memoryID, "reason", reason, "error", err)
	}
}

// Search 执行 FTS + metadata 检索
// 处理流程：
// 1. 校验查询文本和scope
// 2. 生成检索追踪ID
// 3. 执行FTS5全文检索 + metadata过滤
// 4. 返回排序后的结果和诊断信息
// Search 执行 FTS + metadata 检索。
// 处理流程：校验查询文本和 scope -> 生成检索追踪 ID -> 执行 FTS5 全文检索 + metadata 过滤 -> 返回排序结果。
// 诊断信息：返回 FTSHits、FilteredCount、LatencyMS 和 RetrievalTraceID，用于评估检索质量。
func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if s.orchestrator != nil {
		s.logger.Debug("search delegated to orchestrator")
		return s.orchestrator.Search(ctx, req)
	}
	if strings.TrimSpace(req.Query) == "" {
		return SearchResponse{}, fmt.Errorf("VALIDATION_FAILED: query is required")
	}
	if err := ValidateSearchScopes(req.Scope, req.WorkspaceID, req.ProjectID, req.RepoID, req.SessionID); err != nil {
		s.logger.Error("search scope validation failed", "error", err)
		return SearchResponse{}, err
	}
	if req.Limit <= 0 {
		req.Limit = s.cfg.Retrieval.DefaultLimit
	}
	traceID, err := idgen.New("rt")
	if err != nil {
		s.logger.Error("search trace id generation failed", "error", err)
		return SearchResponse{}, err
	}
	s.logger.Info("search started",
		"trace_id", traceID,
		"query_len", len([]rune(req.Query)),
		"scopes", req.Scope,
		"memory_types", req.MemoryTypes,
		"limit", req.Limit,
	)
	startedAt := time.Now()
	results, diag, err := s.repo.Search(ctx, req)
	if err != nil {
		s.logger.Error("search failed", "trace_id", traceID, "error", err, "latency_ms", time.Since(startedAt).Milliseconds())
		return SearchResponse{}, err
	}
	diag.RetrievalTraceID = traceID
	diag.LatencyMS = time.Since(startedAt).Milliseconds()
	diag.RetrievalIntent = "general_search"
	diag.RetrievalMode = "fts_metadata"
	diag.UsedFTS = true
	s.logger.Info("search completed",
		"trace_id", traceID,
		"result_count", len(results),
		"fts_hits", diag.FTSHits,
		"filtered_count", diag.FilteredCount,
		"latency_ms", diag.LatencyMS,
	)
	return SearchResponse{RetrievalTraceID: traceID, Results: results, Diagnostics: diag}, nil
}

// Context 构造压缩上下文包，为 Agent 提供可注入 prompt 的记忆集合。
// 完整流程：
//  1. 校验任务描述和 token 预算（默认 1800 字符）
//  2. 确定检索范围：project_local > user_global > repo_local > session
//  3. 确定记忆类型：设计复查任务优先检索 review_checkpoint
//  4. 执行主检索（FTS + metadata）
//  5. 补充用户偏好记忆（如主结果中未包含 preference 类型）
//  6. 补充 review_checkpoint（如主结果中未包含，且为设计复查任务）
//  7. 按 token budget 逐条压缩和裁剪记忆
//  8. 收集 constraints 列表用于 Agent prompt 约束注入
//
// 设计说明：使用字符数近似 token 数，后续可替换为 tokenizer；compress 使用 rune 计算，正确处理中文。
func (s *Service) Context(ctx context.Context, req ContextRequest) (ContextResponse, error) {
	if s.orchestrator != nil {
		s.logger.Debug("context delegated to orchestrator")
		return s.orchestrator.Context(ctx, req)
	}
	startedAt := time.Now()
	// Step 1: 参数校验——task 为必填，token_budget 使用配置默认值
	if strings.TrimSpace(req.Task) == "" {
		s.logger.Error("context task is empty")
		return ContextResponse{}, fmt.Errorf("VALIDATION_FAILED: task is required")
	}
	s.logger.Info("context started",
		"task_len", len([]rune(req.Task)),
		"token_budget", req.TokenBudget,
		"workspace_id", req.WorkspaceID,
		"project_id", req.ProjectID,
	)
	if req.TokenBudget <= 0 {
		req.TokenBudget = s.cfg.Retrieval.DefaultTokenBudget
	}
	// Step 2: 确定检索记忆类型——常规任务检索 7 种核心类型，设计复查任务额外优先检索 review_checkpoint
	searchTypes := []string{TypeConstraint, TypeDecision, TypeFailure, TypePreference, TypeProjectFact, TypeProcedure, TypeTemporaryState}
	if isDesignReviewTask(req.Task) {
		searchTypes = append([]string{TypeReviewCheckpoint}, searchTypes...)
	}
	// Step 3: 执行主检索——FTS5 全文检索 + scope/type 元数据过滤
	searchResp, err := s.Search(ctx, SearchRequest{
		Query:           req.Task,
		WorkspaceID:     req.WorkspaceID,
		ProjectID:       req.ProjectID,
		RepoID:          req.RepoID,
		SessionID:       req.SessionID,
		Scope:           contextSearchScopes(req),
		MemoryTypes:     searchTypes,
		Limit:           s.cfg.Retrieval.DefaultLimit,
		IncludeArchived: false,
		IncludeEvidence: req.IncludeEvidenceSummary,
	})
	if err != nil {
		return ContextResponse{}, err
	}
	// Step 4: 补充用户偏好——主结果中未包含 preference 时，单独检索 user_global 范围的偏好记忆（限 3 条）
	if !containsMemoryType(searchResp.Results, TypePreference) {
		preferenceResp, prefErr := s.Search(ctx, SearchRequest{
			Query:           "用户偏好 架构 风险 工程落地 preference",
			WorkspaceID:     req.WorkspaceID,
			ProjectID:       req.ProjectID,
			RepoID:          req.RepoID,
			SessionID:       req.SessionID,
			Scope:           []string{ScopeUserGlobal},
			MemoryTypes:     []string{TypePreference},
			Limit:           3,
			IncludeArchived: false,
			IncludeEvidence: req.IncludeEvidenceSummary,
		})
		if prefErr == nil {
			searchResp.Results = append(searchResp.Results, preferenceResp.Results...)
		}
	}
	// Step 5: 补充复查检查点——设计复查任务且主结果中未包含 review_checkpoint 时，单独检索 project_local/repo_local 范围
	if isDesignReviewTask(req.Task) && !containsMemoryType(searchResp.Results, TypeReviewCheckpoint) {
		checkpointResp, checkpointErr := s.Search(ctx, SearchRequest{
			Query:           "设计复查 架构评审 文档完整性 review_checkpoint",
			WorkspaceID:     req.WorkspaceID,
			ProjectID:       req.ProjectID,
			RepoID:          req.RepoID,
			SessionID:       req.SessionID,
			Scope:           checkpointSearchScopes(req),
			MemoryTypes:     []string{TypeReviewCheckpoint},
			Limit:           3,
			IncludeArchived: false,
			IncludeEvidence: req.IncludeEvidenceSummary,
		})
		if checkpointErr == nil {
			searchResp.Results = append(checkpointResp.Results, searchResp.Results...)
		}
	}
	// Step 6: 按 token budget 逐条压缩记忆——从搜索结果中依次裁剪，直到预算耗尽
	memories := make([]ContextMemory, 0, len(searchResp.Results))
	usedIDs := make([]string, 0, len(searchResp.Results))
	constraints := make([]string, 0)
	remaining := req.TokenBudget
	for _, result := range searchResp.Results {
		// review_checkpoint 需要展开结构化字段（结论、基线、待处理项等）再压缩
		content := result.Content
		if result.MemoryType == TypeReviewCheckpoint {
			content = s.checkpointContext(ctx, result)
		}
		// compress 使用 rune 计算，正确处理中文截断；返回空表示预算已耗尽
		compressed := compress(content, remaining)
		if compressed == "" {
			break
		}
		remaining -= len([]rune(compressed))
		// whyIncluded 记录这条记忆被选中的原因：scope + 任务相关性 + 状态
		why := []string{result.Scope, "task_fit", result.State}
		if result.MemoryType == TypeReviewCheckpoint {
			why = append(why, "review_checkpoint")
		}
		memories = append(memories, ContextMemory{
			MemoryID:    result.MemoryID,
			Type:        result.MemoryType,
			Compressed:  compressed,
			WhyIncluded: why,
		})
		usedIDs = append(usedIDs, result.MemoryID)
		// 收集 constraints 列表——约束类记忆单独提取，用于 Agent prompt 的硬约束注入
		if result.MemoryType == TypeConstraint {
			constraints = append(constraints, compressed)
		}
	}
	// Step 7: 构造 summary——取第一条记忆的压缩内容作为上下文概要
	summary := ""
	if len(memories) > 0 {
		summary = memories[0].Compressed
	}
	return ContextResponse{
		ContextPack: ContextPack{
			Summary:     summary,
			Memories:    memories,
			Constraints: constraints,
			CodeRefs:    []CodeRef{},
		},
		UsedMemoryIDs:    FilterPersistentMemoryIDs(usedIDs),
		RetrievalTraceID: searchResp.Diagnostics.RetrievalTraceID,
		LatencyMS:        time.Since(startedAt).Milliseconds(),
	}, nil
}

// checkpointContext 构造复查检查点的上下文内容
// 将review_checkpoint的结构化信息压缩为可注入Agent prompt的文本
// 包含：检查点类型、目标文档、结论、已确认基线、忽略项、延期项、待处理项、下次复查策略
func (s *Service) checkpointContext(ctx context.Context, result SearchResult) string {
	// 从 repository 获取 review_checkpoint 结构化数据；获取失败或不存在时降级为纯文本 content
	checkpoint, ok, err := s.repo.GetReviewCheckpoint(ctx, result.MemoryID)
	if err != nil || !ok {
		return result.Content
	}
	// 拼接结构化字段为可注入 Agent prompt 的文本，每行一个 key: value 格式
	parts := []string{result.Content}
	// checkpoint_type: 检查点类型（如 design_review, architecture_review）
	if checkpoint.CheckpointType != "" {
		parts = append(parts, "checkpoint_type: "+checkpoint.CheckpointType)
	}
	// target_docs: 被复查的文档列表
	if checkpoint.TargetDocsJSON != "" {
		parts = append(parts, "target_docs: "+checkpoint.TargetDocsJSON)
	}
	// conclusion: 复查结论
	if checkpoint.Conclusion != "" {
		parts = append(parts, "conclusion: "+checkpoint.Conclusion)
	}
	// confirmed_baseline: 已确认的基线内容（不会再次复查）
	if checkpoint.ConfirmedBaselineJSON != "" {
		parts = append(parts, "confirmed_baseline: "+checkpoint.ConfirmedBaselineJSON)
	}
	// ignored_items: 被忽略的检查项（已确认无需修改）
	if checkpoint.IgnoredItemsJSON != "" {
		parts = append(parts, "ignored_items: "+checkpoint.IgnoredItemsJSON)
	}
	// deferred_items: 延期处理的检查项（留到下次复查）
	if checkpoint.DeferredItemsJSON != "" {
		parts = append(parts, "deferred_items: "+checkpoint.DeferredItemsJSON)
	}
	// open_items: 待处理的检查项（需要本次复查解决）
	if checkpoint.OpenItemsJSON != "" {
		parts = append(parts, "open_items: "+checkpoint.OpenItemsJSON)
	}
	// next_review_policy: 下次复查策略（触发条件、时间间隔等）
	if checkpoint.NextReviewPolicyJSON != "" {
		parts = append(parts, "next_review_policy: "+checkpoint.NextReviewPolicyJSON)
	}
	return strings.Join(parts, "\n")
}

// contextSearchScopes 计算上下文检索的作用域列表
// 根据请求中的workspace、project、repo、session信息确定检索范围
// 优先级：project_local > user_global > repo_local > session
func contextSearchScopes(req ContextRequest) []string {
	// 基础 scope: user_global（用户级偏好和决策，始终包含）
	scopes := []string{ScopeUserGlobal}
	// project_local 插入到最前面（优先级最高），需要 workspace + project 标识
	if req.WorkspaceID != "" && req.ProjectID != "" {
		scopes = append([]string{ScopeProjectLocal}, scopes...)
	}
	// repo_local 和 session scope 按需追加，优先级递减
	if req.WorkspaceID != "" && req.RepoID != "" {
		scopes = append(scopes, ScopeRepoLocal)
	}
	if req.WorkspaceID != "" && req.SessionID != "" {
		scopes = append(scopes, ScopeSession)
	}
	return scopes
}

// checkpointSearchScopes 计算复查检查点检索的作用域列表
// 只在project_local和repo_local范围内检索review_checkpoint
func checkpointSearchScopes(req ContextRequest) []string {
	scopes := make([]string, 0, 2)
	if req.WorkspaceID != "" && req.ProjectID != "" {
		scopes = append(scopes, ScopeProjectLocal)
	}
	if req.WorkspaceID != "" && req.RepoID != "" {
		scopes = append(scopes, ScopeRepoLocal)
	}
	return scopes
}

// containsMemoryType 检查搜索结果中是否包含指定记忆类型
func containsMemoryType(results []SearchResult, memoryType string) bool {
	for _, result := range results {
		if result.MemoryType == memoryType {
			return true
		}
	}
	return false
}

// Review 执行 pending memory 查询和状态流转
// 支持六种操作：
// - list: 查询pending_review状态的记忆列表
// - approve: 批准记忆，状态转为stable，记录用户确认
// - reject: 拒绝记忆，状态转为archived
// - archive: 归档记忆，状态转为archived
// - edit: 编辑记忆内容，更新版本号和search_text
// - delete: 删除记忆，写入tombstone，删除FTS条目
// Review 执行 pending memory 查询和状态流转。
// 六种操作：
//   - list：查询 pending_review 状态的记忆列表，支持 scope 过滤
//   - approve：pending_review -> stable，记录 user_confirmed=true
//   - reject：-> archived，记录审核意见
//   - archive：-> archived，记录审核意见
//   - edit：原地更新内容，version+1，同步 FTS，记录编辑历史
//   - delete：-> deleted，写入 tombstone，删除 FTS，记录审核历史
func (s *Service) Review(ctx context.Context, req ReviewRequest) (ReviewResponse, error) {
	s.logger.Info("review started",
		"action", req.Action,
		"memory_id", req.MemoryID,
		"workspace_id", req.WorkspaceID,
	)
	switch req.Action {
	// list: 查询 pending_review 状态的记忆，支持 scope 过滤和分页
	case "list":
		if req.Limit <= 0 {
			req.Limit = s.cfg.Retrieval.DefaultLimit
		}
		items, err := s.repo.ListReview(ctx, req)
		if err != nil {
			s.logger.Error("review list failed", "error", err)
		} else {
			s.logger.Debug("review list completed", "count", len(items))
		}
		return ReviewResponse{Results: items}, err
	// approve: pending_review -> stable，记录 user_confirmed=true（用户确认的稳定记忆）
	case "approve":
		item, err := s.repo.Approve(ctx, req.MemoryID, req.Reviewer, req.Feedback)
		if err != nil {
			s.logger.Error("review approve failed", "memory_id", req.MemoryID, "error", err)
		} else {
			s.logger.Info("review approved", "memory_id", item.ID, "new_state", item.State)
			s.recordAccessFeedback(ctx, item.ID, "user_confirmed", item.SourceQuality)
			s.enqueueEmbeddingBestEffort(ctx, item.ID, "review_approve")
		}
		return ReviewResponse{MemoryID: item.ID, State: item.State, UserConfirmed: item.UserConfirmed}, err
	// reject/archive: -> archived，记录审核意见
	case "reject", "archive":
		item, err := s.repo.RejectOrArchive(ctx, req.MemoryID, req.Action, req.Reviewer, req.Feedback)
		if err != nil {
			s.logger.Error("review reject/archive failed", "action", req.Action, "memory_id", req.MemoryID, "error", err)
		} else {
			s.logger.Info("review rejected/archived", "action", req.Action, "memory_id", item.ID, "new_state", item.State)
			s.recordAccessFeedback(ctx, item.ID, "user_rejected", item.SourceQuality)
		}
		return ReviewResponse{MemoryID: item.ID, State: item.State, UserConfirmed: item.UserConfirmed}, err
	// edit: 原地更新内容，version+1，同步 FTS 索引，记录编辑历史
	case "edit":
		req.EditContent = strings.TrimSpace(req.EditContent)
		if req.EditContent == "" {
			return ReviewResponse{}, fmt.Errorf("VALIDATION_FAILED: edit_content is required")
		}
		// 编辑后的内容同样需要通过内容最小化检查（长度、关键词数、salient span 数）
		if err := ingest.CheckMinimizedContent(s.cfg.Memory, ingest.MinimizationInput{
			Content: req.EditContent,
		}); err != nil {
			s.logger.Error("review edit content check failed", "memory_id", req.MemoryID, "error", err)
			return ReviewResponse{}, err
		}
		// 获取原记忆对象，用于继承 title 和构建新的 search_text
		item, err := s.repo.Get(ctx, req.MemoryID)
		if err != nil {
			s.logger.Error("review edit get memory failed", "memory_id", req.MemoryID, "error", err)
			return ReviewResponse{}, err
		}
		// 用新内容重建 FTS 文档（保留原 title，content/normalized_content 替换为编辑后的内容）
		searchText := ingest.BuildSearchText(ingest.SearchTextInput{
			Title:             item.Title,
			Content:           req.EditContent,
			NormalizedContent: req.EditContent,
		})
		updated, err := s.repo.Edit(ctx, req.MemoryID, req.EditContent, req.Reviewer, req.Feedback, searchText)
		if err != nil {
			s.logger.Error("review edit failed", "memory_id", req.MemoryID, "error", err)
		} else {
			s.logger.Info("review edited", "memory_id", updated.ID, "new_version", updated.Version)
			s.enqueueEmbeddingBestEffort(ctx, updated.ID, "review_edit")
		}
		return ReviewResponse{MemoryID: updated.ID, State: updated.State, UserConfirmed: updated.UserConfirmed}, err
	// delete: -> deleted，写入 tombstone，删除 FTS 条目，记录审核历史
	case "delete":
		item, err := s.repo.Delete(ctx, req.MemoryID, req.Reviewer, req.Feedback)
		if err != nil {
			s.logger.Error("review delete failed", "memory_id", req.MemoryID, "error", err)
		} else {
			s.logger.Info("review deleted", "memory_id", item.ID)
		}
		return ReviewResponse{MemoryID: item.ID, State: item.State, UserConfirmed: item.UserConfirmed}, err
	default:
		s.logger.Error("review unsupported action", "action", req.Action)
		return ReviewResponse{}, fmt.Errorf("VALIDATION_FAILED: unsupported review action %q", req.Action)
	}
}

// buildCheckpoint 构造复查检查点
// 将ReviewCheckpointInput转换为持久化的ReviewCheckpoint结构
// 必填字段：checkpoint_type、conclusion、target_docs
func buildCheckpoint(memoryID string, item MemoryItem, input ReviewCheckpointInput, now time.Time) (*ReviewCheckpoint, error) {
	// 三个必填字段：checkpoint_type（类型）、conclusion（结论）、target_docs（被复查文档列表）
	if input.CheckpointType == "" || input.Conclusion == "" || len(input.TargetDocs) == 0 {
		return nil, fmt.Errorf("VALIDATION_FAILED: checkpoint_type, conclusion and target_docs are required")
	}
	id, err := idgen.New("rcp")
	if err != nil {
		return nil, err
	}
	// 将所有结构化输入序列化为 JSON 字符串，用于持久化存储
	reviewIntent, err := toJSON(input.ReviewIntent)
	if err != nil {
		return nil, err
	}
	targetDocs, err := toJSON(input.TargetDocs)
	if err != nil {
		return nil, err
	}
	targetSections, err := toJSON(input.TargetSections)
	if err != nil {
		return nil, err
	}
	targetHashes, err := toJSON(input.TargetHashes)
	if err != nil {
		return nil, err
	}
	confirmedBaseline, err := toJSON(input.ConfirmedBaseline)
	if err != nil {
		return nil, err
	}
	ignoredItems, err := toJSON(input.IgnoredItems)
	if err != nil {
		return nil, err
	}
	deferredItems, err := toJSON(input.DeferredItems)
	if err != nil {
		return nil, err
	}
	openItems, err := toJSON(input.OpenItems)
	if err != nil {
		return nil, err
	}
	nextPolicy, err := toJSON(input.NextReviewPolicy)
	if err != nil {
		return nil, err
	}
	// 继承 memory_item 的隔离标识（workspace/project/repo/session/task），确保检查点与记忆的作用域一致
	return &ReviewCheckpoint{
		ID:                    id,
		MemoryID:              memoryID,
		WorkspaceID:           item.WorkspaceID,
		ProjectID:             item.ProjectID,
		RepoID:                item.RepoID,
		SessionID:             item.SessionID,
		TaskID:                item.TaskID,
		CheckpointType:        input.CheckpointType,
		ReviewIntentJSON:      reviewIntent,
		TargetDocsJSON:        targetDocs,
		TargetSectionsJSON:    targetSections,
		TargetHashesJSON:      targetHashes,
		Conclusion:            input.Conclusion,
		ConfirmedBaselineJSON: confirmedBaseline,
		IgnoredItemsJSON:      ignoredItems,
		DeferredItemsJSON:     deferredItems,
		OpenItemsJSON:         openItems,
		NextReviewPolicyJSON:  nextPolicy,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}

// sourceQuality 根据来源类型返回来源质量评分
// 用于保留分数计算，值越大表示来源越可靠
// 评分规则：
// - user_declared/user_confirmed: 1.0（用户声明或确认，最可靠）
// - manual_review: 0.8（手动审核，较可靠）
// - 其他: 0.7（默认值）
func sourceQuality(sourceType string) float64 {
	switch sourceType {
	case "user_declared", "user_confirmed":
		return 1.0
	case "manual_review":
		return 0.8
	default:
		return 0.7
	}
}

// defaultDecayRate 根据记忆类型返回默认衰减率
// 衰减率控制记忆的遗忘速度，值越大衰减越快
// 评分规则：
// - decision/constraint/preference: 0.3（重要决策和偏好，衰减慢）
// - failure/procedure: 0.45（失败经验和流程，衰减中等）
// - temporary_state: 1.2（临时状态，衰减快）
// - 其他: 0.8（默认值）
func firstPositiveFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func defaultDecayRate(memoryType string) float64 {
	switch memoryType {
	case TypeDecision, TypeConstraint, TypePreference:
		return 0.3
	case TypeFailure, TypeProcedure:
		return 0.45
	case TypeTemporaryState:
		return 1.2
	default:
		return 0.8
	}
}

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// isDesignReviewTask 判断是否为设计复查任务
// 通过关键词匹配识别设计复查、架构评审、文档完整性检查等任务
// 匹配关键词：设计复查、架构评审、文档完整性、review
func isDesignReviewTask(task string) bool {
	task = strings.ToLower(task)
	return strings.Contains(task, "设计复查") ||
		strings.Contains(task, "架构评审") ||
		strings.Contains(task, "文档完整性") ||
		strings.Contains(task, "review")
}

// compress 按token预算压缩内容
// 如果内容超过预算，截断并添加省略号
// 设计说明：使用字符数近似 token 数，后续可替换为 tokenizer
func compress(content string, budget int) string {
	content = strings.TrimSpace(content)
	if budget <= 0 || content == "" {
		return ""
	}
	// 使用 rune 切片而非 byte 切片，确保中文等多字节字符不会被截断成乱码
	runes := []rune(content)
	if len(runes) <= budget {
		return content
	}
	// budget <= 3 时无法容纳 "..." 后缀，直接截断
	if budget <= 3 {
		return string(runes[:budget])
	}
	// 预留 3 个 rune 给 "..." 省略号，表示内容被截断
	return string(runes[:budget-3]) + "..."
}

// decodeStringSlice 解码JSON字符串切片
func decodeStringSlice(raw string) []string {
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
