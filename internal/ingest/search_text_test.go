package ingest

import (
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestBuildSearchTextIncludesRetrievalFields(t *testing.T) {
	text := BuildSearchText(SearchTextInput{
		Title:         "暂不引入 Kafka",
		Content:       "当前异步需求不足。",
		Keywords:      []string{"Kafka", "架构决策"},
		RetrievalCues: []string{"为什么没有用 Kafka"},
	})
	for _, want := range []string{"暂不引入 Kafka", "keywords: Kafka 架构决策", "retrieval: 为什么没有用 Kafka"} {
		if !strings.Contains(text, want) {
			t.Fatalf("search text missing %q: %s", want, text)
		}
	}
}

func TestCheckMinimizedContentRejectsLargeContent(t *testing.T) {
	cfg := config.Default().Memory
	err := CheckMinimizedContent(cfg, MinimizationInput{Content: strings.Repeat("x", cfg.MaxContentChars+1)})
	if err == nil {
		t.Fatal("CheckMinimizedContent() error = nil, want CONTENT_TOO_LARGE")
	}
}
