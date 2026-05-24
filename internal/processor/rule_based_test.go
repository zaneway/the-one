package processor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/memory"
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
	event := rawEvent(capture.EventUserDeclaration, "P3 不要引入外部 LLM Provider，必须保持 rule_based 本地可测。")
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
		"decision_summary": "P3 只实现 rule_based Provider。",
		"reason_summary":   "保持本地可测，外部 LLM 放到二期。",
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
	event.OutputSummary = "ok github.com/zaneway/the-one/internal/memory"
	event.SourceRefsJSON = refsJSON(map[string]any{"exit_code": 0})

	evidence, err := provider.ExtractEvidence(context.Background(), EvidenceInput{RawEvent: event})
	if err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence count = %d, want 0", len(evidence))
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
	event := rawEvent(capture.EventTaskResult, "P3 详细设计复查完成，存在少量遗漏，已补充自动写入规则。")
	event.SourceRefsJSON = refsJSON(map[string]any{
		"target_docs": []map[string]any{{
			"path": "doc/The One 长期记忆系统 P3 详细设计.md",
			"role": "implementation_design",
		}},
		"review_intent":      []string{"logic_consistency", "acceptance_completeness"},
		"conclusion":         "supplemented",
		"confirmed_baseline": []string{"P3 只实现 rule_based Provider"},
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
			TaskSummary:    "实现 P3",
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
	return memory.Evidence{
		ID:                   "ev_001",
		SourceType:           drafts[0].SourceType,
		InterpretedStatement: drafts[0].InterpretedStatement,
		KeywordsJSON:         string(keywordsJSON),
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
