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
	"time"

	"github.com/zaneway/theone/internal/adapter"
	"github.com/zaneway/theone/internal/app"
	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
)

// version 是 theone 的实现版本，会通过 MCP initialize 响应回传给 Agent。
// dev 分支上的版本号以 -dev 结尾，便于下游在做兼容性检查时识别"非稳定版本"。
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
//	observe-envelope - 从 stdin 读取 IngestEnvelope，= ingest 别名
//	ingest         - 统一入站（单条/批量包络，kind 路由，P0）
//	prefetch-context - 读路径：SessionBinder + BindTaskFromPrompt + memory.context（P3）
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
		runtime := adapter.NewTurnRuntimeWithExpandMode(stateStore, adapter.ResolveExpandMode(cfg))
		requests, err := runtime.BuildObserveRequests(payload)
		if err != nil {
			return err
		}
		anyRequests := make([]any, 0, len(requests))
		for _, req := range requests {
			anyRequests = append(anyRequests, req)
		}
		return callLocalObserveBatch(context.Background(), cfg, anyRequests, payload.SessionID, payload.TaskID)
	case "observe-envelope", "ingest":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		return runIngest(context.Background(), cfg)
	case "prefetch-context":
		cfg, err := parseConfig(args[1:], false)
		if err != nil {
			return err
		}
		return runPrefetchContext(context.Background(), cfg)
	default:
		return fmt.Errorf("unknown command %q: expected serve, health, status, search, context, observe, observe-turn, observe-envelope, ingest, or prefetch-context", args[0])
	}
}

// parseConfig 解析子命令的 CLI flag 并装载最终配置。
// 入参：args（子命令之后剩余的 flag 列表）、includeStatusFlag（是否为 status 子命令注册 include-config）。
// 返回：合并后的 config.Config；任意 flag 解析失败时返回错误。
// 设计约束：flag 输出写入 stderr，避免污染子命令的 stdout 协议流。
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

// statusIncludeConfig 为 status 子命令单独解析 --include-config flag。
// 与 parseConfig 不同：这里只关心 include-config 布尔值，不重新走完整配置装载流程，
// 避免在 status 子命令里重复打开 SQLite。
// 设计约束：解析失败一律返回 false，由上层在不附带 config 的情况下输出默认 status 响应。
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

// writePIDFile 把 theone 进程 PID 写入 ~/.theone/theone.pid。
// 入参：pid（当前进程 PID）。
// 返回：写入过程中任意步骤的错误，由 tryWritePIDFile 转为 warn 日志。
// 设计约束：父目录自动创建，文件权限 0o644。
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

// pidFilePath 拼出 PID 文件的绝对路径：$HOME/.theone/theone.pid。
// 解析 home 失败时把错误向上抛，由调用方决定是否降级为 warn。
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

// decodeJSONParams 从 stdin 读取一个 JSON 对象并返回 map[string]any。
// 使用 UseNumber 避免浮点精度丢失；空输入返回明确错误，区分 EOF 与解码失败。
// 入参：input 任意 io.Reader（测试时常用 strings.Reader 注入）。
// 返回：解析后的参数 map；输入不合法时返回错误。
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

// decodeTurnPayload 从 stdin 读取 observe-turn 所需的 TurnPayload JSON。
// 设计与 decodeJSONParams 一致，但解码目标为强类型 TurnPayload，便于 TurnRuntime 直接处理。
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

// decodeIngestEnvelope 从 stdin 读取 ingest 控制面的 IngestEnvelope。
// 留作 future 兼容入口；当前 runIngest 直接走 adapter.DecodeIngestInput 支持单条/批量。
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

// runtimeStateDir 解析运行时状态目录：<db-dir>/runtime-state。
// 用于 FileStateStore、IngestLedger、AtomicDedup、FailureQueue 等本地文件型组件。
func runtimeStateDir(cfg config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Storage.Path), "runtime-state")
}

