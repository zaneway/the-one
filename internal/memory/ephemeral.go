package memory

import "strings"

const EphemeralMemoryIDPrefix = "rawevt:"

// IsEphemeralMemoryID 判断 memory_id 是否为检索 fallback 产生的临时 ID（非 memory_item）。
func IsEphemeralMemoryID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), EphemeralMemoryIDPrefix)
}

// FilterPersistentMemoryIDs 过滤掉临时 memory_id，保留可关联 memory_item 的 ID。
func FilterPersistentMemoryIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || IsEphemeralMemoryID(id) {
			continue
		}
		out = append(out, id)
	}
	return out
}
