package adapter

import "strings"

const (
	KindSessionLifecycle = "session.lifecycle"
	KindTurnCompleted    = "turn.completed"
	KindCaptureAtomic    = "capture.atomic"
)

// ExpandModeLegacy 与 v1 TurnRuntime 行为一致。
const ExpandModeLegacy = "legacy"

// ExpandModeV2 启用 atomic 分流（P2）；P0 仅解析配置，路由仍按 kind。
const ExpandModeV2 = "v2"

var atomicEventTypes = map[string]struct{}{
	"tool.result.summary": {},
	"file.edit.summary":   {},
}

// InferKind 按 §4.4 对单条入站事件推断 kind（legacy 语义）。
func InferKind(eventType string, payload map[string]any) (string, error) {
	return InferKindWithExpandMode(eventType, payload, ExpandModeLegacy)
}

// InferKindWithExpandMode 按展开模式推断 kind；v2 下原子事件不得因 Turn 字段误判。
func InferKindWithExpandMode(eventType string, payload map[string]any, expandMode string) (string, error) {
	if k := strings.TrimSpace(stringFromPayload(payload, "kind")); k != "" {
		return k, nil
	}
	switch strings.TrimSpace(eventType) {
	case "session.start", "session.end":
		return KindSessionLifecycle, nil
	}
	if _, ok := atomicEventTypes[strings.TrimSpace(eventType)]; ok {
		if hasInvalidAtomicShape(payload) {
			return "", errInvalidAtomicShape
		}
		return KindCaptureAtomic, nil
	}
	if nonEmpty(payload, "user_summary") || nonEmpty(payload, "agent_summary") {
		return KindTurnCompleted, nil
	}
	if strings.TrimSpace(eventType) != "" {
		return KindCaptureAtomic, nil
	}
	return "", errMissingEventType
}

func hasInvalidAtomicShape(payload map[string]any) bool {
	if nonEmpty(payload, "user_summary") || nonEmpty(payload, "agent_summary") {
		return true
	}
	if sliceNonEmpty(payload, "file_edits") || sliceNonEmpty(payload, "tool_results") {
		return true
	}
	return false
}

func nonEmpty(payload map[string]any, key string) bool {
	v, ok := payload[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) != ""
	default:
		return true
	}
}

func sliceNonEmpty(payload map[string]any, key string) bool {
	v, ok := payload[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case []any:
		return len(t) > 0
	case []map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

func stringFromPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
