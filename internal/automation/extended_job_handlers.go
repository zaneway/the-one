package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zaneway/theone/internal/codeindex"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/docindex"
	"github.com/zaneway/theone/internal/memory"
)

type codeRefRepository interface {
	WriteCodeRef(ctx context.Context, ref memory.CodeRef) (memory.CodeRef, error)
	GetCodeRef(ctx context.Context, id string) (memory.CodeRef, error)
	ListCodeRefsForRefresh(ctx context.Context, repoID string, limit int) ([]memory.CodeRef, error)
}

type docSnapshotRepository interface {
	WriteDocSnapshot(ctx context.Context, snapshot docindex.DocumentSnapshot) (docindex.DocumentSnapshot, error)
}

type embeddingRepository interface {
	UpsertMemoryEmbedding(ctx context.Context, memoryID, model string, vector []float32) error
}

type accessLogCleanupRepository interface {
	CleanupMemoryAccessLogs(ctx context.Context, eventType string, before time.Time) (int, error)
}

type extendedJobHandler struct {
	cfg  config.Config
	repo any
}

func newExtendedJobHandler(cfg config.Config, repo any) JobHandler {
	return extendedJobHandler{cfg: cfg, repo: repo}
}

// CanHandle 判断是否为相关 job 类型。
// job 类型：resolve_code_ref, refresh_code_ref_status, build_doc_snapshot, compute_embedding, cleanup_access_log。
func (h extendedJobHandler) CanHandle(jobType string) bool {
	switch jobType {
	case JobTypeResolveCodeRef, JobTypeRefreshCodeRefStatus, JobTypeBuildDocSnapshot, JobTypeComputeEmbedding, JobTypeCleanupAccessLog:
		return true
	default:
		return false
	}
}

func (h extendedJobHandler) RunJob(ctx context.Context, job AsyncJob) (map[string]any, error) {
	switch job.JobType {
	case JobTypeResolveCodeRef:
		return h.runResolveCodeRef(ctx, job)
	case JobTypeRefreshCodeRefStatus:
		return h.runRefreshCodeRefStatus(ctx, job)
	case JobTypeBuildDocSnapshot:
		return h.runBuildDocSnapshot(ctx, job)
	case JobTypeComputeEmbedding:
		return h.runComputeEmbedding(ctx, job)
	case JobTypeCleanupAccessLog:
		return h.runCleanupAccessLog(ctx, job)
	default:
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: unsupported job_type %q", job.JobType)
	}
}

// runResolveCodeRef 解析并持久化 code_ref。
// 流程：从 payload 中提取 code_ref → 写入 repository → 可选使用 CodeIndexAdapter 在线解析。
// resolve_mode=persist_only 时只写入不解析；resolve_mode=adapter 时使用 local_basic 解析器。
func (h extendedJobHandler) runResolveCodeRef(ctx context.Context, job AsyncJob) (map[string]any, error) {
	repo, ok := any(h.repo).(codeRefRepository)
	if !ok {
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: code_ref repository unavailable")
	}
	var payload resolveCodeRefPayload
	if err := decodeJobPayload(job.PayloadJSON, &payload); err != nil {
		return nil, err
	}
	refs := payload.CodeRefs
	if payload.CodeRef != nil {
		refs = append(refs, *payload.CodeRef)
	}
	if len(refs) == 0 && (payload.RepoID != "" || payload.FilePath != "" || payload.Symbol != "") {
		refs = append(refs, memory.CodeRef{
			MemoryID:      firstNonEmptyString(payload.MemoryID, targetMemoryID(job)),
			RepoID:        payload.RepoID,
			CommitHash:    payload.CommitHash,
			FilePath:      payload.FilePath,
			Symbol:        payload.Symbol,
			LineStart:     payload.LineStart,
			LineEnd:       payload.LineEnd,
			ContentHash:   payload.ContentHash,
			RefSummary:    payload.RefSummary,
			ResolveStatus: payload.ResolveStatus,
		})
	}
	if len(refs) == 0 {
		return map[string]any{"status": "skipped", "reason": "no_code_ref_payload"}, nil
	}
	written := 0
	resolveMode := firstNonEmptyString(payload.ResolveMode, "persist_only")
	adapter := h.newCodeIndexAdapter(payload.RepoRoot)
	for _, ref := range refs {
		if ref.MemoryID == "" {
			ref.MemoryID = firstNonEmptyString(payload.MemoryID, targetMemoryID(job))
		}
		if ref.ResolveStatus == "" {
			ref.ResolveStatus = memory.CodeRefStatusUnresolved
		}
		persisted, err := repo.WriteCodeRef(ctx, ref)
		if err != nil {
			return nil, err
		}
		if resolveMode == "adapter" {
			if adapter == nil {
				written++
				continue
			}
			resolved, err := adapter.ResolveCodeRefs(ctx, []memory.CodeRef{persisted})
			if err == nil && len(resolved) == 1 {
				if _, err := repo.WriteCodeRef(ctx, resolved[0]); err != nil {
					return nil, err
				}
			}
		}
		written++
	}
	return map[string]any{"status": "completed", "code_ref_count": written}, nil
}

