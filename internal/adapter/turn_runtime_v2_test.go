package adapter

import (
	"path/filepath"
	"testing"

	"github.com/zaneway/theone/internal/capture"
)

func TestTurnRuntimeV2SkipsToolAndFileExpansion(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntimeWithExpandMode(store, ExpandModeV2)
	requests, err := runtime.BuildObserveRequests(TurnPayload{
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		RepoID:        "repo_a",
		AgentType:     "cursor",
		SessionID:     "sess_v2",
		TaskID:        "task_v2",
		TurnID:        "turn_v2",
		UserSummary:   "用户问题",
		AgentSummary:  "助手回答",
		IsSubstantive: true,
		ToolResults: []ToolResultInput{{
			ToolName:      "Shell",
			OutputSummary: "ok",
			ExitCode:      0,
		}},
		FileEdits: []FileEditInput{{
			FilePath:       "internal/foo.go",
			ContentSummary: "edit",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(requests, capture.EventToolResultSummary) || hasEvent(requests, capture.EventFileEditSummary) {
		t.Fatalf("v2 should not expand tool/file in turn, got %+v", requests)
	}
	if !hasEvent(requests, capture.EventTurnCompleted) {
		t.Fatalf("v2 should still emit base turn, got %+v", requests)
	}
	if hasEvent(requests, capture.EventConversationMessage) || hasEvent(requests, capture.EventAgentResponseSummary) {
		t.Fatalf("v2 should emit one turn.completed base event, got %+v", requests)
	}
}

func TestTurnRuntimeLegacySkipsToolResultsButKeepsFileEdits(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	requests, err := runtime.BuildObserveRequests(TurnPayload{
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		RepoID:        "repo_a",
		AgentType:     "cursor",
		SessionID:     "sess_legacy",
		TaskID:        "task_legacy",
		TurnID:        "turn_legacy",
		UserSummary:   "用户",
		AgentSummary:  "助手",
		IsSubstantive: true,
		ToolResults: []ToolResultInput{{
			ToolName:      "Shell",
			OutputSummary: "ok",
			ExitCode:      0,
		}},
		FileEdits: []FileEditInput{{
			FilePath:       "internal/foo.go",
			ContentSummary: "edit",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasEvent(requests, capture.EventToolResultSummary) {
		t.Fatalf("legacy should not expand tool results, got %+v", requests)
	}
	if !hasEvent(requests, capture.EventFileEditSummary) {
		t.Fatalf("legacy should still expand file edits, got %+v", requests)
	}
}
