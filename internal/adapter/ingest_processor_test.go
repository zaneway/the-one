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

func TestObserveFromAtomicFileEditCarriesChangeMetadata(t *testing.T) {
	req, err := observeFromAtomic(IngestEnvelope{
		Producer:  "cursor_hook:afterFileEdit",
		AgentType: "cursor",
		EventType: capture.EventFileEditSummary,
		Payload: map[string]any{
			"workspace_id":    "ws",
			"project_id":      "project",
			"repo_id":         "repo",
			"content_summary": "【事实】调整 token 过期判断边界",
			"file_path":       "internal/auth/middleware.go",
			"change_type":     "modify",
			"symbol":          "ValidateToken",
			"before_hash":     "sha256:before",
			"after_hash":      "sha256:after",
			"keywords":        []any{"auth", "token"},
			"salient_spans":   []any{"调整 token 过期判断边界"},
		},
	}, "sess_test", "task_test")
	if err != nil {
		t.Fatalf("observeFromAtomic() error = %v", err)
	}
	if req.EventType != capture.EventFileEditSummary || req.Actor != capture.ActorAgent {
		t.Fatalf("request event/actor = %s/%s, want file.edit.summary/agent", req.EventType, req.Actor)
	}
	for _, ref := range req.SourceRefs {
		if ref["source_type"] != "file_edit_summary" {
			continue
		}
		if ref["file_path"] != "internal/auth/middleware.go" || ref["symbol"] != "ValidateToken" {
			t.Fatalf("file source ref missing path/symbol: %+v", ref)
		}
		if ref["before_hash"] != "sha256:before" || ref["after_hash"] != "sha256:after" {
			t.Fatalf("file source ref missing hashes: %+v", ref)
		}
		return
	}
	t.Fatalf("missing file_edit_summary source ref: %+v", req.SourceRefs)
}

func TestBuildRequestsCarriesEnvelopeProducerForTurnCompleted(t *testing.T) {
	dir := t.TempDir()
	p := &IngestProcessor{
		StateStore: NewFileStateStore(dir),
		ExpandMode: ExpandModeV2,
	}
	requests, err := p.buildRequests(IngestEnvelope{
		Producer:  "claude_code_hook:Stop",
		AgentType: "claude_code",
		Kind:      KindTurnCompleted,
		EventType: capture.EventAgentResponseSummary,
		Payload: map[string]any{
			"workspace_id":   "ws",
			"project_id":     "project",
			"repo_id":        "repo",
			"agent_type":     "claude_code",
			"user_summary":   "用户要求检查 provenance",
			"agent_summary":  "已完成 provenance 检查",
			"is_substantive": true,
			"completed_at":   "2026-06-05T12:00:00Z",
			"started_at":     "2026-06-05T11:59:00Z",
			"turn_id":        "turn_provenance",
			"session_id":     "sess_provenance",
			"task_id":        "task_provenance",
			"keywords":       []any{"provenance"},
			"salient_spans":  []any{"producer must be preserved"},
		},
	}, KindTurnCompleted, "sess_provenance", "task_provenance")
	if err != nil {
		t.Fatalf("buildRequests() error = %v", err)
	}
	req := findRequestByEvent(requests, capture.EventAgentResponseSummary)
	if req.EventType == "" {
		t.Fatalf("missing agent.response.summary in %+v", requests)
	}
	for _, ref := range req.SourceRefs {
		if ref["producer"] == "claude_code_hook:Stop" {
			return
		}
	}
	t.Fatalf("source_refs = %+v, want envelope producer", req.SourceRefs)
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

func findRequestByEvent(requests []capture.ObserveRequest, eventType string) capture.ObserveRequest {
	for _, req := range requests {
		if req.EventType == eventType {
			return req
		}
	}
	return capture.ObserveRequest{}
}
