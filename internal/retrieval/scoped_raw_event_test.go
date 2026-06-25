package retrieval

import (
	"testing"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

func TestScopedRawEventPrefersProjectScope(t *testing.T) {
	scope := scopedRawEvent(capture.RawEvent{
		SessionID: "sess_1",
		ProjectID: "the-one",
		RepoID:    "repo_a",
	})
	if scope != memory.ScopeProjectLocal {
		t.Fatalf("scope = %q, want %q", scope, memory.ScopeProjectLocal)
	}
}
