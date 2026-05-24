package memory

import (
	"encoding/json"
	"time"
)

// ============================================================================
// 作用域（Scope）常量定义
// 作用域决定了记忆的可见范围和隔离级别，是记忆系统的核心隔离机制
// ============================================================================
const (
	// ScopeUserGlobal 用户全局作用域
	// 用于用户长期偏好、跨项目工作方式等，按用户隔离，不绑定具体项目
	// 例如：用户偏好先进行架构分析再给实现方案
	ScopeUserGlobal = "user_global"

	// ScopeProjectLocal 项目本地作用域
	// 用于项目级记忆，如项目事实、架构决策、项目约束等
	// 必须按 workspace_id + project_id 隔离，不能跨项目共享
	ScopeProjectLocal = "project_local"

	// ScopeRepoLocal 仓库本地作用域
	// 用于仓库级记忆，如代码引用、与具体repo绑定的经验
	// 必须按 workspace_id + repo_id 隔离
	ScopeRepoLocal = "repo_local"

	// ScopeSession 会话作用域
	// 用于当前任务状态、短期上下文、未巩固的临时信息
	// 必须按 workspace_id + session_id 隔离，不进入长期稳定记忆
	ScopeSession = "session"
)

// ============================================================================
// 记忆类型（Memory Type）常量定义
// 记忆类型决定了记忆的内容分类和默认处理策略
// ============================================================================
const (
	// TypePreference 用户偏好类型
	// 记录用户的工程偏好、沟通偏好等，长期保存，可用户编辑
	// 例如：用户偏好技术方案先分析架构边界、风险和工程落地
	TypePreference = "preference"

	// TypeRequirement 需求类型
	// 记录用户明确提出的系统需求、阶段目标和验收条件
	TypeRequirement = "requirement"

	// TypeDecision 架构决策类型
	// 记录架构决策和决策原因，默认进入pending_review状态，长期保存
	// 例如：项目决定暂不引入Kafka，原因是当前异步需求不足
	TypeDecision = "decision"

	// TypeConstraint 约束类型
	// 记录项目约束、安全约束、技术边界等，默认进入pending_review状态，长期保存
	// 例如：认证模块要求请求内同步完成校验
	TypeConstraint = "constraint"

	// TypeAssumption 设计假设类型
	// 记录架构、分期或实现方案成立的前提假设
	TypeAssumption = "assumption"

	// TypeOpenIssue 开放问题类型
	// 记录待确认问题、未决风险和需要后续处理的设计点
	TypeOpenIssue = "open_issue"

	// TypeFailure 失败经验类型
	// 记录失败经验、事故结论、踩坑记录等，高价值长期保存
	// 例如：上次认证问题根因是过期时间边界判断错误
	TypeFailure = "failure"

	// TypeProjectFact 项目事实类型
	// 记录项目事实、模块职责、部署方式等，中长期保存
	// 例如：项目使用Go语言，部署在Kubernetes集群
	TypeProjectFact = "project_fact"

	// TypeProcedure 流程类型
	// 记录调试流程、评审流程、实施步骤等，长期保存
	// 例如：慢请求问题要先看metrics、trace和DB pool
	TypeProcedure = "procedure"

	// TypeTemporaryState 临时状态类型
	// 记录临时任务状态，默认5天后自动清理
	// 例如：当前正在修复的bug状态
	TypeTemporaryState = "temporary_state"

	// TypeSessionSummary 会话摘要类型
	// 记录 session/task 结果摘要，只服务短中期连续性
	TypeSessionSummary = "session_summary"

	// TypeReviewCheckpoint 设计复查检查点类型
	// 记录设计复查、架构评审、文档校验后的结构化检查点
	// 用于压缩重复设计复查的历史上下文，中长期保存
	TypeReviewCheckpoint = "review_checkpoint"
)

// ============================================================================
// 记忆状态（State）常量定义
// 状态机驱动记忆的生命周期流转
// ============================================================================
const (
	// StateProvisional 临时状态
	// 自动捕获的记忆首先进入此状态，需要进一步巩固
	StateProvisional = "provisional"

	// StatePendingReview 待审核状态
	// 高影响记忆（如架构决策、安全约束）需要用户确认
	StatePendingReview = "pending_review"

	// StateStable 稳定状态
	// 经过确认或巩固的记忆，可被正常检索和使用
	StateStable = "stable"

	// StateArchived 归档状态
	// 过期或不再活跃的记忆，默认不参与检索
	StateArchived = "archived"

	// StateDeleted 删除状态
	// 终态，不可恢复，只保留最小墓碑标记
	StateDeleted = "deleted"
)

