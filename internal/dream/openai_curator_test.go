package dream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

func TestOpenAICuratorUsesExternalModel(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dreamChatCompletionResponseJSON(`{
			"groups": [{
				"projection_id": "topic_memory_dream",
				"topic_key": "memory-system",
				"title": "Obsidian Dream Export",
				"summary": "Dream export turns persistent memories into readonly Obsidian knowledge notes.",
				"source_memory_ids": ["mem_decision", "mem_constraint"],
				"source_map": {
					"design": ["mem_decision"],
					"boundary": ["mem_constraint"]
				},
				"route_category": "knowledge",
				"route_subject": "memory-systems",
				"memory_type_bucket": "decisions"
			}]
		}`)))
	}))
	defer server.Close()

	curator, err := NewOpenAICurator(OpenAICuratorConfig{
		APIKey:          "test-key",
		BaseURL:         server.URL,
		Model:           "gpt-5-mini",
		Timeout:         time.Second,
		MaxOutputTokens: 400,
	})
	if err != nil {
		t.Fatalf("NewOpenAICurator() error = %v", err)
	}

	out, err := curator.Curate(context.Background(), CurationInput{
		Config: CurationConfig{MaxInputMemories: 10, MaxInputChars: 4000, MinGroupSize: 2, RequireSourceMemoryIDs: true},
		Memories: []MemoryRecord{
			{ID: "mem_decision", MemoryType: memory.TypeDecision, Title: "Dream route", Content: "Use route model C internally."},
			{ID: "mem_constraint", MemoryType: memory.TypeConstraint, Title: "Readonly vault", Content: "Obsidian vault export is readonly."},
		},
	})
	if err != nil {
		t.Fatalf("Curate() error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("external requests = %d, want 1", len(requests))
	}
	if userContent := dreamRequestUserContent(requests[0]); !strings.Contains(userContent, "dream_curate_memories") || !strings.Contains(userContent, "mem_decision") {
		t.Fatalf("user content = %s, want dream curation task and memory ids", userContent)
	}
	if len(out.Groups) != 1 {
		t.Fatalf("groups = %+v, want one curated group", out.Groups)
	}
	group := out.Groups[0]
	if group.ProjectionID != "topic_memory_dream" || group.RouteCategory != RouteKnowledge || len(group.SourceMemoryIDs) != 2 {
		t.Fatalf("group = %+v, want mapped model output", group)
	}
	if group.SourceMap["boundary"][0] != "mem_constraint" {
		t.Fatalf("source_map = %+v, want preserved source map", group.SourceMap)
	}
}

func dreamRequestUserContent(request map[string]any) string {
	messages, _ := request["messages"].([]any)
	for _, message := range messages {
		item, _ := message.(map[string]any)
		if item["role"] == "user" {
			content, _ := item["content"].(string)
			return content
		}
	}
	return ""
}

func dreamChatCompletionResponseJSON(outputText string) string {
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
		}]
	}`
}
