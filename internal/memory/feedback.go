package memory

import "context"

// AccessFeedbackWriter 写入 memory_access_log，供 review 与显式记忆反馈闭环使用。
type AccessFeedbackWriter interface {
	RecordMemoryAccess(ctx context.Context, memoryID, eventType string, sourceQuality float64) error
}

func (s *Service) recordAccessFeedback(ctx context.Context, memoryID, eventType string, sourceQuality float64) {
	if s.accessFeedback == nil || memoryID == "" || eventType == "" {
		return
	}
	_ = s.accessFeedback.RecordMemoryAccess(ctx, memoryID, eventType, sourceQuality)
}