// ============================================================================
// 记忆层级（Tier）常量定义
// 层级决定了记忆的保留时长和衰减策略
// ============================================================================
const (
	// TierTemporary 临时层级
	// 保留5天，适用于临时任务状态和短期上下文
	TierTemporary = "temporary"

	// TierShortTerm 短期层级
	// 适用于重复失败、会话摘要和未确认但短期有用的候选
	TierShortTerm = "short_term"

	// TierLongTerm 长期层级
	// 保留365天，适用于重要决策、失败经验等
	TierLongTerm = "long_term"

	// TierDurable 持久层级
	// 默认不自动删除，适用于用户显式声明、pinned记忆等
	TierDurable = "durable"

	// TierArchived 归档层级
	// 配合 state=archived 排除默认检索
	TierArchived = "archived"
)

// ============================================================================
// 输入结构体定义
// ============================================================================

// EvidenceInput 证据输入结构体
// 用于 memory.remember 显式写入时携带的证据信息
// 设计原则：只保存解释后的语句和关键片段，不保存完整原文
type EvidenceInput struct {
	// InterpretedStatement 解释后的语句
	// 对原始内容的理解和总结，用于后续检索和上下文注入
	// 例如："用户明确要求技术方案先分析架构边界、风险和工程落地"
	InterpretedStatement string `json:"interpreted_statement"`

	// Keywords 关键词列表
	// 用于FTS全文检索和语义匹配
	// 例如：["架构边界", "风险", "工程落地"]
	Keywords []string `json:"keywords"`

	// SalientSpans 显著片段列表
	// 原始内容中的关键片段，用于证据展示和检索
	// 例如：["以后技术方案先分析架构边界、风险和工程落地"]
	SalientSpans []string `json:"salient_spans"`

	// SourceRef 来源引用
	// 记录证据的来源信息，如文件路径、commit hash、符号等
	// 不保存完整源码，只保存定位信息
	SourceRef map[string]any `json:"source_ref"`
}

// ReviewCheckpointInput 设计复查检查点输入结构体
// 用于 P1 手动写入复查检查点，记录设计复查的结构化结论
// 设计约束：不允许保存完整文档正文，只保存路径、章节、hash、摘要和结论
type ReviewCheckpointInput struct {
	// CheckpointType 检查点类型
	// 可选值：architecture_design_review（架构设计复查）、
	//         iteration_plan_review（迭代计划复查）、
	//         implementation_design_review（实现设计复查）、
	//         requirements_review（需求复查）
	CheckpointType string `json:"checkpoint_type"`

	// ReviewIntent 复查意图列表
	// 描述本次复查的目标，如逻辑完整性、业务闭环、分期可验收性等
	// 例如：["logic_completeness", "business_loop", "phase_consistency"]
	ReviewIntent []string `json:"review_intent"`

	// TargetDocs 目标文档列表
	// 记录复查的文档信息，包括路径、角色、内容hash、修改时间
	// 不保存完整文档正文，只保存元数据
	TargetDocs []map[string]any `json:"target_docs"`

	// TargetSections 目标章节列表
	// 记录复查的具体章节，用于下次复查时定位变化部分
	TargetSections []map[string]any `json:"target_sections"`

	// TargetHashes 目标哈希列表
	// 记录文档或章节的内容hash，用于检测变化
	TargetHashes []map[string]any `json:"target_hashes"`

	// Conclusion 复查结论
	// 可选值：no_major_gap（无重大缺失）、has_major_gap（有重大缺失）、
	//         supplemented（已补充）、deferred（延期处理）、baseline_frozen（基线已冻结）
	Conclusion string `json:"conclusion"`

	// ConfirmedBaseline 已确认基线列表
	// 记录用户已确认的设计基线，下次复查时不重复提出
	// 例如：["一期只做AI Coding Agent记忆层", "Code Index与Memory分层"]
	ConfirmedBaseline []string `json:"confirmed_baseline"`

	// IgnoredItems 已忽略项列表
	// 记录用户明确忽略或延期处理的问题，避免下次复查重复提出
	IgnoredItems []string `json:"ignored_items"`

	// DeferredItems 延期项列表
	// 记录决定延期处理的问题，与IgnoredItems类似但语义不同
	DeferredItems []string `json:"deferred_items"`

	// OpenItems 待处理项列表
	// 记录仍需关注的开放问题
	OpenItems []string `json:"open_items"`

	// NextReviewPolicy 下次复查策略
	// 描述下次复查的重点和读取策略
	// 例如：{"focus": "major_logic_gap_only", "read_strategy": "checkpoint_first_then_current_doc_or_diff"}
	NextReviewPolicy map[string]any `json:"next_review_policy"`
}

