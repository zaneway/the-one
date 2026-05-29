package adapter

import "strings"

// HookProducer 生成 Driver producer 前缀（ingest / prefetch 审计用）。
func HookProducer(agentType, hookName string) string {
	at := strings.TrimSpace(agentType)
	if at == "" {
		at = "cursor"
	}
	hn := strings.TrimSpace(hookName)
	if hn == "" {
		hn = "hook"
	}
	return at + "_hook:" + hn
}
