package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/capture"
)

// TurnRuntime 负责将 Turn payload 展开为多个 ObserveRequest。
// 设计约束：TurnRuntime 不直接写入 raw_event，只负责事件展开和去重状态管理。
type TurnRuntime struct {
	store      StateStore
	expandMode string
}

// NewTurnRuntime 创建 Turn 运行时（legacy 展开）。
func NewTurnRuntime(store StateStore) *TurnRuntime {
	return NewTurnRuntimeWithExpandMode(store, ExpandModeLegacy)
}

// NewTurnRuntimeWithExpandMode 创建带展开模式的 Turn 运行时。
func NewTurnRuntimeWithExpandMode(store StateStore, expandMode string) *TurnRuntime {
	return &TurnRuntime{store: store, expandMode: NormalizeExpandMode(expandMode)}
}

type ToolResultInput struct {
	ToolName         string                   `json:"tool_name"`
	InputSummary     string                   `json:"input_summary"`
	OutputSummary    string                   `json:"output_summary"`
	ExitCode         int                      `json:"exit_code"`
	RawPayloadJSON   string                   `json:"raw_payload_json"`
	PayloadSchema    string                   `json:"payload_schema"`
	RawPayloadHash   string                   `json:"raw_payload_hash"`
	RedactionState   string                   `json:"redaction_state"`
	RedactionPolicy  string                   `json:"redaction_policy"`
	TruncationPolicy capture.TruncationPolicy `json:"truncation"`
}

type FileEditInput struct {
	FilePath         string                   `json:"file_path"`
	ContentSummary   string                   `json:"content_summary"`
	Symbol           string                   `json:"symbol"`
	BeforeHash       string                   `json:"before_hash"`
	AfterHash        string                   `json:"after_hash"`
	ChangeType       string                   `json:"change_type"`
	RawPayloadJSON   string                   `json:"raw_payload_json"`
	PayloadSchema    string                   `json:"payload_schema"`
	RawPayloadHash   string                   `json:"raw_payload_hash"`
	RedactionState   string                   `json:"redaction_state"`
	RedactionPolicy  string                   `json:"redaction_policy"`
	TruncationPolicy capture.TruncationPolicy `json:"truncation"`
}

type TurnPayload struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id"`
	AgentType   string `json:"agent_type"`
	SessionID   string `json:"session_id"`
	TaskID      string `json:"task_id"`

	TurnID        string `json:"turn_id"`
	UserSummary   string `json:"user_summary"`
	AgentSummary  string `json:"agent_summary"`
	StartedAt     string `json:"started_at"`
	CompletedAt   string `json:"completed_at"`
	IsSubstantive bool   `json:"is_substantive"`

	TaskSummary    string `json:"task_summary"`
	TaskStatus     string `json:"task_status"`
	OutcomeSummary string `json:"outcome_summary"`

	UserDeclarationSummary string `json:"user_declaration_summary"`
	UserCorrectionSummary  string `json:"user_correction_summary"`
	DecisionSummary        string `json:"decision_summary"`
	ReasonSummary          string `json:"reason_summary"`

	Keywords          []string          `json:"keywords"`
	SalientSpans      []string          `json:"salient_spans"`
	UserKeywords      []string          `json:"user_keywords"`
	UserSalientSpans  []string          `json:"user_salient_spans"`
	AgentKeywords     []string          `json:"agent_keywords"`
	AgentSalientSpans []string          `json:"agent_salient_spans"`
	ToolResults       []ToolResultInput `json:"tool_results"`
	FileEdits         []FileEditInput   `json:"file_edits"`

	RetrievalTraceID string   `json:"retrieval_trace_id"`
	UsedMemoryIDs    []string `json:"used_memory_ids"`
	InjectedToPrompt bool     `json:"injected_to_prompt"`

	SemanticSummaryVersion string `json:"semantic_summary_version"`
	UserPromptChars        int    `json:"user_prompt_chars"`
	AgentResponseChars     int    `json:"agent_response_chars"`

	UserRawPayload      string                   `json:"user_raw_payload"`
	AgentRawPayload     string                   `json:"agent_raw_payload"`
	UserRawPayloadHash  string                   `json:"user_raw_payload_hash"`
	AgentRawPayloadHash string                   `json:"agent_raw_payload_hash"`
	PayloadSchema       string                   `json:"payload_schema"`
	RedactionState      string                   `json:"redaction_state"`
	RedactionPolicy     string                   `json:"redaction_policy"`
	TruncationPolicy    capture.TruncationPolicy `json:"truncation"`
	UserTruncation      capture.TruncationPolicy `json:"user_truncation"`
	AgentTruncation     capture.TruncationPolicy `json:"agent_truncation"`

	// SkipBaseTurn 为 true 时仅展开 tool/file/decision 等增量事件，不写 conversation/agent 回合。
	SkipBaseTurn bool `json:"skip_base_turn"`
}

