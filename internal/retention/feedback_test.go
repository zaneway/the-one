package retention

import (
	"testing"
	"time"
)

func TestComputeAccessFeedbackAggregatesPositiveEventsWithSpacing(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	summary := ComputeAccessFeedback([]AccessFeedbackEvent{
		{EventType: "retrieved", EventWeight: 0.2, SourceQuality: 1.0, CreatedAt: base},
		{EventType: "injected", EventWeight: 0.5, SourceQuality: 1.0, CreatedAt: base.Add(24 * time.Hour)},
		{EventType: "user_confirmed", EventWeight: 2.0, SourceQuality: 1.0, CreatedAt: base.Add(48 * time.Hour)},
		{EventType: "user_rejected", EventWeight: -3.0, SourceQuality: 1.0, CreatedAt: base.Add(72 * time.Hour)},
	})
	if summary.ReinforcementCount != 3 {
		t.Fatalf("reinforcement count = %v, want 3 positive events", summary.ReinforcementCount)
	}
	want := (0.2 + 0.5 + 2.0) * 0.4
	if summary.EffectiveReinforcement != want {
		t.Fatalf("effective reinforcement = %v, want %v (burst spacing within 2 days)", summary.EffectiveReinforcement, want)
	}
}

func TestComputeAccessFeedbackSingleEventUsesFullSpacing(t *testing.T) {
	now := time.Now().UTC()
	summary := ComputeAccessFeedback([]AccessFeedbackEvent{
		{EventType: "user_confirmed", SourceQuality: 0.8, CreatedAt: now},
	})
	if summary.EffectiveReinforcement != 1.6 {
		t.Fatalf("effective reinforcement = %v, want 2.0*0.8", summary.EffectiveReinforcement)
	}
}
