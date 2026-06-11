package retrieval

import (
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/memory"
)

const retrievalDiagMaxHits = 20

// memoryHitSummary 检索/注入排查用的紧凑命中摘要。
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

func formatHitScore(score float64) string {
	return fmt.Sprintf("%.4f", score)
}

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
		if include == matched {
			out = append(out, result)
		}
	}
	return out
}

func searchResultsFromCandidates(candidates []Candidate) []memory.SearchResult {
	out := make([]memory.SearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, searchResultFromMemory(candidate.Memory))
	}
	return out
}

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
