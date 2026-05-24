package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/memory"
)

const RuleBasedProviderName = "rule_based"

type RuleBasedProvider struct{}

func NewRuleBasedProvider() RuleBasedProvider {
	return RuleBasedProvider{}
}

func (RuleBasedProvider) Name() string {
	return RuleBasedProviderName
}

func (RuleBasedProvider) ExtractEvidence(ctx context.Context, input EvidenceInput) ([]EvidenceDraft, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	event := input.RawEvent
	statement := eventStatement(event, input.Task, input.Session)
	keywords := decodeStringSlice(event.KeywordsJSON)
	spans := decodeStringSlice(event.SalientSpansJSON)
	sourceRef := baseSourceRef(event)

	switch event.EventType {
	case capture.EventUserDeclaration:
		return evidenceIfStatement("user_declared", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventUserCorrection:
		return evidenceIfStatement("user_confirmed", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventAgentDecision:
		decision := firstNonEmpty(sourceString(sourceRef, "decision_summary"), statement)
		reason := sourceString(sourceRef, "reason_summary")
		return evidenceIfStatement("agent_summary", joinSentences(decision, reason), keywords, spans, sourceRef, input, false), nil
	case capture.EventToolResultSummary:
		if !isFailedToolEvent(event, sourceRef) {
			return nil, nil
		}
		return evidenceIfStatement("tool_output", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventTaskResult:
		return evidenceIfStatement("task_result", firstNonEmpty(input.Task.OutcomeSummary, statement), keywords, spans, sourceRef, input, false), nil
	case capture.EventSessionEnd:
		return evidenceIfStatement("session_summary", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventFileEditSummary:
		if !hasAnySignal(statement, "原因", "决策", "约束", "失败", "修复", "because", "decision", "constraint", "failure", "fix") {
			return nil, nil
		}
		return evidenceIfStatement("file_edit_summary", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventConversationMessage:
		if !hasSemanticSignal(statement) {
			return nil, nil
		}
		return evidenceIfStatement("agent_summary", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventAgentResponseSummary:
		if !hasAnySignal(statement, "结论", "决策", "约束", "复查", "假设", "待确认", "conclusion", "decision", "constraint", "review", "assumption") {
			return nil, nil
		}
		return evidenceIfStatement("agent_summary", statement, keywords, spans, sourceRef, input, false), nil
	default:
		return nil, nil
	}
}

func (RuleBasedProvider) GenerateCandidates(ctx context.Context, input CandidateInput) ([]MemoryCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	statement := strings.TrimSpace(input.Evidence.InterpretedStatement)
	if statement == "" {
		return nil, nil
	}
	sourceRef := baseSourceRef(input.RawEvent)
	keywords := decodeStringSlice(input.Evidence.KeywordsJSON)
	if len(keywords) == 0 {
		keywords = decodeStringSlice(input.RawEvent.KeywordsJSON)
	}
	evidenceIDs := []string{}
	if input.Evidence.ID != "" {
		evidenceIDs = append(evidenceIDs, input.Evidence.ID)
	}

	switch input.RawEvent.EventType {
	case capture.EventUserCorrection:
		return []MemoryCandidate{baseCandidate(input, statement, inheritedCorrectionType(sourceRef), inheritedCorrectionScope(input, sourceRef), keywords, evidenceIDs, "user_correction")}, nil
	case capture.EventUserDeclaration, capture.EventConversationMessage:
		candidate, ok := classifyDeclaration(input, statement, keywords, evidenceIDs)
		if !ok {
			return nil, nil
		}
		return []MemoryCandidate{candidate}, nil
	case capture.EventAgentDecision:
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeDecision, memory.ScopeProjectLocal, keywords, evidenceIDs, "architecture_decision")}, nil
	case capture.EventToolResultSummary:
		if !isFailedToolEvent(input.RawEvent, sourceRef) {
			return nil, nil
		}
		if isRepeatedFailure(input.RawEvent, input.Task, input.RelatedMemory) || hasAnySignal(statement, "重复", "复现", "root cause", "根因", "repeated") {
			return []MemoryCandidate{baseCandidate(input, statement, memory.TypeFailure, failureScope(input.RawEvent), keywords, evidenceIDs, "repeated_failure_signature")}, nil
		}
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeTemporaryState, memory.ScopeSession, keywords, evidenceIDs, "session_only_state")}, nil
	case capture.EventTaskResult:
		if isDesignReview(input.RawEvent, input.Task, statement) {
			return checkpointCandidate(input, statement, keywords, evidenceIDs)
		}
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeSessionSummary, memory.ScopeSession, keywords, evidenceIDs, "task_result_summary")}, nil
	case capture.EventSessionEnd:
		if isDesignReview(input.RawEvent, input.Task, statement) {
			return checkpointCandidate(input, statement, keywords, evidenceIDs)
		}
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeSessionSummary, memory.ScopeSession, keywords, evidenceIDs, "session_summary")}, nil
	case capture.EventAgentResponseSummary:
		if isDesignReview(input.RawEvent, input.Task, statement) {
			return checkpointCandidate(input, statement, keywords, evidenceIDs)
		}
		if hasAnySignal(statement, "决策", "decision") {
			return []MemoryCandidate{baseCandidate(input, statement, memory.TypeDecision, memory.ScopeProjectLocal, keywords, evidenceIDs, "architecture_decision")}, nil
		}
	}
	return nil, nil
}

