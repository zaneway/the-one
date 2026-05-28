package automation

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
	"github.com/zaneway/theone/internal/scoring"
)

// ============================================================================
// Admission 决策常量
// 决定候选记忆是否进入长期记忆、以何种状态和层级写入
// ============================================================================
const (
	// DecisionDrop 丢弃：候选记忆不写入，如普通工具成功输出、scope 无效等
	DecisionDrop = "drop"
	// DecisionWriteRawOnly 仅保留原始数据：不写入 memory_item，但保留 evidence 和 candidate 诊断记录
	DecisionWriteRawOnly = "write_raw_only"
	// DecisionWriteTemporary 写入临时记忆：tier=temporary，默认 5 天后自动清理
	DecisionWriteTemporary = "write_temporary"
	// DecisionWriteProvisional 写入临时状态记忆：state=provisional，需要后续巩固
	DecisionWriteProvisional = "write_provisional"
	// DecisionWritePendingReview 写入待审核记忆：state=pending_review，需要用户确认
	DecisionWritePendingReview = "write_pending_review"
	// DecisionWriteStable 写入稳定记忆：state=stable，可被正常检索和使用
	DecisionWriteStable = "write_stable"
)

// AdmissionController 是准入控制器。
// 职责：根据候选记忆的特征和上下文，决定是否写入长期记忆以及写入状态。
// 设计约束：准入决策基于规则和加权评分公式，不依赖外部 LLM 调用。
type AdmissionController struct{}

// AdmissionInput 是准入决策的输入结构体。
type AdmissionInput struct {
	Candidate       processor.MemoryCandidate // 待评估的候选记忆
	RelatedMemory   []memory.MemoryItem       // 同 scope 下的相关已有记忆，用于冲突检测
	TaskSummary     string                    // 当前任务摘要，用于任务相关性评分
	OutcomeSummary  string                    // 当前任务结果摘要
	RecentScopeLoad int                       // 近期同 scope 记忆数量，用于干扰风险评估
}

type AdmissionResult struct {
	Decision         string   `json:"decision"`
	AdmissionScore   float64  `json:"admission_score"`
	MemoryType       string   `json:"memory_type"`
	Scope            string   `json:"scope"`
	InitialState     string   `json:"initial_state"`
	InitialTier      string   `json:"initial_tier"`
	DecayRate        float64  `json:"decay_rate"`
	RequiresReview   bool     `json:"requires_review"`
	UserConfirmed    bool     `json:"user_confirmed"`
	ReasonCodes      []string `json:"reason_codes"`
	ScopeValid       bool     `json:"scope_valid"`
	HighImpact       bool     `json:"high_impact"`
	ConflictRisk     float64  `json:"conflict_risk"`
	InterferenceRisk float64  `json:"interference_risk"`
	DecayRisk        float64  `json:"decay_risk"`
}

func NewAdmissionController() AdmissionController {
	return AdmissionController{}
}

