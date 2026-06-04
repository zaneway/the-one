package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/scoring"
)

const RuleBasedProviderName = "rule_based"

type RuleBasedProvider struct{}

func NewRuleBasedProvider() RuleBasedProvider {
	return RuleBasedProvider{}
}

func (RuleBasedProvider) Name() string {
	return RuleBasedProviderName
}

// ExtractEvidence 从 raw_event 中抽取 evidence 草稿。
// 核心逻辑：按事件类型路由，不同类型采用不同的抽取策略和质量过滤。
// 设计原则：只抽取有记忆价值的事件，低信号事件（如普通文件编辑、普通对话）直接跳过。
//
// 事件类型路由策略：
//   - user_declared / user_confirmed：用户主动声明或纠正，最高置信度，直接抽取
//   - agent_decision：Agent 决策，合并 decision_summary 和 reason_summary
//   - tool_result_summary：仅保留失败的工具调用（exit_code!=0 或包含错误关键词）
//   - task_result / session_end：任务/会话结束摘要，直接抽取
//   - file_edit_summary：仅保留包含决策/约束/失败等高信号关键词的编辑摘要
//   - conversation_message：仅保留包含语义信号（记住、必须、需求等）的对话
//   - agent_response_summary：仅保留包含结论/决策/约束等高信号的响应摘要
func (RuleBasedProvider) ExtractEvidence(ctx context.Context, input EvidenceInput) ([]EvidenceDraft, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	event := input.RawEvent
	// eventStatement 按优先级从多个字段中提取最佳描述文本
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
		// 合并决策摘要和原因摘要为完整的 evidence 语句
		decision := firstNonEmpty(sourceString(sourceRef, "decision_summary"), statement)
		reason := sourceString(sourceRef, "reason_summary")
		return evidenceIfStatement("agent_summary", joinSentences(decision, reason), keywords, spans, sourceRef, input, false), nil
	case capture.EventToolResultSummary:
		// 只保留失败的工具调用，成功的工具输出通常不含记忆价值
		if !isFailedToolEvent(event, sourceRef) {
			return nil, nil
		}
		return evidenceIfStatement("tool_output", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventTaskResult:
		return evidenceIfStatement("task_result", firstNonEmpty(input.Task.OutcomeSummary, statement), keywords, spans, sourceRef, input, false), nil
	case capture.EventSessionEnd:
		return evidenceIfStatement("session_summary", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventFileEditSummary:
		// 普通文件编辑无记忆价值，仅保留包含决策/约束/失败信号的编辑
		if !hasAnySignal(statement, "原因", "决策", "约束", "失败", "修复", "because", "decision", "constraint", "failure", "fix") {
			return nil, nil
		}
		return evidenceIfStatement("file_edit_summary", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventConversationMessage:
		// 普通对话无记忆价值，仅保留包含语义信号（记住、必须、需求等）的对话
		if !hasSemanticSignal(statement) {
			return nil, nil
		}
		return evidenceIfStatement("agent_summary", statement, keywords, spans, sourceRef, input, false), nil
	case capture.EventAgentResponseSummary:
		// Agent 响应摘要中仅保留包含结论/决策/约束等高信号内容
		if !hasAnySignal(statement, "结论", "决策", "约束", "复查", "假设", "待确认", "conclusion", "decision", "constraint", "review", "assumption") {
			return nil, nil
		}
		return evidenceIfStatement("agent_summary", statement, keywords, spans, sourceRef, input, false), nil
	default:
		return nil, nil
	}
}

// GenerateCandidates 从 evidence 中生成候选记忆。
// 核心逻辑：按事件类型路由到不同的候选分类策略，每种类型产生不同 memoryType 和 scope 的候选记忆。
//
// 候选分类策略：
//   - user_correction：继承原记忆的 type 和 scope，用于覆盖写入
//   - user_declaration / conversation_message：通过 classifyDeclaration 做信号分类
//     （开放问题 → open_issue，假设 → assumption，需求 → requirement，约束 → constraint，偏好 → preference）
//   - agent_decision：固定为 TypeDecision + ScopeProjectLocal（架构决策）
//   - tool_result_summary：失败的工具调用，重复失败 → TypeFailure，单次失败 → TypeTemporaryState
//   - task_result / session_end / agent_response_summary：先检测是否为设计复查（→ checkpoint），否则按类型处理
func (RuleBasedProvider) GenerateCandidates(ctx context.Context, input CandidateInput) ([]MemoryCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	statement := strings.TrimSpace(input.Evidence.InterpretedStatement)
	if statement == "" {
		return nil, nil
	}
	sourceRef := baseSourceRef(input.RawEvent)
	// 优先使用 evidence 的 keywords，回退到 raw_event 的 keywords
	keywords := decodeStringSlice(input.Evidence.KeywordsJSON)
	if len(keywords) == 0 {
		keywords = decodeStringSlice(input.RawEvent.KeywordsJSON)
	}
	evidenceIDs := []string{}
	if input.Evidence.ID != "" {
		evidenceIDs = append(evidenceIDs, input.Evidence.ID)
	}
	eventScore := scoring.ScoreRawEvent(scoring.RawEventInput{
		EventType:      input.RawEvent.EventType,
		OccurredAt:     input.RawEvent.OccurredAt,
		ContentSummary: input.RawEvent.ContentSummary,
		InputSummary:   input.RawEvent.InputSummary,
		OutputSummary:  input.RawEvent.OutputSummary,
		KeywordsJSON:   input.RawEvent.KeywordsJSON,
		SourceRefsJSON: input.RawEvent.SourceRefsJSON,
		Query:          statement,
		Now:            input.Now,
	})

	switch input.RawEvent.EventType {
	case capture.EventUserCorrection:
		// 用户纠正：继承原记忆的 type 和 scope，确保覆盖写入时保持一致
		return []MemoryCandidate{baseCandidate(input, statement, inheritedCorrectionType(sourceRef), inheritedCorrectionScope(input, sourceRef), keywords, evidenceIDs, "user_correction", eventScore)}, nil
	case capture.EventUserDeclaration, capture.EventConversationMessage:
		// 用户声明或对话消息：通过关键词信号分类为不同记忆类型
		candidate, ok := classifyDeclaration(input, statement, keywords, evidenceIDs, eventScore)
		if !ok {
			return nil, nil
		}
		return []MemoryCandidate{candidate}, nil
	case capture.EventAgentDecision:
		// Agent 决策：固定为架构决策类型，项目级作用域
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeDecision, memory.ScopeProjectLocal, keywords, evidenceIDs, "architecture_decision", eventScore)}, nil
	case capture.EventToolResultSummary:
		if !isFailedToolEvent(input.RawEvent, sourceRef) {
			return nil, nil
		}
		// 重复失败或包含根因信号：标记为持久失败签名（可跨 session 复用）
		if isRepeatedFailure(input.RawEvent, input.Task, input.RelatedMemory) || hasAnySignal(statement, "重复", "复现", "root cause", "根因", "repeated") {
			return []MemoryCandidate{baseCandidate(input, statement, memory.TypeFailure, failureScope(input.RawEvent), keywords, evidenceIDs, "repeated_failure_signature", eventScore)}, nil
		}
		// 单次失败：仅保留为 session 级临时状态
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeTemporaryState, memory.ScopeSession, keywords, evidenceIDs, "session_only_state", eventScore)}, nil
	case capture.EventTaskResult:
		// 设计复查结果：生成 review_checkpoint 记忆（含 target_docs 和 hash）
		if isDesignReview(input.RawEvent, input.Task, statement) {
			return checkpointCandidate(input, statement, keywords, evidenceIDs)
		}
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeSessionSummary, memory.ScopeSession, keywords, evidenceIDs, "task_result_summary", eventScore)}, nil
	case capture.EventSessionEnd:
		if isDesignReview(input.RawEvent, input.Task, statement) {
			return checkpointCandidate(input, statement, keywords, evidenceIDs)
		}
		return []MemoryCandidate{baseCandidate(input, statement, memory.TypeSessionSummary, memory.ScopeSession, keywords, evidenceIDs, "session_summary", eventScore)}, nil
	case capture.EventAgentResponseSummary:
		if isDesignReview(input.RawEvent, input.Task, statement) {
			return checkpointCandidate(input, statement, keywords, evidenceIDs)
		}
		// 包含决策信号：归类为架构决策
		if hasAnySignal(statement, "决策", "decision") {
			return []MemoryCandidate{baseCandidate(input, statement, memory.TypeDecision, memory.ScopeProjectLocal, keywords, evidenceIDs, "architecture_decision", eventScore)}, nil
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

// evidenceConfidence 计算 evidence 的置信度分数（0~1）。
// 计算规则：
//   - 基础分 0.50
//   - 来源加权：user_declared +0.30, user_confirmed +0.35, agent_summary +0.20, tool_output +0.10
//   - 高质量采集（CaptureLevel>=3）+0.10
//   - 有可追溯来源（content_hash/target_event_id/file_path）+0.10
//   - 低质量采集（CaptureLevel<=1）-0.15
//   - 歧义标记（ambiguous）-0.20
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

// classifyDeclaration 对用户声明/对话消息做信号分类，决定候选记忆的类型和作用域。
// 分类优先级（先匹配先返回）：
//  1. 开放问题信号（待确认、未决、todo） → TypeOpenIssue, ScopeProjectLocal/RepoLocal
//  2. 假设信号（假设、前置假设） → TypeAssumption, ScopeProjectLocal
//  3. 需求信号（验收、需求、必须） → TypeRequirement, ScopeProjectLocal
//  4. 约束信号（不要、禁止、不得） → TypeConstraint, ScopeProjectLocal
//  5. 偏好信号（以后、偏好、我希望） → TypePreference, ScopeUserGlobal
//  6. 有关联项目 → TypeProjectFact, ScopeProjectLocal
//  7. 默认 → TypePreference, ScopeUserGlobal
func classifyDeclaration(input CandidateInput, statement string, keywords, evidenceIDs []string, eventScore float64) (MemoryCandidate, bool) {
	switch {
	case hasAnySignal(statement, "待确认", "未决", "开放问题", "后续确认", "open issue", "todo", "risk"):
		return baseCandidate(input, statement, memory.TypeOpenIssue, scopedProjectOrRepo(input.RawEvent), keywords, evidenceIDs, "open_issue_recorded", eventScore), true
	case hasAnySignal(statement, "假设", "前置假设", "默认认为", "基于", "assume", "assumption"):
		return baseCandidate(input, statement, memory.TypeAssumption, memory.ScopeProjectLocal, keywords, evidenceIDs, "assumption_recorded", eventScore), true
	case hasAnySignal(statement, "验收", "需求", "目标", "必须支持", "必须满足", "requirement", "acceptance"):
		return baseCandidate(input, statement, memory.TypeRequirement, memory.ScopeProjectLocal, keywords, evidenceIDs, "requirement_declared", eventScore), true
	case hasAnySignal(statement, "不要", "不能", "禁止", "不得", "不引入", "边界约束", "安全", "合规", "must not", "constraint"):
		return baseCandidate(input, statement, memory.TypeConstraint, memory.ScopeProjectLocal, keywords, evidenceIDs, "constraint_declared", eventScore), true
	case hasAnySignal(statement, "以后", "偏好", "我希望", "回答", "沟通", "prefer", "preference"):
		return baseCandidate(input, statement, memory.TypePreference, memory.ScopeUserGlobal, keywords, evidenceIDs, "user_declared", eventScore), true
	case input.RawEvent.ProjectID != "":
		return baseCandidate(input, statement, memory.TypeProjectFact, memory.ScopeProjectLocal, keywords, evidenceIDs, "project_fact_declared", eventScore), true
	default:
		return baseCandidate(input, statement, memory.TypePreference, memory.ScopeUserGlobal, keywords, evidenceIDs, "user_declared", eventScore), true
	}
}

// baseCandidate 构造候选记忆的基础字段。
// scope 决定了哪些 identity 字段会被填充（见 scopedIdentity）。
// EncodingDepth 固定为 2（rule_based 提取器不做深度编码，留给 admission 评估）。
func baseCandidate(input CandidateInput, content, memoryType, scope string, keywords, evidenceIDs []string, reason string, eventScore float64) MemoryCandidate {
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
		EventScore:        eventScore,
		CandidateReason:   []string{reason},
		SourceEvidenceIDs: evidenceIDs,
	}
}

