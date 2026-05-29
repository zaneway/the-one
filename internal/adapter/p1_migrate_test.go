package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
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
