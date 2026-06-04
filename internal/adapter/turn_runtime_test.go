package adapter

import (
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
	if len(requests) < 6 {
		t.Fatalf("requests len = %d, want >= 6", len(requests))
	}
	assertHasEvent(t, requests, capture.EventTaskStart)
	assertHasEvent(t, requests, capture.EventConversationMessage)
	assertHasEvent(t, requests, capture.EventAgentResponseSummary)
	assertAgentHasMemoryContextRef(t, requests)
	assertHasEvent(t, requests, capture.EventToolResultSummary)
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
	if hasEvent(second, capture.EventConversationMessage) || hasEvent(second, capture.EventAgentResponseSummary) {
		t.Fatalf("second requests should be debounced for base turn events, got %+v", second)
	}
}

func TestTurnRuntimeWrapsLegacyAgentSummaryAsStructuredContent(t *testing.T) {
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
		if req.EventType == capture.EventAgentResponseSummary {
			if !capture.HasStructuredContentSummaryTag(req.ContentSummary) {
				t.Fatalf("agent content_summary = %q, want structured tag", req.ContentSummary)
			}
			return
		}
	}
	t.Fatalf("missing agent.response.summary event: %+v", requests)
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
	userReq := findEvent(t, requests, capture.EventConversationMessage)
	agentReq := findEvent(t, requests, capture.EventAgentResponseSummary)
	if strings.Join(userReq.SalientSpans, "|") != "用户要求修复 Codex hook 记忆污染" {
		t.Fatalf("user salient_spans = %+v", userReq.SalientSpans)
	}
	if strings.Join(agentReq.SalientSpans, "|") != "TurnRuntime 按 actor 分离 spans" {
		t.Fatalf("agent salient_spans = %+v", agentReq.SalientSpans)
	}
	if strings.Join(userReq.Keywords, "|") != "codex|用户需求" {
		t.Fatalf("user keywords = %+v", userReq.Keywords)
	}
	if strings.Join(agentReq.Keywords, "|") != "TurnRuntime|salient_spans" {
		t.Fatalf("agent keywords = %+v", agentReq.Keywords)
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

func hasEvent(requests []capture.ObserveRequest, eventType string) bool {
	for _, req := range requests {
		if req.EventType == eventType {
			return true
		}
	}
	return false
}

func assertAgentHasMemoryContextRef(t *testing.T, requests []capture.ObserveRequest) {
	t.Helper()
	for _, req := range requests {
		if req.EventType != capture.EventAgentResponseSummary {
			continue
		}
		for _, ref := range req.SourceRefs {
			if ref["source_type"] == "memory_context" {
				return
			}
		}
	}
	t.Fatalf("agent.response.summary missing memory_context source_ref")
}
