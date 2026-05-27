package mcp

import (
	"encoding/json"
)

// RawObjectSchema 是 MCP tool 输入的最小合法 JSON Schema。
func RawObjectSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":true}`)
}

func BoolPtr(v bool) *bool {
	return &v
}

func StringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func IntegerProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func NumberProp(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func BooleanProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func StringArrayProp(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}

func ObjectProp(description string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": true, "description": description}
}

func ObjectArrayProp(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": true}, "description": description}
}

func EnumStringProp(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}

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