// RememberRequest memory.remember 请求结构体
// 用于用户或Agent显式写入高价值记忆
// 支持多种记忆类型和作用域，可携带证据和复查检查点
type RememberRequest struct {
	// Content 记忆内容
	// 必填，记忆的核心内容，不超过memory.max_content_chars限制
	Content string `json:"content"`

	// Title 记忆标题
	// 可选，用于展示和检索，如"用户偏好：先架构分析再实现"
	Title string `json:"title"`

	// MemoryType 记忆类型
	// 必填，决定记忆的分类和默认处理策略
	// 可选值：preference、requirement、decision、constraint、assumption、open_issue、
	//         failure、project_fact、procedure、temporary_state、session_summary、review_checkpoint
	MemoryType string `json:"memory_type"`

	// Scope 作用域
	// 必填，决定记忆的可见范围和隔离级别
	// 可选值：user_global、project_local、repo_local、session
	Scope string `json:"scope"`

	// WorkspaceID 工作空间ID
	// project_local、repo_local、session作用域必填
	WorkspaceID string `json:"workspace_id"`

	// UserID 用户ID
	// user_global作用域必填，默认使用local_default_user
	UserID string `json:"user_id"`

	// ProjectID 项目ID
	// project_local作用域必填
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID
	// repo_local作用域必填
	RepoID string `json:"repo_id"`

	// SessionID 会话ID
	// session作用域必填
	SessionID string `json:"session_id"`

	// TaskID 任务ID
	// 可选，用于任务级归因
	TaskID string `json:"task_id"`

	// SourceType 来源类型
	// 记录记忆的来源，影响默认状态和强化策略
	// 可选值：user_declared（用户声明）、user_confirmed（用户确认）、
	//         manual_review（手动审核）、agent_summary（Agent总结）等
	SourceType string `json:"source_type"`

	// Importance 重要性
	// 范围0-1，默认0.5，影响准入评分和检索排序
	Importance float64 `json:"importance"`

	// Confidence 置信度
	// 范围0-1，默认0.7，影响保留分数和检索排序
	Confidence float64 `json:"confidence"`

	// Pinned 是否置顶
	// 置顶记忆不会被自动归档或删除，tier设为durable
	Pinned bool `json:"pinned"`

	// Tags 标签列表
	// 用于分类和过滤，如["communication", "architecture"]
	Tags []string `json:"tags"`

	// Keywords 关键词列表
	// 用于FTS全文检索，不超过memory.max_keyword_count限制
	Keywords []string `json:"keywords"`

	// Entities 实体列表
	// 记忆中提到的实体，如人名、项目名、技术名等
	Entities []string `json:"entities"`

	// RetrievalCues 检索线索列表
	// 用于提高检索命中率的补充关键词
	RetrievalCues []string `json:"retrieval_cues"`

	// ReviewCheckpoint 复查检查点
	// 当memory_type为review_checkpoint时必填
	// 记录设计复查的结构化结论
	ReviewCheckpoint *ReviewCheckpointInput `json:"review_checkpoint"`

	// Evidence 证据
	// 记忆的支撑证据，用于解释记忆的来源和可信度
	Evidence EvidenceInput `json:"evidence"`
}

// RememberResponse memory.remember 响应结构体
type RememberResponse struct {
	// MemoryID 记忆ID
	// 新创建记忆的唯一标识
	MemoryID string `json:"memory_id"`

	// State 记忆状态
	// 根据记忆类型和来源类型决定的初始状态
	// 例如：user_declared的preference记忆默认为stable
	State string `json:"state"`

	// Tier 记忆层级
	// 根据记忆类型和来源类型决定的初始层级
	// 例如：user_declared的记忆默认为durable
	Tier string `json:"tier"`

	// Deduped 是否去重
	// 如果存在相同内容的记忆，返回true
	Deduped bool `json:"deduped"`
}

// SearchRequest memory.search 请求结构体
// 用于按任务、查询、scope检索相关记忆
type SearchRequest struct {
	// Query 查询文本
	// 必填，用于FTS全文检索和语义匹配
	Query string `json:"query"`

	// WorkspaceID 工作空间ID
	// 用于scope过滤
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID
	// 用于project_local作用域过滤
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID
	// 用于repo_local作用域过滤
	RepoID string `json:"repo_id"`

	// SessionID 会话ID
	// 用于session作用域过滤
	SessionID string `json:"session_id"`

	// Scope 作用域过滤列表
	// 可选，限定搜索范围，如["project_local", "user_global"]
	Scope []string `json:"scope"`

	// MemoryTypes 记忆类型过滤列表
	// 可选，限定搜索的记忆类型，如["decision", "constraint", "failure"]
	MemoryTypes []string `json:"memory_types"`

	// Limit 结果数量限制
	// 默认10，最大可配置
	Limit int `json:"limit"`

	// IncludeArchived 是否包含归档记忆
	// 默认false，只返回stable和pending_review状态的记忆
	IncludeArchived bool `json:"include_archived"`

	// IncludeEvidence 是否包含证据摘要
	// 默认false，返回结果中不包含evidence_refs
	IncludeEvidence bool `json:"include_evidence"`

	// IncludeCodeRefs 是否包含代码引用
	// 默认false；P4-C3 后 code_task intent 即使未显式开启也可返回相关 code_ref
	IncludeCodeRefs bool `json:"include_code_refs"`
}

