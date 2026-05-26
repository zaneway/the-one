package retrieval

import (
	"math"
	"sort"
	"strings"

	"github.com/zaneway/the-one/internal/memory"
)

const (
	contextReserveRatio    = 0.10
	contextDefaultCodeRefs = 8
)

type contextBucketName string

const (
	bucketStableDesign      contextBucketName = "stable_design"
	bucketPreferenceProcess contextBucketName = "preferences_procedures"
	bucketFailure           contextBucketName = "failure_memories"
	bucketRecent            contextBucketName = "recent_session_state"
	bucketCodeRefs          contextBucketName = "code_refs"
	bucketReviewCheckpoint  contextBucketName = "review_checkpoint"
	bucketDocChanged        contextBucketName = "doc_changed_sections"
)

type contextBucketSpec struct {
	name             contextBucketName
	maxItems         int
	maxTokensPerItem int
	weight           int
}

type contextBuilderOptions struct {
	Intent      RetrievalIntent
	TokenBudget int
}

type contextBudgetReport struct {
	TotalTokens     int
	ReservedTokens  int
	AvailableTokens int
	UsedTokens      int
	MemoryCount     int
	Buckets         map[contextBucketName]contextBucketReportBucket
}

type contextBucketReportBucket struct {
	BudgetTokens int
	UsedTokens   int
	ItemCount    int
}

// buildContextPack 按 P4-C5 多 bucket 预算构造可注入上下文。
// 核心流程：
//  1. 按 intent 选择 bucket profile（不同意图下各 bucket 的权重和限制不同）
//  2. 将检索结果分配到 6 个 bucket：stable_design / preferences_procedures / failure / recent_session / code_refs / review_checkpoint
//  3. 按 bucket 权重分配 token 预算（预留 10% 作为安全余量）
//  4. 每个 bucket 内按分数降序填充，压缩内容到预算限制
//  5. 单独处理 code_refs（最多 8 条）和 constraints 收集
//
// token 估算：ceil(rune_count/2)，不依赖外部 tokenizer，保证本地测试和线上行为一致。
func buildContextPack(results []memory.SearchResult, opts contextBuilderOptions) (memory.ContextPack, []string, contextBudgetReport) {
	if opts.TokenBudget <= 0 {
		opts.TokenBudget = defaultContextBudget
	}
	specs := contextBucketProfile(opts.Intent)
	report := newContextBudgetReport(opts.TokenBudget, specs)
	buckets := bucketSearchResults(results)
	memories := make([]memory.ContextMemory, 0, len(results))
	usedIDs := make([]string, 0, len(results))
	constraints := make([]string, 0)
	codeRefs := make([]memory.CodeRef, 0)
	seenMemoryIDs := map[string]bool{}

	for _, spec := range specs {
		if spec.name == bucketCodeRefs || spec.name == bucketDocChanged {
			continue
		}
		candidates := buckets[spec.name]
		sortBucketResults(candidates)
		bucketBudget := report.Buckets[spec.name].BudgetTokens
		bucketUsed := 0
		bucketItems := 0
		for _, result := range candidates {
			if seenMemoryIDs[result.MemoryID] || bucketItems >= spec.maxItems || bucketUsed >= bucketBudget {
				continue
			}
			if shouldDropContextCandidate(result, opts.Intent) {
				continue
			}
			maxTokens := minInt(spec.maxTokensPerItem, bucketBudget-bucketUsed)
			compressed := compressForTokenBudget(result.Content, maxTokens)
			if compressed == "" {
				continue
			}
			usedTokens := estimateContextTokens(compressed)
			if usedTokens <= 0 {
				continue
			}
			why := append([]string(nil), result.WhyIncluded...)
			if result.MemoryType == memory.TypeReviewCheckpoint {
				why = appendFallbackReasons(why, "review_checkpoint")
			}
			memories = append(memories, memory.ContextMemory{
				MemoryID:       result.MemoryID,
				Type:           result.MemoryType,
				Compressed:     compressed,
				WhyIncluded:    why,
				ScoreBreakdown: result.ScoreBreakdown,
				Unconfirmed:    result.State == memory.StatePendingReview || result.State == memory.StateProvisional,
				Historical:     result.State == memory.StateArchived,
				SessionOnly:    result.Scope == memory.ScopeSession,
			})
			usedIDs = append(usedIDs, result.MemoryID)
			seenMemoryIDs[result.MemoryID] = true
			bucketUsed += usedTokens
			bucketItems++
			report.UsedTokens += usedTokens
			if result.MemoryType == memory.TypeConstraint {
				constraints = append(constraints, compressed)
			}
			codeRefs = appendCodeRefs(codeRefs, result.CodeRefs, contextDefaultCodeRefs)
		}
		entry := report.Buckets[spec.name]
		entry.UsedTokens = bucketUsed
		entry.ItemCount = bucketItems
		report.Buckets[spec.name] = entry
	}
	codeBudget := report.Buckets[bucketCodeRefs].BudgetTokens
	if len(codeRefs) > 0 && codeBudget > 0 {
		used := 0
		kept := make([]memory.CodeRef, 0, len(codeRefs))
		for _, ref := range codeRefs {
			if len(kept) >= contextDefaultCodeRefs {
				break
			}
			refTokens := estimateContextTokens(ref.FilePath + " " + ref.Symbol + " " + ref.RefSummary)
			if refTokens == 0 {
				refTokens = 1
			}
			if used+refTokens > codeBudget && len(kept) > 0 {
				break
			}
			kept = append(kept, ref)
			used += minInt(refTokens, codeBudget-used)
		}
		codeRefs = kept
		entry := report.Buckets[bucketCodeRefs]
		entry.UsedTokens = used
		entry.ItemCount = len(codeRefs)
		report.Buckets[bucketCodeRefs] = entry
		report.UsedTokens += used
	}
	report.MemoryCount = len(memories)
	summary := ""
	if len(memories) > 0 {
		summary = memories[0].Compressed
	}
	return memory.ContextPack{
		Summary:     summary,
		Memories:    memories,
		Constraints: constraints,
		CodeRefs:    codeRefs,
	}, usedIDs, report
}

