package adapter

import (
	"strings"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
)

// DefaultSuppressRawEventTypes 返回 ingest 默认不写 raw_event 的事件类型。
func DefaultSuppressRawEventTypes() []string {
	return []string{
		capture.EventSessionStart,
		capture.EventToolResultSummary,
		capture.EventFileEditSummary,
	}
}

// ResolveSuppressRawEventTypes 解析配置；未显式配置时使用默认列表。
// 显式配置为空数组 [] 时表示不抑制任何事件类型。
func ResolveSuppressRawEventTypes(cfg config.Config) []string {
	if cfg.Adapter.SuppressRawEventTypes != nil {
		return normalizeSuppressRawEventTypes(cfg.Adapter.SuppressRawEventTypes)
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

func (p *IngestProcessor) shouldSuppressRawEvent(eventType string) bool {
	eventType = strings.TrimSpace(eventType)
	for _, t := range p.SuppressRawEventTypes {
		if strings.TrimSpace(t) == eventType {
			return true
		}
	}
	return false
}
