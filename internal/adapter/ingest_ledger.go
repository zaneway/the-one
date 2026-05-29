package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// IngestLedger 记录已成功处理的 (ingest_id, event_index)。
type IngestLedger struct {
	filePath string
	mu       sync.Mutex
}

type ledgerFile struct {
	Entries map[string]bool `json:"entries"`
}

func NewIngestLedger(dirPath string) *IngestLedger {
	return &IngestLedger{filePath: filepath.Join(dirPath, "ingest-ledger.json")}
}

func ledgerKey(ingestID string, eventIndex int) string {
	return fmt.Sprintf("%s:%d", ingestID, eventIndex)
}

func (l *IngestLedger) Contains(ingestID string, eventIndex int) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := l.loadLocked()
	if err != nil {
		return false, err
	}
	return data.Entries[ledgerKey(ingestID, eventIndex)], nil
}

func (l *IngestLedger) Mark(ingestID string, eventIndex int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := l.loadLocked()
	if err != nil {
		return err
	}
	if data.Entries == nil {
		data.Entries = make(map[string]bool)
	}
	data.Entries[ledgerKey(ingestID, eventIndex)] = true
	return l.saveLocked(data)
}

func (l *IngestLedger) loadLocked() (ledgerFile, error) {
	var data ledgerFile
	raw, err := os.ReadFile(l.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ledgerFile{Entries: map[string]bool{}}, nil
		}
		return ledgerFile{}, err
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return ledgerFile{}, fmt.Errorf("decode ingest ledger: %w", err)
	}
	if data.Entries == nil {
		data.Entries = map[string]bool{}
	}
	return data, nil
}

func (l *IngestLedger) saveLocked(data ledgerFile) error {
	if err := os.MkdirAll(filepath.Dir(l.filePath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(l.filePath, raw, 0o644)
}
