package capture

// CaptureQuality 是 agent_session.capture_quality_json 的服务层结构。
type CaptureQuality struct {
	HasSessionStart           bool     `json:"has_session_start"`
	HasSessionEnd             bool     `json:"has_session_end"`
	HasTaskResult             bool     `json:"has_task_result"`
	CapturedEventCount        int      `json:"captured_event_count"`
	DedupedEventCount         int      `json:"deduped_event_count"`
	ToolCallCount             int      `json:"tool_call_count"`
	ToolResultCount           int      `json:"tool_result_count"`
	FileEditCount             int      `json:"file_edit_count"`
	ConversationMessageCount  int      `json:"conversation_message_count"`
	MissingCapabilities       []string `json:"missing_capabilities"`
	ContentBoundaryRejections int      `json:"content_boundary_rejections"`
	LastEventAt               string   `json:"last_event_at,omitempty"`
}

// CaptureLevel 根据 Adapter 实际可用能力计算 Level1-Level4，不使用理论目标等级。
func CaptureLevel(capabilities CaptureCapabilities) int {
	if capabilities.ConversationCapture &&
		capabilities.ToolCallCapture &&
		capabilities.ToolOutputCapture &&
		capabilities.FileEditCapture &&
		capabilities.SessionLifecycle &&
		capabilities.MCPObserve {
		return 4
	}
	if capabilities.ToolCallCapture && capabilities.ToolOutputCapture {
		return 3
	}
	if capabilities.SessionLifecycle && capabilities.MCPObserve {
		return 2
	}
	return 1
}

// MissingCapabilities 返回距离 Level4 目标仍缺失的能力，用于 capture quality 诊断。
func MissingCapabilities(capabilities CaptureCapabilities) []string {
	missing := make([]string, 0, 6)
	if !capabilities.ConversationCapture {
		missing = append(missing, "conversation_capture")
	}
	if !capabilities.ToolCallCapture {
		missing = append(missing, "tool_call_capture")
	}
	if !capabilities.ToolOutputCapture {
		missing = append(missing, "tool_output_capture")
	}
	if !capabilities.FileEditCapture {
		missing = append(missing, "file_edit_capture")
	}
	if !capabilities.SessionLifecycle {
		missing = append(missing, "session_lifecycle")
	}
	if !capabilities.MCPObserve {
		missing = append(missing, "mcp_observe")
	}
	return missing
}

// ApplyAcceptedEvent 将一次已接受 observe 写入反映到 capture quality。
func ApplyAcceptedEvent(quality CaptureQuality, req ObserveRequest, deduped bool) CaptureQuality {
	if deduped {
		quality.DedupedEventCount++
		return quality
	}
	quality.CapturedEventCount++
	quality.LastEventAt = req.OccurredAt
	switch req.EventType {
	case EventSessionStart:
		quality.HasSessionStart = true
	case EventSessionEnd:
		quality.HasSessionEnd = true
	case EventTaskResult:
		quality.HasTaskResult = true
	case EventToolCall:
		quality.ToolCallCount++
	case EventToolResultSummary:
		quality.ToolResultCount++
	case EventFileEditSummary:
		quality.FileEditCount++
	case EventConversationMessage:
		quality.ConversationMessageCount++
	}
	quality.MissingCapabilities = MissingCapabilities(req.CaptureCapabilities)
	return quality
}

// ApplyContentBoundaryRejection 记录内容边界拒绝次数；P2 拒绝事件不写 raw_event。
func ApplyContentBoundaryRejection(quality CaptureQuality) CaptureQuality {
	quality.ContentBoundaryRejections++
	return quality
}