func newContextBudgetReport(total int, specs []contextBucketSpec) contextBudgetReport {
	reserved := int(math.Ceil(float64(total) * contextReserveRatio))
	available := maxInt(0, total-reserved)
	totalWeight := 0
	for _, spec := range specs {
		totalWeight += spec.weight
	}
	report := contextBudgetReport{
		TotalTokens:     total,
		ReservedTokens:  reserved,
		AvailableTokens: available,
		Buckets:         make(map[contextBucketName]contextBucketReportBucket, len(specs)),
	}
	allocated := 0
	for i, spec := range specs {
		budget := 0
		if totalWeight > 0 {
			if i == len(specs)-1 {
				budget = maxInt(0, available-allocated)
			} else {
				budget = int(math.Floor(float64(available*spec.weight) / float64(totalWeight)))
				allocated += budget
			}
		}
		report.Buckets[spec.name] = contextBucketReportBucket{BudgetTokens: budget}
	}
	return report
}

// contextBucketProfile 根据检索意图返回 bucket 配置。
// 不同意图下的 bucket 权重调整策略：
//   - ArchitectureReview：checkpoint 权重提升到 30%，新增 doc_changed bucket（20%）
//   - TaskContinuation：recent_session 权重提升到 35%（延续场景需要最新状态）
//   - FailureRecall：failure 权重提升到 40%（聚焦失败记忆）
//   - CodeTask：code_refs 权重提升到 20%，preferences 提升到 25%
func contextBucketProfile(intent RetrievalIntent) []contextBucketSpec {
	specs := []contextBucketSpec{
		{name: bucketStableDesign, maxItems: 6, maxTokensPerItem: 180, weight: 30},
		{name: bucketPreferenceProcess, maxItems: 4, maxTokensPerItem: 120, weight: 20},
		{name: bucketFailure, maxItems: 5, maxTokensPerItem: 160, weight: 20},
		{name: bucketRecent, maxItems: 4, maxTokensPerItem: 120, weight: 15},
		{name: bucketCodeRefs, maxItems: 8, maxTokensPerItem: 80, weight: 10},
		{name: bucketReviewCheckpoint, maxItems: 2, maxTokensPerItem: 260, weight: 5},
	}
	switch intent {
	case IntentArchitectureReview:
		return []contextBucketSpec{
			{name: bucketStableDesign, maxItems: 6, maxTokensPerItem: 180, weight: 25},
			{name: bucketReviewCheckpoint, maxItems: 2, maxTokensPerItem: 260, weight: 30},
			{name: bucketPreferenceProcess, maxItems: 4, maxTokensPerItem: 120, weight: 15},
			{name: bucketDocChanged, maxItems: 6, maxTokensPerItem: 160, weight: 20},
			{name: bucketCodeRefs, maxItems: 8, maxTokensPerItem: 80, weight: 10},
			{name: bucketFailure, maxItems: 5, maxTokensPerItem: 160, weight: 8},
			{name: bucketRecent, maxItems: 4, maxTokensPerItem: 120, weight: 7},
		}
	case IntentTaskContinuation:
		specs[0].weight = 20
		specs[3].weight = 35
	case IntentFailureRecall:
		specs[2].weight = 40
		specs[1].weight = 15
	case IntentCodeTask:
		specs[4].weight = 20
		specs[1].weight = 25
	}
	return specs
}

func bucketSearchResults(results []memory.SearchResult) map[contextBucketName][]memory.SearchResult {
	buckets := map[contextBucketName][]memory.SearchResult{}
	for _, result := range results {
		bucket := bucketForSearchResult(result)
		buckets[bucket] = append(buckets[bucket], result)
	}
	return buckets
}

