package mcp

import "strings"

// MCPToolName 将内部 canonical 名（memory.health）转为对 Cursor 等严格 Host 友好的暴露名（memory_health）。
// MCP 规范允许点号，但 Cursor 当前只接受字母、数字和下划线，并可能过滤带点号的工具。
func MCPToolName(canonical string) string {
	return strings.ReplaceAll(canonical, ".", "_")
}
