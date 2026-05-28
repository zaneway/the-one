package scoring

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	DefaultRawEventWindowHours = 5
)

type RawEventPolicy struct {
	WindowHours int
}

type RawEventInput struct {
	EventType      string
	OccurredAt     time.Time
	ContentSummary string
	InputSummary   string
	OutputSummary  string
	KeywordsJSON   string
	SourceRefsJSON string
	Query          string
	Now            time.Time
}

func ScoreRawEvent(input RawEventInput) float64 {
	content := strings.TrimSpace(firstNonEmpty(input.ContentSummary, input.OutputSummary, input.InputSummary))
	if content == "" {
		return 0
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	base := baseEventTypeScore(input.EventType)
	recencyBoost := recencyBoost(input.OccurredAt, now)
	queryFit := queryFit(content, input.KeywordsJSON, input.Query)
	sourceRichness := 0.0
	if strings.TrimSpace(input.SourceRefsJSON) != "" {
		sourceRichness = 0.03
	}
	return clamp(base+recencyBoost+0.22*queryFit+sourceRichness, 0, 1)
}

func WithinRawEventWindow(occurredAt, now time.Time, policy RawEventPolicy) bool {
	if occurredAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	windowHours := policy.WindowHours
	if windowHours <= 0 {
		windowHours = DefaultRawEventWindowHours
	}
	age := now.Sub(occurredAt)
	return age >= 0 && age <= time.Duration(windowHours)*time.Hour
}

func RawEventEntrySignal(sourceType string, reasons []string, eventScore float64) float64 {
	if sourceType == "user_confirmed" || contains(reasons, "user_correction") {
		return 1.0
	}
	if sourceType == "user_declared" || contains(reasons, "user_declared") || contains(reasons, "user_declaration") {
		return max(eventScore, 0.9)
	}
	if contains(reasons, "architecture_decision") {
		return max(eventScore, 0.8)
	}
	return clamp(eventScore, 0, 1)
}

func baseEventTypeScore(eventType string) float64 {
	switch eventType {
	case "user.correction":
		return 0.90
	case "user.declaration":
		return 0.82
	case "agent.decision":
		return 0.76
	default:
		return 0.45
	}
}

func recencyBoost(occurredAt, now time.Time) float64 {
	if occurredAt.IsZero() {
		return 0
	}
	hours := now.Sub(occurredAt).Hours()
	switch {
	case hours <= 1:
		return 0.10
	case hours <= 3:
		return 0.06
	case hours <= DefaultRawEventWindowHours:
		return 0.03
	default:
		return 0
	}
}

func queryFit(content, keywordsJSON, query string) float64 {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0.5
	}
	hit := 0.0
	if strings.Contains(strings.ToLower(content), query) {
		hit += 0.7
	}
	var keywords []string
	_ = json.Unmarshal([]byte(keywordsJSON), &keywords)
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(strings.ToLower(keyword))
		if keyword != "" && strings.Contains(query, keyword) {
			hit += 0.2
			break
		}
	}
	return clamp(hit, 0, 1)
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

func max(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
