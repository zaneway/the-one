package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New 生成带业务前缀的随机 ID。P1 使用随机 ID 避免本地多工具并发写入时出现时间序列冲突。
func New(prefix string) (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}