func (r *TurnRuntime) BuildObserveRequests(payload TurnPayload) ([]capture.ObserveRequest, error) {
	if strings.TrimSpace(payload.WorkspaceID) == "" || strings.TrimSpace(payload.ProjectID) == "" || strings.TrimSpace(payload.RepoID) == "" {
		return nil, fmt.Errorf("missing required scope fields: workspace_id/project_id/repo_id")
	}
	if strings.TrimSpace(payload.AgentType) == "" {
		return nil, fmt.Errorf("missing required field: agent_type")
	}
	state, err := r.store.Load()
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(payload.SessionID)
	taskID := strings.TrimSpace(payload.TaskID)
	if sessionID == "" {
		return nil, fmt.Errorf("missing required session_id")
	}

	common := capture.ObserveRequest{
		SessionID:     sessionID,
		TaskID:        taskID,
		AgentType:     payload.AgentType,
		WorkspaceID:   payload.WorkspaceID,
		ProjectID:     payload.ProjectID,
		RepoID:        payload.RepoID,
		SourceChannel: capture.SourceChannelAgentSession,
		OccurredAt:    firstNonEmpty(payload.CompletedAt, payload.StartedAt),
		Keywords:      semanticKeywords(payload.Keywords),
		SalientSpans:  payload.SalientSpans,
		SourceRefs: []capture.SourceRef{
			{
				"source_type":      "agent_session",
				"capture_method":   capture.CaptureMethodAdapterHook,
				"protocol_version": ProtocolV1,
			},
		},
	}

	requests := make([]capture.ObserveRequest, 0, 8)
	if payload.TaskSummary != "" && normalize(payload.TaskSummary) != normalize(state.LastTaskSummary) {
		taskReq := common
		taskReq.EventType = capture.EventTaskStart
		taskReq.Actor = capture.ActorAdapter
		taskReq.ContentSummary = capture.EnsureStructuredContentSummary(taskReq.EventType, "任务开始："+payload.TaskSummary)
		taskReq.Task = &capture.TaskInput{
			TaskSummary: payload.TaskSummary,
			Status:      capture.StatusActive,
		}
		requests = append(requests, taskReq)
		state.LastTaskSummary = payload.TaskSummary
	}

	expandV2 := IsExpandModeV2(r.expandMode)
	hasHighSignal := !expandV2 && (len(payload.FileEdits) > 0 || strings.TrimSpace(payload.DecisionSummary) != "")
	signature := turnSignature(payload.TurnID, payload.UserSummary, payload.AgentSummary)
	emitBaseTurn := !payload.SkipBaseTurn && (payload.IsSubstantive || hasHighSignal)
	if emitBaseTurn && !(payload.TurnID != "" && payload.TurnID == state.LastTurnID && signature == state.LastTurnSig) {
		if turnReq, ok := buildBaseTurnRequest(common, payload); ok {
			requests = append(requests, turnReq)
		}
		state.LastTurnID = payload.TurnID
		state.LastTurnSig = signature
	}

	if !expandV2 {
		for _, item := range payload.FileEdits {
			if strings.TrimSpace(item.FilePath) == "" {
				continue
			}
			editReq := common
			editReq.EventType = capture.EventFileEditSummary
			editReq.Actor = capture.ActorAgent
			editReq.ContentSummary = capture.EnsureStructuredContentSummary(editReq.EventType, firstNonEmpty(item.ContentSummary, "文件修改："+item.FilePath))
			applyAtomicRawPayload(&editReq, item.RawPayloadJSON, item.PayloadSchema, item.RawPayloadHash, item.RedactionState, item.RedactionPolicy, item.TruncationPolicy)
			editReq.SourceRefs = append(editReq.SourceRefs, capture.SourceRef{
				"source_type": "file_edit_summary",
				"file_path":   item.FilePath,
				"symbol":      item.Symbol,
				"before_hash": item.BeforeHash,
				"after_hash":  item.AfterHash,
				"change_type": item.ChangeType,
			})
			requests = append(requests, editReq)
		}
	}

	if strings.TrimSpace(payload.DecisionSummary) != "" {
		decisionReq := common
		decisionReq.EventType = capture.EventAgentDecision
		decisionReq.Actor = capture.ActorAgent
		decisionReq.ContentSummary = capture.EnsureStructuredContentSummary(decisionReq.EventType, payload.DecisionSummary)
		decisionReq.SourceRefs = append(decisionReq.SourceRefs, capture.SourceRef{
			"source_type":      "agent_decision",
			"decision_summary": payload.DecisionSummary,
			"reason_summary":   payload.ReasonSummary,
		})
		requests = append(requests, decisionReq)
	}

	if isTerminalStatus(payload.TaskStatus) && strings.TrimSpace(payload.OutcomeSummary) != "" {
		resultReq := common
		resultReq.EventType = capture.EventTaskResult
		resultReq.Actor = capture.ActorAdapter
		resultReq.ContentSummary = capture.EnsureStructuredContentSummary(resultReq.EventType, payload.OutcomeSummary)
		resultReq.Task = &capture.TaskInput{
			TaskSummary:    firstNonEmpty(payload.TaskSummary, state.LastTaskSummary),
			Status:         payload.TaskStatus,
			OutcomeSummary: payload.OutcomeSummary,
		}
		requests = append(requests, resultReq)
	}

	if err := r.store.Save(state); err != nil {
		return nil, err
	}
	return requests, nil
}

