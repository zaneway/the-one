package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AtomicDedupStore 持久化 atomic_fingerprint 去重（§5.2）。
type AtomicDedupStore struct {
	dirPath  string
	filePath string
	mu       sync.Mutex
}

type atomicDedupFile struct {
	Entries map[string]struct{} `json:"entries"`
}

func NewAtomicDedupStore(dirPath string) *AtomicDedupStore {
	return &AtomicDedupStore{
		dirPath:  dirPath,
		filePath: filepath.Join(dirPath, "atomic-dedup.json"),
	}
}

func (s *AtomicDedupStore) key(sessionID, eventType, fingerprint string) string {
	return sessionID + "|" + eventType + "|" + fingerprint
}

// Contains 是否已处理过该 atomic 指纹。
func (s *AtomicDedupStore) Contains(sessionID, eventType, fingerprint string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	_, ok := data.Entries[s.key(sessionID, eventType, fingerprint)]
	return ok, nil
}

// Mark 记录已处理的 atomic 指纹。
func (s *AtomicDedupStore) Mark(sessionID, eventType, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return err
	}
	if data.Entries == nil {
		data.Entries = make(map[string]struct{})
	}
	data.Entries[s.key(sessionID, eventType, fingerprint)] = struct{}{}
	return s.saveLocked(data)
}

func (s *AtomicDedupStore) loadLocked() (atomicDedupFile, error) {
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return atomicDedupFile{Entries: make(map[string]struct{})}, nil
		}
		return atomicDedupFile{}, fmt.Errorf("load atomic dedup: %w", err)
	}
	var data atomicDedupFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return atomicDedupFile{}, fmt.Errorf("decode atomic dedup: %w", err)
	}
	if data.Entries == nil {
		data.Entries = make(map[string]struct{})
	}
	return data, nil
}

func (s *AtomicDedupStore) saveLocked(data atomicDedupFile) error {
	if err := os.MkdirAll(s.dirPath, 0o755); err != nil {
		return fmt.Errorf("create atomic dedup dir: %w", err)
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode atomic dedup: %w", err)
	}
	if err := os.WriteFile(s.filePath, out, 0o644); err != nil {
		return fmt.Errorf("write atomic dedup: %w", err)
	}
	return nil
}
