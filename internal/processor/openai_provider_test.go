package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/memory"
)

func TestOpenAIProviderEnhanceObserveSimplifiesAndExtractsKeywords(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionResponseJSON(`{
			"input_summary": "用户要求写入前做语义简化。",
			"output_summary": "",
			"content_summary": "【事实】用户要求记忆写入前做语义等价简化。\n【约束】基于简化语义提取关键词。",
			"keywords": ["语义简化", "关键词提取"],
			"salient_spans": ["语义等价简化"],
			"semantic_equivalent": true
		}`)))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	out, err := provider.EnhanceObserve(context.Background(), capture.SemanticEnhanceInput{
		EventType:      capture.EventUserDeclaration,
		Actor:          capture.ActorUser,
		InputSummary:   strings.Repeat("用户要求写入前做语义简化。", 20),
		ContentSummary: strings.Repeat("【事实】用户要求记忆写入前做语义等价简化。", 20),
	})
	if err != nil {
		t.Fatalf("EnhanceObserve() error = %v", err)
	}
	if !out.SemanticEquivalent || out.ContentSummary != "【事实】用户要求记忆写入前做语义等价简化。\n【约束】基于简化语义提取关键词。" {
		t.Fatalf("EnhanceObserve() = %+v, want semantic preserving simplification", out)
	}
	if strings.Join(out.Keywords, ",") != "语义简化,关键词提取" {
		t.Fatalf("keywords = %+v, want semantic keywords", out.Keywords)
	}
	if userContent := requestUserContent(request); !strings.Contains(userContent, "user.declaration") {
		t.Fatalf("request user content = %#v, want serialized observe input", userContent)
	}
	systemContent := requestSystemContent(request)
	for _, want := range []string{"content_summary", "semantic_equivalent=false", "【事实】", "full_text/full_output/full_diff"} {
		if !strings.Contains(systemContent, want) {
			t.Fatalf("system content = %q, want to contain %q", systemContent, want)
		}
	}
}

func TestOpenAIProviderEnhanceObserveUsesConfiguredPrompt(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionResponseJSON(`{
			"input_summary": "",
			"output_summary": "",
			"content_summary": "【事实】custom prompt applied",
			"keywords": ["custom"],
			"salient_spans": [],
			"semantic_equivalent": true
		}`)))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:                "test-key",
		BaseURL:               server.URL,
		Model:                 "gpt-5-mini",
		Timeout:               time.Second,
		MaxOutputTokens:       400,
		SemanticEnhancePrompt: "CUSTOM SEMANTIC PROMPT",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	if _, err := provider.EnhanceObserve(context.Background(), capture.SemanticEnhanceInput{
		EventType:      capture.EventUserDeclaration,
		ContentSummary: "【事实】custom",
	}); err != nil {
		t.Fatalf("EnhanceObserve() error = %v", err)
	}
	if !strings.HasPrefix(requestSystemContent(request), "CUSTOM SEMANTIC PROMPT") {
		t.Fatalf("system content = %q, want configured prompt", requestSystemContent(request))
	}
}

func TestOpenAIProviderExtractEvidenceUsesChatCompletionsAPI(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q, want bearer test-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionResponseJSON(`{
			"evidence": [{
				"source_type": "user_declared",
				"interpreted_statement": "用户要求先设计再实现",
				"keywords": ["设计", "实现"],
				"salient_spans": ["先设计再实现"],
				"source_ref": {"producer": "test"},
				"confidence": 0.91
			}]
		}`)))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	out, err := provider.ExtractEvidence(context.Background(), EvidenceInput{
		RawEvent: capture.RawEvent{
			ID:              "evt_1",
			EventType:       capture.EventUserDeclaration,
			ContentSummary:  "以后先设计再实现。",
			SourceRefsJSON:  `{"producer":"test"}`,
			RawPayloadJSON:  `{"message":"以后先设计再实现。","trace_id":"raw-1"}`,
			PayloadSchema:   "conversation_message.v1",
			RawPayloadHash:  "sha256:raw-openai",
			RedactionState:  capture.RedactionStateRedacted,
			RedactionPolicy: "theone.default.v1",
			Truncation: capture.TruncationPolicy{
				Truncated:         true,
				OriginalSizeBytes: 4096,
				StoredSizeBytes:   1024,
				MaxSizeBytes:      1024,
				Reason:            "max_raw_payload_bytes",
			},
		},
		Now: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if len(out) != 1 || out[0].InterpretedStatement != "用户要求先设计再实现" || out[0].Confidence != 0.91 {
		t.Fatalf("ExtractEvidence() = %+v, want parsed evidence", out)
	}
	if request["model"] != "gpt-5-mini" {
		t.Fatalf("model = %v, want gpt-5-mini", request["model"])
	}
	responseFormat, ok := request["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", request["response_format"])
	}
	userContent := requestUserContent(request)
	if !strings.Contains(userContent, "以后先设计再实现") {
		t.Fatalf("user content = %#v, want serialized event content", userContent)
	}
	for _, want := range []string{"raw_payload_json", "payload_schema", "raw_payload_hash", "redaction_state", "truncation", "sha256:raw-openai", "conversation_message.v1"} {
		if !strings.Contains(userContent, want) {
			t.Fatalf("user content = %s, want raw event payload metadata %q", userContent, want)
		}
	}
	systemContent := requestSystemContent(request)
	for _, want := range []string{"判断输入是否值得保存", "不值得保存时返回空数组", "source_ref", "confidence"} {
		if !strings.Contains(systemContent, want) {
			t.Fatalf("system content = %q, want to contain %q", systemContent, want)
		}
	}
}