// Decide 执行准入决策，是 Admission 控制器的核心方法。
// 处理流程：
//  1. 校验 scope 合法性（无效 scope 直接 drop）
//  2. 收集 scope 和 candidate 维度的 reason 标记
//  3. 估算 9 维准入特征（future_need、encoding_depth、stability 等）
//  4. 计算加权准入评分（正向特征加权 - 风险特征加权，clamp 到 [0,1]）
//  5. 先尝试特殊决策（用户纠正 → stable，架构决策 → pending_review 等）
//  6. 特殊决策未命中时，按评分区间做通用决策（<0.3 drop，0.3-0.5 raw_only，0.5-0.7 provisional，0.7-0.85 pending_review，>=0.85 stable）
//
// 评分公式：
// score = 0.22*futureNeed + 0.18*encodingDepth + 0.16*stability + 0.14*taskControlSignal
//   - 0.12*episodicSemanticValue + 0.08*retrievalTrainability
//   - 0.16*interferenceRisk - 0.12*decayRisk - 0.10*conflictRisk
func (AdmissionController) Decide(input AdmissionInput) AdmissionResult {
	candidate := input.Candidate
	reasons := reasonSet{}
	logger := slog.Default()
	// Step 1: scope 合法性校验——无效 scope 直接丢弃
	scopeValid := validateCandidateScope(candidate)
	if !scopeValid {
		reasons.add("scope_invalid")
		res := result(candidate, DecisionDrop, 0, "", "", false, false, reasons, false, false, 0, 0, 0)
		logger.Info("admission decided",
			"decision_source", "scope_validation",
			"decision", res.Decision,
			"admission_score", res.AdmissionScore,
			"memory_type", candidate.MemoryType,
			"scope", candidate.Scope,
			"write_memory_item", writesToMemoryItem(res.Decision),
			"reason_codes", res.ReasonCodes,
		)
		return res
	}
	// Step 2: 收集维度标记（scope 类型、candidate 来源等）
	addScopeReasons(candidate, &reasons)
	addCandidateReasons(candidate, &reasons)

	// Step 3: 估算 9 维准入特征
	features := estimateFeatures(input)
	// Step 4: 加权评分——正向特征加权求和后减去风险特征，clamp 到 [0,1]
	score := clamp(
		0.22*features.futureNeed+ // 未来需要概率（重复话题、任务相关性、项目 scope）
			0.18*features.encodingDepthScore+ // 编码深度（0-4 归一化到 0-1）
			0.16*features.stability+ // 稳定性（来源数、用户确认、置信度）
			0.14*features.taskControlSignal+ // 任务控制信号（用户声明的"以后记住"等）
			0.10*features.rawEventSignal+ // 高信号 raw_event 来源：纠正/声明/决策
			0.12*features.episodicSemanticValue+ // 语义价值（按记忆类型评分）
			0.08*features.retrievalTrainability- // 检索可训练性（retrieval_cues 和 keywords 数量）
			0.16*features.interferenceRisk- // 干扰风险（scope 负载、模糊表述、pending 记忆）
			0.12*features.decayRisk- // 衰减风险（session scope、临时类型衰减快）
			0.10*features.conflictRisk, // 冲突风险（与已有 stable 记忆内容矛盾）
		0,
		1,
	)
	logger.Info("admission score computed",
		"memory_type", candidate.MemoryType,
		"scope", candidate.Scope,
		"admission_score", score,
		"score_band", admissionScoreBand(score),
		"feature_future_need", features.futureNeed,
		"feature_encoding_depth", features.encodingDepthScore,
		"feature_stability", features.stability,
		"feature_task_control_signal", features.taskControlSignal,
		"feature_raw_event_signal", features.rawEventSignal,
		"feature_semantic_value", features.episodicSemanticValue,
		"feature_retrieval_trainability", features.retrievalTrainability,
		"feature_interference_risk", features.interferenceRisk,
		"feature_decay_risk", features.decayRisk,
		"feature_conflict_risk", features.conflictRisk,
	)

	// Step 5: 冲突检测和高影响标记
	if features.conflictRisk > 0 {
		reasons.add("conflicts_with_stable_memory")
	}
	highImpact := isHighImpact(candidate, features)
	if highImpact {
		reasons.add("high_impact_requires_review")
	}

	// Step 6: 决策——先尝试特殊决策（按记忆类型和来源的硬规则），未命中时按评分区间决策
	decision, state, tier, requiresReview, userConfirmed := specialDecision(candidate, highImpact, &reasons)
	decisionSource := "special_decision"
	if decision == "" {
		decisionSource = "score_decision"
		decision, state, tier, requiresReview, userConfirmed = scoreDecision(candidate, score, &reasons)
	}
	res := result(candidate, decision, score, state, tier, requiresReview, userConfirmed, reasons, scopeValid, highImpact, features.conflictRisk, features.interferenceRisk, features.decayRisk)
	logger.Info("admission decided",
		"decision_source", decisionSource,
		"decision", res.Decision,
		"admission_score", res.AdmissionScore,
		"score_band", admissionScoreBand(res.AdmissionScore),
		"memory_type", res.MemoryType,
		"scope", res.Scope,
		"initial_state", res.InitialState,
		"initial_tier", res.InitialTier,
		"write_memory_item", writesToMemoryItem(res.Decision),
		"requires_review", res.RequiresReview,
		"user_confirmed", res.UserConfirmed,
		"reason_codes", res.ReasonCodes,
	)
	return res
}