func buildBaseTurnRequest(common capture.ObserveRequest, payload TurnPayload) (capture.ObserveRequest, bool) {
	userEventType, userSummary := userTurnSummary(payload)
	agentSummary := strings.TrimSpace(payload.AgentSummary)
	if userSummary == "" && agentSummary == "" {
		return capture.ObserveRequest{}, false
	}
	req := common
	req.EventType = baseTurnEventType(userEventType)
	req.Actor = baseTurnActor(req.EventType)
	req.Keywords = semanticKeywords(mergeStringSlices(
		firstNonEmptySlice(payload.UserKeywords, payload.Keywords),
		firstNonEmptySlice(payload.AgentKeywords, payload.Keywords),
	))
	req.SalientSpans = mergeStringSlices(
		firstNonEmptySlice(payload.UserSalientSpans, payload.SalientSpans),
		firstNonEmptySlice(payload.AgentSalientSpans, payload.SalientSpans),
	)
	req.ContentSummary = turnContentSummary(userEventType, userSummary, agentSummary)
	req.SourceRefs = appendSemanticDigestSourceRef(req.SourceRefs, "user_prompt", payload.UserPromptChars, payload.SemanticSummaryVersion)
	req.SourceRefs = appendSemanticDigestSourceRef(req.SourceRefs, "agent_response", payload.AgentResponseChars, payload.SemanticSummaryVersion)
	req.SourceRefs = appendRetrievalSourceRefs(req.SourceRefs, payload)
	applyCombinedTurnRawPayload(&req, payload)
	return req, true
}