func TestOpenAIProviderGenerateCandidatesParsesStructuredOutput(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionResponseJSON(`{
			"candidates": [{
				"memory_type": "preference",
				"scope": "project_local",
				"title": "设计优先",
				"content": "用户偏好先设计再实现。",
				"keywords": ["设计"],
				"entities": ["theone"],
				"retrieval_cues": ["设计评审"],
				"tags": ["workflow"],
				"confidence": 0.88,
				"importance": 0.73,
				"encoding_depth": 2,
				"candidate_reason": ["explicit_user_preference"]
			}]
		}`)))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	out, err := provider.GenerateCandidates(context.Background(), CandidateInput{
		Evidence: memory.Evidence{
			ID:                   "ev_1",
			InterpretedStatement: "用户要求先设计再实现",
		},
		RawEvent: capture.RawEvent{
			ID:          "evt_1",
			EventType:   capture.EventUserDeclaration,
			WorkspaceID: "ws",
			ProjectID:   "project_a",
			SessionID:   "sess_1",
			TaskID:      "task_1",
		},
		Now: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GenerateCandidates() error = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(out))
	}
	got := out[0]
	if got.MemoryType != memory.TypePreference || got.Scope != memory.ScopeProjectLocal || got.Content != "用户偏好先设计再实现。" {
		t.Fatalf("candidate = %+v, want model generated preference", got)
	}
	if got.WorkspaceID != "ws" || got.ProjectID != "project_a" || got.SourceEvidenceIDs[0] != "ev_1" {
		t.Fatalf("candidate lineage = %+v, want raw event/evidence lineage", got)
	}
	systemContent := requestSystemContent(request)
	for _, want := range []string{"选择 memory_type", "选择 scope", "不要编造", "user_global"} {
		if !strings.Contains(systemContent, want) {
			t.Fatalf("system content = %q, want to contain %q", systemContent, want)
		}
	}
}

func TestOpenAIProviderExtractAndCandidateUseConfiguredPrompts(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		userContent := requestUserContent(request)
		if strings.Contains(userContent, "extract_evidence") {
			_, _ = w.Write([]byte(chatCompletionResponseJSON(`{"evidence":[]}`)))
			return
		}
		_, _ = w.Write([]byte(chatCompletionResponseJSON(`{"candidates":[]}`)))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:                   "test-key",
		BaseURL:                  server.URL,
		Model:                    "gpt-5-mini",
		Timeout:                  time.Second,
		MaxOutputTokens:          400,
		ExtractEvidencePrompt:    "CUSTOM EVIDENCE PROMPT",
		GenerateCandidatesPrompt: "CUSTOM CANDIDATE PROMPT",
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	if _, err := provider.ExtractEvidence(context.Background(), EvidenceInput{
		RawEvent: capture.RawEvent{ID: "evt_1", EventType: capture.EventUserDeclaration, ContentSummary: "【事实】custom"},
	}); err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}
	if _, err := provider.GenerateCandidates(context.Background(), CandidateInput{
		Evidence: memory.Evidence{ID: "ev_1", InterpretedStatement: "custom"},
		RawEvent: capture.RawEvent{ID: "evt_1", EventType: capture.EventUserDeclaration},
	}); err != nil {
		t.Fatalf("GenerateCandidates() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if !strings.HasPrefix(requestSystemContent(requests[0]), "CUSTOM EVIDENCE PROMPT") {
		t.Fatalf("extract system content = %q, want configured prompt", requestSystemContent(requests[0]))
	}
	if !strings.HasPrefix(requestSystemContent(requests[1]), "CUSTOM CANDIDATE PROMPT") {
		t.Fatalf("candidate system content = %q, want configured prompt", requestSystemContent(requests[1]))
	}
}

