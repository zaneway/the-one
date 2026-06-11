package retrieval

import (
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/memory"
)

// retrievalDiagMaxHits 限制检索/注入诊断日志中记录的命中数，避免长尾日志撑爆文件。
// 设计上保留前 N 条与最后 N 条之间的折中：这里只保留前 20 条，按 relevance 顺序，
// 与"为什么这条记忆进/不进 context"的可解释性诉求一致。
const retrievalDiagMaxHits = 20

// memoryHitSummary 检索/注入排查用的紧凑命中摘要。
// 字段说明：
//   - Rank：原始结果中的 1-based 序号；
//   - MemoryID/MemoryType/Scope：定位信息；
//   - Score：保留 4 位小数的可读分数字符串；
//   - Why：rerank 给出的入选理由（仅 search 命中携带）；
//   - Bucket/Reason：context pack 阶段的目标桶与进/弃原因。
type memoryHitSummary struct {
	Rank       int      `json:"rank"`
	MemoryID   string   `json:"memory_id"`
	MemoryType string   `json:"memory_type"`
	Scope      string   `json:"scope"`
	Score      string   `json:"score"`
	Why        []string `json:"why,omitempty"`
	Bucket     string   `json:"bucket,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// summarizeSearchResults 把 search 阶段原始结果压缩为诊断摘要。
// 超过 retrievalDiagMaxHits 时只保留前 N 条，避免单条日志过大。
// 入参为 search 排序后的结果集；返回切片长度为 0 时返回 nil。
func summarizeSearchResults(results []memory.SearchResult) []memoryHitSummary {
	if len(results) == 0 {
		return nil
	}
	limit := len(results)
	if limit > retrievalDiagMaxHits {
		limit = retrievalDiagMaxHits
	}
	out := make([]memoryHitSummary, 0, limit)
	for i := 0; i < limit; i++ {
		result := results[i]
		out = append(out, memoryHitSummary{
			Rank:       i + 1,
			MemoryID:   result.MemoryID,
			MemoryType: result.MemoryType,
			Scope:      result.Scope,
			Score:      formatHitScore(result.Score),
			Why:        compactWhyIncluded(result.WhyIncluded),
		})
	}
	return out
}

// summarizeContextPackHits 把 context 阶段的入桶/出桶命中压缩为诊断摘要。
// 与 summarizeSearchResults 区别在于携带 Bucket/Reason，用于解释"为什么进/不进 context"。
func summarizeContextPackHits(hits []contextPackHit) []memoryHitSummary {
	if len(hits) == 0 {
		return nil
	}
	limit := len(hits)
	if limit > retrievalDiagMaxHits {
		limit = retrievalDiagMaxHits
	}
	out := make([]memoryHitSummary, 0, limit)
	for i := 0; i < limit; i++ {
		hit := hits[i]
		out = append(out, memoryHitSummary{
			Rank:       i + 1,
			MemoryID:   hit.MemoryID,
			MemoryType: hit.MemoryType,
			Scope:      hit.Scope,
			Score:      formatHitScore(hit.Score),
			Bucket:     hit.Bucket,
			Reason:     hit.Reason,
		})
	}
	return out
}

// formatHitScore 把 float64 分数量化为 4 位小数字符串，避免日志里出现浮点精度噪声。
func formatHitScore(score float64) string {
	return fmt.Sprintf("%.4f", score)
}

// compactWhyIncluded 清理入选理由中的空白并过滤空串。
// 返回值不直接复用原 slice，确保 caller 修改原 reasons 时不会影响诊断日志。
func compactWhyIncluded(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason != "" {
			out = append(out, reason)
		}
	}
	return out
}

// filterResultsByIDs 按 id 集合过滤结果集。
// include=true 时返回 id 集合内的子集；include=false 时返回 id 集合外的子集。
// 空 id 集合直接返回 nil；空 MemoryID 的结果会被忽略，避免空 ID 误匹配。
func filterResultsByIDs(results []memory.SearchResult, ids map[string]bool, include bool) []memory.SearchResult {
	if len(ids) == 0 {
		return nil
	}
	out := make([]memory.SearchResult, 0)
	for _, result := range results {
		if result.MemoryID == "" {
			continue
		}
		matched := ids[result.MemoryID]
		// include/matched 同步为 true 或同步为 false 才保留：
		//   include=true && matched=true  -> 保留（白名单）
		//   include=false && matched=false -> 保留（黑名单补集）
		if include == matched {
			out = append(out, result)
		}
	}
	return out
}

// searchResultsFromCandidates 把 rerank 后的候选列表转换为 SearchResult。
// 主要用于在 trace 写入和 access log 写入前统一数据形态。
func searchResultsFromCandidates(candidates []Candidate) []memory.SearchResult {
	out := make([]memory.SearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, searchResultFromMemory(candidate.Memory))
	}
	return out
}

// logRetrievalStage 记录检索链路某个阶段的命中明细。
// 入参：
//   - stage/phase：阶段分类（如 "search" / "post_filter"），用于日志聚合；
//   - traceID：与 retrieval_trace 关联的全局 trace id；
//   - req：原始 search 请求；
//   - results：当前阶段的命中集合；
//   - extra：调用方追加的额外字段（与 base attrs 合并输出）。
//
// 设计约束：query 不直接写入日志，只写入 shortHashForLog，避免敏感 query 落入明文日志。
func (o *MemoryOrchestrator) logRetrievalStage(stage, phase, traceID string, req memory.SearchRequest, results []memory.SearchResult, extra ...any) {
	if o == nil || o.logger == nil {
		return
	}
	attrs := []any{
		"stage", stage,
		"phase", phase,
		"trace_id", traceID,
		"query_hash", shortHashForLog(req.Query),
		"workspace_id", req.WorkspaceID,
		"project_id", req.ProjectID,
		"repo_id", req.RepoID,
		"session_id", req.SessionID,
		"hit_count", len(results),
		"hits", summarizeSearchResults(results),
	}
	attrs = append(attrs, extra...)
	o.logger.Info("检索候选明细", attrs...)
}

// logContextPackDiagnostics 记录 context pack 构造阶段的 token 预算与命中明细。
// 入参：traceID 与 taskHash 用于串联同一请求的检索/注入日志；retrieved 是 rerank 后的
// 候选集合，report 来自 buildContextPack，包含入桶/出桶/预算消耗。
// 输出字段：检索意图、候选数、注入数、丢弃数、总/已用/可用 token，
// 以及候选/注入/丢弃三组命中摘要，便于排障"为什么这条记忆没进 context"。
func (o *MemoryOrchestrator) logContextPackDiagnostics(traceID, taskHash string, intent RetrievalIntent, retrieved []memory.SearchResult, report contextBudgetReport) {
	if o == nil || o.logger == nil {
		return
	}
	diag := report.Diagnostics
	o.logger.Info("上下文注入明细",
		"trace_id", traceID,
		"task_hash", taskHash,
		"retrieval_intent", string(intent),
		"candidate_count", len(retrieved),
		"injected_count", len(diag.Injected),
		"dropped_count", len(diag.Dropped),
		"token_budget", report.TotalTokens,
		"token_used", report.UsedTokens,
		"token_available", report.AvailableTokens,
		"candidate_hits", summarizeSearchResults(retrieved),
		"injected_hits", summarizeContextPackHits(diag.Injected),
		"dropped_hits", summarizeContextPackHits(diag.Dropped),
	)
}