// SearchResult 搜索结果项
type SearchResult struct {
	// MemoryID 记忆ID
	MemoryID string `json:"memory_id"`

	// MemoryType 记忆类型
	MemoryType string `json:"memory_type"`

	// Scope 作用域
	Scope string `json:"scope"`

	// Title 记忆标题
	Title string `json:"title,omitempty"`

	// Content 记忆内容
	Content string `json:"content"`

	// Score 检索分数
	// 综合BM25、scope、confidence、importance等因素计算
	Score float64 `json:"score"`

	// Confidence 置信度
	Confidence float64 `json:"confidence"`

	// State 记忆状态
	State string `json:"state"`

	// Tier 记忆层级
	Tier string `json:"tier"`

	// EvidenceRefs 证据引用列表
	// 可选，当IncludeEvidence=true时返回
	EvidenceRefs []string `json:"evidence_refs,omitempty"`

	// ScoreBreakdown P4 检索分数拆解
	// 可选，P4 Retrieval Orchestrator 接入后返回，用于解释最终排序来源
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`

	// WhyIncluded P4 注入原因
	// 可选，解释该记忆为什么被召回或注入上下文
	WhyIncluded []string `json:"why_included,omitempty"`

	// CodeRefs P4 代码引用
	// 可选，只返回文件路径、symbol、hash和解析状态，不返回源码
	CodeRefs []CodeRef `json:"code_refs,omitempty"`
}

// ScoreBreakdown P4 检索分数拆解。
// 设计约束：字段是可解释排序的稳定 API，后续算法可调整权重但不应改变字段语义。
type ScoreBreakdown struct {
	// BM25 FTS/BM25 召回分
	BM25 float64 `json:"bm25"`

	// Semantic 语义向量分；未启用 embedding/vector 时为 0
	Semantic float64 `json:"semantic"`

	// TaskFit 当前任务匹配度
	TaskFit float64 `json:"task_fit"`

	// ScopeFit scope 匹配度
	ScopeFit float64 `json:"scope_fit"`

	// Retention 记忆保留价值分
	Retention float64 `json:"retention"`

	// RelationSupport 关系支持分
	RelationSupport float64 `json:"relation_support"`

	// SourceQuality 来源质量分
	SourceQuality float64 `json:"source_quality"`

	// Recency 近期活跃分
	Recency float64 `json:"recency"`

	// ConflictPenalty 冲突惩罚
	ConflictPenalty float64 `json:"conflict_penalty"`

	// StalenessPenalty 过期或代码引用失效惩罚
	StalenessPenalty float64 `json:"staleness_penalty"`

	// ContextCostPenalty 上下文成本惩罚
	ContextCostPenalty float64 `json:"context_cost_penalty"`

	// Final 最终排序分
	Final float64 `json:"final"`
}

// CodeRef P4 代码引用响应模型。
// 设计约束：只保存和返回定位、hash、摘要和解析状态，不保存源码正文或调用关系事实。
type CodeRef struct {
	// ID code_ref 记录 ID
	ID string `json:"id,omitempty"`

	// MemoryID 关联的 memory_id
	MemoryID string `json:"memory_id,omitempty"`

	// RepoID 仓库 ID
	RepoID string `json:"repo_id"`

	// CommitHash 代码引用对应的 commit hash，可为空表示当前工作区
	CommitHash string `json:"commit_hash,omitempty"`

	// FilePath 仓库内相对路径
	FilePath string `json:"file_path,omitempty"`

	// Symbol 代码符号名
	Symbol string `json:"symbol,omitempty"`

	// LineStart 起始行号
	LineStart int `json:"line_start,omitempty"`

	// LineEnd 结束行号
	LineEnd int `json:"line_end,omitempty"`

	// ContentHash 引用内容 hash
	ContentHash string `json:"content_hash,omitempty"`

	// RefSummary 引用摘要，不包含源码全文
	RefSummary string `json:"ref_summary,omitempty"`

	// ResolveStatus 解析状态，如 resolved/unresolved/stale/missing/ambiguous
	ResolveStatus string `json:"resolve_status"`
}

const (
	// CodeRefStatusUnresolved 表示 code_ref 尚未被 Code Index 解析。
	CodeRefStatusUnresolved = "unresolved"

	// CodeRefStatusResolved 表示 code_ref 已成功解析到当前代码位置。
	CodeRefStatusResolved = "resolved"

	// CodeRefStatusStale 表示 code_ref 的 hash 或位置已经过期，但仍可作为历史引用。
	CodeRefStatusStale = "stale"

	// CodeRefStatusMissing 表示 code_ref 指向的文件或符号已经不存在。
	CodeRefStatusMissing = "missing"

	// CodeRefStatusAmbiguous 表示 code_ref 解析到多个候选位置，不能安全自动选择。
	CodeRefStatusAmbiguous = "ambiguous"
)

// CodeRefQuery 是 code_ref 诊断和检索查询条件。
// 查询必须按 memory_id 或 repo_id + file_path 收敛，避免扫描完整代码引用表。
type CodeRefQuery struct {
	// MemoryID 关联 memory_id，可单独作为查询条件。
	MemoryID string `json:"memory_id,omitempty"`

	// RepoID 仓库 ID；按文件查询时必填。
	RepoID string `json:"repo_id,omitempty"`

	// FilePath 仓库内相对路径；按文件查询时必填。
	FilePath string `json:"file_path,omitempty"`

	// Symbol 符号名，可选过滤条件。
	Symbol string `json:"symbol,omitempty"`

	// ResolveStatus 解析状态，可选过滤条件。
	ResolveStatus string `json:"resolve_status,omitempty"`

	// Limit 返回数量限制。
	Limit int `json:"limit,omitempty"`
}

// SearchDiagnostics 搜索诊断信息
// 用于评估检索质量和性能
type SearchDiagnostics struct {
	// RetrievalTraceID 检索追踪ID
	// 用于关联memory_access_log中的访问记录
	RetrievalTraceID string `json:"retrieval_trace_id,omitempty"`

	// FTSHits FTS命中数
	// FTS5全文检索匹配的记忆数量
	FTSHits int `json:"fts_hits"`

	// FilteredCount 过滤后数量
	// 经过scope、state、type过滤后的结果数量
	FilteredCount int `json:"filtered_count"`

	// LatencyMS 检索延迟（毫秒）
	// 从请求到响应的总耗时
	LatencyMS int64 `json:"latency_ms"`

	// Fallback 降级策略
	// 当某些检索能力不可用时的降级方式
	Fallback string `json:"fallback"`

	// RetrievalMode P4 检索模式
	// 可选，P4 Retrieval Orchestrator 接入后返回，如 fts_relation/code_aware
	RetrievalMode string `json:"retrieval_mode,omitempty"`

	// RetrievalIntent P4 检索意图
	// 可选，表示本次请求被识别为通用检索、代码任务、架构复查等
	RetrievalIntent string `json:"retrieval_intent,omitempty"`

	// UsedFTS 是否使用 FTS 召回
	UsedFTS bool `json:"used_fts"`

	// UsedVector 是否使用向量召回
	UsedVector bool `json:"used_vector"`

	// UsedRelation 是否使用关系扩展
	UsedRelation bool `json:"used_relation"`

	// UsedCodeIndex 是否使用 Code Index
	UsedCodeIndex bool `json:"used_code_index"`

	// UsedDocIndex 是否使用 Doc Index
	UsedDocIndex bool `json:"used_doc_index"`

	// FallbackReasons P4 降级原因列表
	// 可选，用于区分 vector_disabled、code_index_unavailable 等多种降级原因
	FallbackReasons []string `json:"fallback_reason,omitempty"`
}

// SearchResponse memory.search 响应结构体
type SearchResponse struct {
	// Results 搜索结果列表
	Results []SearchResult `json:"results"`

	// Diagnostics 诊断信息
	Diagnostics SearchDiagnostics `json:"diagnostics"`
}

// ContextRequest memory.context 请求结构体
// 用于根据当前任务构造可注入Agent prompt的压缩上下文包
type ContextRequest struct {
	// Task 任务描述
	// 必填，用于检索相关记忆和构造上下文
	Task string `json:"task"`

	// WorkspaceID 工作空间ID
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID
	RepoID string `json:"repo_id"`

	// SessionID 会话ID
	SessionID string `json:"session_id"`

	// AgentType Agent类型
	// 可选，如codex、claude_code、cursor
	AgentType string `json:"agent_type"`

	// TokenBudget Token预算
	// 上下文包的Token数量限制，默认1800
	TokenBudget int `json:"token_budget"`

	// IncludeCodeRefs 是否包含代码引用
	// 默认false，不返回code_refs
	IncludeCodeRefs bool `json:"include_code_refs"`

	// IncludeEvidenceSummary 是否包含证据摘要
	// 默认false，不返回证据详情
	IncludeEvidenceSummary bool `json:"include_evidence_summary"`
}

// ContextPack 上下文包
// 构造好的可注入Agent prompt的压缩记忆集合
type ContextPack struct {
	// Summary 上下文摘要
	// 对注入记忆的整体总结
	Summary string `json:"summary"`

	// Memories 注入的记忆列表
	// 按优先级和相关性排序的记忆
	Memories []ContextMemory `json:"memories"`

	// Constraints 约束列表
	// 当前项目的约束条件
	Constraints []string `json:"constraints"`

	// CodeRefs 代码引用列表
	// 与任务相关的代码引用
	CodeRefs []CodeRef `json:"code_refs"`

	// ReviewStrategy 设计复查策略
	// 可选，仅设计复查任务返回，用于说明本次复查应关注全量文档、变化章节或 checkpoint 差异
	ReviewStrategy *ReviewStrategy `json:"review_strategy,omitempty"`
}

// ContextMemory 上下文记忆项
type ContextMemory struct {
	// MemoryID 记忆ID
	MemoryID string `json:"memory_id"`

	// Type 记忆类型
	Type string `json:"type"`

	// Compressed 压缩后的内容
	// 按token budget裁剪后的记忆内容
	Compressed string `json:"compressed"`

	// WhyIncluded 包含原因列表
	// 解释为什么这条记忆被注入上下文
	// 例如：["task_match", "failure_memory", "high_retention_score"]
	WhyIncluded []string `json:"why_included"`

	// ScoreBreakdown P4 检索分数拆解
	// 可选，用于解释该记忆为什么被注入上下文
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`

	// Unconfirmed 是否未确认
	// pending_review/provisional 记忆注入时需要显式标记，避免 Agent 当作稳定事实
	Unconfirmed bool `json:"unconfirmed,omitempty"`

	// Historical 是否历史归档信息
	// archived 记忆只有在历史查询场景中才允许以摘要形式注入
	Historical bool `json:"historical,omitempty"`

	// SessionOnly 是否仅当前 session 有效
	SessionOnly bool `json:"session_only,omitempty"`
}

