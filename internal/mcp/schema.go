package mcp

import (
	"encoding/json"
)

// RawObjectSchema 是 MCP tool 输入的最小合法 JSON Schema。
func RawObjectSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":true}`)
}

// BoolPtr 返回 bool 指针，常用于 ToolSpec 中可选的 ReadOnlyHint / DestructiveHint 等字段。
func BoolPtr(v bool) *bool {
	return &v
}

// StringProp 生成带 description 的字符串 JSON Schema 片段。
// 用法：放在 ToolSpec.InputSchema 的 properties 中。
func StringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// IntegerProp 生成带 description 的整数 JSON Schema 片段。
func IntegerProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

// NumberProp 生成带 description 的浮点 JSON Schema 片段。
func NumberProp(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

// BooleanProp 生成带 description 的布尔 JSON Schema 片段。
func BooleanProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

// StringArrayProp 生成带 description 的字符串数组 JSON Schema 片段。
func StringArrayProp(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}

// ObjectProp 生成带 description 的自由对象 JSON Schema 片段（additionalProperties=true）。
func ObjectProp(description string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": true, "description": description}
}

// ObjectArrayProp 生成带 description 的对象数组 JSON Schema 片段，元素允许任意字段。
func ObjectArrayProp(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}, "description": description}
}

// EnumStringProp 生成带 description 的字符串枚举 JSON Schema 片段。
// values 决定 enum 取值范围；客户端在 tools/list 中可看到候选值。
func EnumStringProp(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}

// ObjectSchema 把 properties 与 required 拼成完整的 JSON Schema object 并序列化为 RawMessage。
// additionalProperties=false 保证客户端漏传字段会被 SDK 拒绝，避免下游收到非法 payload。
// required 为空时省略该字段，由下游 handler 自己判空。
func ObjectSchema(required []string, properties map[string]any) json.RawMessage {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	return raw
}
