package capture

import (
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestCheckMinimizedObserveRejectsFullOutputSourceRef(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:     EventToolResultSummary,
		SourceChannel: SourceChannelMCPTool,
		OutputSummary: "测试失败摘要",
		SourceRefs: []SourceRef{
			{
				"source_type": "tool_output",
				"full_output": "完整工具输出不允许进入 raw_event",
			},
		},
	}

	err := CheckMinimizedObserve(cfg, req)
	if err == nil || !strings.Contains(err.Error(), "CONTENT_TOO_LARGE") {
		t.Fatalf("CheckMinimizedObserve() error = %v, want CONTENT_TOO_LARGE", err)
	}
}

func TestCheckMinimizedObserveRejectsLongSalientSpan(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:     EventConversationMessage,
		SourceChannel: SourceChannelMCPTool,
		SalientSpans:  []string{strings.Repeat("x", cfg.MaxSalientSpanChars+1)},
	}

	err := CheckMinimizedObserve(cfg, req)
	if err == nil || !strings.Contains(err.Error(), "salient_span exceeds") {
		t.Fatalf("CheckMinimizedObserve() error = %v, want salient span length error", err)
	}
}

func TestContentHashAndDedupKeyAreStable(t *testing.T) {
	req := ObserveRequest{
		SessionID:     "sess_001",
		EventType:     EventToolResultSummary,
		AgentType:     "codex",
		WorkspaceID:   "ws_local",
		ProjectID:     "proj_the_one",
		Actor:         ActorTool,
		ToolName:      "go test",
		OutputSummary: "ok",
		Keywords:      []string{"go test", "pass"},
		SalientSpans:  []string{"go test ./... pass"},
		SourceRefs:    []SourceRef{{"source_type": "tool_output", "exit_code": 0}},
		SourceChannel: SourceChannelAgentSession,
	}

	first, err := ComputeContentHash(req)
	if err != nil {
		t.Fatalf("ComputeContentHash() error = %v", err)
	}
	second, err := ComputeContentHash(req)
	if err != nil {
		t.Fatalf("ComputeContentHash() second error = %v", err)
	}
	if first != second {
		t.Fatalf("hash not stable: %q != %q", first, second)
	}
	key, err := DedupKey(req)
	if err != nil {
		t.Fatalf("DedupKey() error = %v", err)
	}
	if !strings.Contains(key, "sess_001|tool.result.summary") {
		t.Fatalf("dedup key = %q, want session/event suffix", key)
	}
}
