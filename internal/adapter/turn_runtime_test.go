package adapter

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/capture"
)

func TestTurnRuntimeBuildObserveRequests(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	requests, err := runtime.BuildObserveRequests(TurnPayload{
		WorkspaceID:            "ws",
		ProjectID:              "project_a",
		RepoID:                 "repo_a",
		AgentType:              "cursor",
		SessionID:              "sess_001",
		TaskID:                 "task_001",
		TurnID:                 "turn_001",
		UserSummary:            "修复 token 过期问题",
		AgentSummary:           "完成修复并通过测试",
		IsSubstantive:          true,
		TaskSummary:            "修复认证过期边界",
		TaskStatus:             capture.StatusSucceeded,
		OutcomeSummary:         "任务完成",
		UserDeclarationSummary: "",
		UserCorrectionSummary:  "",
		DecisionSummary:        "采用单点时间比较策略",
		ReasonSummary:          "降低边界误差",
		Keywords:               []string{"auth", "token"},
		SalientSpans:           []string{"通过全部测试"},
		ToolResults:            []ToolResultInput{{ToolName: "go test", OutputSummary: "ok", ExitCode: 0}},
		FileEdits:              []FileEditInput{{FilePath: "internal/auth/middleware.go", ContentSummary: "调整过期判断"}},
		RetrievalTraceID:       "rt_test",
		UsedMemoryIDs:          []string{"mem_a", "mem_b"},
		InjectedToPrompt:       true,
	})
	if err != nil {
		t.Fatalf("BuildObserveRequests() error = %v", err)
	}
	if len(requests) < 5 {
		t.Fatalf("requests len = %d, want >= 5", len(requests))
	}
	assertHasEvent(t, requests, capture.EventTaskStart)
	assertHasEvent(t, requests, capture.EventTurnCompleted)
	assertMissingEvent(t, requests, capture.EventConversationMessage)
	assertMissingEvent(t, requests, capture.EventAgentResponseSummary)
	assertTurnHasMemoryContextRef(t, requests)
	assertMissingEvent(t, requests, capture.EventToolResultSummary)
	assertHasEvent(t, requests, capture.EventFileEditSummary)
	assertHasEvent(t, requests, capture.EventAgentDecision)
	assertHasEvent(t, requests, capture.EventTaskResult)
}

func TestTurnRuntimeDebounceSameTurn(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	payload := TurnPayload{
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		RepoID:        "repo_a",
		AgentType:     "cursor",
		SessionID:     "sess_001",
		TaskID:        "task_001",
		TurnID:        "turn_001",
		UserSummary:   "问题描述",
		AgentSummary:  "结果摘要",
		IsSubstantive: true,
	}
	first, err := runtime.BuildObserveRequests(payload)
	if err != nil {
		t.Fatalf("first BuildObserveRequests() error = %v", err)
	}
	second, err := runtime.BuildObserveRequests(payload)
	if err != nil {
		t.Fatalf("second BuildObserveRequests() error = %v", err)
	}
	if len(first) == 0 {
		t.Fatalf("first requests len = 0, want > 0")
	}
	if hasEvent(second, capture.EventTurnCompleted) {
		t.Fatalf("second requests should be debounced for base turn events, got %+v", second)
	}
}

func TestTurnRuntimeWrapsLegacyTurnSummaryAsStructuredContent(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	requests, err := runtime.BuildObserveRequests(TurnPayload{
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		RepoID:        "repo_a",
		AgentType:     "cursor",
		SessionID:     "sess_structured",
		TaskID:        "task_structured",
		TurnID:        "turn_structured",
		UserSummary:   "用户要求调整摘要",
		AgentSummary:  "已完成捕获规则更新",
		IsSubstantive: true,
	})
	if err != nil {
		t.Fatalf("BuildObserveRequests() error = %v", err)
	}
	for _, req := range requests {
		if req.EventType == capture.EventTurnCompleted {
			if !capture.HasStructuredContentSummaryTag(req.ContentSummary) || !strings.Contains(req.ContentSummary, "已完成捕获规则更新") {
				t.Fatalf("turn content_summary = %q, want structured turn summary", req.ContentSummary)
			}
			return
		}
	}
	t.Fatalf("missing turn.completed event: %+v", requests)
}

