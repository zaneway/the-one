package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/zaneway/theone/internal/dream"
	"github.com/zaneway/theone/internal/mcp"
)

// RegisterDreamTools 注册 Obsidian Dream 只读投影工具到 MCP 注册表。
func RegisterDreamTools(registry *mcp.Registry, service *dream.Service, logger *slog.Logger) {
	handler := &DreamHandler{service: service, logger: logger}
	registry.RegisterTool(dreamExportSpec(handler.Export))
}

type DreamHandler struct {
	service *dream.Service
	logger  *slog.Logger
}

// Export 处理 memory.dream.export 工具调用。
// 功能：将当前持久化记忆投影为只读 Markdown Vault，支持 dry-run 和 scope 过滤。
func (h *DreamHandler) Export(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
	startedAt := time.Now()
	var req dream.RunRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, validationError("invalid dream export params")
	}
	if h.service == nil {
		return nil, validationError("dream service is unavailable")
	}
	resp, err := h.service.Run(ctx, req)
	if err != nil {
		return nil, toMCPError(err)
	}
	h.logger.Info("dream export completed",
		"input_dry_run", req.DryRun,
		"input_workspace_id", req.WorkspaceID,
		"input_project_id", req.ProjectID,
		"input_repo_id", req.RepoID,
		"input_limit", req.Limit,
		"output_planned", resp.Planned,
		"output_written", resp.Written,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	return resp, nil
}