// admissionFeatures 准入特征向量，9 维特征用于加权评分。
// 正向特征（值越高越容易准入）：futureNeed、encodingDepthScore、stability、taskControlSignal、episodicSemanticValue、retrievalTrainability
// 风险特征（值越高越不容易准入）：interferenceRisk、decayRisk、conflictRisk
type admissionFeatures struct {
	futureNeed            float64 // 未来需要概率：重复话题 + 任务相关性 + 项目 scope + 用户偏好
	encodingDepthScore    float64 // 编码深度分：0-4 归一化到 0-1，越高表示加工越深
	stability             float64 // 稳定性分：来源数、用户确认、置信度的加权和
	taskControlSignal     float64 // 任务控制信号：用户声明"以后记住"等控制性语言时为 1.0
	rawEventSignal        float64 // 原始事件高信号权重：user.correction/user.declaration/agent.decision
	episodicSemanticValue float64 // 语义价值：按记忆类型评分（decision/constraint=0.9，temporary=0.3）
	retrievalTrainability float64 // 检索可训练性：retrieval_cues 和 keywords 数量的归一化值
	interferenceRisk      float64 // 干扰风险：scope 负载过高、模糊表述、存在 pending 记忆
	decayRisk             float64 // 衰减风险：session scope 或临时类型衰减快
	conflictRisk          float64 // 冲突风险：与已有 stable 记忆内容矛盾（含"改为""不再"等信号词）
}