// runRefreshCodeRefStatus 批量刷新已有 code_ref 的解析状态。
// 流程：从 payload/target 中获取待刷新的 code_ref 列表 → 使用 CodeIndexAdapter 重新解析 → 写回更新。
// 设计意图：代码变更后批量刷新 code_ref 的行号和 hash 状态。
func (h extendedJobHandler) runRefreshCodeRefStatus(ctx context.Context, job AsyncJob) (map[string]any, error) {
	repo, ok := any(h.repo).(codeRefRepository)
	if !ok {
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: code_ref repository unavailable")
	}
	var payload resolveCodeRefPayload
	if err := decodeJobPayload(job.PayloadJSON, &payload); err != nil {
		return nil, err
	}
	refs := payload.CodeRefs
	if payload.CodeRef != nil {
		refs = append(refs, *payload.CodeRef)
	}
	if len(refs) == 0 && job.TargetType == TargetTypeCodeRef && job.TargetID != "" {
		ref, err := repo.GetCodeRef(ctx, job.TargetID)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		repoID := firstNonEmptyString(payload.RepoID, job.TargetID)
		if job.TargetType != TargetTypeWorkspace && job.TargetType != TargetTypeRepo && job.TargetType != TargetTypeCodeRef {
			repoID = payload.RepoID
		}
		if repoID == "" {
			return nil, fmt.Errorf("VALIDATION_FAILED: repo_id or code_ref target is required")
		}
		listed, err := repo.ListCodeRefsForRefresh(ctx, repoID, firstPositive(payload.Limit, h.cfg.CodeIndex.MaxResolveRefs, 30))
		if err != nil {
			return nil, err
		}
		refs = listed
	}
	adapter := h.newCodeIndexAdapter(payload.RepoRoot)
	if adapter == nil {
		return map[string]any{"status": "skipped", "reason": "code_index_disabled", "code_ref_count": len(refs)}, nil
	}
	resolved, err := adapter.ResolveCodeRefs(ctx, refs)
	if err != nil {
		return nil, err
	}
	updated := 0
	for _, ref := range resolved {
		if _, err := repo.WriteCodeRef(ctx, ref); err != nil {
			return nil, err
		}
		updated++
	}
	return map[string]any{"status": "completed", "code_ref_count": updated}, nil
}

// runBuildDocSnapshot 构建并持久化 Markdown 文档快照。
// 流程：从 payload 或 target_id 获取 doc_path → BuildMarkdownSnapshot → WriteDocSnapshot。
// 设计意图：为 Doc Index 策略提供文档变更检测基础。
func (h extendedJobHandler) runBuildDocSnapshot(ctx context.Context, job AsyncJob) (map[string]any, error) {
	repo, ok := any(h.repo).(docSnapshotRepository)
	if !ok {
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: doc snapshot repository unavailable")
	}
	var snapshot docindex.DocumentSnapshot
	if err := decodeJobPayload(job.PayloadJSON, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Path == "" && job.TargetType == TargetTypeDocPath {
		snapshot.Path = job.TargetID
	}
	if snapshot.ContentHash == "" {
		if !h.cfg.DocIndex.Enabled {
			return map[string]any{"status": "skipped", "reason": "docindex_disabled"}, nil
		}
		built, err := docindex.BuildMarkdownSnapshot(docindex.MarkdownBuildOptions{
			WorkspaceID:         snapshot.WorkspaceID,
			ProjectID:           snapshot.ProjectID,
			RepoID:              snapshot.RepoID,
			Path:                snapshot.Path,
			Role:                snapshot.Role,
			MaxDocSizeKB:        h.cfg.DocIndex.MaxDocSizeKB,
			MaxSections:         h.cfg.DocIndex.MaxSections,
			StoreSectionSummary: h.cfg.DocIndex.StoreSectionSummary,
		})
		if err != nil {
			return nil, err
		}
		snapshot = built
	}
	written, err := repo.WriteDocSnapshot(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":        "completed",
		"snapshot_id":   written.ID,
		"doc_path":      written.Path,
		"section_count": written.SectionCount,
	}, nil
}

// runComputeEmbedding 写入预计算的 memory embedding 向量。
// 设计意图：embedding 由外部 provider 生成（如 OpenAI），本 handler 只负责持久化。
// provider=none 或 payload 为空时安全跳过。
func (h extendedJobHandler) runComputeEmbedding(ctx context.Context, job AsyncJob) (map[string]any, error) {
	var payload computeEmbeddingPayload
	if err := decodeJobPayload(job.PayloadJSON, &payload); err != nil {
		return nil, err
	}
	if len(payload.Embedding) == 0 {
		return map[string]any{"status": "skipped", "reason": "embedding_payload_missing"}, nil
	}
	repo, ok := any(h.repo).(embeddingRepository)
	if !ok {
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: embedding repository unavailable")
	}
	memoryID := firstNonEmptyString(payload.MemoryID, targetMemoryID(job))
	model := firstNonEmptyString(payload.Model, h.cfg.Embedding.Model, h.cfg.Embedding.Provider)
	if memoryID == "" || model == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: memory_id and embedding model are required")
	}
	vector := make([]float32, len(payload.Embedding))
	for i, value := range payload.Embedding {
		vector[i] = float32(value)
	}
	if err := repo.UpsertMemoryEmbedding(ctx, memoryID, model, vector); err != nil {
		return nil, err
	}
	return map[string]any{"status": "completed", "memory_id": memoryID, "embedding_model": model, "embedding_dim": len(vector)}, nil
}

