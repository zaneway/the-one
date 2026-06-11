package retention

import (
	"math"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

// ComputeScore 按架构 §8.2 计算 retention_score（0~1）。
func ComputeScore(in Input) float64 {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	if in.State == memory.StateDeleted {
		return 0
	}

	salience := computeSalience(in)
	product := salience *
		encodingDepthFactor(in.EncodingDepth) *
		consolidationFactor(in.State) *
		confidenceFactor(in.Confidence) *
		RelationFactor(in.Relations) *
		lifecycleFactor(in.Tier, in.State)

	score := product +
		in.Access.BaseActivationNorm +
		explicitBoost(in) -
		in.Access.NegativePenalty -
		stalenessPenalty(in, now) -
		ConflictPenalty(in.Relations)

	if in.Relations.IsSuperseded || in.State == memory.StateArchived {
		score -= 0.25
	}
	if in.StaleCodeRefCount > 0 {
		score -= math.Min(0.2, 0.05*float64(in.StaleCodeRefCount))
	}

	return clamp(score, 0, 1)
}

// ComputeTier 根据 retention_score 与强化信号计算 tier（架构 §8.2–§8.3）。
func ComputeTier(in Input) string {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	if in.Pinned {
		return in.Tier
	}
	if in.SourceType == "user_declared" && in.UserConfirmed && in.State == memory.StateStable {
		return memory.TierDurable
	}
	if in.Tier == memory.TierTemporary && !hasTemporaryPersistenceSignal(in) {
		if !in.ValidUntil.IsZero() && in.ValidUntil.After(now) {
			return memory.TierTemporary
		}
		if in.ValidUntil.IsZero() {
			return memory.TierTemporary
		}
	}

	score := in.RetentionScore
	if score == 0 {
		score = ComputeScore(in)
	}

	switch {
	case score > 0.85 && in.UserConfirmed && in.State == memory.StateStable && in.MemoryType != memory.TypeReviewCheckpoint:
		return memory.TierDurable
	case score > 0.85 && in.UserConfirmed && in.State == memory.StateStable && in.MemoryType == memory.TypeReviewCheckpoint:
		return memory.TierLongTerm
	case in.EffectiveReinforcement >= 5 && score >= 0.60 && in.State == memory.StateStable:
		return memory.TierLongTerm
	case in.EffectiveReinforcement >= 3 && score >= 0.45 && in.State == memory.StateStable:
		return memory.TierReinforcedShort
	case score >= 0.60:
		return memory.TierLongTerm
	case score >= 0.30:
		return memory.TierShortTerm
	default:
		return memory.TierShortTerm
	}
}

// hasTemporaryPersistenceSignal 判断临时层记忆是否具备"值得保留"信号：
// 强化次数或基础激活度达到阈值，避免临时层在每次重算时被一致地降级到 deleted。
func hasTemporaryPersistenceSignal(in Input) bool {
	return in.EffectiveReinforcement >= 3 || in.Access.EffectiveReinforcement >= 3 || in.Access.BaseActivationNorm >= 0.15
}

// computeSalience 计算 salience 主成分，权重为：memory_type 35%、importance 25%、
// source_type 20%、scope 20%。结果夹到 [0.1, 1.0] 区间，避免零信号记忆参与后续乘积。
func computeSalience(in Input) float64 {
	return clamp(
		0.35*typeWeight(in.MemoryType)+
			0.25*in.Importance+
			0.20*sourceWeight(in.SourceType)+
			0.20*scopeWeight(in.Scope),
		0.1, 1.0,
	)
}

// typeWeight 按记忆类型给基础权重：决策/约束最高，临时态最低。
// 未识别类型走 default=0.65，与"common_knowledge"持平，避免新类型被压成低信号。
func typeWeight(memoryType string) float64 {
	switch memoryType {
	case memory.TypeDecision, memory.TypeConstraint:
		return 0.95
	case memory.TypeFailure:
		return 0.90
	case memory.TypeProcedure, memory.TypePreference:
		return 0.85
	case memory.TypeProjectFact:
		return 0.75
	case "skill":
		return 0.70
	case "common_knowledge":
		return 0.65
	case memory.TypeSessionSummary:
		return 0.50
	case "temporary_state":
		return 0.30
	default:
		return 0.65
	}
}

// sourceWeight 按 source_type 给基础权重：用户显式声明权重最高，自动日志最低。
func sourceWeight(sourceType string) float64 {
	switch sourceType {
	case "user_declared", "user_confirmed":
		return 1.0
	case "multi_session_consolidation", "task_result":
		return 0.85
	case "agent_summary", "manual_review":
		return 0.70
	case "session_summary":
		return 0.65
	case "tool_output", "file_edit_summary":
		return 0.55
	case "import":
		return 0.50
	case "auto_log":
		return 0.35
	default:
		return 0.55
	}
}

// scopeWeight 按 scope 给基础权重：项目级最稳定，session 级最易过期。
// 兼容 "global_common"（已弃用）走 0.70，避免老数据被错误归零。
func scopeWeight(scope string) float64 {
	switch scope {
	case memory.ScopeProjectLocal:
		return 0.90
	case memory.ScopeUserGlobal:
		return 0.85
	case memory.ScopeRepoLocal:
		return 0.80
	case "global_common":
		return 0.70
	case memory.ScopeSession:
		return 0.35
	default:
		return 0.70
	}
}

// encodingDepthFactor 把 encoding_depth 映射为 0.6~1.0 的因子。
// depth 越深说明记忆经过多轮强化，因子越大；超过 4 按 4 计算。
func encodingDepthFactor(depth int) float64 {
	if depth < 0 {
		depth = 0
	}
	if depth > 4 {
		depth = 4
	}
	return 0.6 + 0.1*float64(depth)
}

// consolidationFactor 按 memory_state 给因子：stable=1.0 满额，provisional/pending_review
// 打折，archived 取 0.45 让记忆进入低保留区但不为 0，便于 recover 流程介入。
func consolidationFactor(state string) float64 {
	switch state {
	case memory.StateProvisional:
		return 0.70
	case memory.StatePendingReview:
		return 0.80
	case memory.StateStable:
		return 1.00
	case memory.StateArchived:
		return 0.45
	case memory.StateDeleted:
		return 0
	default:
		return 0.70
	}
}

// confidenceFactor 把置信度夹到 [0.2, 1.0]，避免 0 置信度记忆被一票否决。
func confidenceFactor(confidence float64) float64 {
	return clamp(confidence, 0.2, 1.0)
}

// lifecycleFactor 按 tier 给生命周期因子，durable=1.20 最高，temporary=0.40 最低。
// archived 状态额外统一为 0.50，保证归档不会被遗忘但也不会回流。
func lifecycleFactor(tier, state string) float64 {
	if state == memory.StateArchived {
		return 0.50
	}
	switch tier {
	case memory.TierTemporary:
		return 0.40
	case memory.TierShortTerm:
		return 0.65
	case memory.TierReinforcedShort:
		return 0.80
	case memory.TierLongTerm:
		return 1.00
	case memory.TierDurable:
		return 1.20
	case memory.TierArchived:
		return 0.50
	default:
		return 0.65
	}
}

// explicitBoost 显式权重加成：pinned/user_declared/user_confirmed/durable 各自加分，
// 总加成上限 0.4，避免多重强信号叠加突破 [0,1] 区间。
func explicitBoost(in Input) float64 {
	boost := 0.0
	if in.Pinned {
		boost += 0.30
	}
	if in.SourceType == "user_declared" {
		boost += 0.25
	}
	if in.UserConfirmed {
		boost += 0.20
	}
	if in.Tier == memory.TierDurable && in.UserConfirmed {
		boost += 0.30
	}
	return math.Min(0.4, boost)
}

// stalenessPenalty 根据有效期、supersedes、最后访问/验证时间计算陈旧度惩罚。
// 入参：in 评分输入，now 当前时间。
// 优先级：valid_until 过期 > 已 superseded > 已 supersedes 别人 > TTL 接近过期。
// 关键分支说明：
//   - LastValidatedAt 非空时返回 0：表示已通过校验，无陈旧度；
//   - IsSuperseded 时返回 1.0 直接清零；
//   - 接近 TTL 0.8 阈值时返回 0.2 轻量惩罚，让记忆有机会被强化抵消。
func stalenessPenalty(in Input, now time.Time) float64 {
	if !in.ValidUntil.IsZero() && in.ValidUntil.Before(now) {
		return 0.4
	}
	if in.Relations.IsSuperseded {
		return 1.0
	}
	if in.SupersedesID != "" {
		return 0
	}
	reference := in.UpdatedAt
	if !in.LastValidatedAt.IsZero() {
		return 0
	}
	if !in.LastAccessedAt.IsZero() {
		reference = in.LastAccessedAt
	}
	if reference.IsZero() {
		reference = in.CreatedAt
	}
	ttlDays := defaultTTLDays(in.MemoryType, in.TemporaryTTLDays)
	ageDays := ageInDays(now, reference)
	if ttlDays > 0 && ageDays > float64(ttlDays)*0.8 {
		return 0.2
	}
	return 0
}

// defaultTTLDays 返回不同 memory_type 的默认 TTL 天数。
// 决策/约束/失败/复查检查点保留最长（365 天），临时态跟随 temporaryTTLDays 配置。
func defaultTTLDays(memoryType string, temporaryTTL int) int {
	if temporaryTTL <= 0 {
		temporaryTTL = 5
	}
	switch memoryType {
	case memory.TypeReviewCheckpoint:
		return 365
	case memory.TypeSessionSummary:
		return 90
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeFailure:
		return 365
	case "temporary_state":
		return temporaryTTL
	default:
		return 180
	}
}

// clamp 把 value 限制到 [minValue, maxValue] 区间，超界即截断。
func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func recordInput(record MemoryRecord, access AccessFeedbackSummary, relations RelationSignals, staleCodeRefs int, temporaryTTL int, now time.Time) Input {
	validUntil := time.Time{}
	if record.HasValidUntil {
		validUntil = record.ValidUntil
	}
	lastValidated := time.Time{}
	if record.HasLastValidatedAt {
		lastValidated = record.LastValidatedAt
	}
	lastAccessed := time.Time{}
	if record.HasLastAccessedAt {
		lastAccessed = record.LastAccessedAt
	}
	effective := access.EffectiveReinforcement
	if effective == 0 {
		effective = record.EffectiveReinforcement
	}
	return Input{
		State:                  record.State,
		Tier:                   record.Tier,
		MemoryType:             record.MemoryType,
		Scope:                  record.Scope,
		SourceType:             record.SourceType,
		Confidence:             record.Confidence,
		Importance:             record.Importance,
		SourceQuality:          record.SourceQuality,
		EncodingDepth:          record.EncodingDepth,
		DecayRate:              record.DecayRate,
		UserConfirmed:          record.UserConfirmed,
		Pinned:                 record.Pinned,
		SupersedesID:           record.SupersedesID,
		EffectiveReinforcement: effective,
		Access:                 access,
		Relations:              relations,
		ValidUntil:             validUntil,
		LastValidatedAt:        lastValidated,
		LastAccessedAt:         lastAccessed,
		UpdatedAt:              record.UpdatedAt,
		CreatedAt:              record.CreatedAt,
		Now:                    now,
		StaleCodeRefCount:      staleCodeRefs,
		TemporaryTTLDays:       temporaryTTL,
	}
}