// estimateFeatures 估算候选记忆的 9 维准入特征。
// 每个特征的计算方式：
// - futureNeed: 0.4*重复话题 + 0.3*任务相关性 + 0.2*项目scope + 0.1*用户偏好
// - encodingDepthScore: encoding_depth/4.0（归一化到 0-1）
// - stability: 0.35*来源数 + 0.25*固定基线 + 0.25*用户确认 + 0.15*置信度
// - taskControlSignal: 按记忆类型和用户声明关键词评分
// - episodicSemanticValue: 按记忆类型固定评分（decision=0.9, temporary=0.3）
// - retrievalTrainability: 0.2*retrieval_cues数 + 0.1*keywords数
// - interferenceRisk: scope负载 + 模糊表述 + pending记忆存在
// - decayRisk: 按scope和记忆类型评分（session scope=0.6, temporary=0.8）
// - conflictRisk: 与stable记忆内容矛盾（含"改为""不再"等信号词）
func estimateFeatures(input AdmissionInput) admissionFeatures {
	candidate := input.Candidate
	// futureNeed 子特征：重复话题、任务相关性、项目scope、用户偏好
	repeatedTopic := repeatedTopicScore(candidate, input.RelatedMemory)
	taskRelevance := taskRelevanceScore(candidate, input.TaskSummary+" "+input.OutcomeSummary)
	projectScope := projectScopeRelevance(candidate)
	userPreference := 0.0
	if candidate.MemoryType == memory.TypePreference || candidate.SourceType == "user_declared" {
		userPreference = 1.0
	}
	futureNeed := clamp(0.4*repeatedTopic+0.3*taskRelevance+0.2*projectScope+0.1*userPreference, 0, 1)

	// stability 子特征：来源数（evidence 数量/2）、用户确认、置信度
	sourceCount := clamp(float64(len(candidate.SourceEvidenceIDs))/2.0, 0, 1)
	confirmation := 0.0
	if candidate.SourceType == "user_declared" || candidate.SourceType == "user_confirmed" {
		confirmation = 1.0
	}
	stability := clamp(0.35*sourceCount+0.25*0.2+0.25*confirmation+0.15*clamp(candidate.Confidence, 0, 1), 0, 1)

	// interferenceRisk 子特征：scope负载过高（>=5）、模糊表述、存在pending/provisional记忆
	interference := 0.0
	if input.RecentScopeLoad >= 5 {
		interference += 0.4
	}
	if hasReason(candidate, "ambiguous") || hasSignal(candidate.Content, "可能", "也许", "不确定", "ambiguous") {
		interference += 0.3
	}
	for _, item := range input.RelatedMemory {
		if item.State == memory.StatePendingReview || item.State == memory.StateProvisional {
			interference += 0.3
			break
		}
	}

	// conflictRisk 子特征：与 stable 记忆内容矛盾（含"改为""不再""instead""deprecated"等信号词）
	conflict := 0.0
	for _, item := range input.RelatedMemory {
		if item.State == memory.StateStable && memory.LikelyConflict(candidate.MemoryType, candidate.Scope, candidate.Content, item) {
			conflict += 0.4
		}
		if item.Confidence > 0 && item.Confidence < 0.5 {
			conflict += 0.2
		}
	}

	return admissionFeatures{
		futureNeed:            futureNeed,
		encodingDepthScore:    clamp(float64(candidate.EncodingDepth)/4.0, 0, 1),
		stability:             stability,
		taskControlSignal:     taskControlSignal(candidate),
		rawEventSignal:        rawEventSignal(candidate),
		episodicSemanticValue: semanticValue(candidate.MemoryType),
		retrievalTrainability: clamp(0.2*float64(len(candidate.RetrievalCues))+0.1*float64(len(candidate.Keywords)), 0, 1),
		interferenceRisk:      clamp(interference, 0, 1),
		decayRisk:             decayRisk(candidate),
		conflictRisk:          clamp(conflict, 0, 1),
	}
}

func rawEventSignal(candidate processor.MemoryCandidate) float64 {
	return scoring.RawEventEntrySignal(candidate.SourceType, candidate.CandidateReason, candidate.EventScore)
}

// specialDecision 基于记忆类型和来源的硬规则决策，优先于评分决策。
// 决策优先级：
//  1. 普通工具成功输出 → drop（不污染长期记忆）
//  2. 用户纠正 → stable + durable（用户纠正直接信任）
//  3. 架构决策/安全约束/复查检查点 → pending_review（需要用户确认）
//  4. 高影响失败（importance>=0.8）→ pending_review
//  5. 重复失败签名 → provisional（短期保留观察模式）
//  6. 需求声明 → stable 或 pending_review（按影响级别）
//  7. 假设/开放问题 → pending_review 或 provisional
//  8. 临时状态/会话摘要 → temporary（仅 session scope）
//  9. 用户声明偏好 → stable + durable
//
// 返回值：decision, initialState, initialTier, requiresReview, userConfirmed
// 返回空 decision 表示未命中特殊规则，由 scoreDecision 接管。
func specialDecision(candidate processor.MemoryCandidate, highImpact bool, reasons *reasonSet) (string, string, string, bool, bool) {
	// 普通工具成功输出不再直接丢弃，交由评分区间统一决策并记录原因码。
	if hasReason(candidate, "ordinary_success_output") {
		reasons.add("ordinary_success_output")
		return "", "", "", false, false
	}
	// 用户纠正直接写入 stable，旧记忆会被 supersede 或 archived
	if candidate.SourceType == "user_confirmed" || hasReason(candidate, "user_correction") {
		reasons.add("user_correction")
		return DecisionWriteStable, memory.StateStable, tierForStable(candidate, highImpact), false, true
	}
	switch candidate.MemoryType {
	case memory.TypeDecision:
		reasons.add("architecture_decision")
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeConstraint:
		reasons.add("security_constraint")
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeReviewCheckpoint:
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeFailure:
		if highImpact || candidate.Importance >= 0.8 {
			reasons.add("high_impact_failure")
			return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
		}
		if hasReason(candidate, "repeated_failure_signature") {
			reasons.add("repeated_failure_signature")
			return DecisionWriteProvisional, memory.StateProvisional, memory.TierShortTerm, false, false
		}
	case memory.TypeRequirement:
		reasons.add("requirement_declared")
		if highImpact {
			return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
		}
		return DecisionWriteStable, memory.StateStable, memory.TierLongTerm, false, candidate.SourceType == "user_declared"
	case memory.TypeAssumption:
		reasons.add("assumption_recorded")
		if highImpact {
			return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
		}
		return DecisionWriteProvisional, memory.StateProvisional, memory.TierShortTerm, false, false
	case memory.TypeOpenIssue:
		reasons.add("open_issue_recorded")
		if candidate.Scope == memory.ScopeSession {
			return DecisionWriteTemporary, memory.StateProvisional, memory.TierTemporary, false, false
		}
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		reasons.add("session_only_state")
		return DecisionWriteTemporary, memory.StateProvisional, memory.TierTemporary, false, false
	case memory.TypePreference:
		if candidate.SourceType == "user_declared" && !highImpact {
			reasons.add("user_declared")
			return DecisionWriteStable, memory.StateStable, memory.TierDurable, false, true
		}
	}
	if highImpact {
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	}
	return "", "", "", false, false
}