func TestOpenAIProviderCheckHealthUsesChatCompletionsAPI(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q, want bearer test-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionResponseJSON(`{"ok":true}`)))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	status, err := provider.CheckHealth(context.Background())
	if err != nil {
		t.Fatalf("CheckHealth() error = %v", err)
	}
	if status.Provider != "openai" || status.Model != "gpt-5-mini" || status.LatencyMS < 0 {
		t.Fatalf("status = %+v, want openai model and latency", status)
	}
	if request["model"] != "gpt-5-mini" {
		t.Fatalf("model = %v, want gpt-5-mini", request["model"])
	}
	responseFormat, ok := request["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", request["response_format"])
	}
	userContent := requestUserContent(request)
	if !strings.Contains(userContent, "health_check") {
		t.Fatalf("user content = %#v, want health check payload", userContent)
	}
}

func TestOpenAIProviderLogsRequestAndResponseBodies(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionResponseJSON(`{"evidence":[]}`)))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	if _, err := provider.ExtractEvidence(context.Background(), EvidenceInput{
		RawEvent: capture.RawEvent{
			ID:             "evt_log_1",
			EventType:      capture.EventUserDeclaration,
			ContentSummary: "以后先设计再实现。",
		},
	}); err != nil {
		t.Fatalf("ExtractEvidence() error = %v", err)
	}

	logs := buf.String()
	for _, want := range []string{
		"openai provider request",
		"openai provider response",
		"theone_evidence",
		"request_body",
		"response_body",
		"以后先设计再实现。",
		"evidence",
		"resp_test",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs = %q, want to contain %q", logs, want)
		}
	}
}

func TestOpenAIProviderLogsFailedRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIProviderConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	if _, err := provider.ExtractEvidence(context.Background(), EvidenceInput{
		RawEvent: capture.RawEvent{ID: "evt_log_2", EventType: capture.EventUserDeclaration},
	}); err == nil {
		t.Fatal("ExtractEvidence() error = nil, want provider failure")
	}

	logs := buf.String()
	for _, want := range []string{
		"openai provider request",
		"openai provider request failed",
		"request_body",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs = %q, want to contain %q", logs, want)
		}
	}
}

func TestOpenAIProviderRequiresAPIKey(t *testing.T) {
	_, err := NewOpenAIProvider(OpenAIProviderConfig{
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
	})
	if err == nil {
		t.Fatal("NewOpenAIProvider() error = nil, want missing key error")
	}
}

func TestProviderLogBodyTruncatesLongPayload(t *testing.T) {
	long := strings.Repeat("a", providerLogBodyMaxChars+10)
	got := providerLogBody(long)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Fatalf("providerLogBody() = %q, want truncated suffix", got)
	}
	if len(got) != providerLogBodyMaxChars+len("...(truncated)") {
		t.Fatalf("truncated length = %d, want %d", len(got), providerLogBodyMaxChars+len("...(truncated)"))
	}
}

func requestSystemContent(request map[string]any) string {
	return requestMessageContent(request, "system")
}

func requestUserContent(request map[string]any) string {
	return requestMessageContent(request, "user")
}

func requestMessageContent(request map[string]any, role string) string {
	messages, _ := request["messages"].([]any)
	for _, message := range messages {
		item, _ := message.(map[string]any)
		if item["role"] == role {
			content, _ := item["content"].(string)
			return content
		}
	}
	return ""
}

func chatCompletionResponseJSON(outputText string) string {
	escaped, _ := json.Marshal(outputText)
	return `{
		"id": "resp_test",
		"object": "chat.completion",
		"created": 1781000000,
		"model": "gpt-5-mini",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": ` + string(escaped) + `
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`
}