// bucketForSearchResult 将检索结果分配到对应的 context bucket。
// 分配规则：
//   - review_checkpoint → review_checkpoint bucket
//   - session 作用域 / temporary_state / session_summary → recent_session bucket
//   - constraint/decision/project_fact/requirement/assumption/open_issue → stable_design bucket
//   - preference/procedure → preferences_procedures bucket
//   - failure → failure_memories bucket
//   - 其余 → stable_design bucket（兜底）
func bucketForSearchResult(result memory.SearchResult) contextBucketName {
	if result.MemoryType == memory.TypeReviewCheckpoint {
		return bucketReviewCheckpoint
	}
	if result.Scope == memory.ScopeSession || result.MemoryType == memory.TypeTemporaryState || result.MemoryType == memory.TypeSessionSummary {
		return bucketRecent
	}
	switch result.MemoryType {
	case memory.TypeConstraint, memory.TypeDecision, memory.TypeProjectFact, memory.TypeRequirement, memory.TypeAssumption, memory.TypeOpenIssue:
		return bucketStableDesign
	case memory.TypePreference, memory.TypeProcedure:
		return bucketPreferenceProcess
	case memory.TypeFailure:
		return bucketFailure
	default:
		return bucketStableDesign
	}
}

func sortBucketResults(results []memory.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		left := contextResultScore(results[i])
		right := contextResultScore(results[j])
		if left == right {
			return stateRank(results[i].State) > stateRank(results[j].State)
		}
		return left > right
	})
}

func contextResultScore(result memory.SearchResult) float64 {
	if result.ScoreBreakdown != nil && result.ScoreBreakdown.Final > 0 {
		return result.ScoreBreakdown.Final
	}
	return result.Score
}

func stateRank(state string) int {
	switch state {
	case memory.StateStable:
		return 4
	case memory.StatePendingReview:
		return 3
	case memory.StateProvisional:
		return 2
	case memory.StateArchived:
		return 1
	default:
		return 0
	}
}

// shouldDropContextCandidate 判断是否应从上下文中丢弃该候选。
// 丢弃条件：
//   - temporary_state 且非 TaskContinuation 意图（临时状态对非延续场景无价值）
//   - provisional 状态且分数 < 0.2（低分的临时记忆不值得注入）
func shouldDropContextCandidate(result memory.SearchResult, intent RetrievalIntent) bool {
	if result.MemoryType == memory.TypeTemporaryState && intent != IntentTaskContinuation {
		return true
	}
	if result.State == memory.StateProvisional && contextResultScore(result) < 0.2 {
		return true
	}
	return false
}

// compressForTokenBudget 按 token 预算压缩内容。
// 压缩策略：token_budget * 2 = 最大字符数（粗估 2 字符 ≈ 1 token）。
// 超出预算时截断并添加 "..." 后缀。
func compressForTokenBudget(content string, tokenBudget int) string {
	content = strings.TrimSpace(content)
	if tokenBudget <= 0 || content == "" {
		return ""
	}
	maxRunes := tokenBudget * 2
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func estimateContextTokens(content string) int {
	runes := len([]rune(content))
	if runes == 0 {
		return 0
	}
	return (runes + 1) / 2
}

func contextBudgetMap(report contextBudgetReport) map[string]int {
	out := map[string]int{
		"total":        report.TotalTokens,
		"reserved":     report.ReservedTokens,
		"available":    report.AvailableTokens,
		"used":         report.UsedTokens,
		"remaining":    maxInt(0, report.TotalTokens-report.ReservedTokens-report.UsedTokens),
		"memory_count": report.MemoryCount,
	}
	for name, bucket := range report.Buckets {
		prefix := "bucket_" + string(name)
		out[prefix+"_budget"] = bucket.BudgetTokens
		out[prefix+"_used"] = bucket.UsedTokens
		out[prefix+"_items"] = bucket.ItemCount
	}
	return out
}

func addDocChangedSectionBudget(report *contextBudgetReport, strategy *memory.ReviewStrategy) {
	if report == nil || strategy == nil || len(strategy.ChangedSections) == 0 {
		return
	}
	entry := report.Buckets[bucketDocChanged]
	if entry.BudgetTokens <= 0 {
		return
	}
	used := 0
	items := 0
	for _, section := range strategy.ChangedSections {
		if items >= 6 || used >= entry.BudgetTokens {
			break
		}
		sectionTokens := estimateContextTokens(section)
		if sectionTokens == 0 {
			continue
		}
		if used+sectionTokens > entry.BudgetTokens && items > 0 {
			break
		}
		used += minInt(sectionTokens, entry.BudgetTokens-used)
		items++
	}
	entry.UsedTokens = used
	entry.ItemCount = items
	report.Buckets[bucketDocChanged] = entry
	report.UsedTokens += used
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