// scoreDecision 按准入评分区间做通用决策（当 specialDecision 未命中时）。
// 评分区间决策规则：
//   - < 0.30: drop（评分过低，丢弃）
//   - 0.30 ~ 0.50: session scope → temporary，其他 → raw_only（低分只保留原始数据）
//   - 0.50 ~ 0.70: provisional（临时状态，需要后续巩固）
//   - 0.70 ~ 0.85: 用户声明偏好 → stable + durable，其他 → pending_review
//   - >= 0.85: 用户声明 → stable，其他 → stable + long_term
func scoreDecision(candidate processor.MemoryCandidate, score float64, reasons *reasonSet) (string, string, string, bool, bool) {
	logger := slog.Default()
	switch {
	case score < 0.30:
		reasons.add("candidate_dropped_by_score")
		logger.Info("admission score band decision",
			"score_band", "<0.30",
			"admission_score", score,
			"decision", DecisionDrop,
			"write_memory_item", false,
			"memory_type", candidate.MemoryType,
			"scope", candidate.Scope,
		)
		return DecisionDrop, "", "", false, false
	case score < 0.50:
		if candidate.Scope == memory.ScopeSession {
			reasons.add("session_only_state")
			logger.Info("admission score band decision",
				"score_band", "0.30-0.50",
				"admission_score", score,
				"decision", DecisionWriteTemporary,
				"write_memory_item", true,
				"memory_type", candidate.MemoryType,
				"scope", candidate.Scope,
			)
			return DecisionWriteTemporary, memory.StateProvisional, memory.TierTemporary, false, false
		}
		logger.Info("admission score band decision",
			"score_band", "0.30-0.50",
			"admission_score", score,
			"decision", DecisionWriteRawOnly,
			"write_memory_item", false,
			"memory_type", candidate.MemoryType,
			"scope", candidate.Scope,
		)
		return DecisionWriteRawOnly, "", "", false, false
	case score < 0.70:
		logger.Info("admission score band decision",
			"score_band", "0.50-0.70",
			"admission_score", score,
			"decision", DecisionWriteProvisional,
			"write_memory_item", true,
			"memory_type", candidate.MemoryType,
			"scope", candidate.Scope,
		)
		return DecisionWriteProvisional, memory.StateProvisional, memory.TierShortTerm, false, false
	case score < 0.85:
		if candidate.SourceType == "user_declared" && candidate.MemoryType == memory.TypePreference {
			reasons.add("user_declared")
			logger.Info("admission score band decision",
				"score_band", "0.70-0.85",
				"admission_score", score,
				"decision", DecisionWriteStable,
				"write_memory_item", true,
				"memory_type", candidate.MemoryType,
				"scope", candidate.Scope,
			)
			return DecisionWriteStable, memory.StateStable, memory.TierDurable, false, true
		}
		logger.Info("admission score band decision",
			"score_band", "0.70-0.85",
			"admission_score", score,
			"decision", DecisionWritePendingReview,
			"write_memory_item", true,
			"memory_type", candidate.MemoryType,
			"scope", candidate.Scope,
		)
		return DecisionWritePendingReview, memory.StatePendingReview, memory.TierLongTerm, true, false
	default:
		if candidate.SourceType == "user_declared" {
			reasons.add("user_declared")
			logger.Info("admission score band decision",
				"score_band", ">=0.85",
				"admission_score", score,
				"decision", DecisionWriteStable,
				"write_memory_item", true,
				"memory_type", candidate.MemoryType,
				"scope", candidate.Scope,
			)
			return DecisionWriteStable, memory.StateStable, tierForStable(candidate, false), false, true
		}
		logger.Info("admission score band decision",
			"score_band", ">=0.85",
			"admission_score", score,
			"decision", DecisionWriteStable,
			"write_memory_item", true,
			"memory_type", candidate.MemoryType,
			"scope", candidate.Scope,
		)
		return DecisionWriteStable, memory.StateStable, memory.TierLongTerm, false, false
	}
}

