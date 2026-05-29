package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const p1MigratedMarker = ".p1-migrated"

// EnsureP1Migration 一次性将 legacy session.json 拆为 binding + turn-dedup。
func EnsureP1Migration(dirPath string) error {
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return fmt.Errorf("create runtime state dir: %w", err)
	}
	marker := filepath.Join(dirPath, p1MigratedMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}

	legacyPath := filepath.Join(dirPath, "session.json")
	legacyData, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return writeP1Marker(marker)
		}
		return fmt.Errorf("read legacy session.json: %w", err)
	}

	var legacy RuntimeState
	if err := json.Unmarshal(legacyData, &legacy); err != nil {
		return fmt.Errorf("decode legacy session.json: %w", err)
	}

	agentType := "cursor"
	bindingPath := filepath.Join(dirPath, "binding."+agentType+".json")
	if _, err := os.Stat(bindingPath); os.IsNotExist(err) {
		sessionID := strings.TrimSpace(legacy.SessionID)
		taskID := strings.TrimSpace(legacy.TaskID)
		if sessionID != "" {
			state := BindingState{
				AgentType:             agentType,
				SessionID:             sessionID,
				TaskID:                defaultTaskID(agentType, taskID),
				ExternalSessionKey:    sessionID,
				TaskFromPromptPending: taskID == "" || taskID == defaultTaskID(agentType, ""),
				BoundAt:               time.Now().Format(time.RFC3339Nano),
			}
			if isSyntheticSessionID(agentType, sessionID) {
				state.TaskFromPromptPending = true
			}
			data, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				return fmt.Errorf("encode migrated binding: %w", err)
			}
			if err := os.WriteFile(bindingPath, data, 0o644); err != nil {
				return fmt.Errorf("write migrated binding: %w", err)
			}
		}
	}

	dedupPath := filepath.Join(dirPath, "turn-dedup.json")
	if _, err := os.Stat(dedupPath); os.IsNotExist(err) {
		dedup := TurnDedupState{
			LastTaskSummary: legacy.LastTaskSummary,
			LastTurnID:      legacy.LastTurnID,
			LastTurnSig:     legacy.LastTurnSig,
		}
		if err := writeTurnDedup(dirPath, dedup); err != nil {
			return err
		}
	}

	backup := legacyPath + ".bak"
	if err := os.Rename(legacyPath, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("archive session.json: %w", err)
	}
	return writeP1Marker(marker)
}

func writeP1Marker(path string) error {
	return os.WriteFile(path, []byte(time.Now().Format(time.RFC3339Nano)+"\n"), 0o644)
}

func writeTurnDedup(dirPath string, dedup TurnDedupState) error {
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(dedup, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dirPath, "turn-dedup.json"), data, 0o644)
}
