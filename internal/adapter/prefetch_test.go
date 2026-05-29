package adapter

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

func TestTaskIDFromPromptStable(t *testing.T) {
	a := TaskIDFromPrompt("修复 ingest 绑定")
	b := TaskIDFromPrompt("修复  ingest   绑定")
	if a == "" || a != b {
		t.Fatalf("task ids = %q vs %q", a, b)
	}
	if a == "task_cursor_auto" {
		t.Fatalf("unexpected auto task id")
	}
}

func TestBindTaskFromPrompt(t *testing.T) {
	dir := t.TempDir()
	b := NewSessionBinder(dir)
	conv := "conv_bind_prompt"
	_, err := b.Resolve(ResolveInput{
		AgentType: "cursor",
		Producer:  "cursor_hook:sessionStart",
		EventType: "session.start",
		Envelope: IngestEnvelope{
			SessionID: conv,
			Payload: map[string]any{
				"agent_type":      "cursor",
				"conversation_id": conv,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = b.MarkBootstrapTask("cursor")
	bound, taskID, err := b.BindTaskFromPrompt(BindTaskFromPromptInput{
		AgentType:          "cursor",
		ExternalSessionKey: conv,
		NormalizedPrompt:   "实现 P3 prefetch",
		Observe: func(ctx context.Context, req capture.ObserveRequest) (capture.ObserveResponse, error) {
			if req.EventType != capture.EventTaskStart {
				t.Fatalf("event_type = %s", req.EventType)
			}
			return capture.ObserveResponse{Accepted: true}, nil
		},
	})
	if err != nil || !bound {
		t.Fatalf("bound=%v err=%v", bound, err)
	}
	state, ok, _ := b.Load("cursor")
	if !ok || state.TaskFromPromptPending || state.TaskID != taskID {
		t.Fatalf("state = %+v", state)
	}
}

func TestPrefetchProcessorBindAndContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime-state")
	b := NewSessionBinder(dir)
	conv := "conv_prefetch_p3"
	_, _ = b.Resolve(ResolveInput{
		AgentType: "cursor",
		EventType: "session.start",
		Producer:  "cursor_hook:sessionStart",
		Envelope: IngestEnvelope{
			SessionID: conv,
			Payload:   map[string]any{"agent_type": "cursor", "conversation_id": conv},
		},
	})
	_ = b.MarkBootstrapTask("cursor")

	var contextCalls int
	p := &PrefetchProcessor{
		Binder:   b,
		StateDir: dir,
		Context: func(ctx context.Context, req memory.ContextRequest) (memory.ContextResponse, error) {
			contextCalls++
			if req.SessionID != conv {
				t.Fatalf("session_id = %q", req.SessionID)
			}
			return memory.ContextResponse{
				ContextPack:      memory.ContextPack{Summary: "命中记忆"},
				UsedMemoryIDs:    []string{"mem_x"},
				RetrievalTraceID: "rt_test",
			}, nil
		},
		Observe: func(ctx context.Context, req capture.ObserveRequest) (capture.ObserveResponse, error) {
			return capture.ObserveResponse{Accepted: true}, nil
		},
	}
	out := p.Run(context.Background(), PrefetchRequest{
		Task:           "实现 P3 prefetch-context",
		ConversationID: conv,
		AgentType:      "cursor",
		GenerationID:   "gen_p3_001",
		TokenBudget:    800,
	})
	if !out.OK || !out.TaskBound || contextCalls != 1 {
		t.Fatalf("out=%+v contextCalls=%d", out, contextCalls)
	}
	if out.GenerationID != "gen_p3_001" || out.InjectMarkdown == "" {
		t.Fatalf("generation/inject missing: %+v", out)
	}
}
