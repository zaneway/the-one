package diagnostics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/docindex"
	"github.com/zaneway/the-one/internal/mcp"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retrieval"
	"github.com/zaneway/the-one/internal/storage/sqlite"
)

type Store interface {
	Ping(ctx context.Context) error
	Status() sqlite.Status
	ListRetrievalTraces(ctx context.Context, query retrieval.TraceQuery) ([]retrieval.TraceRecord, error)
	ListMemoryAccessLogs(ctx context.Context, query retrieval.AccessLogQuery) ([]retrieval.AccessLogRecord, error)
	ListCodeRefs(ctx context.Context, query memory.CodeRefQuery) ([]memory.CodeRef, error)
	GetDocSnapshot(ctx context.Context, id string, includeSections bool) (docindex.DocumentSnapshot, error)
	ListDocSnapshots(ctx context.Context, query docindex.SnapshotQuery) ([]docindex.DocumentSnapshot, error)
}

// Service 聚合 P0 health/status 所需的运行时状态。
type Service struct {
	version   string
	cfg       config.Config
	store     Store
	startedAt time.Time
}

// NewService 创建 P0 诊断服务。该服务只暴露非敏感配置摘要和存储能力。
func NewService(version string, cfg config.Config, store Store) *Service {
	return &Service{
		version:   version,
		cfg:       cfg,
		store:     store,
		startedAt: time.Now(),
	}
}

// RegisterTools 注册 P0 诊断工具。后续 P1 工具应复用同一个 Registry。
func RegisterTools(registry *mcp.Registry, service *Service) {
	registry.Register("memory.health", service.HealthTool)
	registry.Register("memory.status", service.StatusTool)
	registry.Register("memory.retrieval.traces", service.RetrievalTracesTool)
	registry.Register("memory.retrieval.access_logs", service.RetrievalAccessLogsTool)
	registry.Register("memory.code_refs", service.CodeRefsTool)
	registry.Register("memory.docindex.snapshots", service.DocSnapshotsTool)
	registry.Register("memory.docindex.diff", service.DocDiffTool)
}

// HealthResponse 是 memory.health 的响应结构。
type HealthResponse struct {
	RequestID string        `json:"request_id,omitempty"`
	OK        bool          `json:"ok"`
	Version   string        `json:"version"`
	UptimeMS  int64         `json:"uptime_ms"`
	Storage   HealthStorage `json:"storage"`
	Error     *mcp.Error    `json:"error,omitempty"`
}

// HealthStorage 描述 health 响应中的存储可用性摘要。
type HealthStorage struct {
	OK      bool   `json:"ok"`
	Backend string `json:"backend"`
}

// HealthTool 执行轻量 SQLite ping 验证存储层可用性。
// 处理流程：调用 store.Ping() -> 返回 ok/version/uptime/storage 状态。
// 设计说明：ping 失败不 panic，返回结构化错误供 Agent 重试或降级。
func (s *Service) HealthTool(ctx context.Context, _ json.RawMessage) (any, *mcp.Error) {
	if err := s.store.Ping(ctx); err != nil {
		return HealthResponse{
			OK:       false,
			Version:  s.version,
			UptimeMS: time.Since(s.startedAt).Milliseconds(),
			Storage:  HealthStorage{OK: false, Backend: "sqlite"},
			Error: &mcp.Error{
				ErrorCode:    "STORAGE_UNAVAILABLE",
				Message:      "sqlite ping failed",
				Retryable:    true,
				FallbackHint: "restart memoryd or check db path",
			},
		}, nil
	}
	return HealthResponse{
		OK:       true,
		Version:  s.version,
		UptimeMS: time.Since(s.startedAt).Milliseconds(),
		Storage:  HealthStorage{OK: true, Backend: "sqlite"},
	}, nil
}

// StatusRequest 是 memory.status 的请求参数。
type StatusRequest struct {
	IncludeConfig bool `json:"include_config"`
}

// StatusResponse 是 memory.status 的响应结构，包含 capability 和 migration 状态。
type StatusResponse struct {
	RequestID  string                 `json:"request_id,omitempty"`
	Version    string                 `json:"version"`
	Storage    StatusStorage          `json:"storage"`
	Migrations sqlite.MigrationStatus `json:"migrations"`
	Config     map[string]any         `json:"config,omitempty"`
}

// StatusStorage 描述 status 响应中的 SQLite 能力信息。
type StatusStorage struct {
	Backend           string   `json:"backend"`
	DBPath            string   `json:"db_path"`
	SQLite            bool     `json:"sqlite"`
	FTS5              bool     `json:"fts5"`
	SQLiteVec         bool     `json:"sqlite_vec"`
	FallbackRetrieval []string `json:"fallback_retrieval"`
}

// StatusTool 返回存储能力、migration 状态和可选配置摘要。
// 处理流程：读取 store.Status() -> 组装 capability 信息 -> 可选附加非敏感配置。
// 设计说明：include_config 为 true 时暴露 processor/automation/embedding/retention 配置，不暴露路径和密钥。
func (s *Service) StatusTool(_ context.Context, raw json.RawMessage) (any, *mcp.Error) {
	var req StatusRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, &mcp.Error{
				ErrorCode: "VALIDATION_FAILED",
				Message:   "invalid status params",
				Retryable: false,
			}
		}
	}
	storeStatus := s.store.Status()
	response := StatusResponse{
		Version: s.version,
		Storage: StatusStorage{
			Backend:           storeStatus.Backend,
			DBPath:            storeStatus.DBPath,
			SQLite:            storeStatus.Capabilities.SQLite,
			FTS5:              storeStatus.Capabilities.FTS5,
			SQLiteVec:         storeStatus.Capabilities.SQLiteVec,
			FallbackRetrieval: storeStatus.Capabilities.FallbackRetrieval,
		},
		Migrations: storeStatus.Migrations,
	}
	// include_config=true 时暴露非敏感配置摘要（processor/automation/embedding/retention）
	// 不暴露路径、密钥等敏感信息
	if req.IncludeConfig {
		response.Config = map[string]any{
			"processor_provider":                 s.cfg.Processor.Provider,
			"processor_enable_auto_processing":   s.cfg.Processor.EnableAutoProcessing,
			"processor_max_related_events":       s.cfg.Processor.MaxRelatedEvents,
			"processor_max_candidates_per_event": s.cfg.Processor.MaxCandidatesPerEvent,
			"automation_worker_enabled":          s.cfg.Automation.WorkerEnabled,
			"automation_poll_interval_ms":        s.cfg.Automation.PollIntervalMS,
			"automation_batch_size":              s.cfg.Automation.BatchSize,
			"automation_max_attempts":            s.cfg.Automation.MaxAttempts,
			"automation_retry_base_delay_ms":     s.cfg.Automation.RetryBaseDelayMS,
			"automation_running_timeout_ms":      s.cfg.Automation.RunningTimeoutMS,
			"embedding_provider":                 s.cfg.Embedding.Provider,
			"retention_job_enabled":              s.cfg.Retention.JobEnabled,
			"retrieval_timeout_ms":               s.cfg.Retrieval.OnlineTimeoutMS,
			"default_token_budget":               s.cfg.Retrieval.DefaultTokenBudget,
			"sqlite_vec_enabled":                 s.cfg.Storage.SQLiteVecEnabled,
			"storage_busy_timeout_ms":            s.cfg.Storage.BusyTimeoutMS,
		}
	}
	return response, nil
}
