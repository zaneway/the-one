package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/zaneway/theone/internal/adapter"
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
//	search - 从 stdin 读取 JSON 并调用 memory.search（用于诊断和候选检索）
//	context - 从 stdin 读取 JSON 并调用 memory.context（用于回答前注入上下文）
//	observe - 从 stdin 读取 JSON 并调用 memory.observe（供 Hook/脚本本地写入入口复用）
//	observe-turn - 从 stdin 读取 Turn payload，聚合后批量写入 memory.observe
//	observe-envelope - 从 stdin 读取 IngestEnvelope，面向 wrapper/log collector 接入
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
	case "observe":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		params, err := decodeJSONParams(os.Stdin)
		if err != nil {
			return fmt.Errorf("decode observe params: %w", err)
		}
		return callLocalTool(context.Background(), cfg, "memory.observe", params)
	case "search":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		params, err := decodeJSONParams(os.Stdin)
		if err != nil {
			return fmt.Errorf("decode search params: %w", err)
		}
		return callLocalTool(context.Background(), cfg, "memory.search", params)
	case "context":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		params, err := decodeJSONParams(os.Stdin)
		if err != nil {
			return fmt.Errorf("decode context params: %w", err)
		}
		return callLocalTool(context.Background(), cfg, "memory.context", params)
	case "observe-turn":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		payload, err := decodeTurnPayload(os.Stdin)
		if err != nil {
			return fmt.Errorf("decode observe-turn payload: %w", err)
		}
		stateStore := adapter.NewFileStateStore(runtimeStateDir(cfg))
		runtime := adapter.NewTurnRuntime(stateStore)
		requests, err := runtime.BuildObserveRequests(payload)
		if err != nil {
			return err
		}
		anyRequests := make([]any, 0, len(requests))
		for _, req := range requests {
			anyRequests = append(anyRequests, req)
		}
		return callLocalObserveBatch(context.Background(), cfg, anyRequests, payload.SessionID, payload.TaskID)
	case "observe-envelope":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		envelope, err := decodeIngestEnvelope(os.Stdin)
		if err != nil {
			return fmt.Errorf("decode observe-envelope payload: %w", err)
		}
		if err := adapter.ValidateIngestEnvelope(envelope); err != nil {
			return err
		}
		payload, err := adapter.TurnPayloadFromEnvelope(envelope)
		if err != nil {
			return err
		}
		stateStore := adapter.NewFileStateStore(runtimeStateDir(cfg))
		runtime := adapter.NewTurnRuntime(stateStore)
		requests, err := runtime.BuildObserveRequests(payload)
		if err != nil {
			return err
		}
		anyRequests := make([]any, 0, len(requests))
		for _, req := range requests {
			anyRequests = append(anyRequests, req)
		}
		return callLocalObserveBatch(context.Background(), cfg, anyRequests, envelope.SessionID, payload.TaskID)
	default:
		return fmt.Errorf("unknown command %q: expected serve, health, status, search, context, observe, observe-turn, or observe-envelope", args[0])
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

func decodeJSONParams(input io.Reader) (map[string]any, error) {
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	var params map[string]any
	if err := decoder.Decode(&params); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("stdin is empty, expected JSON object")
		}
		return nil, err
	}
	if len(params) == 0 {
		return nil, fmt.Errorf("params object is empty")
	}
	return params, nil
}

func decodeTurnPayload(input io.Reader) (adapter.TurnPayload, error) {
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	var payload adapter.TurnPayload
	if err := decoder.Decode(&payload); err != nil {
		if err == io.EOF {
			return adapter.TurnPayload{}, fmt.Errorf("stdin is empty, expected JSON object")
		}
		return adapter.TurnPayload{}, err
	}
	return payload, nil
}

func decodeIngestEnvelope(input io.Reader) (adapter.IngestEnvelope, error) {
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	var envelope adapter.IngestEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		if err == io.EOF {
			return adapter.IngestEnvelope{}, fmt.Errorf("stdin is empty, expected JSON object")
		}
		return adapter.IngestEnvelope{}, err
	}
	return envelope, nil
}

func runtimeStateDir(cfg config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Storage.Path), "runtime-state")
}

func callLocalObserveBatch(ctx context.Context, cfg config.Config, requests []any, sessionID, taskID string) error {
	runtime, err := app.New(ctx, cfg, version)
	if err != nil {
		return err
	}
	defer runtime.Close()

	results := make([]any, 0, len(requests))
	failures := make([]adapter.FailureRecord, 0, 1)
	queue := adapter.NewFailureQueue(runtimeStateDir(cfg))
	for _, req := range requests {
		result, toolErr := runtime.CallTool(ctx, "memory.observe", req)
		if toolErr != nil {
			record := adapter.FailureRecord{
				ErrorCode:    toolErr.ErrorCode,
				ErrorSummary: toolErr.Message,
				SessionID:    sessionID,
				TaskID:       taskID,
			}
			failures = append(failures, record)
			_ = queue.Append(record)
			continue
		}
		results = append(results, result)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"count":         len(results),
		"results":       results,
		"failure_count": len(failures),
		"failures":      failures,
	})
}
