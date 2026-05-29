package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/capture"
)

func TestInferKind(t *testing.T) {
	k, err := InferKind("session.start", map[string]any{})
	if err != nil || k != KindSessionLifecycle {
		t.Fatalf("session.start kind = %q err=%v", k, err)
	}
	k, err = InferKind("tool.result.summary", map[string]any{"tool_name": "Shell"})
	if err != nil || k != KindCaptureAtomic {
		t.Fatalf("tool kind = %q err=%v", k, err)
	}
	_, err = InferKind("tool.result.summary", map[string]any{"user_summary": "x"})
	if err == nil {
		t.Fatal("expected INVALID_ATOMIC_SHAPE")
	}
	k, err = InferKind("agent.response.summary", map[string]any{
		"user_summary":  "hi",
		"agent_summary": "bye",
	})
	if err != nil || k != KindTurnCompleted {
		t.Fatalf("turn kind = %q err=%v", k, err)
	}
}

func TestIngestProcessorLedgerDedup(t *testing.T) {
	dir := t.TempDir()
	ledger := NewIngestLedger(dir)
	ingestID := "ing_test_001"
	if err := ledger.Mark(ingestID, 0); err != nil {
		t.Fatal(err)
	}
	hit, err := ledger.Contains(ingestID, 0)
	if err != nil || !hit {
		t.Fatalf("contains = %v err=%v", hit, err)
	}
}

func TestIngestProcessorRejectsMCPProducer(t *testing.T) {
	dir := t.TempDir()
	p := &IngestProcessor{
		Binder:     NewSessionBinder(dir),
		Ledger:     NewIngestLedger(dir),
		Failures:   NewFailureQueue(dir),
		StateStore: NewFileStateStore(dir),
		Observe: func(ctx context.Context, req capture.ObserveRequest) (capture.ObserveResponse, error) {
			return capture.ObserveResponse{Accepted: true}, nil
		},
	}
	out := p.Process(context.Background(), "ing_mcp", []IngestWorkItem{{
		EventIndex: 0,
		Envelope: IngestEnvelope{
			IngestID:        "ing_mcp",
			ProtocolVersion: ProtocolV1,
			Producer:        "mcp:memory_observe",
			SessionID:       "sess_001",
			EventType:       "conversation.message",
			Payload:         map[string]any{"agent_type": "cursor"},
		},
	}})
	if out.OK || !strings.Contains(out.Error, "WRONG_TRANSPORT") {
		t.Fatalf("result = %+v", out)
	}
}

func TestSessionBinderBootstrapTaskPending(t *testing.T) {
	dir := t.TempDir()
	b := NewSessionBinder(dir)
	_, err := b.Resolve(ResolveInput{
		AgentType: "cursor",
		Producer:  "cursor_hook:afterFileEdit",
		EventType: "file.edit.summary",
		Envelope: IngestEnvelope{
			SessionID: "conv-001",
			Payload: map[string]any{
				"agent_type": "cursor",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.MarkBootstrapTask("cursor"); err != nil {
		t.Fatal(err)
	}
	state, ok, err := b.Load("cursor")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if state.TaskID != "task_cursor_auto" || !state.TaskFromPromptPending {
		t.Fatalf("state = %+v", state)
	}
}
