package processor

import (
	"context"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

// Provider 将 raw_event 事实层事件转换为可解释的 evidence 和 memory candidate。
// 设计约束：Provider 不得写入存储、决定准入或调用外部索引；只负责信号抽取和候选生成。
type Provider interface {
	// Name 返回 Provider 名称，如 "rule_based"。
	Name() string
	// ExtractEvidence 从 raw_event 中抽取 evidence 草稿。
	// 只抽取有记忆价值的事件，低信号事件返回空切片。
	ExtractEvidence(ctx context.Context, input EvidenceInput) ([]EvidenceDraft, error)
	// GenerateCandidates 从 evidence 中生成候选记忆。
	// rule_based 走本地规则；openai 走外部模型结构化输出。
	GenerateCandidates(ctx context.Context, input CandidateInput) ([]MemoryCandidate, error)
}

// RawEventProcessor 表示 Provider 支持在一次处理内同时产出 evidence 与候选记忆。
// openai 自动处理链路使用该能力，避免同一 raw_event 发生两次外部模型调用。
type RawEventProcessor interface {
	ProcessRawEvent(ctx context.Context, input EvidenceInput) ([]ProcessedEvidence, error)
}

// ProcessedEvidence 将 evidence 草稿与基于该 evidence 生成的候选记忆绑定。
// 持久化时仍写入既有 evidence 与 memory_candidate 表，不引入新表或新状态。
type ProcessedEvidence struct {
	Evidence   EvidenceDraft
	Candidates []MemoryCandidate
}

// HealthChecker 表示 Provider 支持轻量可用性探测。
// 该接口只验证外部依赖是否可调用，不产生 evidence/candidate，也不写入存储。
type HealthChecker interface {
	CheckHealth(ctx context.Context) (HealthStatus, error)
}

// HealthStatus 是外部 Provider 健康探测结果。
// 不包含 API Key、Base URL 等敏感配置，只返回可排障的非敏感摘要。
type HealthStatus struct {
	Provider  string
	Model     string
	LatencyMS int64
}

// CaptureQualitySnapshot 是 capture quality 的轻量快照。
// 用于 Provider 做信号过滤时参考捕获质量，避免低质量事件产生高置信度 evidence。
type CaptureQualitySnapshot struct {
	CaptureLevel                  int
	CapturedEventCount            int
	ToolResultCount               int
	FileEditCount                 int
	ConversationMessageCount      int
	ContentBoundaryRejectionCount int
}

// EvidenceInput 是 ExtractEvidence 的输入。
// 包含原始事件、会话/任务上下文、捕获质量和相关事件。
type EvidenceInput struct {
	RawEvent       capture.RawEvent       // 待抽取的原始事件
	Session        capture.AgentSession   // 所属会话
	Task           capture.AgentTask      // 所属任务
	CaptureQuality CaptureQualitySnapshot // 捕获质量快照
	Now            time.Time              // 当前时间，用于分数计算
}

// EvidenceDraft 是 ExtractEvidence 的输出。
// 表示从原始事件中抽取的可解释证据草稿。
type EvidenceDraft struct {
	SourceType           string         // 证据来源类型，如 user_declared、agent_summary、tool_output
	InterpretedStatement string         // 解释后的语句，对原始内容的理解和总结
	Keywords             []string       // 关键词列表，用于检索
	SalientSpans         []string       // 显著片段列表，原始内容中的关键信息
	SourceRef            map[string]any // 来源引用，记录文件路径、hash 等定位信息
	Confidence           float64        // 置信度，范围 0-1
}

// CandidateInput 是 GenerateCandidates 的输入。
// 包含已持久化的 evidence、原始事件、会话/任务上下文和相关记忆。
type CandidateInput struct {
	Evidence      memory.Evidence      // 已持久化的 evidence
	RawEvent      capture.RawEvent     // 原始事件
	Session       capture.AgentSession // 所属会话
	Task          capture.AgentTask    // 所属任务
	RelatedMemory []memory.MemoryItem  // 相关已有记忆（用于重复失败和冲突检测）
	Now           time.Time            // 当前时间
}

// ReviewCheckpointDraft 是设计复查检查点的草稿结构。
// 从 raw_event 的 source_ref 中提取，用于生成 review_checkpoint 类型的候选记忆。
type ReviewCheckpointDraft struct {
	CheckpointType    string           // 检查点类型，如 design_review、architecture_review
	ReviewIntent      []string         // 复查意图列表，如 logic_completeness、business_loop
	TargetDocs        []map[string]any // 被复查的文档列表，包含路径、角色、hash
	TargetSections    []map[string]any // 被复查的章节列表
	TargetHashes      []map[string]any // 文档/章节的内容 hash
	Conclusion        string           // 复查结论，如 no_major_gap、has_major_gap
	ConfirmedBaseline []string         // 已确认的基线内容
	IgnoredItems      []string         // 被忽略的检查项
	DeferredItems     []string         // 延期处理的检查项
	OpenItems         []string         // 待处理的检查项
	NextReviewPolicy  map[string]any   // 下次复查策略
}

// MemoryCandidate 是 GenerateCandidates 的输出。
// 表示一条待准入的候选记忆，包含完整的分类、作用域和元数据。
type MemoryCandidate struct {
	CandidateID       string                 // 候选 ID，由 admission 阶段生成
	MemoryType        string                 // 记忆类型，如 decision、constraint、failure
	Scope             string                 // 作用域，如 user_global、project_local、session
	WorkspaceID       string                 // 工作空间 ID
	UserID            string                 // 用户 ID
	ProjectID         string                 // 项目 ID
	RepoID            string                 // 仓库 ID
	SessionID         string                 // 会话 ID
	TaskID            string                 // 任务 ID
	SourceType        string                 // 来源类型，如 user_declared、agent_summary
	Title             string                 // 记忆标题
	Content           string                 // 记忆内容
	Keywords          []string               // 关键词列表
	Entities          []string               // 实体列表
	RetrievalCues     []string               // 检索线索列表
	Tags              []string               // 标签列表
	Confidence        float64                // 置信度，范围 0-1
	Importance        float64                // 重要性，范围 0-1
	EncodingDepth     int                    // 编码深度，0-4
	EventScore        float64                // 事件分数，由 scoring 模块计算
	ReviewCheckpoint  *ReviewCheckpointDraft // 可选的复查检查点草稿
	CandidateReason   []string               // 候选原因列表，用于准入决策
	SourceEvidenceIDs []string               // 来源 evidence ID 列表
}
