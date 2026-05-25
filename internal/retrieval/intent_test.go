package retrieval

import "testing"

func TestDetectIntent(t *testing.T) {
	tests := []struct {
		name string
		text string
		want RetrievalIntent
	}{
		{name: "architecture review", text: "继续复查 P4 详细设计是否有逻辑缺失", want: IntentArchitectureReview},
		{name: "task continuation", text: "继续上次 auth token 任务", want: IntentTaskContinuation},
		{name: "failure recall", text: "这个报错为什么又出现了", want: IntentFailureRecall},
		{name: "preference", text: "我的偏好以后先分析架构边界", want: IntentUserPreference},
		{name: "code task", text: "检查 internal/auth/service.go 里的 ValidateToken 函数", want: IntentCodeTask},
		{name: "general", text: "为什么没有使用 Kafka", want: IntentGeneralSearch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectIntent(tt.text, ""); got != tt.want {
				t.Fatalf("DetectIntent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectSearchIntentUsesHint(t *testing.T) {
	req := SearchRequest{
		Query:      "复查详细设计",
		IntentHint: IntentFailureRecall,
	}
	if got := DetectSearchIntent(req); got != IntentFailureRecall {
		t.Fatalf("DetectSearchIntent() = %q, want hint %q", got, IntentFailureRecall)
	}
}

func TestDetectContextIntentUsesHint(t *testing.T) {
	req := ContextRequest{
		Task:       "继续复查 internal/retrieval/rerank.go 的详细设计",
		IntentHint: IntentCodeTask,
	}
	if got := DetectContextIntent(req); got != IntentCodeTask {
		t.Fatalf("DetectContextIntent() = %q, want hint %q", got, IntentCodeTask)
	}
}

func TestDetectIntentPriorityKeepsArchitectureReviewAheadOfCodeTask(t *testing.T) {
	got := DetectIntent("", "复查 internal/retrieval/rerank.go 的详细设计是否有逻辑缺失")
	if got != IntentArchitectureReview {
		t.Fatalf("DetectIntent() = %q, want architecture review priority", got)
	}
}
