package capture

import (
	"context"
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestResolveSuppressRawEventTypesFromConfigPrefersCapture(t *testing.T) {
	cfg := config.Config{
		Capture: config.CaptureConfig{
			SuppressRawEventTypes: []string{EventTurnCompleted},
		},
		Adapter: config.AdapterConfig{
			SuppressRawEventTypes: []string{EventSessionStart},
		},
	}
	got := ResolveSuppressRawEventTypesFromConfig(cfg)
	if len(got) != 1 || got[0] != EventTurnCompleted {
		t.Fatalf("suppress types = %+v, want capture override", got)
	}
}

func TestResolveSuppressRawEventTypesFromConfigFallsBackToAdapter(t *testing.T) {
	cfg := config.Config{
		Adapter: config.AdapterConfig{
			SuppressRawEventTypes: []string{EventFileEditSummary},
		},
	}
	got := ResolveSuppressRawEventTypesFromConfig(cfg)
	if len(got) != 1 || got[0] != EventFileEditSummary {
		t.Fatalf("suppress types = %+v, want adapter override", got)
	}
}

func TestServiceObserveSuppressesDefaultRawEventTypes(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(config.Default(), repo)

	resp, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventToolResultSummary,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		Actor:          ActorTool,
		ContentSummary: "【事实】抑制事件不应写入 raw_event。",
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if !resp.Accepted || len(repo.events) != 0 {
		t.Fatalf("response=%+v events=%d, want accepted without raw_event", resp, len(repo.events))
	}
	if !containsDiagnostic(resp.Diagnostics, "event_type_suppressed") {
		t.Fatalf("diagnostics = %+v, want event_type_suppressed", resp.Diagnostics)
	}
}

func TestServiceObserveCanDisableSuppressList(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(testConfigWithoutSuppress(), repo)

	resp, err := service.Observe(context.Background(), ObserveRequest{
		EventType:      EventToolResultSummary,
		SourceChannel:  SourceChannelMCPTool,
		WorkspaceID:    "ws",
		AgentType:      "codex",
		Actor:          ActorTool,
		ContentSummary: "【事实】关闭抑制后应写入 raw_event。",
	})
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if resp.RawEventID == "" || len(repo.events) != 1 {
		t.Fatalf("response=%+v events=%d, want raw_event persisted", resp, len(repo.events))
	}
}
