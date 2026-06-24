package adapter

import (
	"fmt"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

// FormatInjectMarkdown 将 memory.context 响应格式化为 Cursor 注入 Markdown。
func FormatInjectMarkdown(resp memory.ContextResponse, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 4000
	}
	pack := resp.ContextPack
	if !hasContextContent(pack) {
		return ""
	}

	lines := []string{
		"# theone 记忆上下文（系统自动注入）",
		"",
		"以下内容由 `theone` 根据当前任务召回，供回答参考；其中标记「未确认」的条目不可当作强约束。",
		"",
	}
	constraintContents := normalizedConstraintContents(pack.Constraints)
	memoryContents := normalizedMemoryContents(pack.Memories)
	if s := strings.TrimSpace(pack.Summary); s != "" && !containsNormalizedContent(constraintContents, s) && !containsNormalizedContent(memoryContents, s) {
		lines = append(lines, s, "")
	}
	if len(pack.Constraints) > 0 {
		lines = append(lines, "## 约束")
		for _, item := range pack.Constraints {
			if t := strings.TrimSpace(item); t != "" {
				lines = append(lines, "- "+t)
			}
		}
		lines = append(lines, "")
	}
	if len(pack.Memories) > 0 {
		memoryLines := make([]string, 0, len(pack.Memories))
		for _, item := range pack.Memories {
			content := strings.TrimSpace(item.Compressed)
			if content == "" || containsNormalizedContent(constraintContents, content) {
				continue
			}
			flags := make([]string, 0, 3)
			if item.Unconfirmed {
				flags = append(flags, "未确认")
			}
			if item.SessionOnly {
				flags = append(flags, "会话级")
			}
			if item.Historical {
				flags = append(flags, "历史")
			}
			suffix := ""
			if len(flags) > 0 {
				suffix = " (" + strings.Join(flags, ", ") + ")"
			}
			idPart := ""
			if id := strings.TrimSpace(item.MemoryID); id != "" {
				idPart = " `" + id + "`"
			}
			memoryLines = append(memoryLines, fmt.Sprintf("- [%s] %s%s%s", item.Type, content, idPart, suffix))
		}
		if len(memoryLines) > 0 {
			lines = append(lines, "## 相关记忆")
			lines = append(lines, memoryLines...)
			lines = append(lines, "")
		}
	}
	if len(pack.CodeRefs) > 0 {
		lines = append(lines, "## 代码引用")
		for i, item := range pack.CodeRefs {
			if i >= 8 {
				break
			}
			fp := strings.TrimSpace(item.FilePath)
			rs := strings.TrimSpace(item.RefSummary)
			if fp == "" && rs == "" {
				continue
			}
			symbol := strings.TrimSpace(item.Symbol)
			line := fmt.Sprintf("- `%s`", fp)
			if symbol != "" {
				line += " `" + symbol + "`"
			}
			if rs != "" {
				line += " — " + rs
			}
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(text) > maxChars {
		text = strings.TrimSpace(text[:max(0, maxChars-24)]) + "\n\n…（记忆上下文已截断）"
	}
	return text
}

func normalizedConstraintContents(constraints []string) map[string]struct{} {
	contents := make(map[string]struct{}, len(constraints))
	for _, constraint := range constraints {
		if content := strings.TrimSpace(constraint); content != "" {
			contents[content] = struct{}{}
		}
	}
	return contents
}

func normalizedMemoryContents(memories []memory.ContextMemory) map[string]struct{} {
	contents := make(map[string]struct{}, len(memories))
	for _, item := range memories {
		if content := strings.TrimSpace(item.Compressed); content != "" {
			contents[content] = struct{}{}
		}
	}
	return contents
}

func containsNormalizedContent(contents map[string]struct{}, content string) bool {
	_, ok := contents[strings.TrimSpace(content)]
	return ok
}

// ClaudeSurfaceContent 生成 Claude Code 侧车文件正文（P4，无 Cursor frontmatter）。
func ClaudeSurfaceContent(body string) string {
	stamp := time.Now().Format(time.RFC3339Nano)
	return strings.TrimSpace(fmt.Sprintf(`# theone 记忆上下文（Claude Code 自动注入）

<!-- theone-context updated_at=%s -->

%s
`, stamp, strings.TrimSpace(body)))
}

// RuleFileContent 生成 theone-injected-context.mdc 正文。
func RuleFileContent(body string, alwaysApply bool) string {
	stamp := time.Now().Format(time.RFC3339Nano)
	flag := "false"
	if alwaysApply {
		flag = "true"
	}
	return fmt.Sprintf(`---
description: theone 本轮记忆上下文（beforeSubmitPrompt 自动刷新，勿手工编辑）
alwaysApply: %s
---

<!-- theone-injected-context updated_at=%s -->

%s
`, flag, stamp, strings.TrimSpace(body))
}

func hasContextContent(pack memory.ContextPack) bool {
	if strings.TrimSpace(pack.Summary) != "" {
		return true
	}
	if len(pack.Constraints) > 0 || len(pack.Memories) > 0 || len(pack.CodeRefs) > 0 {
		return true
	}
	if pack.ReviewStrategy != nil {
		return true
	}
	return false
}
