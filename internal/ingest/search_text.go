package ingest

import "strings"

// SearchTextInput 是 P1 FTS search_text 的输入字段集合。
type SearchTextInput struct {
	Title             string
	Content           string
	NormalizedContent string
	Keywords          []string
	Tags              []string
	RetrievalCues     []string
	Entities          []string
}

// BuildSearchText 按 P1 详细设计拼接 FTS 文档。这里不解析 JSON，调用方传入已归一化的数组。
func BuildSearchText(input SearchTextInput) string {
	parts := []string{
		input.Title,
		input.Content,
		input.NormalizedContent,
		"keywords: " + strings.Join(input.Keywords, " "),
		"tags: " + strings.Join(input.Tags, " "),
		"retrieval: " + strings.Join(input.RetrievalCues, " "),
		"entities: " + strings.Join(input.Entities, " "),
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