func TestTurnRuntimeUsesActorSpecificKeywordsAndSpans(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	requests, err := runtime.BuildObserveRequests(TurnPayload{
		WorkspaceID:       "ws",
		ProjectID:         "project_a",
		RepoID:            "repo_a",
		AgentType:         "codex",
		SessionID:         "sess_actor_fields",
		TaskID:            "task_actor_fields",
		TurnID:            "turn_actor_fields",
		UserSummary:       "【事件】用户要求修复 Codex hook 记忆污染",
		AgentSummary:      "【结论/决策】修复 TurnRuntime 按 actor 分离 spans",
		IsSubstantive:     true,
		Keywords:          []string{"hook", "turn-completed", "trace:rt_bad"},
		SalientSpans:      []string{"legacy mixed span"},
		UserKeywords:      []string{"codex", "用户需求"},
		UserSalientSpans:  []string{"用户要求修复 Codex hook 记忆污染"},
		AgentKeywords:     []string{"TurnRuntime", "salient_spans"},
		AgentSalientSpans: []string{"TurnRuntime 按 actor 分离 spans"},
	})
	if err != nil {
		t.Fatalf("BuildObserveRequests() error = %v", err)
	}
	turnReq := findEvent(t, requests, capture.EventTurnCompleted)
	if strings.Join(turnReq.SalientSpans, "|") != "用户要求修复 Codex hook 记忆污染|TurnRuntime 按 actor 分离 spans" {
		t.Fatalf("turn salient_spans = %+v", turnReq.SalientSpans)
	}
	if strings.Join(turnReq.Keywords, "|") != "codex|用户需求|TurnRuntime|salient_spans" {
		t.Fatalf("turn keywords = %+v", turnReq.Keywords)
	}
}

func TestTurnRuntimeRecordsSemanticDigestSourceRefs(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	requests, err := runtime.BuildObserveRequests(TurnPayload{
		WorkspaceID:            "ws",
		ProjectID:              "project_a",
		RepoID:                 "repo_a",
		AgentType:              "cursor",
		SessionID:              "sess_digest",
		TaskID:                 "task_digest",
		TurnID:                 "turn_digest",
		UserSummary:            "【事件】用户要求记录 prompt 语义摘要",
		AgentSummary:           "【结论/决策】记录 response 语义摘要",
		IsSubstantive:          true,
		UserPromptChars:        42,
		AgentResponseChars:     128,
		SemanticSummaryVersion: "semantic_digest_v1",
	})
	if err != nil {
		t.Fatalf("BuildObserveRequests() error = %v", err)
	}
	turnReq := findEvent(t, requests, capture.EventTurnCompleted)
	assertHasSourceRef(t, turnReq, "user_prompt", 42)
	assertHasSourceRef(t, turnReq, "agent_response", 128)
}

