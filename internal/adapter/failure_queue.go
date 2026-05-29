package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FailureRecord 记录 observe 批量写入失败的事件。
// 失败记录写入 dead_letter.jsonl 文件，用于后续诊断和人工重试。
type FailureRecord struct {
	IngestID     string `json:"ingest_id"`               // 批级 ingest_id
	EventIndex   int    `json:"event_index,omitempty"`   // 批内下标，单条为 0
	ErrorCode    string `json:"error_code"`              // 错误码，如 VALIDATION_FAILED、STORAGE_BUSY
	ErrorSummary string `json:"error_summary"`           // 错误摘要，不包含完整堆栈
	RetryCount   int    `json:"retry_count"`             // 已重试次数
	NextRetryAt  string `json:"next_retry_at,omitempty"` // 下次重试时间（RFC3339），当前未使用
	LastErrorAt  string `json:"last_error_at"`           // 最后一次失败时间（RFC3339）
	SessionID    string `json:"session_id"`              // 关联的 session ID
	TaskID       string `json:"task_id,omitempty"`       // 关联的 task ID
	ContentHash  string `json:"content_hash,omitempty"`  // 事件内容 hash，用于去重
}

// FailureQueue 基于 JSONL 文件的失败记录队列。
// 设计约束：append-only 写入，不提供重试调度能力；重试由外部 automation worker 负责。
type FailureQueue struct {
	filePath string // dead_letter.jsonl 文件路径
}

// NewFailureQueue 创建失败队列。
// dirPath 为队列文件目录，通常为 <data-dir>/runtime-state/。
func NewFailureQueue(dirPath string) *FailureQueue {
	return &FailureQueue{filePath: filepath.Join(dirPath, "dead_letter.jsonl")}
}

// Append 将失败记录追加到 dead_letter.jsonl 文件。
// 文件不存在时自动创建；LastErrorAt 为空时使用当前时间。
func (q *FailureQueue) Append(record FailureRecord) error {
	if err := os.MkdirAll(filepath.Dir(q.filePath), 0o755); err != nil {
		return fmt.Errorf("create dead letter dir: %w", err)
	}
	if record.LastErrorAt == "" {
		record.LastErrorAt = time.Now().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal failure record: %w", err)
	}
	file, err := os.OpenFile(q.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open dead letter file: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append dead letter record: %w", err)
	}
	return nil
}
