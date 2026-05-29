package adapter

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// NormalizePrompt 归一化用户 prompt（与 Hook runtime-lib 一致）。
func NormalizePrompt(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

// PromptFingerprint 返回 sha1(normalized)[:16]。
func PromptFingerprint(text string) string {
	normalized := NormalizePrompt(text)
	if normalized == "" {
		return ""
	}
	sum := sha1.Sum([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

// TaskIDFromPrompt 由首条用户 prompt 生成稳定 task_id（§6.2.2）。
func TaskIDFromPrompt(text string) string {
	fp := PromptFingerprint(text)
	if fp == "" {
		return ""
	}
	return "task_" + fp
}
