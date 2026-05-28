package automation

import (
	"context"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/retention"
	"github.com/zaneway/theone/internal/retrieval"
)

// RecordTaskSuccessFeedback 在任务成功结束时，为同 task 已 injected 的记忆写入 task_success 访问日志。
func (s *Service) RecordTaskSuccessFeedback(ctx context.Context, taskID, sessionID string) error {
	if taskID == "" {
		return nil
	}
	logs, err := s.repo.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{TaskID: taskID, Limit: 200})
	if err != nil {
		return err
	}
	now := time.Now()
	seen := make(map[string]struct{})
	for _, log := range logs {
		if log.EventType != "injected" {
			continue
		}
		if log.MemoryID == "" {
			continue
		}
		if _, ok := seen[log.MemoryID]; ok {
			continue
		}
		seen[log.MemoryID] = struct{}{}
		id, err := idgen.New("mal")
		if err != nil {
			return err
		}
		sourceQuality := log.SourceQuality
		if sourceQuality <= 0 {
			sourceQuality = 1.0
		}
		if _, err := s.repo.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
			ID:            id,
			MemoryID:      log.MemoryID,
			SessionID:     firstNonEmpty(sessionID, log.SessionID),
			TaskID:        taskID,
			EventType:     "task_success",
			EventWeight:   retention.AccessLogEventWeight("task_success"),
			SourceQuality: sourceQuality,
			CreatedAt:     now,
		}); err != nil {
			return err
		}
	}
	return nil
}

var _ capture.TaskResultRecorder = (*Service)(nil)
