package sqlite

import (
	"context"
	"strings"

	"github.com/zaneway/theone/internal/retention"
)

// ListAccessEvents 批量读取 memory_access_log 明细，供 retention 计算完整访问信号。
func (s *Store) ListAccessEvents(ctx context.Context, memoryIDs []string) (map[string][]retention.AccessFeedbackEvent, error) {
	result := make(map[string][]retention.AccessFeedbackEvent, len(memoryIDs))
	if len(memoryIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(memoryIDs))
	args := make([]any, len(memoryIDs))
	for i, memoryID := range memoryIDs {
		placeholders[i] = "?"
		args[i] = memoryID
	}
	query := `select memory_id, event_type, event_weight, source_quality, created_at
		from memory_access_log
		where memory_id in (` + strings.Join(placeholders, ",") + `)
		order by memory_id, created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()

	for rows.Next() {
		var memoryID, eventType, createdAt string
		var eventWeight, sourceQuality float64
		if err := rows.Scan(&memoryID, &eventType, &eventWeight, &sourceQuality, &createdAt); err != nil {
			return nil, storageErr(err)
		}
		result[memoryID] = append(result[memoryID], retention.AccessFeedbackEvent{
			EventType:     eventType,
			EventWeight:   eventWeight,
			SourceQuality: sourceQuality,
			CreatedAt:     parseTime(createdAt),
		})
	}
	return result, storageErr(rows.Err())
}

// AggregateAccessFeedback 兼容旧接口：仅返回强化摘要（不含 decay 修正的 base_activation）。
func (s *Store) AggregateAccessFeedback(ctx context.Context, memoryIDs []string) (map[string]retention.AccessFeedbackSummary, error) {
	eventsByMemory, err := s.ListAccessEvents(ctx, memoryIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]retention.AccessFeedbackSummary, len(eventsByMemory))
	for memoryID, events := range eventsByMemory {
		result[memoryID] = retention.ComputeAccessFeedback(events)
	}
	return result, nil
}
