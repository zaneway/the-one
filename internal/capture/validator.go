package capture

import (
	"fmt"
	"strings"

	"github.com/zaneway/theone/internal/config"
)

// NormalizeObserve 归一化并校验 observe 请求的阶段边界
// 处理流程：
// 1. 去除所有字段的空白字符
// 2. 校验event_type合法性
// 3. 设置默认source_channel（mcp_tool）
// 4. 校验source_channel合法性
// 5. 设置默认sensitivity（normal）
// 6. 校验actor合法性
// 7. agent_session来源事件必须有workspace_id和agent_type
// 8. 设置默认agent_type
// 9. 归一化keywords和salient_spans
// 10. 归一化session和task信息
// 11. 校验source_refs中的capture_method
// 设计说明：该方法只处理P2 RawEvent层所需的轻量校验，内容边界由CheckMinimizedObserve单独负责
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
	// agent_session 来源的事件有更严格的校验：必须有 workspace_id 和 agent_type
	// session.start 事件除外（此时 session_id 尚未生成）
	if req.SourceChannel == SourceChannelAgentSession {
		if req.WorkspaceID == "" {
			return fmt.Errorf("VALIDATION_FAILED: agent_session event requires workspace_id")
		}
		if req.AgentType == "" {
			return fmt.Errorf("VALIDATION_FAILED: agent_session event requires agent_type")
		}
		// RequireSessionForAgentEvents 配置开启时，非 session.start 事件必须携带 session_id
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

// NormalizeTaskSummary 生成任务查找用的轻量标准化摘要
// 处理流程：去除空白、合并连续空格
// 设计说明：P2 不持久化该值，只用于内存中的任务查找
func NormalizeTaskSummary(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

// validEventType 校验事件类型是否合法
// 合法值：session.start、session.end、task.start、task.result、conversation.message、
//
//	agent.response.summary、tool.call、tool.result.summary、file.edit.summary、
//	user.correction、user.declaration、agent.decision
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

// validSourceChannel 校验来源渠道是否合法
// 合法值：agent_session、mcp_tool、manual_cli
func validSourceChannel(sourceChannel string) bool {
	switch sourceChannel {
	case SourceChannelAgentSession, SourceChannelMCPTool, SourceChannelManualCLI:
		return true
	default:
		return false
	}
}

// validActor 校验行为者是否合法
// 合法值：user、agent、tool、adapter、system
func validActor(actor string) bool {
	switch actor {
	case ActorUser, ActorAgent, ActorTool, ActorAdapter, ActorSystem:
		return true
	default:
		return false
	}
}

// normalizeList 归一化字符串列表，去除每个元素的空白字符
func normalizeList(values []string) {
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
}

// normalizeStatus 归一化状态值
// 为空时返回默认值"active"
func normalizeStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return StatusActive
	}
	return status
}

// validateSourceRefs 校验来源引用中的capture_method
// 遍历所有source_refs，校验capture_method字段的合法性
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

// validCaptureMethod 校验捕获方法是否合法
// 合法值：adapter_hook、wrapper_log、cursor_rule、manual_mcp_call、filesystem_watcher、git_diff
func validCaptureMethod(method string) bool {
	switch method {
	case CaptureMethodAdapterHook, CaptureMethodWrapperLog, CaptureMethodCursorRule,
		CaptureMethodManualMCPCall, CaptureMethodFilesystemWatcher, CaptureMethodGitDiff:
		return true
	default:
		return false
	}
}
