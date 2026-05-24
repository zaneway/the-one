package retrieval

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zaneway/the-one/internal/memory"
)

// RerankOptions 控制 P4-D2 排序公式的上下文参数。
// 这些参数只影响分数估算，不触发任何存储读写。
type RerankOptions struct {
	Query         string
	Task          string
	Scopes        []string
	Intent        RetrievalIntent
	VectorEnabled bool
	TokenBudget   int
	Now           time.Time
}

// RerankCandidates 计算候选记忆的 score breakdown，并按 P4 稳定排序规则返回副本。
// 该函数不会修改输入切片中的元素，便于上层在诊断中保留原始召回顺序。
func RerankCandidates(candidates []Candidate, opts RerankOptions) []Candidate {
	out := append([]Candidate(nil), candidates...)
	if opts.Intent == "" {
		opts.Intent = DetectIntent(opts.Query, opts.Task)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	normalizeFTSScores(out)
	for i := range out {
		ScoreCandidate(&out[i], opts)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return candidateLess(out[i], out[j])
	})
	return out
}

// ScoreCandidate 计算单条候选记忆的 P4 rerank 分数和 score breakdown。
// 缺失字段按 P4 详细设计中的默认值处理，确保排序行为稳定可解释。
func ScoreCandidate(candidate *Candidate, opts RerankOptions) memory.ScoreBreakdown {
	if candidate == nil {
		return memory.ScoreBreakdown{}
	}
	if opts.Intent == "" {
		opts.Intent = DetectIntent(opts.Query, opts.Task)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	breakdown := memory.ScoreBreakdown{
		BM25:               clamp01(candidate.FTSScore),
		Semantic:           semanticScore(*candidate, opts),
		TaskFit:            taskFitScore(*candidate, opts),
		ScopeFit:           scopeFitScore(candidate.Memory, opts),
		Retention:          retentionScore(candidate.Memory, candidate.RetentionScore),
		RelationSupport:    clamp01(candidate.RelationSupport),
		SourceQuality:      sourceQualityScore(candidate.Memory, candidate.SourceQuality),
		Recency:            recencyScore(candidate.Memory, candidate.RecencyFit, opts.Now),
		ConflictPenalty:    clamp01(candidate.ConflictPenalty),
		StalenessPenalty:   stalenessScore(candidate.Memory, candidate.StalenessPenalty),
		ContextCostPenalty: contextCostScore(candidate.Memory.Content, candidate.ContextCostPenalty, opts.TokenBudget),
	}
	breakdown.Final = finalScore(breakdown, opts.VectorEnabled)
	candidate.ScoreBreakdown = breakdown
	candidate.FinalScore = breakdown.Final
	candidate.TaskFit = breakdown.TaskFit
	candidate.ScopeFit = breakdown.ScopeFit
	candidate.RetentionScore = breakdown.Retention
	candidate.SourceQuality = breakdown.SourceQuality
	candidate.RecencyFit = breakdown.Recency
	candidate.ContextCostPenalty = breakdown.ContextCostPenalty
	candidate.InclusionReasons = mergeReasons(candidate.InclusionReasons, inclusionReasons(candidate.Memory, opts, breakdown)...)
	return breakdown
}

func normalizeFTSScores(candidates []Candidate) {
	minScore := math.Inf(1)
	maxScore := math.Inf(-1)
	hits := 0
	for _, candidate := range candidates {
		if candidate.FTSScore > 0 {
			hits++
			if candidate.FTSScore < minScore {
				minScore = candidate.FTSScore
			}
			if candidate.FTSScore > maxScore {
				maxScore = candidate.FTSScore
			}
		}
	}
	if hits == 0 {
		return
	}
	if hits == 1 || maxScore == minScore {
		for i := range candidates {
			if candidates[i].FTSScore > 0 {
				candidates[i].FTSScore = 0.8
			}
		}
		return
	}
	for i := range candidates {
		if candidates[i].FTSScore > 0 {
			candidates[i].FTSScore = (candidates[i].FTSScore - minScore) / (maxScore - minScore)
		}
	}
}

func finalScore(score memory.ScoreBreakdown, vectorEnabled bool) float64 {
	semanticWeight := 0.28
	if !vectorEnabled {
		semanticWeight = 0
	}
	positiveWeights := []struct {
		value  float64
		weight float64
	}{
		{score.Semantic, semanticWeight},
		{score.BM25, 0.22},
		{score.TaskFit, 0.16},
		{score.ScopeFit, 0.12},
		{score.Retention, 0.10},
		{score.RelationSupport, 0.06},
		{score.SourceQuality, 0.04},
		{score.Recency, 0.02},
	}
	totalPositiveWeight := 0.0
	for _, item := range positiveWeights {
		totalPositiveWeight += item.weight
	}
	positive := 0.0
	for _, item := range positiveWeights {
		if item.weight > 0 {
			positive += clamp01(item.value) * (item.weight / totalPositiveWeight)
		}
	}
	raw := positive -
		0.20*clamp01(score.ConflictPenalty) -
		0.16*clamp01(score.StalenessPenalty) -
		0.10*clamp01(score.ContextCostPenalty)
	return clamp01(raw)
}

func semanticScore(candidate Candidate, opts RerankOptions) float64 {
	if !opts.VectorEnabled {
		return 0
	}
	return clamp01(candidate.SemanticScore)
}

func taskFitScore(candidate Candidate, opts RerankOptions) float64 {
	if candidate.TaskFit > 0 {
		return clamp01(candidate.TaskFit)
	}
	textTokens := tokenSet(opts.Query + " " + opts.Task)
	if len(textTokens) == 0 {
		return 0.3
	}
	memoryTokens := tokenSet(candidate.Memory.Title + " " + candidate.Memory.Content + " " + candidate.Memory.KeywordsJSON + " " + candidate.Memory.RetrievalCuesJSON)
	overlap := 0
	for token := range textTokens {
		if memoryTokens[token] {
			overlap++
		}
	}
	base := float64(overlap) / float64(len(textTokens))
	return clamp01(base + intentBoost(candidate.Memory.MemoryType, opts.Intent, len(candidate.CodeRefs) > 0))
}

func intentBoost(memoryType string, intent RetrievalIntent, hasCodeRefs bool) float64 {
	switch intent {
	case IntentArchitectureReview:
		if oneOf(memoryType, memory.TypeReviewCheckpoint, memory.TypeDecision, memory.TypeConstraint, memory.TypeOpenIssue) {
			return 0.25
		}
	case IntentCodeTask:
		if hasCodeRefs || oneOf(memoryType, memory.TypeFailure, memory.TypeProcedure, memory.TypeProjectFact, memory.TypeDecision) {
			return 0.2
		}
	case IntentFailureRecall:
		if oneOf(memoryType, memory.TypeFailure, memory.TypeProcedure) {
			return 0.25
		}
	case IntentTaskContinuation:
		if oneOf(memoryType, memory.TypeTemporaryState, memory.TypeSessionSummary, memory.TypeFailure) {
			return 0.2
		}
	case IntentUserPreference:
		if oneOf(memoryType, memory.TypePreference, memory.TypeProcedure) {
			return 0.25
		}
	}
	return 0
}

func scopeFitScore(item memory.MemoryItem, opts RerankOptions) float64 {
	for _, scope := range opts.Scopes {
		if scope == item.Scope {
			return 1.0
		}
	}
	if item.Scope == memory.ScopeUserGlobal && item.MemoryType == memory.TypePreference {
		return 0.8
	}
	if opts.Intent == IntentTaskContinuation && item.Scope == memory.ScopeSession {
		return 0.7
	}
	if len(opts.Scopes) == 0 {
		return 0.6
	}
	return 0
}

func retentionScore(item memory.MemoryItem, explicit float64) float64 {
	if explicit > 0 {
		return clamp01(explicit)
	}
	if item.RetentionScore > 0 {
		return clamp01(item.RetentionScore)
	}
	switch item.Tier {
	case memory.TierTemporary:
		return 0.2
	case memory.TierShortTerm:
		return 0.4
	case memory.TierLongTerm:
		return 0.7
	case memory.TierDurable:
		return 0.95
	default:
		return 0.4
	}
}

func sourceQualityScore(item memory.MemoryItem, explicit float64) float64 {
	if explicit > 0 {
		return clamp01(explicit)
	}
	if item.SourceQuality > 0 {
		return clamp01(item.SourceQuality)
	}
	return 0.7
}

func recencyScore(item memory.MemoryItem, explicit float64, now time.Time) float64 {
	if explicit > 0 {
		return clamp01(explicit)
	}
	if item.UpdatedAt.IsZero() {
		return 0.1
	}
	age := now.Sub(item.UpdatedAt)
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

func stalenessScore(item memory.MemoryItem, explicit float64) float64 {
	if explicit > 0 {
		return clamp01(explicit)
	}
	switch item.State {
	case memory.StateDeleted:
		return 1.0
	case memory.StateArchived:
		return 0.8
	default:
		return 0
	}
}

func contextCostScore(content string, explicit float64, budget int) float64 {
	if explicit > 0 {
		return clamp01(explicit)
	}
	if budget <= 0 {
		return 0.1
	}
	estimated := math.Ceil(float64(utf8.RuneCountInString(content)) / 2.0)
	return clamp01(estimated / float64(budget))
}

func candidateLess(left, right Candidate) bool {
	if left.FinalScore != right.FinalScore {
		return left.FinalScore > right.FinalScore
	}
	if statePriority(left.Memory.State) != statePriority(right.Memory.State) {
		return statePriority(left.Memory.State) > statePriority(right.Memory.State)
	}
	if tierPriority(left.Memory.Tier) != tierPriority(right.Memory.Tier) {
		return tierPriority(left.Memory.Tier) > tierPriority(right.Memory.Tier)
	}
	if !left.Memory.UpdatedAt.Equal(right.Memory.UpdatedAt) {
		return left.Memory.UpdatedAt.After(right.Memory.UpdatedAt)
	}
	return left.Memory.ID < right.Memory.ID
}

func statePriority(state string) int {
	switch state {
	case memory.StateStable:
		return 4
	case memory.StatePendingReview:
		return 3
	case memory.StateProvisional:
		return 2
	default:
		return 1
	}
}

func tierPriority(tier string) int {
	switch tier {
	case memory.TierDurable:
		return 4
	case memory.TierLongTerm:
		return 3
	case memory.TierShortTerm:
		return 2
	case memory.TierTemporary:
		return 1
	default:
		return 0
	}
}

func inclusionReasons(item memory.MemoryItem, opts RerankOptions, score memory.ScoreBreakdown) []string {
	reasons := []string{}
	if score.TaskFit >= 0.5 {
		reasons = append(reasons, "task_match")
	}
	if score.ScopeFit >= 0.8 {
		reasons = append(reasons, "scope_match")
	}
	if item.MemoryType != "" {
		reasons = append(reasons, item.MemoryType+"_memory")
	}
	if !opts.VectorEnabled {
		reasons = append(reasons, "vector_disabled")
	}
	return reasons
}

func mergeReasons(existing []string, additions ...string) []string {
	seen := map[string]bool{}
	merged := make([]string, 0, len(existing)+len(additions))
	for _, reason := range append(existing, additions...) {
		if reason == "" || seen[reason] {
			continue
		}
		seen[reason] = true
		merged = append(merged, reason)
	}
	return merged
}

func tokenSet(text string) map[string]bool {
	values := map[string]bool{}
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		values[current.String()] = true
		current.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	for _, keyword := range jsonStringValues(text) {
		values[strings.ToLower(keyword)] = true
	}
	return values
}

func jsonStringValues(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err == nil {
		return values
	}
	return nil
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
