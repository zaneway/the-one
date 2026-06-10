package processor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

func TestRuleBasedUserDeclarationPreferenceCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventUserDeclaration, "以后回答技术方案时先分析架构边界、风险和工程落地。")

	evidence := extractOne(t, provider, event)
	if evidence.SourceType != "user_declared" {
		t.Fatalf("source type = %q, want user_declared", evidence.SourceType)
	}
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypePreference || candidates[0].Scope != memory.ScopeUserGlobal {
		t.Fatalf("candidate = %s/%s, want preference/user_global", candidates[0].MemoryType, candidates[0].Scope)
	}
}

func TestRuleBasedUserDeclarationConstraintCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventUserDeclaration, "automation 不要引入外部 LLM Provider，必须保持 rule_based 本地可测。")
	event.ProjectID = "proj_001"

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeConstraint || candidates[0].Scope != memory.ScopeProjectLocal {
		t.Fatalf("candidate = %s/%s, want constraint/project_local", candidates[0].MemoryType, candidates[0].Scope)
	}
}

func TestRuleBasedCandidateTitleKeepsCompleteShortStatement(t *testing.T) {
	provider := NewRuleBasedProvider()
	statement := "Codex hooks 作为主路径，wrapper 仅作为兼容入口。"
	event := rawEvent(capture.EventUserDeclaration, statement)
	event.ProjectID = "proj_001"

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	want := memory.TypeProjectFact + ": " + statement
	if candidates[0].Title != want {
		t.Fatalf("title = %q, want %q", candidates[0].Title, want)
	}
}

func TestRuleBasedUserCorrectionCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventUserCorrection, "当前数据库已经从 MySQL 改为 PostgreSQL。")
	event.SourceRefsJSON = refsJSON(map[string]any{
		"target_memory_id":    "mem_old",
		"target_memory_type":  memory.TypeProjectFact,
		"target_memory_scope": memory.ScopeProjectLocal,
	})

	evidence := extractOne(t, provider, event)
	if evidence.SourceType != "user_confirmed" {
		t.Fatalf("source type = %q, want user_confirmed", evidence.SourceType)
	}
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeProjectFact || candidates[0].Scope != memory.ScopeProjectLocal {
		t.Fatalf("candidate = %s/%s, want project_fact/project_local", candidates[0].MemoryType, candidates[0].Scope)
	}
}

func TestRuleBasedAgentDecisionCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventAgentDecision, "")
	event.SourceRefsJSON = refsJSON(map[string]any{
		"decision_summary": "automation 只实现 rule_based Provider。",
		"reason_summary":   "保持本地可测，外部 LLM 放到后续版本。",
	})

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeDecision || candidates[0].Scope != memory.ScopeProjectLocal {
		t.Fatalf("candidate = %s/%s, want decision/project_local", candidates[0].MemoryType, candidates[0].Scope)
	}
}

func TestRuleBasedToolSuccessNoEvidence(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventToolResultSummary, "")
	event.ToolName = "go test"
	event.OutputSummary = "ok github.com/zaneway/theone/internal/memory"
	event.SourceRefsJSON = refsJSON(map[string]any{"exit_code": 0})

	evidence, err := provider.ExtractEvidence(context.Background(), EvidenceInput{RawEvent: event})
	if err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence count = %d, want 0", len(evidence))
	}
}

func TestRuleBasedAgentResponseBoilerplateNoEvidence(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventAgentResponseSummary, "【结论/决策】Claude 已完成本轮响应")
	event.KeywordsJSON = jsonArray("claude_code", "hook", "turn-completed", "trace:rt_bad")
	event.SalientSpansJSON = jsonArray("Claude 已完成本轮响应", "用户提出真实需求")

	evidence, err := provider.ExtractEvidence(context.Background(), EvidenceInput{RawEvent: event})
	if err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence count = %d, want 0: %+v", len(evidence), evidence)
	}
}

func TestRuleBasedFiltersCaptureMetadataKeywordsAndNoisySpans(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventAgentResponseSummary, "【结论/决策】Codex hooks 作为主接入路径，wrapper 仅作为兼容入口。")
	event.KeywordsJSON = jsonArray("codex", "hook", "turn-completed", "trace:rt_bad", "mem:mem_bad", "wrapper")
	event.SalientSpansJSON = jsonArray("Claude 已完成本轮响应", "Codex hooks 是主接入路径")

	evidence := extractOne(t, provider, event)
	if got := strings.Join(decodeStringSlice(evidence.KeywordsJSON), "|"); got != "codex|wrapper" {
		t.Fatalf("evidence keywords = %q, want semantic keywords only", got)
	}
	if got := strings.Join(decodeStringSlice(evidence.SalientSpansJSON), "|"); got != "Codex hooks 是主接入路径" {
		t.Fatalf("evidence spans = %q, want noisy spans removed", got)
	}
}

