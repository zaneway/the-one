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

// normalizeFTSScores 将 FTS 原始分数归一化到 [0,1] 区间。
// 归一化策略：
//   - 无命中：跳过
//   - 单条命中或所有分数相同：统一设为 0.8（避免除零，给单条结果一个合理分数）
//   - 多条命中：min-max 归一化，(score - min) / (max - min)
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

// finalScore 计算 P4-D2 最终排序分数。
// 公式：positive_sum - conflict_penalty - staleness_penalty - context_cost_penalty
//
// 正向因子（权重归一化后求和）：
//   - Semantic (0.28): 向量相似度，vector_enabled=false 时权重为 0
//   - BM25 (0.22): FTS 全文检索分数（已归一化）
//   - TaskFit (0.16): 查询/任务与记忆内容的 token 重叠度 + intent boost
//   - ScopeFit (0.12): 作用域匹配度（exact match=1.0, partial=0.6~0.8）
//   - Retention (0.10): 保留分数（tier 级别：temporary=0.2, short_term=0.4, long_term=0.7, durable=0.95）
//   - RelationSupport (0.06): 关系扩展支持度
//   - SourceQuality (0.04): 来源质量
//   - Recency (0.02): 时效性（7天内=1.0, 30天=0.6, 90天=0.3, 更久=0.1）
//
// 负向因子（直接扣减）：
//   - ConflictPenalty (0.20): 矛盾关系惩罚
//   - StalenessPenalty (0.16): 过时惩罚（deleted=1.0, archived=0.8）
//   - ContextCostPenalty (0.10): 上下文成本惩罚（token 预算占用比例）
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
	// 权重归一化：当 semantic 被禁用时，其余因子权重按比例放大
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

// taskFitScore 计算任务适配分数。
// 计算方式：
//   - 如果已有预计算的 TaskFit（来自 relation expansion），直接使用
//   - 否则通过 token 重叠率计算：overlap(query+task tokens, memory tokens) / total query+task tokens
//   - 基础分上叠加 intentBoost（根据检索意图和记忆类型的匹配加成 0~0.25）
//   - 无查询 token 时返回 0.3 的默认分
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

// intentBoost 根据检索意图和记忆类型的匹配度返回加成分数。
// 设计意图：不同类型的记忆对不同检索场景的价值不同。
//
// 意图-类型加成映射：
//   - ArchitectureReview: checkpoint/decision/constraint/open_issue → +0.25
//   - CodeTask: 有 code_ref 或 failure/procedure/project_fact/decision → +0.20
//   - FailureRecall: failure/procedure → +0.25
//   - TaskContinuation: temporary_state/session_summary/failure → +0.20
//   - UserPreference: preference/procedure → +0.25
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

// scopeFitScore 计算作用域适配分数。
// 分数规则：
//   - exact match（scope 在请求的 scopes 列表中）= 1.0
//   - 用户全局偏好（UserGlobal + TypePreference）= 0.8（跨作用域有参考价值）
//   - 任务延续意图 + session 作用域 = 0.7（session 级记忆对延续有帮助）
//   - 无 scope 约束 = 0.6（默认召回）
//   - 不匹配 = 0
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

// retentionScore 计算保留分数。
// 优先级：预计算分数 > 记忆 retention_score 字段 > tier 默认值。
// tier 默认值反映记忆生命周期：temporary(0.2) < short_term(0.4) < long_term(0.7) < durable(0.95)。
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

// recencyScore 计算时效性分数。
// 时间衰减梯度：7天内(1.0) > 30天(0.6) > 90天(0.3) > 更久(0.1)。
// 无更新时间的记忆返回最低分 0.1。
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

// stalenessScore 计算过时惩罚分数。
// deleted 状态 = 1.0（完全过时），archived = 0.8（大部分过时），其余 = 0。
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

// contextCostScore 计算上下文成本惩罚分数。
// 按内容 token 数占预算比例计算：estimated_tokens / budget。
// token 估算：字符数 / 2（粗略估算中文约 2 字符 = 1 token）。
// 无预算时返回 0.1 的默认惩罚。
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

// candidateLess 候选记忆的稳定排序比较函数。
// 排序优先级（先比较先排序）：
//  1. FinalScore 降序（核心排序依据）
//  2. State 优先级降序（stable > pending_review > provisional）
//  3. Tier 优先级降序（durable > long_term > short_term > temporary）
//  4. UpdatedAt 降序（更新时间越新越靠前）
//  5. ID 升序（兜底确定性排序）
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

// inclusionReasons 生成候选记忆的入选原因标签。
// 用于诊断和解释为什么某条记忆被包含在检索结果中。
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

// tokenSet 将文本分词为 token 集合。
// 分词规则：按非字母数字下划线字符切分，全部转小写。
// 额外处理：如果文本包含 JSON 字符串数组，将数组元素也加入 token 集合。
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
