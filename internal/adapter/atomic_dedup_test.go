package adapter

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zaneway/theone/internal/capture"
)

func TestAtomicDedupStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime-state")
	store := NewAtomicDedupStore(dir)
	const sid = "sess_atomic"
	const et = "file.edit.summary"
	fp := "abc123"
	hit, err := store.Contains(sid, et, fp)
	if err != nil || hit {
		t.Fatalf("contains = %v err=%v", hit, err)
	}
	if err := store.Mark(sid, et, fp); err != nil {
		t.Fatal(err)
	}
	hit, err = store.Contains(sid, et, fp)
	if err != nil || !hit {
		t.Fatalf("contains after mark = %v err=%v", hit, err)
	}
}

func TestIngestProcessorAtomicDedupV2(t *testing.T) {
	dir := t.TempDir()
	ledger := NewIngestLedger(dir)
	dedup := NewAtomicDedupStore(dir)
	var observeCalls int
	p := &IngestProcessor{
		Binder:      NewSessionBinder(dir),
		Ledger:      ledger,
		Failures:    NewFailureQueue(dir),
		StateStore:  NewFileStateStore(dir),
		AtomicDedup: dedup,
		ExpandMode:  ExpandModeV2,
		Observe: func(ctx context.Context, req capture.ObserveRequest) (capture.ObserveResponse, error) {
			observeCalls++
			return capture.ObserveResponse{Accepted: true}, nil
		},
	}
	item := IngestWorkItem{
		EventIndex: 0,
		Envelope: IngestEnvelope{
			IngestID:        "ing_atomic_dedup",
			ProtocolVersion: ProtocolV1,
			Producer:        "cursor_hook:diagnostic",
			AgentType:       "cursor",
			SessionID:       "conv_atomic",
			EventType:       "diagnostic.atomic",
			Kind:            KindCaptureAtomic,
			Payload: map[string]any{
				"agent_type":      "cursor",
				"workspace_id":    "local_default_workspace",
				"project_id":      "the-one",
				"repo_id":         "the-one",
				"content_summary": "diagnostic atomic event",
			},
		},
	}
	out1 := p.Process(context.Background(), "ing_atomic_dedup", []IngestWorkItem{item})
	if out1.Accepted != 1 || observeCalls < 1 {
		t.Fatalf("first out=%+v observeCalls=%d", out1, observeCalls)
	}
	callsAfterFirst := observeCalls
	out2 := p.Process(context.Background(), "ing_atomic_dedup_retry", []IngestWorkItem{item})
	if out2.Deduped != 1 {
		t.Fatalf("second out=%+v", out2)
	}
	if observeCalls != callsAfterFirst {
		t.Fatalf("atomic dedup should skip second observe, observeCalls=%d after=%d", callsAfterFirst, observeCalls)
	}
}