func TestRuleBasedTurnCompletedExtractsHighSignalEvidence(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventTurnCompleted, "【事件】用户要求一轮问答只写一条 raw_event。\n【结论/决策】采用 turn.completed 合并用户请求和助手应答。")
	event.KeywordsJSON = jsonArray("raw_event", "turn.completed", "adapter")
	event.SalientSpansJSON = jsonArray("一轮问答只写一条 raw_event", "采用 turn.completed")

	evidence := extractOne(t, provider, event)
	if evidence.SourceType != "agent_summary" {
		t.Fatalf("source type = %q, want agent_summary", evidence.SourceType)
	}
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeDecision {
		t.Fatalf("candidate type = %s, want decision", candidates[0].MemoryType)
	}
}

func TestRuleBasedFailedToolTemporaryCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventToolResultSummary, "")
	event.ToolName = "go test"
	event.OutputSummary = "auth token expiry boundary test failed"
	event.SourceRefsJSON = refsJSON(map[string]any{"exit_code": 1, "command_hash": "sha256:abc"})

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeTemporaryState || candidates[0].Scope != memory.ScopeSession {
		t.Fatalf("candidate = %s/%s, want temporary_state/session", candidates[0].MemoryType, candidates[0].Scope)
	}
}

func TestRuleBasedRepeatedFailedToolFailureCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventToolResultSummary, "")
	event.ToolName = "go test"
	event.OutputSummary = "repeated auth token expiry boundary failure"
	event.SourceRefsJSON = refsJSON(map[string]any{"exit_code": 1, "command_hash": "sha256:abc"})

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeFailure || candidates[0].Scope != memory.ScopeRepoLocal {
		t.Fatalf("candidate = %s/%s, want failure/repo_local", candidates[0].MemoryType, candidates[0].Scope)
	}
}

func TestRuleBasedFileEditStructureOnlyNoEvidence(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventFileEditSummary, "修改 internal/memory/service.go 中 Remember 函数。")
	event.SourceRefsJSON = refsJSON(map[string]any{"file_path": "internal/memory/service.go"})

	evidence, err := provider.ExtractEvidence(context.Background(), EvidenceInput{RawEvent: event})
	if err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence count = %d, want 0", len(evidence))
	}
}

func TestRuleBasedDesignReviewCheckpointCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventTaskResult, "automation 详细设计复查完成，存在少量遗漏，已补充自动写入规则。")
	event.SourceRefsJSON = refsJSON(map[string]any{
		"target_docs": []map[string]any{{
			"path": "doc/The One 长期记忆系统 automation 详细设计.md",
			"role": "implementation_design",
		}},
		"review_intent":      []string{"logic_consistency", "acceptance_completeness"},
		"conclusion":         "supplemented",
		"confirmed_baseline": []string{"automation 只实现 rule_based Provider"},
	})

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeReviewCheckpoint || candidates[0].ReviewCheckpoint == nil {
		t.Fatalf("candidate type/checkpoint = %s/%v, want review_checkpoint with draft", candidates[0].MemoryType, candidates[0].ReviewCheckpoint)
	}
	if candidates[0].ReviewCheckpoint.Conclusion != "supplemented" {
		t.Fatalf("checkpoint conclusion = %q, want supplemented", candidates[0].ReviewCheckpoint.Conclusion)
	}
}

func TestRuleBasedTurnCompletedAcceptanceConstraintFallsThroughToCandidate(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventTurnCompleted, "【约束】禁止合并未测代码进入主分支。")

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeConstraint {
		t.Fatalf("candidate type = %s, want constraint", candidates[0].MemoryType)
	}
}

func TestRuleBasedDesignReviewKeywordWithoutCheckpointFallsThrough(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventTaskResult, "任务完成，后续需要补充验收标准。")

	evidence := extractOne(t, provider, event)
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeSessionSummary {
		t.Fatalf("candidate type = %s, want session_summary", candidates[0].MemoryType)
	}
}

