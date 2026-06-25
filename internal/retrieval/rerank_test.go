package retrieval

import (
	"testing"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

func TestScoreCandidateVectorDisabledDoesNotRenormalizePositiveWeights(t *testing.T) {
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	candidate := Candidate{
		Memory: memory.MemoryItem{
			ID:            "mem_decision",
			Scope:         memory.ScopeProjectLocal,
			ProjectID:     "project",
			MemoryType:    memory.TypeDecision,
			Content:       "项目暂不引入 Kafka，避免过早复杂化。",
			State:         memory.StateStable,
			Tier:          memory.TierLongTerm,
			SourceQuality: 0.8,
			UpdatedAt:     now,
		},
		FTSScore:        0.8,
		SemanticScore:   1.0,
		RelationSupport: 0.2,
	}

	score := ScoreCandidate(&candidate, RerankOptions{
		Query:         "为什么没有用 Kafka",
		Scopes:        []string{memory.ScopeProjectLocal},
		Intent:        IntentGeneralSearch,
		VectorEnabled: false,
		TokenBudget:   1000,
		Now:           now,
	})

	if score.Semantic != 0 {
		t.Fatalf("semantic score = %v, want 0 when vector disabled", score.Semantic)
	}
	if score.Final >= 0.6 || score.Final <= 0 {
		t.Fatalf("final score = %v, want conservative score in (0, 0.6) when vector is disabled", score.Final)
	}
	if candidate.ScoreBreakdown.Final != score.Final || candidate.FinalScore != score.Final {
		t.Fatalf("candidate score not persisted: candidate=%+v score=%+v", candidate.ScoreBreakdown, score)
	}
}

func TestRerankCandidatesUsesStableOrdering(t *testing.T) {
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	candidates := []Candidate{
		{
			Memory:   memory.MemoryItem{ID: "mem_old", Scope: memory.ScopeProjectLocal, MemoryType: memory.TypeDecision, Content: "old", State: memory.StateStable, Tier: memory.TierLongTerm, UpdatedAt: now.Add(-time.Hour)},
			FTSScore: 5,
		},
		{
			Memory:   memory.MemoryItem{ID: "mem_new", Scope: memory.ScopeProjectLocal, MemoryType: memory.TypeDecision, Content: "new", State: memory.StateStable, Tier: memory.TierLongTerm, UpdatedAt: now},
			FTSScore: 5,
		},
	}

	got := RerankCandidates(candidates, RerankOptions{
		Query:       "decision",
		Scopes:      []string{memory.ScopeProjectLocal},
		Intent:      IntentGeneralSearch,
		TokenBudget: 1000,
		Now:         now,
	})

	if len(got) != 2 || got[0].Memory.ID != "mem_new" || got[1].Memory.ID != "mem_old" {
		t.Fatalf("RerankCandidates order = %#v, want newer stable memory first", []string{got[0].Memory.ID, got[1].Memory.ID})
	}
	if candidates[0].FinalScore != 0 {
		t.Fatalf("RerankCandidates mutated input candidate: %+v", candidates[0])
	}
}

func TestScoreCandidateIntentBoostsReviewCheckpoint(t *testing.T) {
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	checkpoint := Candidate{
		Memory: memory.MemoryItem{
			ID:         "mem_checkpoint",
			Scope:      memory.ScopeProjectLocal,
			MemoryType: memory.TypeReviewCheckpoint,
			Content:    "上次 retrieval 复查确认 relation 只使用 automation 四类关系。",
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
			UpdatedAt:  now,
		},
	}

	score := ScoreCandidate(&checkpoint, RerankOptions{
		Task:        "复查 retrieval 详细设计是否还有逻辑缺失",
		Scopes:      []string{memory.ScopeProjectLocal},
		Intent:      IntentArchitectureReview,
		TokenBudget: 1000,
		Now:         now,
	})

	if score.TaskFit < 0.25 {
		t.Fatalf("review checkpoint task fit = %v, want intent boost applied", score.TaskFit)
	}
	if !contains(checkpoint.InclusionReasons, "review_checkpoint_memory") {
		t.Fatalf("inclusion reasons = %#v, want review checkpoint reason", checkpoint.InclusionReasons)
	}
}

func TestScopeFitAllowsUserGlobalPreference(t *testing.T) {
	candidate := Candidate{
		Memory: memory.MemoryItem{
			ID:         "mem_pref",
			Scope:      memory.ScopeUserGlobal,
			MemoryType: memory.TypePreference,
			Content:    "用户偏好先分析架构边界。",
			State:      memory.StateStable,
			Tier:       memory.TierDurable,
		},
	}

	score := ScoreCandidate(&candidate, RerankOptions{
		Query:       "设计任务调度模块",
		Scopes:      []string{memory.ScopeProjectLocal},
		Intent:      IntentUserPreference,
		TokenBudget: 1000,
		Now:         time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC),
	})

	if score.ScopeFit != 0.8 {
		t.Fatalf("scope fit = %v, want user_global preference fallback 0.8", score.ScopeFit)
	}
}

