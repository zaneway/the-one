package capture

import (
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestCheckMinimizedObserveRejectsFullOutputSourceRef(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:      EventToolResultSummary,
		SourceChannel:  SourceChannelMCPTool,
		OutputSummary:  "测试失败摘要",
		ContentSummary: "【事件】工具执行结果：go test\n【事实】测试失败摘要",
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
		EventType:      EventConversationMessage,
		SourceChannel:  SourceChannelMCPTool,
		ContentSummary: "【事件】用户消息摘要\n【事实】用户补充需求",
		SalientSpans:   []string{strings.Repeat("x", cfg.MaxSalientSpanChars+1)},
	}

	err := CheckMinimizedObserve(cfg, req)
	if err == nil || !strings.Contains(err.Error(), "salient_span exceeds") {
		t.Fatalf("CheckMinimizedObserve() error = %v, want salient span length error", err)
	}
}

func TestCheckMinimizedObserveRejectsUnstructuredContentSummary(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:      EventConversationMessage,
		SourceChannel:  SourceChannelAgentSession,
		ContentSummary: "用户要求调整记忆捕获摘要格式",
	}

	err := CheckMinimizedObserve(cfg, req)
	if err == nil || !strings.Contains(err.Error(), "CONTENT_QUALITY") {
		t.Fatalf("CheckMinimizedObserve() error = %v, want CONTENT_QUALITY", err)
	}
}

func TestCheckMinimizedObserveRejectsLongConversationWithoutSalientSpans(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:     EventConversationMessage,
		SourceChannel: SourceChannelAgentSession,
		ContentSummary: "【结论/决策】采用结构化索引卡作为 content_summary。\n" +
			"【事实】" + strings.Repeat("这是较长的对话事实摘要，需要用 salient_spans 承载原子事实。", 80),
	}

	err := CheckMinimizedObserve(cfg, req)
	if err == nil || !strings.Contains(err.Error(), "CONTENT_QUALITY: content_summary too long without salient_spans") {
		t.Fatalf("CheckMinimizedObserve() error = %v, want long summary without salient spans quality error", err)
	}
}

func TestCheckMinimizedObserveAcceptsStructuredShortContentSummary(t *testing.T) {
	cfg := config.Default().Capture
	req := ObserveRequest{
		EventType:      EventAgentResponseSummary,
		SourceChannel:  SourceChannelAgentSession,
		ContentSummary: "【结论/决策】统一三端 content_summary 为结构化索引卡。\n【约束】不新增 DB 字段，不修改 retrieval 截断策略。",
		Keywords:       []string{"content_summary", "structured", "memory"},
		SalientSpans:   []string{"结构化摘要必须高价值信息前置"},
	}

	if err := CheckMinimizedObserve(cfg, req); err != nil {
		t.Fatalf("CheckMinimizedObserve() error = %v, want nil", err)
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

func TestContentHashIncludesRawPayloadHash(t *testing.T) {
	req := ObserveRequest{
		EventType:      EventConversationMessage,
		AgentType:      "codex",
		WorkspaceID:    "ws_local",
		Actor:          ActorUser,
		ContentSummary: "【事实】用户要求 raw_event 先保存原始事实。",
		SourceChannel:  SourceChannelAgentSession,
		RawPayloadHash: "sha256:payload-a",
	}
	first, err := ComputeContentHash(req)
	if err != nil {
		t.Fatalf("ComputeContentHash() first error = %v", err)
	}
	req.RawPayloadHash = "sha256:payload-b"
	second, err := ComputeContentHash(req)
	if err != nil {
		t.Fatalf("ComputeContentHash() second error = %v", err)
	}
	if first == second {
		t.Fatalf("hash = %q for different raw_payload_hash values", first)
	}
}
