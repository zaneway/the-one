package ingest

import "strings"

// SearchTextInput 是 FTS search_text 的输入字段集合。
type SearchTextInput struct {
	Title             string
	Content           string
	NormalizedContent string
	Keywords          []string
	Tags              []string
	RetrievalCues     []string
	Entities          []string
}

// BuildSearchText 按约定拼接 FTS 文档。
// 拼接规则：title + content + normalized_content + keywords + tags + retrieval_cues + entities，用换行符分隔。
// 设计说明：search_text 是 FTS5 索引的文档内容，不暴露给客户端，只用于全文检索。
// 调用方传入已归一化的数组，避免重复 JSON 解析。
func BuildSearchText(input SearchTextInput) string {
	parts := compactUniqueParts(
		input.Title,
		input.Content,
		input.NormalizedContent,
		labeledPart("keywords", input.Keywords),
		labeledPart("tags", input.Tags),
		labeledPart("retrieval", input.RetrievalCues),
		labeledPart("entities", input.Entities),
	)
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func labeledPart(label string, values []string) string {
	joined := strings.TrimSpace(strings.Join(values, " "))
	if joined == "" {
		return ""
	}
	return label + ": " + joined
}

func compactUniqueParts(values ...string) []string {
	seen := map[string]struct{}{}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		part := strings.TrimSpace(value)
		if part == "" {
			continue
		}
		key := strings.Join(strings.Fields(part), " ")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, part)
	}
	return parts
}
