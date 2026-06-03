package capture

import (
	"fmt"
	"strings"
)

var structuredContentSummaryTags = []string{"【事件】", "【事实】", "【结论"}
var highValueContentSummaryTags = []string{"【结论", "【约束】"}

// HasStructuredContentSummaryTag 判断 content_summary 是否已经使用结构化索引卡标签。
func HasStructuredContentSummaryTag(value string) bool {
	return containsAny(strings.TrimSpace(value), structuredContentSummaryTags)
}

// EnsureStructuredContentSummary 对旧版自由文本做最小结构化包装，不做 LLM 摘要。
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
	if len([]rune(text)) > 400 && tag != "【结论/决策】" && tag != "【约束】" {
		tag = "【结论/决策】"
	}
	if tag == "【约束】" {
		return tag + text + "\n【事实】见上方约束摘要"
	}
	return tag + text
}

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

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func isLongSummaryWithoutSpansExempt(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case EventToolResultSummary, EventFileEditSummary, EventSessionStart:
		return true
	default:
		return false
	}
}

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
