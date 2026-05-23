package capture

import (
	"strings"
	"testing"

	"github.com/zaneway/the-one/internal/config"
)

func TestNormalizeObserveRequiresSessionForAgentEvent(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:     EventToolCall,
		SourceChannel: SourceChannelAgentSession,
		WorkspaceID:   "ws_local",
		AgentType:     "codex",
	}

	err := NormalizeObserve(cfg, &req)
	if err == nil || !strings.Contains(err.Error(), "SESSION_REQUIRED") {
		t.Fatalf("NormalizeObserve() error = %v, want SESSION_REQUIRED", err)
	}
}

func TestNormalizeObserveRequiresExplicitAgentTypeForAgentSession(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:     EventSessionStart,
		SourceChannel: SourceChannelAgentSession,
		WorkspaceID:   "ws_local",
	}

	err := NormalizeObserve(cfg, &req)
	if err == nil || !strings.Contains(err.Error(), "agent_type") {
		t.Fatalf("NormalizeObserve() error = %v, want agent_type validation", err)
	}
}

func TestNormalizeObserveAllowsSessionStartWithoutSessionID(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:     EventSessionStart,
		SourceChannel: SourceChannelAgentSession,
		WorkspaceID:   "ws_local",
		AgentType:     "claude_code",
		Actor:         ActorAdapter,
		Task: &TaskInput{
			TaskSummary: "  run   P2 tests  ",
		},
		SourceRefs: []SourceRef{{"capture_method": "git_diff"}},
	}

	if err := NormalizeObserve(cfg, &req); err != nil {
		t.Fatalf("NormalizeObserve() error = %v", err)
	}
	if req.Task.TaskSummary != "run P2 tests" {
		t.Fatalf("task summary = %q, want normalized summary", req.Task.TaskSummary)
	}
}

func TestNormalizeObserveRejectsUnsupportedCaptureMethod(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:     EventFileEditSummary,
		SourceChannel: SourceChannelManualCLI,
		SourceRefs:    []SourceRef{{"capture_method": "private_plugin"}},
	}

	err := NormalizeObserve(cfg, &req)
	if err == nil || !strings.Contains(err.Error(), "unsupported capture_method") {
		t.Fatalf("NormalizeObserve() error = %v, want unsupported capture_method", err)
	}
}
