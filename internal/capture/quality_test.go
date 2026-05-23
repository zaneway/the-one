package capture

import "testing"

func TestCaptureLevel(t *testing.T) {
	cases := []struct {
		name string
		cap  CaptureCapabilities
		want int
	}{
		{name: "level1", cap: CaptureCapabilities{MCPObserve: true}, want: 1},
		{name: "level2", cap: CaptureCapabilities{SessionLifecycle: true, MCPObserve: true}, want: 2},
		{name: "level3", cap: CaptureCapabilities{ToolCallCapture: true, ToolOutputCapture: true}, want: 3},
		{name: "level4", cap: CaptureCapabilities{
			ConversationCapture: true,
			ToolCallCapture:     true,
			ToolOutputCapture:   true,
			FileEditCapture:     true,
			SessionLifecycle:    true,
			MCPObserve:          true,
		}, want: 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CaptureLevel(tc.cap); got != tc.want {
				t.Fatalf("CaptureLevel() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestApplyAcceptedEventUpdatesQuality(t *testing.T) {
	req := ObserveRequest{
		EventType:  EventToolResultSummary,
		OccurredAt: "2026-05-23T20:00:00Z",
		CaptureCapabilities: CaptureCapabilities{
			ToolCallCapture:   true,
			ToolOutputCapture: true,
			SessionLifecycle:  true,
			MCPObserve:        true,
		},
	}

	quality := ApplyAcceptedEvent(CaptureQuality{}, req, false)
	if quality.CapturedEventCount != 1 {
		t.Fatalf("captured count = %d, want 1", quality.CapturedEventCount)
	}
	if quality.ToolResultCount != 1 {
		t.Fatalf("tool result count = %d, want 1", quality.ToolResultCount)
	}
	if quality.LastEventAt != req.OccurredAt {
		t.Fatalf("last event at = %q, want %q", quality.LastEventAt, req.OccurredAt)
	}
	if len(quality.MissingCapabilities) == 0 {
		t.Fatal("missing capabilities empty, want missing level4 capabilities")
	}

	quality = ApplyAcceptedEvent(quality, req, true)
	if quality.DedupedEventCount != 1 {
		t.Fatalf("deduped count = %d, want 1", quality.DedupedEventCount)
	}
	if quality.CapturedEventCount != 1 {
		t.Fatalf("captured count after dedup = %d, want unchanged 1", quality.CapturedEventCount)
	}
}