func userTurnSummary(payload TurnPayload) (string, string) {
	switch {
	case strings.TrimSpace(payload.UserCorrectionSummary) != "":
		return capture.EventUserCorrection, strings.TrimSpace(payload.UserCorrectionSummary)
	case strings.TrimSpace(payload.UserDeclarationSummary) != "":
		return capture.EventUserDeclaration, strings.TrimSpace(payload.UserDeclarationSummary)
	default:
		return capture.EventConversationMessage, strings.TrimSpace(payload.UserSummary)
	}
}

func baseTurnEventType(userEventType string) string {
	switch userEventType {
	case capture.EventUserCorrection, capture.EventUserDeclaration:
		return userEventType
	default:
		return capture.EventTurnCompleted
	}
}

func baseTurnActor(eventType string) string {
	switch eventType {
	case capture.EventUserCorrection, capture.EventUserDeclaration:
		return capture.ActorUser
	default:
		return capture.ActorAdapter
	}
}

func turnContentSummary(userEventType, userSummary, agentSummary string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(userSummary) != "" {
		parts = append(parts, capture.EnsureStructuredContentSummary(userEventType, userSummary))
	}
	if strings.TrimSpace(agentSummary) != "" {
		parts = append(parts, capture.EnsureStructuredContentSummary(capture.EventAgentResponseSummary, agentSummary))
	}
	return strings.Join(parts, "\n")
}

func applyTurnRawPayload(req *capture.ObserveRequest, payload TurnPayload, rawPayload, rawPayloadHash string, truncation capture.TruncationPolicy) {
	if truncation == (capture.TruncationPolicy{}) {
		truncation = payload.TruncationPolicy
	}
	applyAtomicRawPayload(req, rawPayload, payload.PayloadSchema, rawPayloadHash, payload.RedactionState, payload.RedactionPolicy, truncation)
}

func applyCombinedTurnRawPayload(req *capture.ObserveRequest, payload TurnPayload) {
	rawPayload, rawPayloadHash, truncation := combinedTurnRawPayload(payload)
	applyAtomicRawPayload(req, rawPayload, payload.PayloadSchema, rawPayloadHash, payload.RedactionState, payload.RedactionPolicy, truncation)
}

func applyAtomicRawPayload(req *capture.ObserveRequest, rawPayload, payloadSchema, rawPayloadHash, redactionState, redactionPolicy string, truncation capture.TruncationPolicy) {
	req.RawPayloadJSON = strings.TrimSpace(rawPayload)
	req.PayloadSchema = strings.TrimSpace(payloadSchema)
	req.RawPayloadHash = strings.TrimSpace(rawPayloadHash)
	req.RedactionState = strings.TrimSpace(redactionState)
	req.RedactionPolicy = strings.TrimSpace(redactionPolicy)
	req.TruncationPolicy = truncation
}

func combinedTurnRawPayload(payload TurnPayload) (string, string, capture.TruncationPolicy) {
	values := map[string]any{}
	if userRaw := rawPayloadValue(payload.UserRawPayload); userRaw != nil {
		values["user"] = userRaw
	}
	if agentRaw := rawPayloadValue(payload.AgentRawPayload); agentRaw != nil {
		values["agent"] = agentRaw
	}
	if len(values) == 0 {
		return "", "", payload.TruncationPolicy
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", "", payload.TruncationPolicy
	}
	sum := sha256.Sum256(data)
	return string(data), "sha256:" + hex.EncodeToString(sum[:]), mergeTruncation(payload.TruncationPolicy, payload.UserTruncation, payload.AgentTruncation)
}

func rawPayloadValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if json.Valid([]byte(raw)) {
		return json.RawMessage(raw)
	}
	return raw
}