func evidenceIfStatement(sourceType, statement string, keywords, spans []string, sourceRef map[string]any, input EvidenceInput, ambiguous bool) []EvidenceDraft {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return nil
	}
	return []EvidenceDraft{{
		SourceType:           sourceType,
		InterpretedStatement: statement,
		Keywords:             keywords,
		SalientSpans:         spans,
		SourceRef:            sourceRef,
		Confidence:           evidenceConfidence(sourceType, input, sourceRef, ambiguous),
	}}
}

func evidenceConfidence(sourceType string, input EvidenceInput, sourceRef map[string]any, ambiguous bool) float64 {
	score := 0.50
	switch sourceType {
	case "user_declared":
		score += 0.30
	case "user_confirmed":
		score += 0.35
	case "agent_summary":
		score += 0.20
	case "tool_output":
		score += 0.10
	}
	if input.CaptureQuality.CaptureLevel >= 3 || input.Session.CaptureLevel >= 3 {
		score += 0.10
	}
	if sourceString(sourceRef, "content_hash") != "" || sourceString(sourceRef, "target_event_id") != "" || sourceString(sourceRef, "file_path") != "" {
		score += 0.10
	}
	if input.CaptureQuality.CaptureLevel > 0 && input.CaptureQuality.CaptureLevel <= 1 {
		score -= 0.15
	}
	if ambiguous {
		score -= 0.20
	}
	return clamp(score, 0, 1)
}

func classifyDeclaration(input CandidateInput, statement string, keywords, evidenceIDs []string) (MemoryCandidate, bool) {
	switch {
	case hasAnySignal(statement, "待确认", "未决", "开放问题", "后续确认", "open issue", "todo", "risk"):
		return baseCandidate(input, statement, memory.TypeOpenIssue, scopedProjectOrRepo(input.RawEvent), keywords, evidenceIDs, "open_issue_recorded"), true
	case hasAnySignal(statement, "假设", "前置假设", "默认认为", "基于", "assume", "assumption"):
		return baseCandidate(input, statement, memory.TypeAssumption, memory.ScopeProjectLocal, keywords, evidenceIDs, "assumption_recorded"), true
	case hasAnySignal(statement, "验收", "需求", "目标", "必须支持", "必须满足", "requirement", "acceptance"):
		return baseCandidate(input, statement, memory.TypeRequirement, memory.ScopeProjectLocal, keywords, evidenceIDs, "requirement_declared"), true
	case hasAnySignal(statement, "不要", "不能", "禁止", "不得", "不引入", "边界约束", "安全", "合规", "must not", "constraint"):
		return baseCandidate(input, statement, memory.TypeConstraint, memory.ScopeProjectLocal, keywords, evidenceIDs, "constraint_declared"), true
	case hasAnySignal(statement, "以后", "偏好", "我希望", "回答", "沟通", "prefer", "preference"):
		return baseCandidate(input, statement, memory.TypePreference, memory.ScopeUserGlobal, keywords, evidenceIDs, "user_declared"), true
	case input.RawEvent.ProjectID != "":
		return baseCandidate(input, statement, memory.TypeProjectFact, memory.ScopeProjectLocal, keywords, evidenceIDs, "project_fact_declared"), true
	default:
		return baseCandidate(input, statement, memory.TypePreference, memory.ScopeUserGlobal, keywords, evidenceIDs, "user_declared"), true
	}
}