// scopedIdentity 根据 scope 决定候选记忆的 identity 字段填充策略。
// 不同 scope 的设计意图：
//   - UserGlobal：只填 userID，跨项目/仓库/会话共享
//   - ProjectLocal：只填 projectID，项目内共享
//   - RepoLocal：只填 repoID，仓库内共享
//   - Session：只填 sessionID，单次会话内有效
//   - default（兜底）：全部填充，保留完整上下文
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

// checkpointCandidate 构造设计复查类型的候选记忆。
// 设计复查记忆包含 ReviewCheckpoint 结构，记录 target_docs、review_intent、conclusion 等
// 用于 Doc Index 策略中对比文档变更。
func checkpointCandidate(input CandidateInput, content string, keywords, evidenceIDs []string) ([]MemoryCandidate, error) {
	draft, ok := reviewCheckpointDraft(input.RawEvent)
	if !ok {
		return nil, nil
	}
	eventScore := scoring.ScoreRawEvent(scoring.RawEventInput{
		EventType:      input.RawEvent.EventType,
		OccurredAt:     input.RawEvent.OccurredAt,
		ContentSummary: input.RawEvent.ContentSummary,
		InputSummary:   input.RawEvent.InputSummary,
		OutputSummary:  input.RawEvent.OutputSummary,
		KeywordsJSON:   input.RawEvent.KeywordsJSON,
		SourceRefsJSON: input.RawEvent.SourceRefsJSON,
		Query:          content,
		Now:            input.Now,
	})
	candidate := baseCandidate(input, content, memory.TypeReviewCheckpoint, memory.ScopeProjectLocal, keywords, evidenceIDs, "design_review_checkpoint", eventScore)
	candidate.ReviewCheckpoint = &draft
	return []MemoryCandidate{candidate}, nil
}

