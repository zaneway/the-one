package diagnostics

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zaneway/theone/internal/codeindex"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/docindex"
	"github.com/zaneway/theone/internal/mcp"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
	"github.com/zaneway/theone/internal/retrieval"
	"github.com/zaneway/theone/internal/storage/sqlite"
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

// Service 聚合 health/status 所需的运行时状态。
type Service struct {
	version         string
	cfg             config.Config
	store           Store
	startedAt       time.Time
	aiHealthChecker processor.HealthChecker
}

// NewService 创建诊断服务。该服务只暴露非敏感配置摘要和存储能力。
func NewService(version string, cfg config.Config, store Store, opts ...ServiceOption) *Service {
	service := &Service{
		version:   version,
		cfg:       cfg,
		store:     store,
		startedAt: time.Now(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service
}

type ServiceOption func(*Service)

// WithAIHealthChecker 注入外部 AI 可用性探测器。
// diagnostics 只依赖该只读接口，不创建 provider，也不接触 API Key。
func WithAIHealthChecker(checker processor.HealthChecker) ServiceOption {
	return func(s *Service) {
		s.aiHealthChecker = checker
	}
}

// RegisterTools 注册诊断工具。其他工具应复用同一个 Registry。
func RegisterTools(registry *mcp.Registry, service *Service) {
	registry.RegisterTool(healthSpec(service.HealthTool))
	registry.RegisterTool(statusSpec(service.StatusTool))
	registry.RegisterTool(retrievalTracesSpec(service.RetrievalTracesTool))
	registry.RegisterTool(retrievalAccessLogsSpec(service.RetrievalAccessLogsTool))
	registry.RegisterTool(codeRefsSpec(service.CodeRefsTool))
	registry.RegisterTool(docSnapshotsSpec(service.DocSnapshotsTool))
	registry.RegisterTool(docDiffSpec(service.DocDiffTool))
}

// HealthResponse 是 memory.health 的响应结构。
type HealthResponse struct {
	RequestID string        `json:"request_id,omitempty"`
	OK        bool          `json:"ok"`
	Version   string        `json:"version"`
	UptimeMS  int64         `json:"uptime_ms"`
	Storage   HealthStorage `json:"storage"`
	AI        *HealthAI     `json:"ai,omitempty"`
	Error     *mcp.Error    `json:"error,omitempty"`
}

// HealthStorage 描述 health 响应中的存储可用性摘要。
type HealthStorage struct {
	OK      bool   `json:"ok"`
	Backend string `json:"backend"`
}

// HealthAI 描述 health 响应中的外部 AI 可用性摘要。
type HealthAI struct {
	Supported bool   `json:"supported"`
	OK        bool   `json:"ok"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

// HealthTool 执行轻量 SQLite ping，并在 Provider 支持时验证外部 AI 可用性。
// 处理流程：调用 store.Ping() -> 可选调用 aiHealthChecker.CheckHealth() -> 返回 ok/version/uptime/storage/ai 状态。
// 设计说明：health 不写入存储，不触发 evidence/candidate；外部 AI 失败会返回结构化错误供 Agent 重试或降级。
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
				FallbackHint: "restart theone or check db path",
			},
		}, nil
	}
	if s.aiHealthChecker != nil {
		status, err := s.aiHealthChecker.CheckHealth(ctx)
		if err != nil {
			return HealthResponse{
				OK:       false,
				Version:  s.version,
				UptimeMS: time.Since(s.startedAt).Milliseconds(),
				Storage:  HealthStorage{OK: true, Backend: "sqlite"},
				AI: &HealthAI{
					Supported: true,
					OK:        false,
					Provider:  status.Provider,
					Model:     status.Model,
					LatencyMS: status.LatencyMS,
					Error:     err.Error(),
				},
				Error: &mcp.Error{
					ErrorCode:    "AI_UNAVAILABLE",
					Message:      "external AI provider health check failed",
					Retryable:    true,
					FallbackHint: "check processor provider credentials, model, base_url, or network reachability",
				},
			}, nil
		}
		return HealthResponse{
			OK:       true,
			Version:  s.version,
			UptimeMS: time.Since(s.startedAt).Milliseconds(),
			Storage:  HealthStorage{OK: true, Backend: "sqlite"},
			AI: &HealthAI{
				Supported: true,
				OK:        true,
				Provider:  status.Provider,
				Model:     status.Model,
				LatencyMS: status.LatencyMS,
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
	CodeIndex  StatusCodeIndex        `json:"code_index"`
	Embedding  StatusEmbedding        `json:"embedding"`
	Vector     StatusVectorIndex      `json:"vector_index"`
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

// StatusCodeIndex 描述 Code Index 当前能力。
type StatusCodeIndex struct {
	Provider     string                 `json:"provider"`
	Enabled      bool                   `json:"enabled"`
	Capabilities codeindex.Capabilities `json:"capabilities"`
}

// StatusEmbedding 描述 embedding provider 和在线降级配置。
type StatusEmbedding struct {
	Provider                    string `json:"provider"`
	Model                       string `json:"model,omitempty"`
	QueryCacheSize              int    `json:"query_cache_size"`
	OnlineQueryEmbeddingEnabled bool   `json:"online_query_embedding_enabled"`
	MemoryEmbeddingEnabled      bool   `json:"memory_embedding_enabled"`
}

// StatusVectorIndex 描述 vector index 后端和 SQLite 能力。
type StatusVectorIndex struct {
	Backend          string `json:"backend"`
	SQLiteVecEnabled string `json:"sqlite_vec_enabled"`
	SQLiteVec        bool   `json:"sqlite_vec"`
	Available        bool   `json:"available"`
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
		CodeIndex:  statusCodeIndex(s.cfg),
		Embedding: StatusEmbedding{
			Provider:                    s.cfg.Embedding.Provider,
			Model:                       s.cfg.Embedding.Model,
			QueryCacheSize:              s.cfg.Embedding.QueryCacheSize,
			OnlineQueryEmbeddingEnabled: s.cfg.Embedding.OnlineQueryEmbeddingEnabled,
			MemoryEmbeddingEnabled:      s.cfg.Embedding.MemoryEmbeddingEnabled,
		},
		Vector: StatusVectorIndex{
			Backend:          s.cfg.VectorIndex.Backend,
			SQLiteVecEnabled: firstNonEmptyString(s.cfg.VectorIndex.SQLiteVecEnabled, s.cfg.Storage.SQLiteVecEnabled),
			SQLiteVec:        storeStatus.Capabilities.SQLiteVec,
			Available:        s.cfg.VectorIndex.Backend != "none" && storeStatus.Capabilities.SQLiteVec,
		},
	}
	// include_config=true 时暴露非敏感配置摘要（processor/automation/embedding/retention）
	// 不暴露路径、密钥等敏感信息
	if req.IncludeConfig {
		response.Config = map[string]any{
			"processor_provider":                 s.cfg.Processor.Provider,
			"processor_max_related_events":       s.cfg.Processor.MaxRelatedEvents,
			"processor_max_candidates_per_event": s.cfg.Processor.MaxCandidatesPerEvent,
			"automation_poll_interval_ms":        s.cfg.Automation.PollIntervalMS,
			"automation_batch_size":              s.cfg.Automation.BatchSize,
			"automation_max_attempts":            s.cfg.Automation.MaxAttempts,
			"automation_retry_base_delay_ms":     s.cfg.Automation.RetryBaseDelayMS,
			"automation_running_timeout_ms":      s.cfg.Automation.RunningTimeoutMS,
			"embedding_provider":                 s.cfg.Embedding.Provider,
			"embedding_query_cache_size":         s.cfg.Embedding.QueryCacheSize,
			"embedding_online_query_enabled":     s.cfg.Embedding.OnlineQueryEmbeddingEnabled,
			"embedding_memory_embedding_enabled": s.cfg.Embedding.MemoryEmbeddingEnabled,
			"vector_index_backend":               s.cfg.VectorIndex.Backend,
			"codeindex_provider":                 s.cfg.CodeIndex.Provider,
			"codeindex_max_resolve_refs":         s.cfg.CodeIndex.MaxResolveRefs,
			"retention_job_enabled":              s.cfg.Retention.JobEnabled,
			"retention_job_interval_ms":          s.cfg.Retention.JobIntervalMS,
			"retrieval_timeout_ms":               s.cfg.Retrieval.OnlineTimeoutMS,
			"default_token_budget":               s.cfg.Retrieval.DefaultTokenBudget,
			"retrieval_max_relation_expansion":   s.cfg.Retrieval.MaxRelationExpansion,
			"retrieval_max_candidates_rerank":    s.cfg.Retrieval.MaxCandidatesBeforeRerank,
			"sqlite_vec_enabled":                 s.cfg.Storage.SQLiteVecEnabled,
			"storage_busy_timeout_ms":            s.cfg.Storage.BusyTimeoutMS,
		}
	}
	return response, nil
}

func statusCodeIndex(cfg config.Config) StatusCodeIndex {
	status := StatusCodeIndex{
		Provider: cfg.CodeIndex.Provider,
		Enabled:  cfg.CodeIndex.Provider != "none",
	}
	if status.Enabled {
		status.Capabilities = codeindex.Capabilities{
			Provider:        cfg.CodeIndex.Provider,
			FilePathResolve: true,
			SymbolResolve:   true,
			CallGraph:       false,
			Impact:          false,
		}
	}
	return status
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