// runCleanupAccessLog 清理低价值的 memory_access_log 明细。
// 清理策略：retrieved 类型保留 30 天，injected 类型保留 180 天。
// 设计意图：控制 access_log 表的增长，保留有价值的反馈事件（user_confirmed 等）。
func (h extendedJobHandler) runCleanupAccessLog(ctx context.Context, job AsyncJob) (map[string]any, error) {
	repo, ok := any(h.repo).(accessLogCleanupRepository)
	if !ok {
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: access log cleanup repository unavailable")
	}
	var payload cleanupAccessLogPayload
	if err := decodeJobPayload(job.PayloadJSON, &payload); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if payload.Now != "" {
		parsed, err := time.Parse(time.RFC3339Nano, payload.Now)
		if err != nil {
			return nil, fmt.Errorf("VALIDATION_FAILED: invalid now: %w", err)
		}
		now = parsed
	}
	retrievedBefore := now.AddDate(0, 0, -firstPositive(payload.RetentionDaysRetrieved, 30))
	injectedBefore := now.AddDate(0, 0, -firstPositive(payload.RetentionDaysInjected, 180))
	retrieved, err := repo.CleanupMemoryAccessLogs(ctx, "retrieved", retrievedBefore)
	if err != nil {
		return nil, err
	}
	injected, err := repo.CleanupMemoryAccessLogs(ctx, "injected", injectedBefore)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":            "completed",
		"retrieved_deleted": retrieved,
		"injected_deleted":  injected,
	}, nil
}

type resolveCodeRefPayload struct {
	MemoryID      string           `json:"memory_id"`
	CodeRef       *memory.CodeRef  `json:"code_ref"`
	CodeRefs      []memory.CodeRef `json:"code_refs"`
	RepoID        string           `json:"repo_id"`
	CommitHash    string           `json:"commit_hash"`
	FilePath      string           `json:"file_path"`
	Symbol        string           `json:"symbol"`
	LineStart     int              `json:"line_start"`
	LineEnd       int              `json:"line_end"`
	ContentHash   string           `json:"content_hash"`
	RefSummary    string           `json:"ref_summary"`
	ResolveStatus string           `json:"resolve_status"`
	RepoRoot      string           `json:"repo_root"`
	ResolveMode   string           `json:"resolve_mode"`
	Limit         int              `json:"limit"`
}

type computeEmbeddingPayload struct {
	MemoryID  string    `json:"memory_id"`
	Model     string    `json:"embedding_model"`
	Embedding []float64 `json:"embedding"`
}

type cleanupAccessLogPayload struct {
	Now                    string `json:"now"`
	RetentionDaysRetrieved int    `json:"retention_days_retrieved"`
	RetentionDaysInjected  int    `json:"retention_days_injected"`
}

func decodeJobPayload(payloadJSON string, target any) error {
	if payloadJSON == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(payloadJSON), target); err != nil {
		return fmt.Errorf("VALIDATION_FAILED: invalid job payload: %w", err)
	}
	return nil
}

func targetMemoryID(job AsyncJob) string {
	if job.TargetType == TargetTypeMemoryItem {
		return job.TargetID
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (h extendedJobHandler) newCodeIndexAdapter(repoRoot string) *codeindex.LocalBasicAdapter {
	if h.cfg.CodeIndex.Provider == "none" {
		return nil
	}
	return codeindex.NewLocalBasicAdapter(h.cfg.CodeIndex, repoRoot)
}