// ReviewStrategy P4 设计复查策略。
// 用于告诉调用方本次复查应关注哪些文档或章节，避免重复展开已经确认的历史结论。
type ReviewStrategy struct {
	// Mode 复查模式，如 full_document/changed_sections/checkpoint_only
	Mode string `json:"mode"`

	// CheckpointID 命中的 review_checkpoint memory_id
	CheckpointID string `json:"checkpoint_id,omitempty"`

	// TargetDocs 目标文档路径
	TargetDocs []string `json:"target_docs,omitempty"`

	// ChangedSections 发生变化的章节
	ChangedSections []string `json:"changed_sections,omitempty"`

	// IgnoredItemsPolicy 已确认忽略项策略
	IgnoredItemsPolicy string `json:"ignored_items_policy,omitempty"`
}

// ContextDiagnostics P4 context 构造诊断。
// 记录预算分配、降级原因和检索模式，不包含完整 prompt 或源码。
type ContextDiagnostics struct {
	// RetrievalIntent P4 检索意图
	RetrievalIntent string `json:"retrieval_intent,omitempty"`

	// RetrievalMode P4 检索模式
	RetrievalMode string `json:"retrieval_mode,omitempty"`

	// UsedDocIndex 是否使用 Doc Index 辅助构造复查策略
	UsedDocIndex bool `json:"used_doc_index,omitempty"`

	// BudgetAllocation 预算分配
	BudgetAllocation map[string]int `json:"budget_allocation,omitempty"`

	// FallbackReasons 降级原因列表
	FallbackReasons []string `json:"fallback_reason,omitempty"`
}

