package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/zaneway/theone/internal/app"
	"github.com/zaneway/theone/internal/config"
)

const version = "v0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "theone: %v\n", err)
		os.Exit(1)
	}
}

// run 是 theone 的命令分发入口。
// 子命令职责：
//
//	serve  - 解析配置、初始化运行时依赖（SQLite + migration + MCP Registry + Worker）、启动 MCP stdio 服务
//	health - 复用运行时调用 memory.health 工具，验证存储层可用性
//	status - 复用运行时调用 memory.status 工具，返回 capability 和配置摘要
func run(args []string) error {
	if len(args) == 0 {
		args = []string{"serve"}
	}

	switch args[0] {
	case "serve":
		//解析配置：按 默认值 -> 配置文件 -> 环境变量 -> CLI flag 优先级合并
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		// 注册 SIGINT/SIGTERM 信号，确保优雅关闭 SQLite 连接和 Worker
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		//加载依赖：初始化 SQLite（WAL + migration + 能力探测）、注册 MCP 工具、创建 Worker
		runtime, err := app.New(ctx, cfg, version)
		if err != nil {
			return err
		}
		defer runtime.Close()
		tryWritePIDFile(os.Getpid())
		//启动 MCP：当前只支持 stdio 传输，通过 stdin/stdout 与 Agent 通信
		return runtime.Serve(ctx)
	case "health":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		return callLocalTool(context.Background(), cfg, "memory.health", map[string]any{})
	case "status":
		cfg, err := parseConfig(args[1:], true)
		if err != nil {
			return err
		}
		includeConfig := statusIncludeConfig(args[1:])
		return callLocalTool(context.Background(), cfg, "memory.status", map[string]any{
			"include_config": includeConfig,
		})
	default:
		return fmt.Errorf("unknown command %q: expected serve, health, or status", args[0])
	}
}

func parseConfig(args []string, includeStatusFlag bool) (config.Config, error) {
	fs := flag.NewFlagSet("theone", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var overrides config.Overrides
	fs.StringVar(&overrides.ConfigPath, "config", "", "config file path")
	fs.StringVar(&overrides.DataDir, "data-dir", "", "data directory")
	fs.StringVar(&overrides.DBPath, "db-path", "", "SQLite database path")
	fs.StringVar(&overrides.MCPAddr, "mcp-addr", "", "MCP address, currently stdio")
	fs.StringVar(&overrides.LogLevel, "log-level", "", "log level: debug, info, warn, error")
	if includeStatusFlag {
		fs.Bool("include-config", false, "include non-sensitive config summary")
	}
	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}
	return config.Load(overrides)
}

func statusIncludeConfig(args []string) bool {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.String("config", "", "")
	fs.String("data-dir", "", "")
	fs.String("db-path", "", "")
	fs.String("mcp-addr", "", "")
	fs.String("log-level", "", "")
	includeConfig := fs.Bool("include-config", false, "")
	if err := fs.Parse(args); err != nil {
		return false
	}
	return *includeConfig
}

func tryWritePIDFile(pid int) {
	// Cursor MCP 进程经常运行在受限环境（例如禁止写 $HOME），此时 PID 文件并非必需。
	// 通过环境变量允许显式关闭 PID 写入，避免启动直接失败或产生噪声告警。
	if os.Getenv("THEONE_DISABLE_PID") == "1" {
		return
	}
	if err := writePIDFile(pid); err != nil {
		slog.Warn("write pid file failed", "error", err)
	}
}

func writePIDFile(pid int) error {
	path, err := pidFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write pid file: create dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

func pidFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve pid file home dir: %w", err)
	}
	return filepath.Join(home, ".theone", "theone.pid"), nil
}

// callLocalTool 为 health/status 子命令提供本地工具调用能力。
// 创建临时运行时实例 -> 通过 MCP Registry 直接调用工具 -> JSON 格式化输出到 stdout。
// 避免 health/status 与 serve 形成两套逻辑。
func callLocalTool(ctx context.Context, cfg config.Config, tool string, params map[string]any) error {
	runtime, err := app.New(ctx, cfg, version)
	if err != nil {
		return err
	}
	defer runtime.Close()
	result, toolErr := runtime.CallTool(ctx, tool, params)
	if toolErr != nil {
		return toolErr
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		slog.Error("encode tool result failed", "tool", tool, "error", err)
		return err
	}
	return nil
}