// reviewCheckpointDraft 从 raw_event 的 source_ref 中提取设计复查 checkpoint 结构。
// 必须包含 target_docs、review_intent、conclusion 三个核心字段才视为有效 checkpoint。
// target_hashes 用于 Doc Index 策略中对比文档 section 级变更。
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

// isFailedToolEvent 判断工具调用是否失败。
// 判断条件：exit_code != 0 或输出摘要包含错误关键词。
func isFailedToolEvent(event capture.RawEvent, ref map[string]any) bool {
	if event.EventType != capture.EventToolResultSummary {
		return false
	}
	if exit, ok := numberValue(ref["exit_code"]); ok && exit != 0 {
		return true
	}
	return hasAnySignal(event.OutputSummary, "error", "failed", "failure", "panic", "exception", "失败", "错误", "报错")
}

// isRepeatedFailure 判断是否为重复失败。
// 判断条件：存在相关历史失败记忆，或输出/任务摘要包含重复信号。
// 重复失败会上升为 TypeFailure（跨 session），单次失败仅为 TypeTemporaryState。
func isRepeatedFailure(event capture.RawEvent, task capture.AgentTask, related []memory.MemoryItem) bool {
	if len(related) > 0 {
		return true
	}
	return hasAnySignal(event.OutputSummary, "again", "repeated", "多次", "重复") || hasAnySignal(task.OutcomeSummary, "again", "repeated", "多次", "重复")
}