func TestScoreCandidateAppliesConflictStalenessAndContextCostPenalty(t *testing.T) {
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	candidate := Candidate{
		Memory: memory.MemoryItem{
			ID:         "mem_penalty",
			Scope:      memory.ScopeProjectLocal,
			MemoryType: memory.TypeDecision,
			Content:    "这是一个非常长的历史设计结论，用于验证上下文成本惩罚会进入最终分数。",
			State:      memory.StateStable,
			Tier:       memory.TierLongTerm,
			UpdatedAt:  now,
		},
		FTSScore:         0.95,
		RelationSupport:  0.7,
		ConflictPenalty:  1.0,
		StalenessPenalty: 0.8,
	}

	score := ScoreCandidate(&candidate, RerankOptions{
		Query:       "历史设计结论",
		Scopes:      []string{memory.ScopeProjectLocal},
		Intent:      IntentGeneralSearch,
		TokenBudget: 10,
		Now:         now,
	})

	if score.ConflictPenalty != 1.0 || score.StalenessPenalty != 0.8 {
		t.Fatalf("penalties = conflict %v stale %v, want explicit penalties", score.ConflictPenalty, score.StalenessPenalty)
	}
	if score.ContextCostPenalty == 0 {
		t.Fatalf("context cost penalty = 0, want long content penalty")
	}
	if score.Final >= 0.8 {
		t.Fatalf("final score = %v, want penalties to reduce final score", score.Final)
	}
}

func TestTaskFitUsesRetrievalProfileAndIgnoresPathNoise(t *testing.T) {
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	relevant := Candidate{
		Memory: memory.MemoryItem{
			ID:                "mem_retrieval_gate",
			Scope:             memory.ScopeProjectLocal,
			MemoryType:        memory.TypeDecision,
			Title:             "context injection gate",
			Content:           "低相关记忆不能进入 prompt。",
			KeywordsJSON:      `["上下文注入","召回评分","污染"]`,
			RetrievalCuesJSON: `["如何防止 FTS 宽召回污染上下文","上下文 注入 门禁 task_fit"]`,
			State:             memory.StatePendingReview,
			Tier:              memory.TierLongTerm,
			UpdatedAt:         now,
		},
	}
	noise := Candidate{
		Memory: memory.MemoryItem{
			ID:         "mem_dream_export",
			Scope:      memory.ScopeProjectLocal,
			MemoryType: memory.TypeDecision,
			Title:      "Dream 导出修复",
			Content:    "Dream 导出使用 .theone/dream-manifest.json 做增量 write/skip。",
			State:      memory.StatePendingReview,
			Tier:       memory.TierLongTerm,
			UpdatedAt:  now,
		},
	}
	opts := RerankOptions{
		Query:       "/Users/zaneway/.theone-data theone 记忆 上下文 注入 门禁 task_fit",
		Scopes:      []string{memory.ScopeProjectLocal},
		Intent:      IntentGeneralSearch,
		TokenBudget: 1000,
		Now:         now,
	}

	relevantScore := taskFitScore(relevant, opts)
	noiseScore := taskFitScore(noise, opts)

	if relevantScore < 0.35 {
		t.Fatalf("relevant task fit = %v, want retrieval profile to produce a usable context relevance score", relevantScore)
	}
	if noiseScore >= 0.20 {
		t.Fatalf("noise task fit = %v, want path/project tokens downweighted", noiseScore)
	}
	if relevantScore <= noiseScore {
		t.Fatalf("relevant task fit = %v noise = %v, want retrieval profile to outrank path-noise memory", relevantScore, noiseScore)
	}
}

func TestTaskFitDoesNotPromotePathOnlyMatch(t *testing.T) {
	candidate := Candidate{
		Memory: memory.MemoryItem{
			ID:         "mem_path_noise",
			Scope:      memory.ScopeProjectLocal,
			MemoryType: memory.TypeDecision,
			Title:      "theone data",
			Content:    "项目目录命名记录。",
			State:      memory.StatePendingReview,
			Tier:       memory.TierLongTerm,
		},
	}

	score := taskFitScore(candidate, RerankOptions{
		Query:  "/Users/zaneway/.theone-data",
		Intent: IntentGeneralSearch,
	})

	if score != 0 {
		t.Fatalf("path-only task fit = %v, want 0", score)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
