package retention

import (
	"math"
	"time"

	"github.com/zaneway/the-one/internal/memory"
)

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