// ContextResponse memory.context 响应结构体
type ContextResponse struct {
	// ContextPack 上下文包
	ContextPack ContextPack `json:"context_pack"`

	// UsedMemoryIDs 使用的记忆ID列表
	// 实际注入上下文的记忆ID
	UsedMemoryIDs []string `json:"used_memory_ids"`

	// RetrievalTraceID 检索追踪ID
	// 用于关联memory_access_log中的访问记录
	RetrievalTraceID string `json:"retrieval_trace_id,omitempty"`

	// LatencyMS 构造延迟（毫秒）
	LatencyMS int64 `json:"latency_ms"`

	// Diagnostics P4 context 构造诊断
	// 可选，P4 Retrieval Orchestrator 接入后返回
	Diagnostics *ContextDiagnostics `json:"diagnostics,omitempty"`
}

// ReviewRequest memory.review 请求结构体
// 支持 list/approve/reject/edit/archive/delete 六种操作
type ReviewRequest struct {
	// Action 操作类型
	// 必填，可选值：list（列表）、approve（批准）、reject（拒绝）、
	//         edit（编辑）、archive（归档）、delete（删除）
	Action string `json:"action"`

	// WorkspaceID 工作空间ID
	// list操作时用于过滤
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目ID
	// list操作时用于过滤
	ProjectID string `json:"project_id"`

	// RepoID 仓库ID
	// list操作时用于过滤
	RepoID string `json:"repo_id"`

	// State 状态过滤
	// list操作时用于过滤，默认pending_review
	State string `json:"state"`

	// Limit 结果数量限制
	// list操作时使用，默认20
	Limit int `json:"limit"`

	// MemoryID 记忆ID
	// approve/reject/edit/archive/delete操作时必填
	MemoryID string `json:"memory_id"`

	// EditContent 编辑后的内容
	// edit操作时必填，用于更新记忆内容
	EditContent string `json:"edit_content"`

	// Feedback 反馈信息
	// approve/reject/edit操作时可选，记录审核意见
	Feedback string `json:"feedback"`

	// Reviewer 审核人
	// approve/reject操作时可选，默认使用当前用户
	Reviewer string `json:"reviewer"`
}

