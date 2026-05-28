package sqlite

import (
	"context"
	"time"

	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/retention"
	"github.com/zaneway/theone/internal/retrieval"
)

// RecordMemoryAccess 实现 memory.AccessFeedbackWriter，将 review 等反馈写入 access log。
func (s *Store) RecordMemoryAccess(ctx context.Context, memoryID, eventType string, sourceQuality float64) error {
	id, err := idgen.New("mal")
	if err != nil {
		return err
	}
	if sourceQuality <= 0 {
		sourceQuality = 1.0
	}
	_, err = s.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:            id,
		MemoryID:      memoryID,
		EventType:     eventType,
		EventWeight:   retention.AccessLogEventWeight(eventType),
		SourceQuality: sourceQuality,
		CreatedAt:     time.Now(),
	})
	return err
}
