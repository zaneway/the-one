package capture

import "context"

// SemanticEnhancer 在 raw_event 写入前执行语义等价简化和关键词提取。
// 实现者不得写入存储；失败或无法确认语义等价时，Observe 会拒绝本次写入。
type SemanticEnhancer interface {
	EnhanceObserve(ctx context.Context, input SemanticEnhanceInput) (SemanticEnhanceOutput, error)
}

// SemanticEnhanceInput 是待简化的 observe 最小化内容。
// 只传递已有摘要字段和引用元数据，不传递完整 prompt、完整输出或完整 diff。
type SemanticEnhanceInput struct {
	EventType      string      `json:"event_type"`
	SourceChannel  string      `json:"source_channel"`
	Actor          string      `json:"actor"`
	ToolName       string      `json:"tool_name"`
	InputSummary   string      `json:"input_summary"`
	OutputSummary  string      `json:"output_summary"`
	ContentSummary string      `json:"content_summary"`
	Keywords       []string    `json:"keywords"`
	SalientSpans   []string    `json:"salient_spans"`
	SourceRefs     []SourceRef `json:"source_refs"`
}

// SemanticEnhanceOutput 是语义增强结果。
// SemanticEquivalent 必须为 true；调用方才会使用简化摘要与关键词。
type SemanticEnhanceOutput struct {
	InputSummary       string   `json:"input_summary"`
	OutputSummary      string   `json:"output_summary"`
	ContentSummary     string   `json:"content_summary"`
	Keywords           []string `json:"keywords"`
	SalientSpans       []string `json:"salient_spans"`
	SemanticEquivalent bool     `json:"semantic_equivalent"`
}
