package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/codeindex"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/diagnostics"
	"github.com/zaneway/theone/internal/logging"
	"github.com/zaneway/theone/internal/mcp"
	"github.com/zaneway/theone/internal/mcp/tools"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/mvp"
	"github.com/zaneway/theone/internal/processor"
	"github.com/zaneway/theone/internal/retrieval"
	"github.com/zaneway/theone/internal/storage/sqlite"
)

// App 组装 theone 运行时依赖。
//
// 负责配置、日志、SQLite、migration 和诊断工具注册；
// 记忆写入、检索和 review 业务基于同一 Registry 扩展。
type App struct {
	cfg               config.Config
	version           string
	logger            *slog.Logger
	logCloser         io.Closer
	store             *sqlite.Store
	registry          *mcp.Registry
	worker            *automation.Worker
	automationService *automation.Service
}

// New 初始化 theone 运行时。
// 初始化顺序：日志 -> SQLite（WAL + migration + 能力探测）-> MCP Registry -> 诊断工具 -> 记忆服务 -> 捕获服务 -> 自动化服务 -> Worker。
// 设计约束：migration 失败时不会启动 MCP 工具，避免半初始化状态对外可见。
func New(ctx context.Context, cfg config.Config, version string) (*App, error) {
	// Step 1: 初始化日志（slog + stderr/file 双写）
	logger, logCloser, err := logging.New(cfg.Logging)
	if err != nil {
		return nil, err
	}
	// Step 2: 打开 SQLite（WAL + PRAGMA + migration + FTS5 能力探测）
	store, err := sqlite.Open(ctx, cfg.Storage, logger)
	if err != nil {
		logger.Error("sqlite open failed", "db_path", cfg.Storage.Path, "error", err)
		_ = logCloser.Close()
		return nil, err
	}
	// Step 3: 创建 MCP 工具注册中心，所有工具通过 registry.Register 注册
	registry := mcp.NewRegistry(logger)
	// Step 4: 初始化 processor provider。外部 AI provider 会复用于自动处理和 health 探测。
	provider, err := newProcessorProvider(cfg, logger)
	if err != nil {
		logger.Error("processor provider init failed", "provider", cfg.Processor.Provider, "error", err)
		_ = store.Close()
		_ = logCloser.Close()
		return nil, err
	}
	diagnosticOptions := make([]diagnostics.ServiceOption, 0, 1)
	if checker, ok := provider.(processor.HealthChecker); ok {
		diagnosticOptions = append(diagnosticOptions, diagnostics.WithAIHealthChecker(checker))
	}
	// Step 5: 注册诊断工具（memory.health / memory.status）
	diagnosticService := diagnostics.NewService(version, cfg, store, diagnosticOptions...)
	diagnostics.RegisterTools(registry, diagnosticService)
	// Step 6: 注册记忆工具（remember/review/search/context）
	var codeIndexAdapter retrieval.CodeIndexAdapter
	if cfg.CodeIndex.Provider != "none" {
		codeIndexAdapter = codeindex.NewLocalBasicAdapter(cfg.CodeIndex, "")
	}
	retrievalOrchestrator := retrieval.NewMemoryOrchestrator(cfg, store,
		retrieval.WithTraceRepository(store),
		retrieval.WithAccessLogRepository(store),
		retrieval.WithRelationRepository(store),
		retrieval.WithCodeRefRepository(store),
		retrieval.WithCodeIndexAdapter(codeIndexAdapter),
		retrieval.WithDocSnapshotRepository(store),
		retrieval.WithReviewCheckpointRepository(store),
		retrieval.WithRawEventRepository(store),
		retrieval.WithLogger(logger),
	)
	// Step 7: 注册 自动化服务（observe 入队、准入管道、remember 准入）
	automationService := automation.NewService(cfg, store, provider)
	memoryService := memory.NewService(cfg, store,
		memory.WithRetrievalOrchestrator(retrievalOrchestrator),
		memory.WithAccessFeedbackWriter(store),
		memory.WithRememberAdmissionDecider(automationService),
	)
	tools.RegisterMemoryTools(registry, memoryService, logger)
	tools.RegisterAutomationTools(registry, automationService, logger)
	// Step 8: 注册 MVP 验收模型工具（run.start / task.record）
	mvpService := mvp.NewService(store)
	tools.RegisterMVPTools(registry, mvpService, logger)
	// Step 9: 注册捕获工具（memory.observe）
	// captureService 持有 automationService 引用，raw_event 写入后可触发自动入队
	// 外部 AI 处理只在 processor/evidence/candidate 阶段执行，避免 raw_event 落库前被改写或拒绝。
	captureService := capture.NewServiceWithAutomation(cfg, store, automationService)
	tools.RegisterCaptureTools(registry, captureService, logger)
	// Step 10: 创建异步 Worker（后台 goroutine，轮询 pending jobs 并执行）
	worker := automation.NewWorker(automationService, store, automation.WorkerConfig{
		PollIntervalMS:   cfg.Automation.PollIntervalMS,
		BatchSize:        cfg.Automation.BatchSize,
		RetryBaseDelayMS: cfg.Automation.RetryBaseDelayMS,
		RunningTimeoutMS: cfg.Automation.RunningTimeoutMS,
	})
	logger.Info("theone initialized",
		"version", version,
		"db_path", cfg.Storage.Path,
		"mcp_addr", cfg.Server.MCPAddr,
	)
	return &App{
		cfg:               cfg,
		version:           version,
		logger:            logger,
		logCloser:         logCloser,
		store:             store,
		registry:          registry,
		worker:            worker,
		automationService: automationService,
	}, nil
}

