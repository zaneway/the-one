package adapter

import (
	"testing"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
)

func TestResolveSuppressRawEventTypesUsesDefaultsWhenUnset(t *testing.T) {
	got := ResolveSuppressRawEventTypes(config.Config{})
	want := DefaultSuppressRawEventTypes()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveSuppressRawEventTypesHonorsExplicitEmptyList(t *testing.T) {
	got := ResolveSuppressRawEventTypes(config.Config{
		Adapter: config.AdapterConfig{
			SuppressRawEventTypes: []string{},
		},
	})
	if len(got) != 0 {
		t.Fatalf("got = %v, want empty", got)
	}
}

func TestResolveSuppressRawEventTypesNormalizesCustomList(t *testing.T) {
	got := ResolveSuppressRawEventTypes(config.Config{
		Adapter: config.AdapterConfig{
			SuppressRawEventTypes: []string{
				" session.start ",
				"session.start",
				capture.EventFileEditSummary,
			},
		},
	})
	if len(got) != 2 || got[0] != capture.EventSessionStart || got[1] != capture.EventFileEditSummary {
		t.Fatalf("got = %v", got)
	}
}

func TestIngestProcessorShouldSuppressRawEvent(t *testing.T) {
	p := &IngestProcessor{
		SuppressRawEventTypes: DefaultSuppressRawEventTypes(),
	}
	if !p.shouldSuppressRawEvent(capture.EventFileEditSummary) {
		t.Fatal("expected file.edit.summary to be suppressed")
	}
	if p.shouldSuppressRawEvent(capture.EventTurnCompleted) {
		t.Fatal("did not expect turn.completed to be suppressed")
	}
}
