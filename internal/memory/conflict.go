package memory

import "strings"

// LikelyConflict 判断候选记忆与同 scope/type 的已有 stable 记忆是否可能存在内容冲突。
// 用于 Admission 与无 target 的用户纠正归档。
func LikelyConflict(candidateType, candidateScope, candidateContent string, item MemoryItem) bool {
	if item.MemoryType != candidateType || item.Scope != candidateScope {
		return false
	}
	text := strings.ToLower(candidateContent + " " + item.Content)
	return strings.Contains(text, "改为") ||
		strings.Contains(text, "不再") ||
		strings.Contains(text, "instead") ||
		strings.Contains(text, "deprecated")
}
