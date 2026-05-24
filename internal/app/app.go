package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/zaneway/the-one/internal/automation"
	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/codeindex"
	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/diagnostics"
	"github.com/zaneway/the-one/internal/logging"
	"github.com/zaneway/the-one/internal/mcp"
	"github.com/zaneway/the-one/internal/mcp/tools"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/processor"
	"github.com/zaneway/the-one/internal/retrieval"
	"github.com/zaneway/the-one/internal/storage/sqlite"
)

// App 组装 memoryd 的 P0 运行时依赖。
//
// P0 只负责配置、日志、SQLite、migration 和 health/status 工具注册；
// 记忆写入、检索和 review 业务会在 P1 基于同一 Registry 扩展。
type App struct {
	cfg      config.Config
	version  string
	logger   *slog.Logger
	store    *sqlite.Store
	registry *mcp.Registry
	worker   *automation.Worker
}

// New 初始化 memoryd 运行时。
// 初始化顺序：日志 -> SQLite（WAL + migration + 能力探测）-> MCP Registry -> 诊断工具 -> 记忆服务 -> 捕获服务 -> 自动化服务 -> Worker。
// 设计约束：migration 失败时不会启动 MCP 工具，避免半初始化状态对外可见。
func New(ctx context.Context, cfg config.Config, version string) (*App, error) {
	// Step 1: 初始化日志（slog + 可选文件输出）
	logger, err := logging.New(cfg.Logging)
	if err != nil {
		return nil, err
	}
	// Step 2: 打开 SQLite（WAL + PRAGMA + migration + FTS5 能力探测）
	store, err := sqlite.Open(ctx, cfg.Storage, logger)
	if err != nil {
		logger.Error("sqlite open failed", "db_path", cfg.Storage.Path, "error", err)
		return nil, err
	}
	// Step 3: 创建 MCP 工具注册中心，所有工具通过 registry.Register 注册
	registry := mcp.NewRegistry(logger)
	// Step 4: 注册 P0 诊断工具（memory.health / memory.status）
	diagnosticService := diagnostics.NewService(version, cfg, store)
	diagnostics.RegisterTools(registry, diagnosticService)
	// Step 5: 注册 P1/P4 记忆工具（remember/review 保持 P1，search/context 通过 P4-C1 Orchestrator 写 trace/access log）
	retrievalOrchestrator := retrieval.NewMemoryOrchestrator(cfg, store,
		retrieval.WithTraceRepository(store),
		retrieval.WithAccessLogRepository(store),
		retrieval.WithRelationRepository(store),
		retrieval.WithCodeRefRepository(store),
		retrieval.WithCodeIndexAdapter(codeindex.NewLocalBasicAdapter(cfg.CodeIndex, "")),
		retrieval.WithDocSnapshotRepository(store),
		retrieval.WithLogger(logger),
	)
	memoryService := memory.NewService(cfg, store, memory.WithRetrievalOrchestrator(retrievalOrchestrator))
	tools.RegisterMemoryTools(registry, memoryService, logger)
	// Step 6: 注册 P3 自动化工具（memory.jobs.* / memory.candidates.* / memory.automation.status）
	// automationService 依赖 store 和 rule-based provider（从事件中提取证据的规则引擎）
	automationService := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	tools.RegisterAutomationTools(registry, automationService, logger)
	// Step 7: 注册 P2 捕获工具（memory.observe）
	// captureService 持有 automationService 引用，raw_event 写入后可触发 P3 入队
	captureService := capture.NewServiceWithAutomation(cfg, store, automationService)
	tools.RegisterCaptureTools(registry, captureService, logger)
	// Step 8: 创建异步 Worker（后台 goroutine，轮询 pending jobs 并执行）
	worker := automation.NewWorker(automationService, store, automation.WorkerConfig{
		PollIntervalMS:   cfg.Automation.PollIntervalMS,
		BatchSize:        cfg.Automation.BatchSize,
		RetryBaseDelayMS: cfg.Automation.RetryBaseDelayMS,
		RunningTimeoutMS: cfg.Automation.RunningTimeoutMS,
	})
	logger.Info("memoryd initialized",
		"version", version,
		"db_path", cfg.Storage.Path,
		"mcp_addr", cfg.Server.MCPAddr,
	)
	return &App{
		cfg:      cfg,
		version:  version,
		logger:   logger,
		store:    store,
		registry: registry,
		worker:   worker,
	}, nil
}

// Serve 启动 MCP stdio 服务。
// 处理流程：校验传输类型 -> 可选启动自动化 Worker（后台 goroutine）-> 启动 stdio 服务器。
// 设计说明：Worker 在独立 goroutine 中运行，通过 context 取消实现优雅关闭。
func (a *App) Serve(ctx context.Context) error {
	// 当前只支持 stdio 传输，其他地址直接拒绝
	if a.cfg.Server.MCPAddr != "stdio" {
		return fmt.Errorf("unsupported mcp addr %q", a.cfg.Server.MCPAddr)
	}
	// 可选启动自动化 Worker：独立 goroutine 运行，通过 context 取消实现优雅关闭
	if a.cfg.Automation.WorkerEnabled && a.worker != nil {
		go func() {
			if err := a.worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				a.logger.Error("automation worker stopped", "error", err)
			}
		}()
	}
	// 启动 MCP stdio 服务器：按行读取 JSON 请求，返回 JSON 响应
	server := mcp.NewStdioServer(a.registry, a.logger)
	return server.Serve(ctx)
}

// CallTool 允许本地 CLI 和测试直接复用 MCP 工具实现，避免 health/status 形成两套逻辑。
func (a *App) CallTool(ctx context.Context, name string, params any) (any, *mcp.Error) {
	return a.registry.Call(ctx, name, params)
}

// Close 释放运行时资源。
func (a *App) Close() error {
	if a.store == nil {
		return nil
	}
	return a.store.Close()
}
