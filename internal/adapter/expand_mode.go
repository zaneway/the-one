package adapter

import (
	"os"
	"strings"

	"github.com/zaneway/theone/internal/config"
)

// NormalizeExpandMode 归一化展开模式。
func NormalizeExpandMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ExpandModeV2, "2":
		return ExpandModeV2
	default:
		return ExpandModeLegacy
	}
}

// IsExpandModeV2 是否为 v2 展开模式。
func IsExpandModeV2(mode string) bool {
	return NormalizeExpandMode(mode) == ExpandModeV2
}

// ResolveExpandMode 环境变量优先于配置文件。
func ResolveExpandMode(cfg config.Config) string {
	if v := strings.TrimSpace(os.Getenv("THEONE_EXPAND_MODE")); v != "" {
		return NormalizeExpandMode(v)
	}
	return NormalizeExpandMode(cfg.Adapter.ExpandMode)
}