func admissionScoreBand(score float64) string {
	switch {
	case score < 0.30:
		return "<0.30"
	case score < 0.50:
		return "0.30-0.50"
	case score < 0.70:
		return "0.50-0.70"
	case score < 0.85:
		return "0.70-0.85"
	default:
		return ">=0.85"
	}
}

func writesToMemoryItem(decision string) bool {
	switch decision {
	case DecisionWriteTemporary, DecisionWriteProvisional, DecisionWritePendingReview, DecisionWriteStable:
		return true
	default:
		return false
	}
}

func result(candidate processor.MemoryCandidate, decision string, score float64, state string, tier string, requiresReview bool, userConfirmed bool, reasons reasonSet, scopeValid bool, highImpact bool, conflictRisk float64, interferenceRisk float64, decayRiskValue float64) AdmissionResult {
	return AdmissionResult{
		Decision:         decision,
		AdmissionScore:   score,
		MemoryType:       candidate.MemoryType,
		Scope:            candidate.Scope,
		InitialState:     state,
		InitialTier:      tier,
		DecayRate:        defaultDecayRate(candidate.MemoryType),
		RequiresReview:   requiresReview,
		UserConfirmed:    userConfirmed,
		ReasonCodes:      reasons.list(),
		ScopeValid:       scopeValid,
		HighImpact:       highImpact,
		ConflictRisk:     conflictRisk,
		InterferenceRisk: interferenceRisk,
		DecayRisk:        decayRiskValue,
	}
}

// validateCandidateScope 校验候选记忆的 scope 是否合法。
// 校验规则与手动写入一致：user_global 必须有 user_id 且无 project/repo，
// project_local 必须有 workspace+project，repo_local 必须有 workspace+repo，session 必须有 workspace+session。
func validateCandidateScope(candidate processor.MemoryCandidate) bool {
	switch candidate.Scope {
	case memory.ScopeUserGlobal:
		return candidate.UserID != "" && candidate.ProjectID == "" && candidate.RepoID == ""
	case memory.ScopeProjectLocal:
		return candidate.WorkspaceID != "" && candidate.ProjectID != ""
	case memory.ScopeRepoLocal:
		return candidate.WorkspaceID != "" && candidate.RepoID != ""
	case memory.ScopeSession:
		return candidate.WorkspaceID != "" && candidate.SessionID != ""
	default:
		return false
	}
}