func TestTurnRuntimeCarriesRawPayloadMetadata(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	requests, err := runtime.BuildObserveRequests(TurnPayload{
		WorkspaceID:     "ws",
		ProjectID:       "project_a",
		RepoID:          "repo_a",
		AgentType:       "codex",
		SessionID:       "sess_raw_payload",
		TaskID:          "task_raw_payload",
		TurnID:          "turn_raw_payload",
		UserSummary:     "【事件】用户要求保留原始输入",
		AgentSummary:    "【结论/决策】已保留原始输出",
		IsSubstantive:   true,
		UserRawPayload:  `{"message":"用户要求保留原始输入"}`,
		AgentRawPayload: `{"message":"已保留原始输出"}`,
		PayloadSchema:   "turn.completed.v1",
		RedactionState:  capture.RedactionStateRaw,
	})
	if err != nil {
		t.Fatalf("BuildObserveRequests() error = %v", err)
	}
	turnReq := findEvent(t, requests, capture.EventTurnCompleted)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(turnReq.RawPayloadJSON), &raw); err != nil {
		t.Fatalf("raw payload json = %q: %v", turnReq.RawPayloadJSON, err)
	}
	if string(raw["user"]) != `{"message":"用户要求保留原始输入"}` || string(raw["agent"]) != `{"message":"已保留原始输出"}` {
		t.Fatalf("raw payload = %q", turnReq.RawPayloadJSON)
	}
	if turnReq.InputSummary != "用户要求保留原始输入" || turnReq.OutputSummary != "已保留原始输出" {
		t.Fatalf("input/output = %q/%q, want original user/agent payload text", turnReq.InputSummary, turnReq.OutputSummary)
	}
	if turnReq.PayloadSchema != "turn.completed.v1" {
		t.Fatalf("payload schema=%q", turnReq.PayloadSchema)
	}
	if turnReq.RawPayloadHash == "" {
		t.Fatal("turn raw payload hash is empty")
	}
	if turnReq.RedactionState != capture.RedactionStateRaw {
		t.Fatalf("redaction state=%q", turnReq.RedactionState)
	}
}

func TestTurnRuntimeRequiresScope(t *testing.T) {
	store := NewFileStateStore(filepath.Join(t.TempDir(), "runtime-state"))
	runtime := NewTurnRuntime(store)
	_, err := runtime.BuildObserveRequests(TurnPayload{
		AgentType:   "cursor",
		SessionID:   "sess_001",
		TaskID:      "task_001",
		UserSummary: "x",
	})
	if err == nil {
		t.Fatalf("BuildObserveRequests() error = nil, want missing scope")
	}
}

func assertHasSourceRef(t *testing.T, req capture.ObserveRequest, sourceType string, chars int) {
	t.Helper()
	for _, ref := range req.SourceRefs {
		if ref["source_type"] != sourceType {
			continue
		}
		if _, ok := ref["content_hash"]; ok {
			t.Fatalf("%s source_ref must not include content_hash: %+v", sourceType, ref)
		}
		if ref["content_chars"] != chars {
			t.Fatalf("%s content_chars = %v, want %d", sourceType, ref["content_chars"], chars)
		}
		if ref["semantic_summary_version"] != "semantic_digest_v1" {
			t.Fatalf("%s semantic_summary_version = %v, want semantic_digest_v1", sourceType, ref["semantic_summary_version"])
		}
		return
	}
	t.Fatalf("missing source_ref %s in %+v", sourceType, req.SourceRefs)
}

func findEvent(t *testing.T, requests []capture.ObserveRequest, eventType string) capture.ObserveRequest {
	t.Helper()
	for _, req := range requests {
		if req.EventType == eventType {
			return req
		}
	}
	t.Fatalf("missing event_type %s in %+v", eventType, requests)
	return capture.ObserveRequest{}
}

func assertHasEvent(t *testing.T, requests []capture.ObserveRequest, eventType string) {
	t.Helper()
	if !hasEvent(requests, eventType) {
		t.Fatalf("requests missing event_type %s", eventType)
	}
}

func assertMissingEvent(t *testing.T, requests []capture.ObserveRequest, eventType string) {
	t.Helper()
	if hasEvent(requests, eventType) {
		t.Fatalf("requests unexpectedly include event_type %s", eventType)
	}
}

func hasEvent(requests []capture.ObserveRequest, eventType string) bool {
	for _, req := range requests {
		if req.EventType == eventType {
			return true
		}
	}
	return false
}

func assertTurnHasMemoryContextRef(t *testing.T, requests []capture.ObserveRequest) {
	t.Helper()
	for _, req := range requests {
		if req.EventType != capture.EventTurnCompleted {
			continue
		}
		for _, ref := range req.SourceRefs {
			if ref["source_type"] == "memory_context" {
				return
			}
		}
	}
	t.Fatalf("turn.completed missing memory_context source_ref")
}