func baseCandidate(input CandidateInput, content, memoryType, scope string, keywords, evidenceIDs []string, reason string) MemoryCandidate {
	workspaceID, userID, projectID, repoID, sessionID := scopedIdentity(input.RawEvent, scope)
	return MemoryCandidate{
		MemoryType:        memoryType,
		Scope:             scope,
		WorkspaceID:       workspaceID,
		UserID:            userID,
		ProjectID:         projectID,
		RepoID:            repoID,
		SessionID:         sessionID,
		TaskID:            input.RawEvent.TaskID,
		SourceType:        input.Evidence.SourceType,
		Title:             candidateTitle(memoryType, content),
		Content:           content,
		Keywords:          keywords,
		RetrievalCues:     keywords,
		Confidence:        defaultFloat(input.Evidence.Confidence, 0.7),
		Importance:        defaultImportance(memoryType),
		EncodingDepth:     2,
		CandidateReason:   []string{reason},
		SourceEvidenceIDs: evidenceIDs,
	}
}

func scopedIdentity(event capture.RawEvent, scope string) (workspaceID, userID, projectID, repoID, sessionID string) {
	switch scope {
	case memory.ScopeUserGlobal:
		return "", "local_default_user", "", "", ""
	case memory.ScopeProjectLocal:
		return event.WorkspaceID, "", event.ProjectID, "", ""
	case memory.ScopeRepoLocal:
		return event.WorkspaceID, "", "", event.RepoID, ""
	case memory.ScopeSession:
		return event.WorkspaceID, "", "", "", event.SessionID
	default:
		return event.WorkspaceID, "local_default_user", event.ProjectID, event.RepoID, event.SessionID
	}
}

func checkpointCandidate(input CandidateInput, content string, keywords, evidenceIDs []string) ([]MemoryCandidate, error) {
	draft, ok := reviewCheckpointDraft(input.RawEvent)
	if !ok {
		return nil, nil
	}
	candidate := baseCandidate(input, content, memory.TypeReviewCheckpoint, memory.ScopeProjectLocal, keywords, evidenceIDs, "design_review_checkpoint")
	candidate.ReviewCheckpoint = &draft
	return []MemoryCandidate{candidate}, nil
}

func reviewCheckpointDraft(event capture.RawEvent) (ReviewCheckpointDraft, bool) {
	ref := baseSourceRef(event)
	targetDocs := sourceMapSlice(ref, "target_docs")
	reviewIntent := sourceStringSlice(ref, "review_intent")
	conclusion := sourceString(ref, "conclusion")
	if len(targetDocs) == 0 || len(reviewIntent) == 0 || conclusion == "" {
		return ReviewCheckpointDraft{}, false
	}
	return ReviewCheckpointDraft{
		CheckpointType:    firstNonEmpty(sourceString(ref, "checkpoint_type"), "design_review"),
		ReviewIntent:      reviewIntent,
		TargetDocs:        targetDocs,
		TargetSections:    sourceMapSlice(ref, "target_sections"),
		TargetHashes:      sourceMapSlice(ref, "target_hashes"),
		Conclusion:        conclusion,
		ConfirmedBaseline: sourceStringSlice(ref, "confirmed_baseline"),
		IgnoredItems:      sourceStringSlice(ref, "ignored_items"),
		DeferredItems:     sourceStringSlice(ref, "deferred_items"),
		OpenItems:         sourceStringSlice(ref, "open_items"),
		NextReviewPolicy:  sourceMap(ref, "next_review_policy"),
	}, true
}

func baseSourceRef(event capture.RawEvent) map[string]any {
	refs := decodeSourceRefs(event.SourceRefsJSON)
	ref := map[string]any{}
	if len(refs) > 0 {
		for k, v := range refs[0] {
			ref[k] = v
		}
	}
	ref["raw_event_id"] = event.ID
	if event.ContentHash != "" {
		ref["content_hash"] = event.ContentHash
	}
	if event.ToolName != "" {
		ref["tool_name"] = event.ToolName
	}
	return ref
}

func eventStatement(event capture.RawEvent, task capture.AgentTask, session capture.AgentSession) string {
	return firstNonEmpty(
		event.ContentSummary,
		event.OutputSummary,
		task.OutcomeSummary,
		task.TaskSummary,
		session.GoalSummary,
		firstString(decodeStringSlice(event.SalientSpansJSON)),
	)
}

