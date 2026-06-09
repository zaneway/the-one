package retention

import (
	"testing"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

func TestComputeScoreUsesArchitectureFormula(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	score := ComputeScore(Input{
		State:         memory.StateStable,
		Tier:          memory.TierLongTerm,
		MemoryType:    memory.TypePreference,
		Scope:         memory.ScopeProjectLocal,
		SourceType:    "user_confirmed",
		Confidence:    0.8,
		Importance:    0.7,
		EncodingDepth: 2,
		UserConfirmed: true,
		Access: AccessFeedbackSummary{
			EffectiveReinforcement: 2.5,
			BaseActivationNorm:     0.35,
		},
		UpdatedAt: now.Add(-48 * time.Hour),
		CreatedAt: now.Add(-72 * time.Hour),
		Now:       now,
	})
	if score < 0.55 || score > 1.0 {
		t.Fatalf("score = %v, want stable confirmed score in (0.55, 1.0]", score)
	}
}

func TestComputeScoreAppliesNegativePenalty(t *testing.T) {
	now := time.Now().UTC()
	base := Input{
		State:         memory.StateStable,
		Tier:          memory.TierLongTerm,
		MemoryType:    memory.TypePreference,
		Scope:         memory.ScopeProjectLocal,
		SourceType:    "agent_summary",
		Confidence:    0.8,
		Importance:    0.7,
		EncodingDepth: 2,
		UpdatedAt:     now,
		CreatedAt:     now,
		Now:           now,
	}
	withPenalty := base
	withPenalty.Access.NegativePenalty = 0.45
	if ComputeScore(withPenalty) >= ComputeScore(base) {
		t.Fatalf("negative penalty should reduce retention score")
	}
}

func TestComputeTierKeepsTemporaryWhenNotExpired(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	tier := ComputeTier(Input{
		State:          memory.StateProvisional,
		Tier:           memory.TierTemporary,
		RetentionScore: 0.2,
		UserConfirmed:  false,
		Pinned:         false,
		ValidUntil:     now.Add(24 * time.Hour),
		Now:            now,
	})
	if tier != memory.TierTemporary {
		t.Fatalf("tier = %q, want temporary while not expired", tier)
	}
}

func TestComputeTierPromotesTemporaryAfterRepeatedAccess(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	tier := ComputeTier(Input{
		State:                  memory.StateStable,
		Tier:                   memory.TierTemporary,
		MemoryType:             memory.TypeProjectFact,
		Scope:                  memory.ScopeProjectLocal,
		RetentionScore:         0.68,
		EffectiveReinforcement: 3.2,
		Access: AccessFeedbackSummary{
			BaseActivationNorm: 0.35,
		},
		UserConfirmed: false,
		Pinned:        false,
		ValidUntil:    now.Add(24 * time.Hour),
		Now:           now,
	})
	if tier == memory.TierTemporary {
		t.Fatalf("tier = %q, want repeated access to move temporary memory into persistent lifecycle", tier)
	}
}

func TestComputeTierPromotesConfirmedHighScoreToDurable(t *testing.T) {
	tier := ComputeTier(Input{
		State:          memory.StateStable,
		Tier:           memory.TierLongTerm,
		MemoryType:     memory.TypePreference,
		RetentionScore: 0.9,
		UserConfirmed:  true,
		Pinned:         false,
	})
	if tier != memory.TierDurable {
		t.Fatalf("tier = %q, want durable", tier)
	}
}

func TestComputeTierPromotesReinforcedShort(t *testing.T) {
	tier := ComputeTier(Input{
		State:                  memory.StateStable,
		Tier:                   memory.TierShortTerm,
		MemoryType:             memory.TypePreference,
		RetentionScore:         0.5,
		EffectiveReinforcement: 3.5,
		UserConfirmed:          false,
		Pinned:                 false,
	})
	if tier != memory.TierReinforcedShort {
		t.Fatalf("tier = %q, want reinforced_short", tier)
	}
}

func TestComputeTierBlocksDurableForPendingReview(t *testing.T) {
	tier := ComputeTier(Input{
		State:          memory.StatePendingReview,
		Tier:           memory.TierLongTerm,
		MemoryType:     memory.TypeDecision,
		RetentionScore: 0.95,
		UserConfirmed:  false,
		Pinned:         false,
	})
	if tier == memory.TierDurable {
		t.Fatalf("tier = %q, pending_review must not auto promote to durable", tier)
	}
}

func TestRelationFactorAndConflictPenalty(t *testing.T) {
	factor := RelationFactor(RelationSignals{SupportingCount: 4, LinkedLongTermCount: 2})
	if factor < 1.0 {
		t.Fatalf("relation factor = %v, want > 1 with supporting edges", factor)
	}
	penalty := ConflictPenalty(RelationSignals{UnresolvedConflictCount: 2})
	if penalty != 0.4 {
		t.Fatalf("conflict penalty = %v, want 0.4", penalty)
	}
}
