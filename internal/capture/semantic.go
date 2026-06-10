package capture

import "context"

// SemanticEnhancer 是旧版 observe 写入前语义增强扩展点。
// 当前 capture 主链路不再调用它；外部 AI 应在 raw_event 落库后通过 processor 抽取 evidence/candidate。
// 保留该接口仅用于兼容已有 provider 能力和单元测试。
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