// isHighImpact 判断候选记忆是否为高影响类型。
// 高影响记忆需要用户确认（进入 pending_review），避免误写入稳定记忆。
// 判定规则：conflict_risk>0、decision/constraint/review_checkpoint 类型、
// requirement 含验收/阶段/边界关键词、failure 且 importance>=0.8。
func isHighImpact(candidate processor.MemoryCandidate, features admissionFeatures) bool {
	if features.conflictRisk > 0 {
		return true
	}
	switch candidate.MemoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeReviewCheckpoint:
		return true
	case memory.TypeRequirement:
		return hasSignal(candidate.Content, "验收", "阶段", "边界", "必须", "acceptance", "phase", "boundary")
	case memory.TypeFailure:
		return candidate.Importance >= 0.8
	}
	return false
}

// taskControlSignal 计算任务控制信号分。
// 用户声明且包含"以后""记住""不要""必须"等控制性语言时返回 1.0。
// 按记忆类型递减：requirement=0.9, decision/constraint=0.8, open_issue=0.7, 临时=0.6, 其他=0.3。
func taskControlSignal(candidate processor.MemoryCandidate) float64 {
	content := candidate.Content
	if candidate.SourceType == "user_declared" && hasSignal(content, "以后", "记住", "不要", "必须") {
		return 1.0
	}
	switch candidate.MemoryType {
	case memory.TypeRequirement:
		return 0.9
	case memory.TypeDecision, memory.TypeConstraint:
		return 0.8
	case memory.TypeOpenIssue:
		return 0.7
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		return 0.6
	default:
		return 0.3
	}
}

// semanticValue 按记忆类型返回语义价值分。
// decision/constraint/requirement/review_checkpoint=0.9（高价值决策类），
// preference/failure/procedure=0.8（高价值经验类），
// assumption/open_issue=0.7, project_fact=0.6, session_summary=0.4, temporary=0.3。
func semanticValue(memoryType string) float64 {
	switch memoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeRequirement, memory.TypeReviewCheckpoint:
		return 0.9
	case memory.TypePreference, memory.TypeFailure, memory.TypeProcedure:
		return 0.8
	case memory.TypeAssumption, memory.TypeOpenIssue:
		return 0.7
	case memory.TypeProjectFact:
		return 0.6
	case memory.TypeSessionSummary:
		return 0.4
	case memory.TypeTemporaryState:
		return 0.3
	default:
		return 0.5
	}
}

// decayRisk 按 scope 和记忆类型返回衰减风险分。
// session scope=0.6（session 结束后衰减快），temporary/session_summary=0.8（默认 5 天清理），
// project_fact=0.4, preference/decision/failure/constraint/requirement=0.2（重要记忆衰减慢）。
func decayRisk(candidate processor.MemoryCandidate) float64 {
	if candidate.Scope == memory.ScopeSession {
		return 0.6
	}
	switch candidate.MemoryType {
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		return 0.8
	case memory.TypeProjectFact:
		return 0.4
	case memory.TypePreference, memory.TypeDecision, memory.TypeFailure, memory.TypeConstraint, memory.TypeRequirement:
		return 0.2
	default:
		return 0.3
	}
}

// tierForStable 为 stable 决策的记忆选择初始层级。
// user_global scope 的 preference 和 procedure 且非高影响 → durable（最高等级，不自动衰减）。
// 其他 → long_term（365 天保留）。
func tierForStable(candidate processor.MemoryCandidate, highImpact bool) string {
	if candidate.MemoryType == memory.TypePreference && candidate.Scope == memory.ScopeUserGlobal && !highImpact {
		return memory.TierDurable
	}
	if candidate.MemoryType == memory.TypeProcedure && candidate.Scope == memory.ScopeUserGlobal && !highImpact {
		return memory.TierDurable
	}
	return memory.TierLongTerm
}

