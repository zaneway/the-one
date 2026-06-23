package adapter

import (
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/memory"
)

func TestFormatInjectMarkdownDeduplicatesStructuralAliases(t *testing.T) {
	tests := []struct {
		name      string
		pack      memory.ContextPack
		content   string
		wantCount int
	}{
		{
			name: "summary aliases memory",
			pack: memory.ContextPack{
				Summary: "唯一摘要记忆正文",
				Memories: []memory.ContextMemory{
					{MemoryID: "mem-summary", Type: memory.TypeDecision, Compressed: "唯一摘要记忆正文"},
				},
			},
			content:   "唯一摘要记忆正文",
			wantCount: 1,
		},
		{
			name: "constraint aliases memory",
			pack: memory.ContextPack{
				Constraints: []string{"唯一约束记忆正文"},
				Memories: []memory.ContextMemory{
					{MemoryID: "mem-constraint", Type: memory.TypeConstraint, Compressed: "唯一约束记忆正文"},
				},
			},
			content:   "唯一约束记忆正文",
			wantCount: 1,
		},
		{
			name: "distinct summary remains",
			pack: memory.ContextPack{
				Summary: "独立摘要正文",
				Memories: []memory.ContextMemory{
					{MemoryID: "mem-other", Type: memory.TypeDecision, Compressed: "另一条记忆正文"},
				},
			},
			content:   "独立摘要正文",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			markdown := FormatInjectMarkdown(memory.ContextResponse{ContextPack: tt.pack}, 4000)
			if got := strings.Count(markdown, tt.content); got != tt.wantCount {
				t.Fatalf("content %q count = %d, want %d\nmarkdown:\n%s", tt.content, got, tt.wantCount, markdown)
			}
		})
	}
}
