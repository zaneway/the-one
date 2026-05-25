package tools

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/zaneway/the-one/internal/mcp"
	"github.com/zaneway/the-one/internal/mvp"
)

type MVPService interface {
	StartRun(ctx context.Context, req mvp.StartRunRequest) (mvp.StartRunResponse, error)
	RecordTask(ctx context.Context, req mvp.RecordTaskRequest) (mvp.RecordTaskResponse, error)
	RecordCapability(ctx context.Context, req mvp.RecordCapabilityRequest) (mvp.RecordCapabilityResponse, error)
	ComputeMetrics(ctx context.Context, req mvp.ComputeMetricsRequest) (mvp.ComputeMetricsResponse, error)
	Report(ctx context.Context, req mvp.ReportRequest) (mvp.ReportResponse, error)
}

// RegisterMVPTools 注册 P5 MVP 验收模型和报告工具。
func RegisterMVPTools(registry *mcp.Registry, service MVPService, logger *slog.Logger) {
	registry.Register("memory.mvp.run.start", func(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
		var req mvp.StartRunRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, validationError("invalid mvp run.start params")
		}
		resp, err := service.StartRun(ctx, req)
		if err != nil {
			logger.Warn("mvp run start failed", "error", err)
			return nil, toMCPError(err)
		}
		return resp, nil
	})
	registry.Register("memory.mvp.task.record", func(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
		var req mvp.RecordTaskRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, validationError("invalid mvp task.record params")
		}
		resp, err := service.RecordTask(ctx, req)
		if err != nil {
			logger.Warn("mvp task record failed", "error", err)
			return nil, toMCPError(err)
		}
		return resp, nil
	})
	registry.Register("memory.mvp.capability.record", func(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
		var req mvp.RecordCapabilityRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, validationError("invalid mvp capability.record params")
		}
		resp, err := service.RecordCapability(ctx, req)
		if err != nil {
			logger.Warn("mvp capability record failed", "error", err)
			return nil, toMCPError(err)
		}
		return resp, nil
	})
	registry.Register("memory.mvp.metrics.compute", func(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
		var req mvp.ComputeMetricsRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, validationError("invalid mvp metrics.compute params")
		}
		resp, err := service.ComputeMetrics(ctx, req)
		if err != nil {
			logger.Warn("mvp metrics compute failed", "error", err)
			return nil, toMCPError(err)
		}
		return resp, nil
	})
	registry.Register("memory.mvp.report", func(ctx context.Context, raw json.RawMessage) (any, *mcp.Error) {
		var req mvp.ReportRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, validationError("invalid mvp report params")
		}
		resp, err := service.Report(ctx, req)
		if err != nil {
			logger.Warn("mvp report failed", "error", err)
			return nil, toMCPError(err)
		}
		return resp, nil
	})
}
