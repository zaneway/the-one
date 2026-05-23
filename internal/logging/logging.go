package logging

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/zaneway/the-one/internal/config"
)

// New 创建结构化 logger。日志默认输出到 stderr，避免污染 stdio 工具响应。
func New(cfg config.LoggingConfig) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	options := &slog.HandlerOptions{Level: level}
	switch cfg.Format {
	case "", "text":
		return slog.New(slog.NewTextHandler(os.Stderr, options)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stderr, options)), nil
	default:
		return nil, fmt.Errorf("CONFIG_INVALID: unsupported log format %q", cfg.Format)
	}
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("CONFIG_INVALID: unsupported log level %q", level)
	}
}
