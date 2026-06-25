package adapter

import (
	"strings"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
)

// DefaultSuppressRawEventTypes 返回 ingest 默认不写 raw_event 的事件类型。
func DefaultSuppressRawEventTypes() []string {
	return capture.DefaultSuppressRawEventTypes()
}

// ResolveSuppressRawEventTypes 解析配置；与 capture 共用同一优先级规则。
func ResolveSuppressRawEventTypes(cfg config.Config) []string {
	return capture.ResolveSuppressRawEventTypesFromConfig(cfg)
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
