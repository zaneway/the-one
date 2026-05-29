package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// AtomicFingerprint 计算 capture.atomic 接入层去重指纹（§5.2）。
func AtomicFingerprint(eventType string, payload map[string]any) (string, error) {
	switch strings.TrimSpace(eventType) {
	case "tool.result.summary":
		tool := stringFromPayload(payload, "tool_name")
		if tool == "" {
			return "", fmt.Errorf("missing tool_name")
		}
		in := truncate(stringFromPayload(payload, "input_summary"), 200)
		out := truncate(stringFromPayload(payload, "output_summary"), 200)
		exit := atomicExitCode(payload)
		raw := strings.Join([]string{tool, in, out, strconv.Itoa(exit)}, "|")
		return hashHex(raw), nil
	case "file.edit.summary":
		path := stringFromPayload(payload, "file_path")
		if path == "" {
			return "", fmt.Errorf("missing file_path")
		}
		change := stringFromPayload(payload, "change_type")
		after := stringFromPayload(payload, "after_hash")
		if after == "" {
			after = truncate(stringFromPayload(payload, "content_summary"), 200)
		}
		raw := strings.Join([]string{path, change, after}, "|")
		return hashHex(raw), nil
	default:
		raw := strings.TrimSpace(eventType) + "|" + stringFromPayload(payload, "content_summary")
		return hashHex(raw), nil
	}
}

func atomicExitCode(payload map[string]any) int {
	v, ok := payload["exit_code"]
	if !ok {
		v = payload["exitCode"]
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func hashHex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}