func runPrefetchContext(ctx context.Context, cfg config.Config) error {
	runtime, err := app.New(ctx, cfg, version)
	if err != nil {
		return err
	}
	defer runtime.Close()

	var req adapter.PrefetchRequest
	decoder := json.NewDecoder(os.Stdin)
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		if err == io.EOF {
			return fmt.Errorf("stdin is empty, expected prefetch JSON object")
		}
		return fmt.Errorf("decode prefetch request: %w", err)
	}
	stateDir := runtimeStateDir(cfg)
	_ = adapter.EnsureP1Migration(stateDir)
	timeout := time.Duration(cfg.Adapter.PrefetchTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	maxInject := cfg.Adapter.MaxInjectChars
	if maxInject <= 0 {
		maxInject = 4000
	}
	repoRoot, _ := os.Getwd()
	processor := &adapter.PrefetchProcessor{
		Binder:    adapter.NewSessionBinder(stateDir),
		StateDir:  stateDir,
		RepoRoot:  repoRoot,
		Timeout:   timeout,
		MaxInject: maxInject,
		Context: func(ctx context.Context, ctxReq memory.ContextRequest) (memory.ContextResponse, error) {
			result, toolErr := runtime.CallTool(ctx, "memory.context", ctxReq)
			if toolErr != nil {
				return memory.ContextResponse{}, fmt.Errorf("%s: %s", toolErr.ErrorCode, toolErr.Message)
			}
			raw, err := json.Marshal(result)
			if err != nil {
				return memory.ContextResponse{}, err
			}
			var resp memory.ContextResponse
			if err := json.Unmarshal(raw, &resp); err != nil {
				return memory.ContextResponse{}, err
			}
			return resp, nil
		},
		Observe: observeFromRuntime(runtime),
	}
	if req.MaxInjectChars <= 0 {
		req.MaxInjectChars = maxInject
	}
	out := processor.Run(ctx, req)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

// observeFromRuntime 把 runtime.CallTool("memory.observe", ...) 包装成 adapter.ObserveFunc。
// 在 tool 错误时返回 error，并把 SDK 错误码/消息组合成可读错误；序列化错误透传。
// 设计约束：返回 ObserveResponse 之前会做 json marshal+unmarshal 一次往返，
// 保证 ingest processor 收到的字段值与 capture service 内部完全一致。
func observeFromRuntime(runtime *app.App) adapter.ObserveFunc {
	return func(ctx context.Context, req capture.ObserveRequest) (capture.ObserveResponse, error) {
		result, toolErr := runtime.CallTool(ctx, "memory.observe", req)
		if toolErr != nil {
			return capture.ObserveResponse{}, fmt.Errorf("%s: %s", toolErr.ErrorCode, toolErr.Message)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return capture.ObserveResponse{}, err
		}
		var resp capture.ObserveResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return capture.ObserveResponse{}, err
		}
		return resp, nil
	}
}

// runIngest 是 theone ingest 控制面的实现。
// 入参：ctx、cfg（已合并的运行时配置）。
// 处理流程：
//  1. 创建本地运行时（独立 app.App，避免污染 serve 进程）；
//  2. 从 stdin 解码 IngestInput（单条或批量包络）；
//  3. 拼装 IngestProcessor（包含 dedup、ledger、failure queue 等本地组件）；
//  4. 调用 Process 把结果 JSON 写入 stdout。
//
// 设计约束：stdin/stdout 仅承载协议流，错误信息走 stderr，避免污染下游解析。
func runIngest(ctx context.Context, cfg config.Config) error {
	runtime, err := app.New(ctx, cfg, version)
	if err != nil {
		return err
	}
	defer runtime.Close()

	items, ingestID, err := adapter.DecodeIngestInput(os.Stdin)
	if err != nil {
		return fmt.Errorf("decode ingest: %w", err)
	}
	stateDir := runtimeStateDir(cfg)
	_ = adapter.EnsureP1Migration(stateDir)
	expandMode := adapter.ResolveExpandMode(cfg)
	processor := &adapter.IngestProcessor{
		Binder:          adapter.NewSessionBinder(stateDir),
		Ledger:          adapter.NewIngestLedger(stateDir),
		Failures:        adapter.NewFailureQueue(stateDir),
		StateStore:      adapter.NewFileStateStore(stateDir),
		AtomicDedup:     adapter.NewAtomicDedupStore(stateDir),
		ExpandMode:      expandMode,
		AtomicStripTurn: cfg.Adapter.AtomicStripTurnFields,
		Observe:         observeFromRuntime(runtime),
		EnsureSession:   runtime.EnsureCaptureSession,
	}
	out := processor.Process(ctx, ingestID, items)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

// callLocalObserveBatch 把 TurnRuntime 展开后的 ObserveRequest 列表逐条写到 capture service。
// 用于 observe-turn 子命令，绕过 ingest 平面直接走 memory.observe 工具。
// 设计约束：每条失败都单独入 FailureQueue，便于后续重试或人工排查；最终 JSON 含 results 与 failures。
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