// defaultDecayRate 按记忆类型返回默认衰减率。
// decision/constraint/preference/requirement=0.3（重要决策，衰减慢），
// failure/procedure/assumption/open_issue=0.45（经验类，衰减中等），
// temporary_state/session_summary=1.2（临时类，衰减快），其他=0.8。
func defaultDecayRate(memoryType string) float64 {
	switch memoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypePreference, memory.TypeRequirement:
		return 0.3
	case memory.TypeFailure, memory.TypeProcedure, memory.TypeAssumption, memory.TypeOpenIssue:
		return 0.45
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		return 1.2
	default:
		return 0.8
	}
}

// repeatedTopicScore 检测候选记忆是否与已有记忆重复话题。
// 遍历相关记忆，如果存在同类型且关键词重叠的记忆，返回 1.0，否则返回 0。
func repeatedTopicScore(candidate processor.MemoryCandidate, related []memory.MemoryItem) float64 {
	if len(related) == 0 || len(candidate.Keywords) == 0 {
		return 0
	}
	for _, item := range related {
		if item.MemoryType == candidate.MemoryType && overlapKeywords(candidate.Keywords, item) {
			return 1
		}
	}
	return 0
}

// taskRelevanceScore 计算候选记忆与当前任务的相关性。
// 将 candidate 的 keywords 与任务文本做子串匹配，返回匹配比例。
func taskRelevanceScore(candidate processor.MemoryCandidate, text string) float64 {
	if len(candidate.Keywords) == 0 {
		return 0
	}
	matches := 0
	text = strings.ToLower(text)
	for _, keyword := range candidate.Keywords {
		if keyword != "" && strings.Contains(text, strings.ToLower(keyword)) {
			matches++
		}
	}
	return clamp(float64(matches)/float64(len(candidate.Keywords)), 0, 1)
}

// projectScopeRelevance 检查候选记忆是否有明确的项目 scope。
// project_local 且有 project_id 或 repo_local 且有 repo_id 时返回 1.0，否则返回 0。
func projectScopeRelevance(candidate processor.MemoryCandidate) float64 {
	switch candidate.Scope {
	case memory.ScopeProjectLocal:
		if candidate.ProjectID != "" {
			return 1
		}
	case memory.ScopeRepoLocal:
		if candidate.RepoID != "" {
			return 1
		}
	}
	return 0
}

func overlapKeywords(keywords []string, item memory.MemoryItem) bool {
	combined := strings.ToLower(item.Title + " " + item.Content + " " + strings.Join(decodeStringSlice(item.KeywordsJSON), " "))
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(combined, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func decodeStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func addScopeReasons(candidate processor.MemoryCandidate, reasons *reasonSet) {
	switch candidate.Scope {
	case memory.ScopeProjectLocal:
		reasons.add("project_scoped")
	case memory.ScopeRepoLocal:
		reasons.add("repo_scoped")
	case memory.ScopeSession:
		reasons.add("session_only_state")
	}
}

func addCandidateReasons(candidate processor.MemoryCandidate, reasons *reasonSet) {
	for _, reason := range candidate.CandidateReason {
		reasons.add(reason)
	}
	if candidate.SourceType == "user_declared" {
		reasons.add("user_declared")
	}
	if candidate.SourceType == "user_confirmed" {
		reasons.add("user_correction")
	}
}

func hasReason(candidate processor.MemoryCandidate, reason string) bool {
	for _, item := range candidate.CandidateReason {
		if item == reason {
			return true
		}
	}
	return false
}

func hasSignal(text string, signals ...string) bool {
	text = strings.ToLower(text)
	for _, signal := range signals {
		if strings.Contains(text, strings.ToLower(signal)) {
			return true
		}
	}
	return false
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

type reasonSet struct {
	items []string
	seen  map[string]bool
}

func (s *reasonSet) add(reason string) {
	if reason == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[reason] {
		return
	}
	s.seen[reason] = true
	s.items = append(s.items, reason)
}

func (s reasonSet) list() []string {
	return append([]string(nil), s.items...)
}
