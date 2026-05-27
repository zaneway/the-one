package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/zaneway/theone/internal/config"
)

// New 创建结构化 logger。
// 设计约束：日志优先同时输出到 stderr 和日志文件；若默认 home 目录在受限环境中不可写，则自动降级为仅 stderr，避免测试和只读沙箱无法启动。
func New(cfg config.LoggingConfig) (*slog.Logger, io.Closer, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, nil, err
	}
	writer := io.Writer(os.Stderr)
	var closer io.Closer
	file, err := openLogFile(cfg.Path)
	if err != nil {
		if !shouldFallbackToStderrOnly(cfg.Path, err) {
			return nil, nil, err
		}
	} else {
		writer = io.MultiWriter(os.Stderr, file)
		closer = file
	}
	options := &slog.HandlerOptions{Level: level}
	switch cfg.Format {
	case "", "text":
		return slog.New(slog.NewTextHandler(writer, options)), closer, nil
	case "json":
		return slog.New(slog.NewJSONHandler(writer, options)), closer, nil
	default:
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, fmt.Errorf("CONFIG_INVALID: unsupported log format %q", cfg.Format)
	}
}

func openLogFile(path string) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("CONFIG_INVALID: logging.path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("CONFIG_INVALID: create log dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("CONFIG_INVALID: open log file: %w", err)
	}
	return file, nil
}

func shouldFallbackToStderrOnly(path string, err error) bool {
	home, homeErr := os.UserHomeDir()
	if homeErr != nil || home == "" {
		return false
	}
	homePrefix := home + string(os.PathSeparator)
	if path != home && !strings.HasPrefix(path, homePrefix) {
		return false
	}
	lowerError := strings.ToLower(err.Error())
	return os.IsPermission(err) || strings.Contains(lowerError, "operation not permitted") || strings.Contains(lowerError, "permission denied")
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
