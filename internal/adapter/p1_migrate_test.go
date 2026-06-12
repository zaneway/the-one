package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEnsureP1MigrationFromSessionJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime-state")
	legacy := RuntimeState{
		SessionID:       "conv-legacy-001",
		TaskID:          "task_cursor_auto",
		LastTurnID:      "turn_a",
		LastTurnSig:     "sig_a",
		LastTaskSummary: "summary",
	}
	data, _ := json.Marshal(legacy)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureP1Migration(dir); err != nil {
		t.Fatal(err)
	}
	binder := NewSessionBinder(dir)
	state, ok, err := binder.Load("cursor")
	if err != nil || !ok {
		t.Fatalf("binding load ok=%v err=%v", ok, err)
	}
	if state.SessionID != "conv-legacy-001" {
		t.Fatalf("session_id = %q", state.SessionID)
	}
	store := NewFileStateStore(dir)
	dedup, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if dedup.LastTurnID != "turn_a" || dedup.LastTurnSig != "sig_a" {
		t.Fatalf("dedup = %+v", dedup)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Fatal("expected session.json archived")
	}
}

func TestSessionBinderResetOnNewSessionStart(t *testing.T) {
	dir := t.TempDir()
	b := NewSessionBinder(dir)
	first, err := b.Resolve(ResolveInput{
		AgentType: "cursor",
		EventType: "session.start",
		Envelope: IngestEnvelope{
			SessionID: "chat-a",
			Payload:   map[string]any{"agent_type": "cursor"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != "chat-a" {
		t.Fatalf("first session = %q", first.SessionID)
	}
	store := NewFileStateStore(dir)
	_ = store.Save(TurnDedupState{LastTurnID: "turn_old", LastTurnSig: "sig_old"})
	second, err := b.Resolve(ResolveInput{
		AgentType: "cursor",
		EventType: "session.start",
		Envelope: IngestEnvelope{
			SessionID: "chat-b",
			Payload:   map[string]any{"agent_type": "cursor"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.ResetDedup {
		t.Fatal("expected ResetDedup on new session")
	}
	if second.SessionID != "chat-b" {
		t.Fatalf("second session = %q", second.SessionID)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	dedup, _ := store.Load()
	if dedup.LastTurnID != "" {
		t.Fatalf("dedup cleared = %+v", dedup)
	}
}

func TestSessionBinderConcurrentSaveLoadDoesNotExposePartialJSON(t *testing.T) {
	dir := t.TempDir()
	b := NewSessionBinder(dir)
	if err := b.Save(BindingState{
		AgentType:          "claude_code",
		SessionID:          "session-initial",
		TaskID:             "task-initial",
		ExternalSessionKey: "session-initial",
		WorkspaceID:        "local_default_workspace",
		ProjectID:          "the-one",
		RepoID:             "the-one",
	}); err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 128)
	stop := make(chan struct{})
	var writers sync.WaitGroup
	var readers sync.WaitGroup

	for i := 0; i < 6; i++ {
		writers.Add(1)
		go func(worker int) {
			defer writers.Done()
			for j := 0; j < 500; j++ {
				if err := b.Save(BindingState{
					AgentType:          "claude_code",
					SessionID:          "session-writer",
					TaskID:             "task-writer",
					ExternalSessionKey: "session-writer",
					WorkspaceID:        "local_default_workspace",
					ProjectID:          "the-one",
					RepoID:             strings.Repeat("repo", worker+j+1),
				}); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}(i)
	}

	for i := 0; i < 12; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				state, ok, err := b.Load("claude_code")
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if ok && state.AgentType != "claude_code" {
					select {
					case errCh <- fmt.Errorf("agent_type = %q", state.AgentType):
					default:
					}
					return
				}
			}
		}()
	}

	writers.Wait()
	close(stop)
	readers.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent binding access returned error: %v", err)
		}
	}
}
