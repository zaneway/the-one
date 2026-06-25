package retrieval

import (
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/memory"
)

func TestBuildContextPackRecordsInjectedAndDroppedDiagnostics(t *testing.T) {
	results := []memory.SearchResult{
		contextTestResult("mem-injected", memory.TypePreference, memory.ScopeUserGlobal, "用户偏好。", 0.9),
		contextTestResult("mem-dropped", memory.TypeTemporaryState, memory.ScopeSession, "临时状态。", 0.8),
	}

	_, usedIDs, report := buildContextPack(results, contextBuilderOptions{
		Intent:      IntentGeneralSearch,
		TokenBudget: 120,
	})

	if len(report.Diagnostics.Injected) != 1 || report.Diagnostics.Injected[0].MemoryID != "mem-injected" {
		t.Fatalf("injected diagnostics = %+v, want mem-injected", report.Diagnostics.Injected)
	}
	if len(report.Diagnostics.Dropped) != 1 || report.Diagnostics.Dropped[0].MemoryID != "mem-dropped" {
		t.Fatalf("dropped diagnostics = %+v, want mem-dropped", report.Diagnostics.Dropped)
	}
	if report.Diagnostics.Dropped[0].Reason != "temporary_state_not_continuation" {
		t.Fatalf("drop reason = %q, want temporary_state_not_continuation", report.Diagnostics.Dropped[0].Reason)
	}
	if len(usedIDs) != 1 || usedIDs[0] != "mem-injected" {
		t.Fatalf("used ids = %+v, want only mem-injected", usedIDs)
	}
}

func TestBuildContextPackUsesBucketBudgetInsteadOfInputOrder(t *testing.T) {
	results := []memory.SearchResult{
		contextTestResult("mem-constraint-1", memory.TypeConstraint, memory.ScopeProjectLocal, strings.Repeat("约束", 180), 0.91),
		contextTestResult("mem-constraint-2", memory.TypeDecision, memory.ScopeProjectLocal, strings.Repeat("决策", 180), 0.9),
		contextTestResult("mem-pref", memory.TypePreference, memory.ScopeUserGlobal, "用户偏好先看风险边界和工程落地。", 0.88),
		contextTestResult("mem-failure", memory.TypeFailure, memory.ScopeProjectLocal, "历史失败说明 context builder 不能让大文本挤掉高价值失败经验。", 0.87),
	}

	pack, usedIDs, report := buildContextPack(results, contextBuilderOptions{
		Intent:      IntentGeneralSearch,
		TokenBudget: 160,
	})

	if !containsString(usedIDs, "mem-pref") || !containsString(usedIDs, "mem-failure") {
		t.Fatalf("used ids = %+v, want preference and failure retained by bucket budget", usedIDs)
	}
	if report.Buckets[bucketStableDesign].ItemCount == 0 || report.Buckets[bucketPreferenceProcess].ItemCount == 0 || report.Buckets[bucketFailure].ItemCount == 0 {
		t.Fatalf("bucket report = %+v, want stable/preference/failure buckets used", report.Buckets)
	}
	if report.UsedTokens > report.AvailableTokens {
		t.Fatalf("used tokens = %d, available = %d", report.UsedTokens, report.AvailableTokens)
	}
	if len(pack.Memories) != len(usedIDs) || len(pack.Constraints) == 0 {
		t.Fatalf("context pack = %+v, used ids = %+v", pack, usedIDs)
	}
}

func TestBuildContextPackMarksStatesAndLimitsCodeRefs(t *testing.T) {
	result := contextTestResult("mem-session", memory.TypeTemporaryState, memory.ScopeSession, "当前任务状态只在本 session 有效。", 0.8)
	result.State = memory.StatePendingReview
	result.CodeRefs = []memory.CodeRef{
		{ID: "cr-1", FilePath: "a.go", Symbol: "A", RefSummary: "resolved"},
		{ID: "cr-2", FilePath: "b.go", Symbol: "B", RefSummary: "resolved"},
	}

	pack, usedIDs, report := buildContextPack([]memory.SearchResult{result}, contextBuilderOptions{
		Intent:      IntentTaskContinuation,
		TokenBudget: 220,
	})

	if len(usedIDs) != 1 || len(pack.Memories) != 1 {
		t.Fatalf("used ids = %+v memories=%+v, want one session memory", usedIDs, pack.Memories)
	}
	if !pack.Memories[0].Unconfirmed || !pack.Memories[0].SessionOnly {
		t.Fatalf("memory flags = %+v, want unconfirmed and session_only", pack.Memories[0])
	}
	if len(pack.CodeRefs) != 2 || report.Buckets[bucketCodeRefs].ItemCount != 2 {
		t.Fatalf("code refs = %+v report=%+v, want code refs budgeted", pack.CodeRefs, report.Buckets[bucketCodeRefs])
	}
}

