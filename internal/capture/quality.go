package capture

// CaptureQuality 会话捕获质量结构体
// 存储在 agent_session.capture_quality_json 字段中，用于评估会话的捕获完整性
// 设计目的：
// - 追踪会话生命周期事件（session.start/end、task.result）的捕获状态
// - 统计各类事件的捕获数量，评估捕获覆盖率
// - 记录缺失的捕获能力，用于诊断和优化
// - 记录内容边界拒绝次数，监控内容最小化策略的执行情况
type CaptureQuality struct {
	HasSessionStart           bool     `json:"has_session_start"`           // 是否收到 session.start 事件
	HasSessionEnd             bool     `json:"has_session_end"`             // 是否收到 session.end 事件
	HasTaskResult             bool     `json:"has_task_result"`             // 是否收到 task.result 事件
	CapturedEventCount        int      `json:"captured_event_count"`        // 成功捕获的事件总数
	DedupedEventCount         int      `json:"deduped_event_count"`         // 去重的事件数量
	ToolCallCount             int      `json:"tool_call_count"`             // tool.call 事件数量
	ToolResultCount           int      `json:"tool_result_count"`           // tool.result.summary 事件数量
	FileEditCount             int      `json:"file_edit_count"`             // file.edit.summary 事件数量
	ConversationMessageCount  int      `json:"conversation_message_count"`  // conversation.message 事件数量
	MissingCapabilities       []string `json:"missing_capabilities"`        // 缺失的捕获能力列表
	ContentBoundaryRejections int      `json:"content_boundary_rejections"` // 内容边界拒绝次数
	LastEventAt               string   `json:"last_event_at,omitempty"`     // 最后一次事件的时间戳
}

// CaptureLevel 根据 Adapter 实际可用能力计算捕获等级（Level1-Level4）
// 等级定义：
// - Level1（基础）：只有基本 MCP observe 能力，无法捕获工具调用和会话生命周期
// - Level2（会话级）：能捕获会话生命周期（session.start/end）和 MCP observe
// - Level3（工具级）：能捕获工具调用（tool_call）和工具输出（tool_output）
// - Level4（完整）：能捕获所有类型事件，包括对话、工具调用、文件编辑和会话生命周期
// 设计说明：
// - 使用实际可用能力而非理论目标等级，确保等级反映真实捕获能力
// - 等级计算是累积的，高等级包含低等级的所有能力
// - 用于评估 Adapter 的捕获能力，指导客户端优化捕获策略
func CaptureLevel(capabilities CaptureCapabilities) int {
	// Level4：完整捕获能力，包含所有6种能力
	if capabilities.ConversationCapture &&
		capabilities.ToolCallCapture &&
		capabilities.ToolOutputCapture &&
		capabilities.FileEditCapture &&
		capabilities.SessionLifecycle &&
		capabilities.MCPObserve {
		return 4
	}
	// Level3：工具级捕获能力，包含工具调用和工具输出
	if capabilities.ToolCallCapture && capabilities.ToolOutputCapture {
		return 3
	}
	// Level2：会话级捕获能力，包含会话生命周期和 MCP observe
	if capabilities.SessionLifecycle && capabilities.MCPObserve {
		return 2
	}
	// Level1：基础捕获能力，只有 MCP observe
	return 1
}

// MissingCapabilities 返回距离 Level4 目标仍缺失的捕获能力列表
// 返回值：包含缺失能力名称的字符串切片，为空表示已达到 Level4
// 设计说明：
// - 用于 capture quality 诊断，帮助识别 Adapter 的能力短板
// - 返回的能力名称与 CaptureCapabilities 结构体字段对应
// - 可用于生成诊断报告，指导客户端优化捕获能力
// - 预分配容量为6，因为 Level4 需要6种能力
func MissingCapabilities(capabilities CaptureCapabilities) []string {
	missing := make([]string, 0, 6)
	// 检查对话捕获能力
	if !capabilities.ConversationCapture {
		missing = append(missing, "conversation_capture")
	}
	// 检查工具调用捕获能力
	if !capabilities.ToolCallCapture {
		missing = append(missing, "tool_call_capture")
	}
	// 检查工具输出捕获能力
	if !capabilities.ToolOutputCapture {
		missing = append(missing, "tool_output_capture")
	}
	// 检查文件编辑捕获能力
	if !capabilities.FileEditCapture {
		missing = append(missing, "file_edit_capture")
	}
	// 检查会话生命周期捕获能力
	if !capabilities.SessionLifecycle {
		missing = append(missing, "session_lifecycle")
	}
	// 检查 MCP observe 能力
	if !capabilities.MCPObserve {
		missing = append(missing, "mcp_observe")
	}
	return missing
}

// ApplyAcceptedEvent 将一次已接受的 observe 事件反映到捕获质量统计
// 处理逻辑：
// 1. 如果是去重事件，只增加去重计数器
// 2. 如果是新事件，增加捕获计数器并更新最后事件时间
// 3. 根据事件类型更新对应的统计计数器
// 4. 更新缺失能力列表
// 设计说明：
// - 每次事件处理后调用，保持质量统计的实时性
// - 去重事件不计入捕获总数，但计入去重总数
// - 最后事件时间用于评估捕获的连续性
// - 缺失能力列表用于诊断和优化
func ApplyAcceptedEvent(quality CaptureQuality, req ObserveRequest, deduped bool) CaptureQuality {
	// 去重事件只增加去重计数器，不更新其他统计
	if deduped {
		quality.DedupedEventCount++
		return quality
	}
	// 新事件增加捕获计数器并更新最后事件时间
	quality.CapturedEventCount++
	quality.LastEventAt = req.OccurredAt
	// 根据事件类型更新对应的统计计数器
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
	// 更新缺失能力列表
	quality.MissingCapabilities = MissingCapabilities(req.CaptureCapabilities)
	return quality
}

// ApplyContentBoundaryRejection 记录内容边界拒绝次数
// 设计说明：
// - 拒绝事件不写 raw_event，但需要记录拒绝次数用于质量监控
// - 拒绝原因通常是内容超过长度限制或包含禁止的原始内容字段
// - 拒绝次数可用于评估 Adapter 的内容最小化策略是否有效
// - 高拒绝次数可能表明 Adapter 需要优化内容提取逻辑
func ApplyContentBoundaryRejection(quality CaptureQuality) CaptureQuality {
	quality.ContentBoundaryRejections++
	return quality
}