func TestRuleBasedFailedToolDoesNotUpgradeOnUnrelatedMemory(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventToolResultSummary, "")
	event.ToolName = "go test"
	event.OutputSummary = "auth token expiry boundary test failed"
	event.SourceRefsJSON = refsJSON(map[string]any{"exit_code": 1})

	evidence := extractOne(t, provider, event)
	candidates, err := provider.GenerateCandidates(context.Background(), CandidateInput{
		Evidence: evidence,
		RawEvent: event,
		RelatedMemory: []memory.MemoryItem{{
			ID:         "mem_pref",
			MemoryType: memory.TypePreference,
			Content:    "回答技术方案时先分析架构边界。",
		}},
		Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("GenerateCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeTemporaryState {
		t.Fatalf("candidate type = %s, want temporary_state", candidates[0].MemoryType)
	}
}

func TestRuleBasedTurnCompletedUsesDeclarationClassification(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventTurnCompleted, "【约束】禁止在未写迁移测试时修改数据库 schema。")
	evidence := memory.Evidence{
		ID:                   "ev_001",
		InterpretedStatement: "禁止在未写迁移测试时修改数据库 schema。",
		SourceType:           "agent_summary",
	}
	candidates := generate(t, provider, event, evidence)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].MemoryType != memory.TypeConstraint {
		t.Fatalf("candidate type = %s, want constraint", candidates[0].MemoryType)
	}
}

func TestRuleBasedInsufficientInputReturnsEmpty(t *testing.T) {
	provider := NewRuleBasedProvider()
	event := rawEvent(capture.EventConversationMessage, "继续")

	evidence, err := provider.ExtractEvidence(context.Background(), EvidenceInput{RawEvent: event})
	if err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence count = %d, want 0", len(evidence))
	}
}

func rawEvent(eventType, content string) capture.RawEvent {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	return capture.RawEvent{
		ID:             "evt_001",
		SessionID:      "sess_001",
		TaskID:         "task_001",
		WorkspaceID:    "ws_001",
		ProjectID:      "proj_001",
		RepoID:         "repo_001",
		AgentType:      "codex",
		EventType:      eventType,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorAgent,
		ContentSummary: content,
		ContentHash:    "sha256:event",
		CreatedAt:      now,
	}
}

func extractOne(t *testing.T, provider RuleBasedProvider, event capture.RawEvent) memory.Evidence {
	t.Helper()
	drafts, err := provider.ExtractEvidence(context.Background(), EvidenceInput{
		RawEvent: event,
		Session: capture.AgentSession{
			ID:           event.SessionID,
			CaptureLevel: 3,
		},
		Task: capture.AgentTask{
			ID:             event.TaskID,
			TaskSummary:    "实现 automation",
			OutcomeSummary: event.ContentSummary,
		},
		CaptureQuality: CaptureQualitySnapshot{CaptureLevel: 3},
	})
	if err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(drafts))
	}
	sourceRefJSON, err := json.Marshal(drafts[0].SourceRef)
	if err != nil {
		t.Fatalf("marshal source ref: %v", err)
	}
	keywordsJSON, err := json.Marshal(drafts[0].Keywords)
	if err != nil {
		t.Fatalf("marshal keywords: %v", err)
	}
	spansJSON, err := json.Marshal(drafts[0].SalientSpans)
	if err != nil {
		t.Fatalf("marshal salient spans: %v", err)
	}
	return memory.Evidence{
		ID:                   "ev_001",
		SourceType:           drafts[0].SourceType,
		InterpretedStatement: drafts[0].InterpretedStatement,
		KeywordsJSON:         string(keywordsJSON),
		SalientSpansJSON:     string(spansJSON),
		SourceRefJSON:        string(sourceRefJSON),
		Confidence:           drafts[0].Confidence,
		CreatedAt:            time.Now().UTC(),
	}
}

func generate(t *testing.T, provider RuleBasedProvider, event capture.RawEvent, evidence memory.Evidence) []MemoryCandidate {
	t.Helper()
	candidates, err := provider.GenerateCandidates(context.Background(), CandidateInput{
		Evidence: evidence,
		RawEvent: event,
		Session:  capture.AgentSession{ID: event.SessionID},
		Task:     capture.AgentTask{ID: event.TaskID, OutcomeSummary: event.ContentSummary},
		Now:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("GenerateCandidates() error = %v", err)
	}
	return candidates
}

func refsJSON(ref map[string]any) string {
	data, err := json.Marshal([]map[string]any{ref})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func jsonArray(values ...string) string {
	data, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(data)
}