func mergeTruncation(base capture.TruncationPolicy, values ...capture.TruncationPolicy) capture.TruncationPolicy {
	out := base
	for _, value := range values {
		if value == (capture.TruncationPolicy{}) {
			continue
		}
		out.Truncated = out.Truncated || value.Truncated
		out.OriginalSizeBytes += value.OriginalSizeBytes
		out.StoredSizeBytes += value.StoredSizeBytes
		if value.MaxSizeBytes > out.MaxSizeBytes {
			out.MaxSizeBytes = value.MaxSizeBytes
		}
		if strings.TrimSpace(value.Reason) != "" {
			out.Reason = joinNonEmpty(out.Reason, value.Reason)
		}
	}
	return out
}

// isTerminalStatus 判断任务状态是否为终态。
// 终态包括：succeeded、failed、interrupted、completed。
func isTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case capture.StatusSucceeded, capture.StatusFailed, capture.StatusInterrupted, capture.StatusCompleted:
		return true
	default:
		return false
	}
}

// turnSignature 计算 Turn 的内容签名（SHA256）。
// 用于内容级去重：相同 turnID + userSummary + agentSummary 生成相同签名。
func turnSignature(turnID, userSummary, agentSummary string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{turnID, userSummary, agentSummary}, "|")))
	return hex.EncodeToString(sum[:])
}

// normalize 归一化字符串：去除首尾空白，合并连续空白为单个空格。
// 用于 task summary 的变更检测。
func normalize(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

// firstNonEmpty 返回第一个非空白字符串。
// 用于优先使用 payload 中的值，回退到 state 中的值。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmptySlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func mergeStringSlices(values ...[]string) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, group := range values {
		for _, value := range group {
			item := strings.TrimSpace(value)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func joinNonEmpty(values ...string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "; ")
}

func semanticKeywords(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		keyword := strings.TrimSpace(value)
		if keyword == "" || isCaptureMetadataKeyword(keyword) {
			continue
		}
		if _, ok := seen[keyword]; ok {
			continue
		}
		seen[keyword] = struct{}{}
		out = append(out, keyword)
	}
	return out
}

func isCaptureMetadataKeyword(keyword string) bool {
	lower := strings.ToLower(strings.TrimSpace(keyword))
	switch lower {
	case "hook", "turn-completed", "trace", "memory-context", "file-edit", "tool-result":
		return true
	default:
		return strings.HasPrefix(lower, "trace:") || strings.HasPrefix(lower, "mem:")
	}
}

func appendSemanticDigestSourceRef(refs []capture.SourceRef, sourceType string, contentChars int, version string) []capture.SourceRef {
	if contentChars <= 0 && strings.TrimSpace(version) == "" {
		return refs
	}
	ref := capture.SourceRef{
		"source_type":    sourceType,
		"capture_method": capture.CaptureMethodAdapterHook,
	}
	if contentChars > 0 {
		ref["content_chars"] = contentChars
	}
	if version = strings.TrimSpace(version); version != "" {
		ref["semantic_summary_version"] = version
	}
	return append(refs, ref)
}

// appendRetrievalSourceRefs 将检索上下文信息追加到 source_refs。
// 当 payload 包含 retrieval_trace_id、used_memory_ids 或 injected_to_prompt 时，
// 构造 memory_context 类型的 source_ref 追加到列表中。
func appendRetrievalSourceRefs(refs []capture.SourceRef, payload TurnPayload) []capture.SourceRef {
	if strings.TrimSpace(payload.RetrievalTraceID) == "" && len(payload.UsedMemoryIDs) == 0 && !payload.InjectedToPrompt {
		return refs
	}
	ref := capture.SourceRef{
		"source_type":    "memory_context",
		"capture_method": capture.CaptureMethodAdapterHook,
	}
	if traceID := strings.TrimSpace(payload.RetrievalTraceID); traceID != "" {
		ref["retrieval_trace_id"] = traceID
	}
	if payload.InjectedToPrompt {
		ref["injected_to_prompt"] = true
	}
	if len(payload.UsedMemoryIDs) > 0 {
		ref["used_memory_ids"] = payload.UsedMemoryIDs
	}
	return append(refs, ref)
}
