package capture

import (
	"fmt"
	"strings"
)

// structuredContentSummaryTags 是已认可的结构化 content_summary 标签前缀集合。
// 任何 capture 写入路径都应在 content_summary 中至少带一个这样的标签，
// 让后续 recall/UI 能按"事件/事实/结论"分流渲染。
var structuredContentSummaryTags = []string{"【事件】", "【事实】", "【结论"}

// highValueContentSummaryTags 是高价值标签前缀，长 content_summary 的"前 400 字符"必须出现其一。
// 长文里只有结论/约束才值钱，让模型/agent 必须先把核心结论写前面。
var highValueContentSummaryTags = []string{"【结论", "【约束】"}

// HasStructuredContentSummaryTag 判断 content_summary 是否已经使用结构化索引卡标签。
// 入参：value（待检测文本）。
// 返回：是否包含 structuredContentSummaryTags 中任一标签。
// 设计约束：先 trim 再判断，避免前后空白干扰。
func HasStructuredContentSummaryTag(value string) bool {
	return containsAny(strings.TrimSpace(value), structuredContentSummaryTags)
}

// EnsureStructuredContentSummary 对旧版自由文本做最小结构化包装，不做 LLM 摘要。
// 入参：eventType（用于决定默认 tag）、value（原始 summary）。
// 返回：被补齐标签后的 summary。
// 处理流程：
//  1. 空串用 fallbackContentSummaryText 生成兜底；
//  2. 已带标签直接返回（不重写）；
//  3. 按 eventType 选择默认标签；长文强制升级为【结论/决策】；
//  4. 约束类额外补一行"【事实】见上方约束摘要"，让 recall 能解释约束来源。
//
// 设计约束：完全本地化，无外部依赖；不会调用模型做摘要。
func EnsureStructuredContentSummary(eventType, value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		text = fallbackContentSummaryText(eventType)
	}
	if HasStructuredContentSummaryTag(text) {
		return text
	}
	tag := "【事件】"
	switch strings.TrimSpace(eventType) {
	case EventAgentResponseSummary, EventAgentDecision, EventTaskResult, EventSessionEnd:
		tag = "【结论/决策】"
	case EventUserCorrection:
		tag = "【约束】"
	case EventFileEditSummary, EventUserDeclaration:
		tag = "【事实】"
	}
	// 长文优先升级为结论/决策类标签（约束类不需要，因为约束天然短）
	if len([]rune(text)) > 400 && tag != "【结论/决策】" && tag != "【约束】" {
		tag = "【结论/决策】"
	}
	if tag == "【约束】" {
		return tag + text + "\n【事实】见上方约束摘要"
	}
	return tag + text
}

// checkStructuredContentSummaryQuality 校验 content_summary 是否满足结构化质量门槛。
// 入参：req（observe 请求，含 ContentSummary/SalientSpans/EventType）。
// 返回：违规时的具体错误；合规时返回 nil。
// 关键规则：
//  1. 必须带结构化标签（HasStructuredContentSummaryTag）；
//  2. 超过 400 字符时，前 400 字符必须包含【结论/【约束；
//  3. 超过 1200 字符且无 salient_spans 时报错（部分事件类型豁免）。
//
// 设计约束：仅做格式校验，不修改原文；违规由 caller 决定丢弃或打回。
func checkStructuredContentSummaryQuality(req ObserveRequest) error {
	summary := strings.TrimSpace(req.ContentSummary)
	if !HasStructuredContentSummaryTag(summary) {
		return fmt.Errorf("CONTENT_QUALITY: content_summary must include structured label")
	}
	runes := []rune(summary)
	if len(runes) > 400 && !containsAny(string(runes[:400]), highValueContentSummaryTags) {
		return fmt.Errorf("CONTENT_QUALITY: content_summary over 400 chars must put conclusion or constraints in first 400 chars")
	}
	if len(runes) > 1200 && len(req.SalientSpans) == 0 && !isLongSummaryWithoutSpansExempt(req.EventType) {
		return fmt.Errorf("CONTENT_QUALITY: content_summary too long without salient_spans")
	}
	return nil
}

// containsAny 判断 value 是否包含 needles 中任一子串。
// 用于结构化标签与豁免事件类型的快速匹配。
func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// isLongSummaryWithoutSpansExempt 判断事件类型是否豁免"长 summary + 无 salient_spans"规则。
// 当前豁免：tool_result_summary、file_edit_summary、session_start。
// 这三类事件本身没有自然"显著片段"，由结构化标签已足够。
func isLongSummaryWithoutSpansExempt(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case EventToolResultSummary, EventFileEditSummary, EventSessionStart:
		return true
	default:
		return false
	}
}

// fallbackContentSummaryText 在 content_summary 为空时给出本地化兜底文本。
// 返回的字符串不含结构化标签，调用方应再走 EnsureStructuredContentSummary 补齐标签。
func fallbackContentSummaryText(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case EventToolResultSummary:
		return "工具执行结果"
	case EventFileEditSummary:
		return "文件修改"
	case EventSessionStart, EventSessionEnd:
		return "会话生命周期：" + strings.TrimSpace(eventType)
	case EventTaskStart:
		return "任务开始"
	case EventTaskResult:
		return "任务结果"
	default:
		return strings.TrimSpace(eventType)
	}
}
