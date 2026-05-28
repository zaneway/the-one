package automation

import (
	"context"
	"fmt"
)

// JobHandler 是异步任务处理器扩展点。
// 设计约束：worker 只负责 claim/retry/failed 状态流转；具体业务逻辑由 handler 承载，避免 automation service switch 持续膨胀。
type JobHandler interface {
	// CanHandle 判断当前 handler 是否支持指定 job_type。
	CanHandle(jobType string) bool

	// RunJob 执行业务任务并返回可持久化的诊断 payload。
	RunJob(ctx context.Context, job AsyncJob) (map[string]any, error)
}

// JobDispatcher 按注册顺序分发 job。
type JobDispatcher struct {
	handlers []JobHandler
}

// NewJobDispatcher 创建轻量 handler registry。
func NewJobDispatcher(handlers ...JobHandler) JobDispatcher {
	copied := make([]JobHandler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			copied = append(copied, handler)
		}
	}
	return JobDispatcher{handlers: copied}
}

// RunJob 将 job 分发给第一个匹配的 handler。
func (d JobDispatcher) RunJob(ctx context.Context, job AsyncJob) (map[string]any, error) {
	for _, handler := range d.handlers {
		if handler.CanHandle(job.JobType) {
			return handler.RunJob(ctx, job)
		}
	}
	return nil, fmt.Errorf("PROVIDER_NOT_FOUND: unsupported job_type %q", job.JobType)
}

type p3JobHandler struct {
	service *Service
}

func (h p3JobHandler) CanHandle(jobType string) bool {
	switch jobType {
	case JobTypeExtractEvidence, JobTypeGenerateMemoryCandidate, JobTypeComputeAdmission:
		return true
	default:
		return false
	}
}

func (h p3JobHandler) RunJob(ctx context.Context, job AsyncJob) (map[string]any, error) {
	switch job.JobType {
	case JobTypeExtractEvidence:
		return h.service.runExtractEvidence(ctx, job)
	case JobTypeGenerateMemoryCandidate:
		return h.service.runGenerateMemoryCandidate(ctx, job)
	case JobTypeComputeAdmission:
		return h.service.runComputeAdmission(ctx, job)
	default:
		return nil, fmt.Errorf("PROVIDER_NOT_FOUND: unsupported job_type %q", job.JobType)
	}
}