// ReviewResponse memory.review 响应结构体
type ReviewResponse struct {
	// MemoryID 记忆ID
	MemoryID string `json:"memory_id,omitempty"`

	// State 新状态
	// 操作后的记忆状态
	State string `json:"state,omitempty"`

	// UserConfirmed 是否用户确认
	// approve操作后为true
	UserConfirmed bool `json:"user_confirmed"`

	// Results 记忆列表
	// list操作时返回的记忆列表
	Results []MemoryItem `json:"results,omitempty"`
}

// MemoryItem 记忆项结构体
// P1 服务层使用的记忆聚合结构，包含记忆的所有元数据和内容
type MemoryItem struct {
	// ID 记忆ID
	// 全局唯一标识
	ID string `json:"memory_id"`

	// Scope 作用域
	// 记忆的可见范围：user_global、project_local、repo_local、session
	Scope string `json:"scope"`

	// WorkspaceID 工作空间ID
	WorkspaceID string `json:"workspace_id,omitempty"`

	// UserID 用户ID
	UserID string `json:"user_id,omitempty"`

	// ProjectID 项目ID
	ProjectID string `json:"project_id,omitempty"`

	// RepoID 仓库ID
	RepoID string `json:"repo_id,omitempty"`

	// SessionID 会话ID
	SessionID string `json:"session_id,omitempty"`

	// TaskID 任务ID
	TaskID string `json:"task_id,omitempty"`

	// MemoryType 记忆类型
	// 决定记忆的分类和默认处理策略
	MemoryType string `json:"memory_type"`

	// SourceType 来源类型
	// 记录记忆的来源，影响默认状态和强化策略
	SourceType string `json:"source_type,omitempty"`

	// CreatedBy 创建者
	// 标记手动写入或自动写入来源，如 memoryd、automation:rule_based
	CreatedBy string `json:"created_by,omitempty"`

	// SourceQuality 来源质量
	// 范围0-1，默认0.7，影响保留分数计算
	SourceQuality float64 `json:"source_quality"`

	// Title 记忆标题
	Title string `json:"title,omitempty"`

	// Content 记忆内容
	// 记忆的核心内容
	Content string `json:"content"`

	// NormalizedContent 归一化内容
	// 用于去重和检索的标准化内容
	NormalizedContent string `json:"normalized_content,omitempty"`

	// SearchText 搜索文本
	// 由title、content、keywords等字段构建，用于FTS索引
	// 不暴露给客户端
	SearchText string `json:"-"`

	// KeywordsJSON 关键词JSON
	// 用于FTS全文检索的关键词
	KeywordsJSON string `json:"keywords_json,omitempty"`

	// EntitiesJSON 实体JSON
	// 记忆中提到的实体
	EntitiesJSON string `json:"entities_json,omitempty"`

	// RetrievalCuesJSON 检索线索JSON
	// 用于提高检索命中率的补充线索
	RetrievalCuesJSON string `json:"retrieval_cues_json,omitempty"`

	// TagsJSON 标签JSON
	// 用于分类和过滤的标签
	TagsJSON string `json:"tags_json,omitempty"`

	// State 记忆状态
	// provisional、pending_review、stable、archived、deleted
	State string `json:"state"`

	// Confidence 置信度
	// 范围0-1，默认0.7
	Confidence float64 `json:"confidence"`

	// Importance 重要性
	// 范围0-1，默认0.5
	Importance float64 `json:"importance"`

	// EncodingDepth 编码深度
	// 范围0-4，表示记忆的加工深度
	// 0: 原始事件指针，1: 表层摘要，2: 语义摘要，3: 实体关系，4: 策略抽象
	EncodingDepth int `json:"encoding_depth"`

	// DecayRate 衰减率
	// 控制记忆的遗忘速度，值越大衰减越快
	DecayRate float64 `json:"decay_rate"`

	// RetentionScore 保留分数
	// 综合考虑多种因素的记忆保留价值评分
	RetentionScore float64 `json:"retention_score"`

	// Tier 记忆层级
	// temporary、short_term、long_term、durable、archived
	Tier string `json:"tier"`

	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`

	// Pinned 是否置顶
	// 置顶记忆不会被自动归档或删除
	Pinned bool `json:"pinned"`

	// UserConfirmed 是否用户确认
	// 经过用户审核的记忆
	UserConfirmed bool `json:"user_confirmed"`

	// Version 版本号
	// 记忆内容更新时递增
	Version int `json:"version"`

	// SupersedesID 被取代的ID
	// 当记忆被新版本取代时，指向旧版本ID
	SupersedesID string `json:"supersedes_id,omitempty"`

	// EvidenceRefs 证据引用列表
	// 关联的证据ID列表
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// Evidence 证据结构体
// repository 层持久化证据的结构，保存可解释证据，不保存完整原文
type Evidence struct {
	// ID 证据ID
	// 全局唯一标识
	ID string

	// RawEventID 原始事件ID
	// P3 自动证据必须绑定 raw_event，P1 手动证据可为空
	RawEventID string

	// SourceType 来源类型
	// 证据的来源，如tool_output、user_correction、agent_decision等
	SourceType string

	// InterpretedStatement 解释后的语句
	// 对原始内容的理解和总结
	InterpretedStatement string

	// KeywordsJSON 关键词JSON
	// 用于检索的关键词
	KeywordsJSON string

	// SalientSpansJSON 显著片段JSON
	// 原始内容中的关键片段
	SalientSpansJSON string

	// SourceRefJSON 来源引用JSON
	// 证据的来源信息，如文件路径、commit hash等
	SourceRefJSON string

	// Confidence 置信度
	// 范围0-1，默认0.7
	Confidence float64

	// CreatedAt 创建时间
	CreatedAt time.Time
}

type MemoryRelation struct {
	ID           string
	SourceID     string
	TargetID     string
	RelationType string
	Weight       float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ReviewCheckpoint 设计复查检查点结构体
// repository 层持久化复查 checkpoint 的结构
// 用于把反复设计复查任务的历史上下文压缩成结构化状态
type ReviewCheckpoint struct {
	// ID 检查点ID
	// 全局唯一标识
	ID string

	// MemoryID 记忆ID
	// 与memory_item(memory_type=review_checkpoint)关联
	MemoryID string

	// WorkspaceID 工作空间ID
	WorkspaceID string

	// ProjectID 项目ID
	ProjectID string

	// RepoID 仓库ID
	RepoID string

	// SessionID 会话ID
	SessionID string

	// TaskID 任务ID
	TaskID string

	// CheckpointType 检查点类型
	// architecture_design_review、iteration_plan_review、
	// implementation_design_review、requirements_review
	CheckpointType string

	// ReviewIntentJSON 复查意图JSON
	// 本次复查的目标
	ReviewIntentJSON string

	// TargetDocsJSON 目标文档JSON
	// 复查的文档信息
	TargetDocsJSON string

	// TargetSectionsJSON 目标章节JSON
	// 复查的具体章节
	TargetSectionsJSON string

	// TargetHashesJSON 目标哈希JSON
	// 文档或章节的内容hash
	TargetHashesJSON string

	// Conclusion 复查结论
	// no_major_gap、has_major_gap、supplemented、deferred、baseline_frozen
	Conclusion string

	// ConfirmedBaselineJSON 已确认基线JSON
	// 用户已确认的设计基线
	ConfirmedBaselineJSON string

	// IgnoredItemsJSON 已忽略项JSON
	// 用户明确忽略或延期的问题
	IgnoredItemsJSON string

	// DeferredItemsJSON 延期项JSON
	// 决定延期处理的问题
	DeferredItemsJSON string

	// OpenItemsJSON 待处理项JSON
	// 仍需关注的开放问题
	OpenItemsJSON string

	// NextReviewPolicyJSON 下次复查策略JSON
	// 下次复查的重点和读取策略
	NextReviewPolicyJSON string

	// CreatedAt 创建时间
	CreatedAt time.Time

	// UpdatedAt 更新时间
	UpdatedAt time.Time
}

func toJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
