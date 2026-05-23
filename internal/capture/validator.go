package capture

import (
	"fmt"
	"strings"

	"github.com/zaneway/the-one/internal/config"
)

// NormalizeObserve 归一化并校验 observe 请求的阶段边界。
//
// 该方法只处理 P2 RawEvent 层所需的轻量校验：事件类型、来源、Actor、session 绑定和 task 摘要归一化；
// 内容长度、全文字段和 source_refs 边界由 CheckMinimizedObserve 单独负责，避免职责混杂。
func NormalizeObserve(cfg config.CaptureConfig, req *ObserveRequest) error {
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.AgentType = strings.TrimSpace(req.AgentType)
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.EventType = strings.TrimSpace(req.EventType)
	req.SourceChannel = strings.TrimSpace(req.SourceChannel)
	req.OccurredAt = strings.TrimSpace(req.OccurredAt)
	req.Actor = strings.TrimSpace(req.Actor)
	req.ToolName = strings.TrimSpace(req.ToolName)
	req.InputSummary = strings.TrimSpace(req.InputSummary)
	req.OutputSummary = strings.TrimSpace(req.OutputSummary)
	req.ContentSummary = strings.TrimSpace(req.ContentSummary)
	req.ContentHash = strings.TrimSpace(req.ContentHash)
	req.Sensitivity = strings.TrimSpace(req.Sensitivity)
	req.RetentionHint = strings.TrimSpace(req.RetentionHint)

	if req.EventType == "" {
		return fmt.Errorf("VALIDATION_FAILED: event_type is required")
	}
	if !validEventType(req.EventType) {
		return fmt.Errorf("VALIDATION_FAILED: unsupported event_type %q", req.EventType)
	}
	if req.SourceChannel == "" {
		req.SourceChannel = SourceChannelMCPTool
	}
	if !validSourceChannel(req.SourceChannel) {
		return fmt.Errorf("VALIDATION_FAILED: unsupported source_channel %q", req.SourceChannel)
	}
	if req.Sensitivity == "" {
		req.Sensitivity = SensitivityNormal
	}
	if req.Actor != "" && !validActor(req.Actor) {
		return fmt.Errorf("VALIDATION_FAILED: unsupported actor %q", req.Actor)
	}
	if req.SourceChannel == SourceChannelAgentSession {
		if req.WorkspaceID == "" {
			return fmt.Errorf("VALIDATION_FAILED: agent_session event requires workspace_id")
		}
		if req.AgentType == "" {
			return fmt.Errorf("VALIDATION_FAILED: agent_session event requires agent_type")
		}
		if cfg.RequireSessionForAgentEvents && req.EventType != EventSessionStart && req.SessionID == "" {
			return fmt.Errorf("SESSION_REQUIRED: agent_session event requires session_id")
		}
	}
	if req.AgentType == "" {
		req.AgentType = strings.TrimSpace(cfg.DefaultAgentType)
	}
	normalizeList(req.Keywords)
	normalizeList(req.SalientSpans)
	if req.Session != nil {
		req.Session.GoalSummary = strings.TrimSpace(req.Session.GoalSummary)
		req.Session.Status = normalizeStatus(req.Session.Status)
	}
	if req.Task != nil {
		req.Task.TaskSummary = NormalizeTaskSummary(req.Task.TaskSummary)
		req.Task.Status = normalizeStatus(req.Task.Status)
		req.Task.OutcomeSummary = strings.TrimSpace(req.Task.OutcomeSummary)
	}
	return validateSourceRefs(req.SourceRefs)
}

// NormalizeTaskSummary 生成任务查找用的轻量标准化摘要，P2 不持久化该值。
func NormalizeTaskSummary(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func validEventType(eventType string) bool {
	switch eventType {
	case EventSessionStart, EventSessionEnd, EventTaskStart, EventTaskResult,
		EventConversationMessage, EventAgentResponseSummary, EventToolCall, EventToolResultSummary,
		EventFileEditSummary, EventUserCorrection, EventUserDeclaration, EventAgentDecision:
		return true
	default:
		return false
	}
}

func validSourceChannel(sourceChannel string) bool {
	switch sourceChannel {
	case SourceChannelAgentSession, SourceChannelMCPTool, SourceChannelManualCLI:
		return true
	default:
		return false
	}
}

func validActor(actor string) bool {
	switch actor {
	case ActorUser, ActorAgent, ActorTool, ActorAdapter, ActorSystem:
		return true
	default:
		return false
	}
}

func normalizeList(values []string) {
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
}

func normalizeStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return StatusActive
	}
	return status
}

func validateSourceRefs(refs []SourceRef) error {
	for _, ref := range refs {
		raw, ok := ref["capture_method"]
		if !ok || raw == nil || raw == "" {
			continue
		}
		method, ok := raw.(string)
		if !ok {
			return fmt.Errorf("VALIDATION_FAILED: source_refs.capture_method must be string")
		}
		if !validCaptureMethod(strings.TrimSpace(method)) {
			return fmt.Errorf("VALIDATION_FAILED: unsupported capture_method %q", method)
		}
		ref["capture_method"] = strings.TrimSpace(method)
	}
	return nil
}

func validCaptureMethod(method string) bool {
	switch method {
	case CaptureMethodAdapterHook, CaptureMethodWrapperLog, CaptureMethodCursorRule,
		CaptureMethodManualMCPCall, CaptureMethodFilesystemWatcher, CaptureMethodGitDiff:
		return true
	default:
		return false
	}
}