// newProcessorProvider 根据 config.Processor.Provider 构造 processor.Provider 实现。
// 入参：cfg（已合并的运行时配置）、logger（provider 内部日志）。
// 返回：构造完成的 Provider 与错误。
// 当前支持：rule_based（默认本地规则）、openai（外部 OpenAI 兼容模型）。
// 错误语义：未知 provider 返回 CONFIG_INVALID 错误码；OpenAI 配置非法时透传 OpenAI provider 错误。
func newProcessorProvider(cfg config.Config, logger *slog.Logger) (processor.Provider, error) {
	switch cfg.Processor.Provider {
	case processor.RuleBasedProviderName:
		return processor.NewRuleBasedProvider(), nil
	case processor.OpenAIProviderName:
		apiKey := strings.TrimSpace(cfg.Processor.OpenAI.APIKey)
		provider, err := processor.NewOpenAIProvider(processor.OpenAIProviderConfig{
			APIKey:                   apiKey,
			BaseURL:                  cfg.Processor.OpenAI.BaseURL,
			Model:                    cfg.Processor.OpenAI.Model,
			Timeout:                  time.Duration(cfg.Processor.OpenAI.TimeoutMS) * time.Millisecond,
			MaxOutputTokens:          int64(cfg.Processor.OpenAI.MaxOutputTokens),
			ExtractEvidencePrompt:    cfg.Processor.OpenAI.ExtractEvidencePrompt,
			GenerateCandidatesPrompt: cfg.Processor.OpenAI.GenerateCandidatesPrompt,
			SemanticEnhancePrompt:    cfg.Processor.OpenAI.SemanticEnhancePrompt,
			Logger:                   logger,
		})
		if err != nil {
			return nil, fmt.Errorf("init openai processor provider: %w", err)
		}
		return provider, nil
	default:
		return nil, fmt.Errorf("CONFIG_INVALID: unsupported processor provider %q", cfg.Processor.Provider)
	}
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
	if a.worker != nil {
		go func() {
			if err := a.worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				a.logger.Error("automation worker stopped", "error", err)
			}
		}()
	}
	startRetentionMaintenanceScheduler(ctx, a.cfg.Retention, a.automationService, a.logger)
	// 启动标准 MCP stdio 服务器：由官方 SDK 处理 initialize/tools/list/tools/call。
	server := mcp.NewSDKServer(a.registry, a.version, a.logger)
	return server.RunStdio(ctx)
}

// CallTool 允许本地 CLI 和测试直接复用 MCP 工具实现，避免 health/status 形成两套逻辑。
func (a *App) CallTool(ctx context.Context, name string, params any) (any, *mcp.Error) {
	return a.registry.Call(ctx, name, params)
}

