package retention

import (
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/memory"
)

func TestComputeScoreUsesConfirmationAndRecency(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	score := ComputeScore(Input{
		State:                  memory.StateStable,
		Tier:                   memory.TierLongTerm,
		Confidence:             0.8,
		Importance:             0.7,
		UserConfirmed:          true,
		EffectiveReinforcement: 2.5,
		UpdatedAt:              now.Add(-48 * time.Hour),
		Now:                    now,
	})
	if score < 0.70 || score > 0.95 {
		t.Fatalf("score = %v, want high stable confirmed score", score)
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
