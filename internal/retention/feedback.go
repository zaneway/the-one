package retention

import (
	"math"
	"time"
)

// AccessFeedbackEvent 是聚合 access log 所需的最小字段。
type AccessFeedbackEvent struct {
	EventType     string
	EventWeight   float64
	SourceQuality float64
	CreatedAt     time.Time
}

// ComputeAccessSignals 从 access log 事件计算强化、激活与负向惩罚（架构 §8.1–§8.2.3）。
func ComputeAccessSignals(events []AccessFeedbackEvent, decayRate float64, now time.Time) AccessFeedbackSummary {
	if now.IsZero() {
		now = time.Now()
	}
	if decayRate <= 0 {
		decayRate = 0.8
	}
	reinforcement := computeEffectiveReinforcement(events)
	baseActivation := computeBaseActivation(events, decayRate, now)
	negative := computeNegativePenalty(events)
	return AccessFeedbackSummary{
		EffectiveReinforcement: reinforcement.EffectiveReinforcement,
		ReinforcementCount:     reinforcement.ReinforcementCount,
		LastReinforcedAt:       reinforcement.LastReinforcedAt,
		BaseActivation:         baseActivation,
		BaseActivationNorm:     1 - math.Exp(-baseActivation),
		NegativePenalty:        negative,
	}
}

// ComputeAccessFeedback 保留旧名，供单测与兼容调用。
func ComputeAccessFeedback(events []AccessFeedbackEvent) AccessFeedbackSummary {
	return ComputeAccessSignals(events, 0.8, time.Now())
}

func computeEffectiveReinforcement(events []AccessFeedbackEvent) AccessFeedbackSummary {
	positive := make([]AccessFeedbackEvent, 0, len(events))
	for _, event := range events {
		weight := eventWeight(event)
		if weight <= 0 {
			continue
		}
		positive = append(positive, AccessFeedbackEvent{
			EventType:     event.EventType,
			EventWeight:   weight,
			SourceQuality: event.SourceQuality,
			CreatedAt:     event.CreatedAt,
		})
	}
	if len(positive) == 0 {
		return AccessFeedbackSummary{}
	}
	span := positive[len(positive)-1].CreatedAt.Sub(positive[0].CreatedAt)
	spacing := spacingFactor(span, len(positive))
	effective := 0.0
	for _, event := range positive {
		sourceQuality := event.SourceQuality
		if sourceQuality <= 0 {
			sourceQuality = 1.0
		}
		effective += event.EventWeight * spacing * sourceQuality
	}
	return AccessFeedbackSummary{
		EffectiveReinforcement: effective,
		ReinforcementCount:     float64(len(positive)),
		LastReinforcedAt:       positive[len(positive)-1].CreatedAt,
	}
}

func computeBaseActivation(events []AccessFeedbackEvent, decayRate float64, now time.Time) float64 {
	sum := 0.0
	for _, event := range events {
		weight := eventWeight(event)
		if weight <= 0 {
			continue
		}
		ageDays := ageInDays(now, event.CreatedAt)
		modifier := EventDecayModifier(event.EventType)
		sum += weight * math.Pow(ageDays+1, -decayRate*modifier)
	}
	return math.Log1p(sum)
}

func computeNegativePenalty(events []AccessFeedbackEvent) float64 {
	sum := 0.0
	for _, event := range events {
		weight := eventWeight(event)
		if weight < 0 {
			sum += weight
		}
	}
	if sum == 0 {
		return 0
	}
	return math.Abs(sum) * 0.15
}

func eventWeight(event AccessFeedbackEvent) float64 {
	if event.EventWeight != 0 {
		return event.EventWeight
	}
	if event.EventType == "" {
		return 0
	}
	return AccessLogEventWeight(event.EventType)
}

func ageInDays(now, eventTime time.Time) float64 {
	if eventTime.IsZero() {
		return 0
	}
	age := now.Sub(eventTime)
	if age < 0 {
		return 0
	}
	return age.Hours() / 24
}

func spacingFactor(span time.Duration, positiveCount int) float64 {
	if positiveCount <= 1 {
		return 1.0
	}
	switch {
	case span <= 2*24*time.Hour:
		return 0.4
	case span <= 14*24*time.Hour:
		return 0.7
	case span <= 90*24*time.Hour:
		return 1.0
	default:
		return 1.2
	}
}