// EnsureCaptureSession 确保 Hook/ingest 控制面需要的 session/task 存在，不写 raw_event。
func (a *App) EnsureCaptureSession(ctx context.Context, req capture.ObserveRequest) error {
	if err := capture.NormalizeObserve(a.cfg.Capture, &req); err != nil {
		return err
	}
	if req.SessionID == "" {
		return fmt.Errorf("SESSION_REQUIRED: session_id is required")
	}
	sessionExists := false
	if _, err := a.store.GetCaptureQuality(ctx, req.SessionID); err == nil {
		sessionExists = true
	} else if !strings.Contains(err.Error(), "SESSION_NOT_FOUND") {
		return err
	}
	capabilitiesJSON, err := jsonText(req.CaptureCapabilities)
	if err != nil {
		return err
	}
	qualityJSON := ""
	if !sessionExists {
		initialQuality := capture.CaptureQuality{
			HasSessionStart:     req.EventType == capture.EventSessionStart,
			MissingCapabilities: capture.MissingCapabilities(req.CaptureCapabilities),
		}
		qualityJSON, err = jsonText(initialQuality)
		if err != nil {
			return err
		}
	}
	session := capture.AgentSession{
		ID:                      req.SessionID,
		AgentType:               req.AgentType,
		WorkspaceID:             req.WorkspaceID,
		ProjectID:               req.ProjectID,
		RepoID:                  req.RepoID,
		CaptureLevel:            capture.CaptureLevel(req.CaptureCapabilities),
		CaptureCapabilitiesJSON: capabilitiesJSON,
		CaptureQualityJSON:      qualityJSON,
		GoalSummary:             ensureSessionGoal(req),
		Status:                  capture.StatusActive,
	}
	if req.Session != nil && req.Session.Status != "" {
		session.Status = req.Session.Status
	}
	stored, err := a.store.UpsertSession(ctx, session)
	if err != nil {
		return err
	}
	if req.TaskID == "" {
		return nil
	}
	task := capture.AgentTask{
		ID:          req.TaskID,
		SessionID:   stored.ID,
		WorkspaceID: firstNonEmpty(req.WorkspaceID, stored.WorkspaceID),
		ProjectID:   firstNonEmpty(req.ProjectID, stored.ProjectID),
		RepoID:      firstNonEmpty(req.RepoID, stored.RepoID),
		TaskSummary: req.TaskID,
		Status:      capture.StatusActive,
	}
	if req.Task != nil {
		if req.Task.TaskSummary != "" {
			task.TaskSummary = req.Task.TaskSummary
		}
		if req.Task.Status != "" {
			task.Status = req.Task.Status
		}
		task.OutcomeSummary = req.Task.OutcomeSummary
	}
	_, err = a.store.UpsertTask(ctx, task)
	return err
}

// Close 释放运行时资源。
func (a *App) Close() error {
	var closeErr error
	if a.store != nil {
		closeErr = a.store.Close()
	}
	if a.logCloser != nil {
		if err := a.logCloser.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

// ensureSessionGoal 在 CaptureSession 建立时给出 GoalSummary 兜底。
// 优先使用 session.GoalSummary（来自 lifecycle event），缺省时回退到 ContentSummary。
// 设计约束：两个值都做 trim 处理，避免注入纯空白字符串污染数据库。
func ensureSessionGoal(req capture.ObserveRequest) string {
	if req.Session != nil && strings.TrimSpace(req.Session.GoalSummary) != "" {
		return strings.TrimSpace(req.Session.GoalSummary)
	}
	return strings.TrimSpace(req.ContentSummary)
}

// jsonText 把任意值序列化为 JSON 字符串。
// 入参：value（任意 JSON 可序列化值）。
// 返回：JSON 文本与序列化错误；错误时返回空串，便于 caller 用 err != nil 判断。
// 用途：写 SQLite 之前把 map/slice 等结构化字段转成 TEXT。
func jsonText(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// firstNonEmpty 返回第一个 trim 后非空的字符串。
// 用于在多个可选来源（如 req.WorkspaceID、stored.WorkspaceID）之间按优先级挑选有效值。
// 设计约束：返回的字符串已 trim，便于直接写入存储。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
