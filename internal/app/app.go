package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/diagnostics"
	"github.com/zaneway/the-one/internal/logging"
	"github.com/zaneway/the-one/internal/mcp"
	"github.com/zaneway/the-one/internal/mcp/tools"
	"github.com/zaneway/the-one/internal/memory"
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
}

// New 初始化 memoryd 运行时。migration 失败时不会启动 MCP 工具，避免半初始化状态对外可见。
func New(ctx context.Context, cfg config.Config, version string) (*App, error) {
	logger, err := logging.New(cfg.Logging)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.Open(ctx, cfg.Storage, logger)
	if err != nil {
		logger.Error("sqlite open failed", "db_path", cfg.Storage.Path, "error", err)
		return nil, err
	}
	registry := mcp.NewRegistry(logger)
	diagnosticService := diagnostics.NewService(version, cfg, store)
	diagnostics.RegisterTools(registry, diagnosticService)
	memoryService := memory.NewService(cfg, store)
	tools.RegisterMemoryTools(registry, memoryService, logger)
	captureService := capture.NewService(cfg, store)
	tools.RegisterCaptureTools(registry, captureService, logger)
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
	}, nil
}

// Serve 启动 P0 MCP stdio 骨架。当前只支持 stdio，其他传输在协议适配明确后再扩展。
func (a *App) Serve(ctx context.Context) error {
	if a.cfg.Server.MCPAddr != "stdio" {
		return fmt.Errorf("unsupported mcp addr %q", a.cfg.Server.MCPAddr)
	}
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
