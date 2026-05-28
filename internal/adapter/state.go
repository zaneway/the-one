package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type RuntimeState struct {
	SessionID       string `json:"session_id"`
	TaskID          string `json:"task_id"`
	LastTaskSummary string `json:"last_task_summary"`
	LastTurnID      string `json:"last_turn_id"`
	LastTurnSig     string `json:"last_turn_sig"`
}

type StateStore interface {
	Load() (RuntimeState, error)
	Save(state RuntimeState) error
}

type FileStateStore struct {
	dirPath  string
	filePath string
}

func NewFileStateStore(dirPath string) *FileStateStore {
	return &FileStateStore{
		dirPath:  dirPath,
		filePath: filepath.Join(dirPath, "session.json"),
	}
}

func (s *FileStateStore) Load() (RuntimeState, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return RuntimeState{}, nil
		}
		return RuntimeState{}, fmt.Errorf("load runtime state: %w", err)
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return RuntimeState{}, fmt.Errorf("decode runtime state: %w", err)
	}
	return state, nil
}

func (s *FileStateStore) Save(state RuntimeState) error {
	if err := os.MkdirAll(s.dirPath, 0o755); err != nil {
		return fmt.Errorf("create runtime state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime state: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0o644); err != nil {
		return fmt.Errorf("write runtime state: %w", err)
	}
	return nil
}
