package retention

import (
	"math"
	"time"

	"github.com/zaneway/the-one/internal/memory"
)

// ComputeScore 计算记忆的保留分数（0~1）。
// 公式：0.35*importance + 0.25*confidence + 0.20*confirmation + 0.10*reinforcement + 0.10*recency - decayPenalty
//
// 正向因子：
//   - Importance (0.35)：记忆重要性（由 admission 或 rule_based 设定）
//   - Confidence (0.25)：置信度（来源可信度）
//   - Confirmation (0.20)：用户确认 + 状态因子（user_confirmed+stable=1.0, stable=0.8, pending=0.4, provisional=0.3）
//   - Reinforcement (0.10)：有效强化次数（effective_reinforcement / 5, max 1.0）
//   - Recency (0.10)：时效性（7天=1.0, 30天=0.6, 90天=0.3, 更久=0.1）
//
// 负向因子：
//   - DecayPenalty：tier 衰减惩罚（temporary=0.40, short_term=0.20, long_term=0.05, durable=0.00）
func ComputeScore(in Input) float64 {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	updatedAt := in.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	score := 0.35*in.Importance +
		0.25*in.Confidence +
		0.20*confirmationFactor(in) +
		0.10*reinforcementFactor(in.EffectiveReinforcement) +
		0.10*recencyFactor(updatedAt, now) -
		decayPenalty(in.Tier)
	return clamp(score, 0, 1)
}

// ComputeTier 根据保留分数和元数据计算记忆的 tier 级别。
// 升级规则：
//   - pinned 记忆：保持当前 tier 不变
//   - temporary 记忆：未过期则保持 temporary（由 cleanup_temporary 模式处理过期）
//   - score > 0.85 + user_confirmed + stable + 非 checkpoint → durable（最高级别）
//   - score > 0.85 + user_confirmed + stable + checkpoint → long_term（checkpoint 不升 durable）
//   - score >= 0.60 → long_term
//   - score >= 0.30 → short_term
//   - score < 0.30 → short_term（兜底，不降为 temporary）
func ComputeTier(in Input) string {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if in.Pinned {
		return in.Tier
	}
	if in.Tier == memory.TierTemporary && !in.ValidUntil.IsZero() && in.ValidUntil.After(now) {
		return memory.TierTemporary
	}
	if in.Tier == memory.TierTemporary && in.ValidUntil.IsZero() {
		return memory.TierTemporary
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
	case score >= 0.60:
		return memory.TierLongTerm
	case score >= 0.30:
		return memory.TierShortTerm
	default:
		return memory.TierShortTerm
	}
}

// confirmationFactor 计算用户确认 + 状态因子。
// 分数规则：
//   - user_confirmed + stable = 1.0（最高确认度）
//   - stable = 0.8（系统确认）
//   - pending_review = 0.4（待审核）
//   - provisional + temporary = 0.1（临时记忆的临时状态，几乎无确认度）
//   - provisional = 0.3（临时状态）
func confirmationFactor(in Input) float64 {
	if in.UserConfirmed && in.State == memory.StateStable {
		return 1.0
	}
	switch in.State {
	case memory.StateStable:
		return 0.8
	case memory.StatePendingReview:
		return 0.4
	case memory.StateProvisional:
		if in.Tier == memory.TierTemporary {
			return 0.1
		}
		return 0.3
	default:
		if in.Tier == memory.TierTemporary {
			return 0.1
		}
		return 0.3
	}
}

// reinforcementFactor 计算强化因子。
// 将有效强化次数归一化到 [0,1]：effective / 5，上限 1.0。
// 强化次数越多，记忆越稳固。
func reinforcementFactor(effective float64) float64 {
	if effective <= 0 {
		return 0
	}
	return math.Min(1.0, effective/5)
}

func recencyFactor(updatedAt, now time.Time) float64 {
	age := now.Sub(updatedAt)
	switch {
	case age <= 7*24*time.Hour:
		return 1.0
	case age <= 30*24*time.Hour:
		return 0.6
	case age <= 90*24*time.Hour:
		return 0.3
	default:
		return 0.1
	}
}

// decayPenalty 根据 tier 返回衰减惩罚。
// tier 越低惩罚越大：temporary(0.40) > short_term(0.20) > long_term(0.05) > durable(0.00)。
// 设计意图：低 tier 记忆需要更高的 importance/confidence 才能获得高保留分数。
func decayPenalty(tier string) float64 {
	switch tier {
	case memory.TierTemporary:
		return 0.40
	case memory.TierShortTerm:
		return 0.20
	case memory.TierLongTerm:
		return 0.05
	case memory.TierDurable:
		return 0.00
	default:
		return 0.20
	}
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func recordInput(record MemoryRecord, now time.Time, score float64) Input {
	validUntil := time.Time{}
	if record.HasValidUntil {
		validUntil = record.ValidUntil
	}
	return Input{
		State:                  record.State,
		Tier:                   record.Tier,
		MemoryType:             record.MemoryType,
		Confidence:             record.Confidence,
		Importance:             record.Importance,
		UserConfirmed:          record.UserConfirmed,
		Pinned:                 record.Pinned,
		EffectiveReinforcement: record.EffectiveReinforcement,
		RetentionScore:         score,
		ValidUntil:             validUntil,
		UpdatedAt:              record.UpdatedAt,
		Now:                    now,
	}
}
