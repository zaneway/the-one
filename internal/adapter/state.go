package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RuntimeState 兼容旧版混写结构；P1 迁移后仅用于读取 legacy session.json。
type RuntimeState struct {
	SessionID       string `json:"session_id"`
	TaskID          string `json:"task_id"`
	LastTaskSummary string `json:"last_task_summary"`
	LastTurnID      string `json:"last_turn_id"`
	LastTurnSig     string `json:"last_turn_sig"`
}

// StateStore 回合去重状态持久化（P1：仅 turn-dedup 字段）。
type StateStore interface {
	Load() (TurnDedupState, error)
	Save(state TurnDedupState) error
	Clear() error
}

// FileStateStore 基于 turn-dedup.json 的去重存储。
type FileStateStore struct {
	dirPath  string
	filePath string
}

// NewFileStateStore 创建去重状态存储并执行 P1 迁移。
func NewFileStateStore(dirPath string) *FileStateStore {
	_ = EnsureP1Migration(dirPath)
	return &FileStateStore{
		dirPath:  dirPath,
		filePath: filepath.Join(dirPath, "turn-dedup.json"),
	}
}

// Load 读取 turn-dedup.json；缺失时返回空状态。
func (s *FileStateStore) Load() (TurnDedupState, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return TurnDedupState{}, nil
		}
		return TurnDedupState{}, fmt.Errorf("load turn dedup: %w", err)
	}
	var state TurnDedupState
	if err := json.Unmarshal(data, &state); err != nil {
		return TurnDedupState{}, fmt.Errorf("decode turn dedup: %w", err)
	}
	return state, nil
}

// Save 仅写入去重字段。
func (s *FileStateStore) Save(state TurnDedupState) error {
	return writeTurnDedup(s.dirPath, state)
}

// Clear 清空回合去重状态（新 Composer 会话时调用）。
func (s *FileStateStore) Clear() error {
	if err := os.MkdirAll(s.dirPath, 0o755); err != nil {
		return fmt.Errorf("create runtime state dir: %w", err)
	}
	return writeTurnDedup(s.dirPath, TurnDedupState{})
}