func isFailedToolEvent(event capture.RawEvent, ref map[string]any) bool {
	if event.EventType != capture.EventToolResultSummary {
		return false
	}
	if exit, ok := numberValue(ref["exit_code"]); ok && exit != 0 {
		return true
	}
	return hasAnySignal(event.OutputSummary, "error", "failed", "failure", "panic", "exception", "失败", "错误", "报错")
}

func isRepeatedFailure(event capture.RawEvent, task capture.AgentTask, related []memory.MemoryItem) bool {
	if len(related) > 0 {
		return true
	}
	return hasAnySignal(event.OutputSummary, "again", "repeated", "多次", "重复") || hasAnySignal(task.OutcomeSummary, "again", "repeated", "多次", "重复")
}

func isDesignReview(event capture.RawEvent, task capture.AgentTask, statement string) bool {
	ref := baseSourceRef(event)
	if len(sourceMapSlice(ref, "target_docs")) > 0 {
		return true
	}
	return hasAnySignal(strings.Join([]string{statement, task.TaskSummary}, " "), "架构设计", "分期规划", "详细设计", "复查", "逻辑缺失", "验收", "review", "design", "architecture")
}

func inheritedCorrectionType(ref map[string]any) string {
	if value := sourceString(ref, "target_memory_type"); value != "" {
		return value
	}
	return memory.TypeProjectFact
}

func inheritedCorrectionScope(input CandidateInput, ref map[string]any) string {
	if value := sourceString(ref, "target_memory_scope"); value != "" {
		return value
	}
	return scopeForProject(input.RawEvent)
}

func failureScope(event capture.RawEvent) string {
	if event.RepoID != "" {
		return memory.ScopeRepoLocal
	}
	return memory.ScopeProjectLocal
}

func scopedProjectOrRepo(event capture.RawEvent) string {
	if event.RepoID != "" {
		return memory.ScopeRepoLocal
	}
	return memory.ScopeProjectLocal
}

func scopeForProject(event capture.RawEvent) string {
	if event.ProjectID != "" {
		return memory.ScopeProjectLocal
	}
	if event.RepoID != "" {
		return memory.ScopeRepoLocal
	}
	return memory.ScopeSession
}

func candidateTitle(memoryType, content string) string {
	content = strings.TrimSpace(content)
	if len([]rune(content)) > 32 {
		content = string([]rune(content)[:32])
	}
	if content == "" {
		return memoryType
	}
	return fmt.Sprintf("%s: %s", memoryType, content)
}

func defaultImportance(memoryType string) float64 {
	switch memoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeRequirement, memory.TypeReviewCheckpoint:
		return 0.8
	case memory.TypeFailure, memory.TypeAssumption, memory.TypeOpenIssue:
		return 0.7
	case memory.TypeTemporaryState, memory.TypeSessionSummary:
		return 0.4
	default:
		return 0.6
	}
}

func hasSemanticSignal(text string) bool {
	return hasAnySignal(text, "记住", "以后", "必须", "不要", "不能", "需求", "验收", "假设", "待确认", "约束", "复查", "prefer", "requirement", "assumption", "open issue", "constraint", "review")
}

func hasAnySignal(text string, signals ...string) bool {
	text = strings.ToLower(text)
	for _, signal := range signals {
		if strings.Contains(text, strings.ToLower(signal)) {
			return true
		}
	}
	return false
}

func decodeStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func decodeSourceRefs(raw string) []map[string]any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var refs []map[string]any
	if err := json.Unmarshal([]byte(raw), &refs); err == nil {
		return refs
	}
	var ref map[string]any
	if err := json.Unmarshal([]byte(raw), &ref); err == nil {
		return []map[string]any{ref}
	}
	return nil
}

func sourceString(ref map[string]any, key string) string {
	value, ok := ref[key]
	if !ok || value == nil {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return str
}

func sourceStringSlice(ref map[string]any, key string) []string {
	value, ok := ref[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func sourceMapSlice(ref map[string]any, key string) []map[string]any {
	value, ok := ref[key]
	if !ok {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			out = append(out, mapped)
		}
	}
	return out
}

func sourceMap(ref map[string]any, key string) map[string]any {
	value, ok := ref[key]
	if !ok {
		return nil
	}
	mapped, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return mapped
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func joinSentences(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

func defaultFloat(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
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
