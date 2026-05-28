package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type FailureRecord struct {
	IngestID     string `json:"ingest_id"`
	ErrorCode    string `json:"error_code"`
	ErrorSummary string `json:"error_summary"`
	RetryCount   int    `json:"retry_count"`
	NextRetryAt  string `json:"next_retry_at,omitempty"`
	LastErrorAt  string `json:"last_error_at"`
	SessionID    string `json:"session_id"`
	TaskID       string `json:"task_id,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
}

type FailureQueue struct {
	filePath string
}

func NewFailureQueue(dirPath string) *FailureQueue {
	return &FailureQueue{filePath: filepath.Join(dirPath, "dead_letter.jsonl")}
}

func (q *FailureQueue) Append(record FailureRecord) error {
	if err := os.MkdirAll(filepath.Dir(q.filePath), 0o755); err != nil {
		return fmt.Errorf("create dead letter dir: %w", err)
	}
	if record.LastErrorAt == "" {
		record.LastErrorAt = time.Now().UTC().Format(time.RFC3339Nano)
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