// isDesignReview 判断是否为设计复查事件。
// 判断条件：source_ref 包含 target_docs，或语句/任务摘要包含架构设计/复查等关键词。
// 设计复查事件会生成 TypeReviewCheckpoint 记忆，用于 Doc Index 策略。
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
	title := conciseTitleText(content, 96)
	if title == "" {
		return memoryType
	}
	return fmt.Sprintf("%s: %s", memoryType, title)
}

func conciseTitleText(content string, maxRunes int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	for _, sep := range []string{"\n", "。", "！", "？", ". ", "! ", "? "} {
		if idx := strings.Index(content, sep); idx > 0 {
			end := idx + len(sep)
			if strings.HasPrefix(sep, "\n") {
				end = idx
			}
			candidate := strings.TrimSpace(content[:end])
			if candidate != "" {
				content = candidate
				break
			}
		}
	}
	runes := []rune(content)
	if maxRunes > 0 && len(runes) > maxRunes {
		return strings.TrimSpace(string(runes[:maxRunes])) + "..."
	}
	return content
}

// defaultImportance 根据 memoryType 返回默认重要性分数。
// 高重要性（0.8）：决策、约束、需求、设计复查 — 架构级信息
// 中高重要性（0.7）：失败、假设、开放问题 — 需要关注但可降级
// 低重要性（0.4）：临时状态、会话摘要 — 生命周期短
// 默认（0.6）：项目事实、偏好等
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

// hasSemanticSignal 判断文本是否包含语义信号词。
// 用于 ConversationMessage 和 AgentResponseSummary 的质量过滤：
// 只有包含"记住、必须、需求、假设"等信号的文本才有记忆价值。
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
