package capture

import (
	"strings"

	"github.com/zaneway/theone/internal/config"
)

// DefaultSuppressRawEventTypes 返回默认不写 raw_event 的事件类型。
// ingest 与 MCP observe 共用同一列表，避免多入口数据面分裂。
func DefaultSuppressRawEventTypes() []string {
	return []string{
		EventSessionStart,
		EventToolResultSummary,
		EventFileEditSummary,
	}
}

// ResolveSuppressRawEventTypesFromConfig 从完整配置解析抑制列表。
// 优先级：capture.suppress_raw_event_types -> adapter.suppress_raw_event_types -> 内置默认。
// 显式配置 [] 表示不抑制任何事件类型。
func ResolveSuppressRawEventTypesFromConfig(cfg config.Config) []string {
	if cfg.Capture.SuppressRawEventTypes != nil {
		return normalizeSuppressRawEventTypes(cfg.Capture.SuppressRawEventTypes)
	}
	if cfg.Adapter.SuppressRawEventTypes != nil {
		return normalizeSuppressRawEventTypes(cfg.Adapter.SuppressRawEventTypes)
	}
	return DefaultSuppressRawEventTypes()
}

// ResolveSuppressRawEventTypes 解析 capture 配置；未显式配置时使用默认列表。
// 显式配置为空数组 [] 时表示不抑制任何事件类型。
func ResolveSuppressRawEventTypes(cfg config.CaptureConfig) []string {
	if cfg.SuppressRawEventTypes != nil {
		return normalizeSuppressRawEventTypes(cfg.SuppressRawEventTypes)
	}
	return DefaultSuppressRawEventTypes()
}

func normalizeSuppressRawEventTypes(types []string) []string {
	out := make([]string, 0, len(types))
	seen := make(map[string]struct{}, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func shouldSuppressRawEvent(cfg config.Config, eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	for _, t := range ResolveSuppressRawEventTypesFromConfig(cfg) {
		if strings.TrimSpace(t) == eventType {
			return true
		}
	}
	return false
}