func TestBuildContextPackDropsLowRelevancePendingDecision(t *testing.T) {
	result := contextTestResult("mem-dream", memory.TypeDecision, memory.ScopeProjectLocal, "Dream 导出改成真正增量，读取旧 manifest 判定 write/skip。", 0.62)
	result.State = memory.StatePendingReview
	result.ScoreBreakdown = &memory.ScoreBreakdown{
		BM25:               1.0,
		TaskFit:            0.14,
		ScopeFit:           1.0,
		Retention:          0.4,
		SourceQuality:      0.7,
		Recency:            1.0,
		ContextCostPenalty: 0.1,
		Final:              0.62,
	}
	result.WhyIncluded = []string{"scope_match", "decision_memory", "vector_disabled"}

	pack, usedIDs, report := buildContextPack([]memory.SearchResult{result}, contextBuilderOptions{
		Intent:      IntentGeneralSearch,
		TokenBudget: 400,
	})

	if len(usedIDs) != 0 || len(pack.Memories) != 0 {
		t.Fatalf("used ids = %+v memories=%+v, want low-relevance pending decision dropped", usedIDs, pack.Memories)
	}
	if len(report.Diagnostics.Dropped) != 1 || report.Diagnostics.Dropped[0].Reason != "low_context_relevance" {
		t.Fatalf("dropped diagnostics = %+v, want low_context_relevance", report.Diagnostics.Dropped)
	}
}

func TestBuildContextPackKeepsRelevantPendingDecision(t *testing.T) {
	result := contextTestResult("mem-retrieval", memory.TypeDecision, memory.ScopeProjectLocal, "检索上下文注入前必须增加相关性门禁，避免低 task_fit 记忆污染 prompt。", 0.58)
	result.State = memory.StatePendingReview
	result.ScoreBreakdown = &memory.ScoreBreakdown{
		BM25:               0.7,
		TaskFit:            0.36,
		ScopeFit:           1.0,
		Retention:          0.4,
		SourceQuality:      0.7,
		Recency:            1.0,
		ContextCostPenalty: 0.1,
		Final:              0.58,
	}

	pack, usedIDs, report := buildContextPack([]memory.SearchResult{result}, contextBuilderOptions{
		Intent:      IntentGeneralSearch,
		TokenBudget: 400,
	})

	if len(usedIDs) != 1 || usedIDs[0] != "mem-retrieval" || len(pack.Memories) != 1 {
		t.Fatalf("used ids = %+v memories=%+v report=%+v, want relevant pending decision injected", usedIDs, pack.Memories, report.Diagnostics)
	}
}

func TestBuildContextPackDropsLowRelevanceStableDecision(t *testing.T) {
	result := contextTestResult("mem-stable-noise", memory.TypeDecision, memory.ScopeProjectLocal, "Dream 导出 manifest 清理策略。", 0.52)
	result.State = memory.StateStable
	result.ScoreBreakdown = &memory.ScoreBreakdown{
		BM25:               0.20,
		TaskFit:            0.04,
		ScopeFit:           1.0,
		Retention:          0.7,
		SourceQuality:      0.8,
		Recency:            1.0,
		ContextCostPenalty: 0.1,
		Final:              0.52,
	}

	pack, usedIDs, report := buildContextPack([]memory.SearchResult{result}, contextBuilderOptions{
		Intent:      IntentGeneralSearch,
		TokenBudget: 400,
	})

	if len(usedIDs) != 0 || len(pack.Memories) != 0 {
		t.Fatalf("used ids = %+v memories=%+v, want low-relevance stable decision dropped", usedIDs, pack.Memories)
	}
	if len(report.Diagnostics.Dropped) != 1 || report.Diagnostics.Dropped[0].Reason != "low_context_relevance" {
		t.Fatalf("dropped diagnostics = %+v, want low_context_relevance", report.Diagnostics.Dropped)
	}
}

func TestEstimateContextTokensUsesCeilRuneHalf(t *testing.T) {
	if got := estimateContextTokens("abcde"); got != 3 {
		t.Fatalf("estimateContextTokens(abcde) = %d, want 3", got)
	}
	if got := estimateContextTokens("架构复查"); got != 2 {
		t.Fatalf("estimateContextTokens(架构复查) = %d, want 2", got)
	}
}

func contextTestResult(id, memoryType, scope, content string, score float64) memory.SearchResult {
	breakdown := memory.ScoreBreakdown{Final: score}
	return memory.SearchResult{
		MemoryID:       id,
		MemoryType:     memoryType,
		Scope:          scope,
		Content:        content,
		Score:          score,
		Confidence:     0.8,
		State:          memory.StateStable,
		Tier:           memory.TierLongTerm,
		ScoreBreakdown: &breakdown,
		WhyIncluded:    []string{"task_match"},
	}
}
