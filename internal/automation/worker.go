package automation

import (
	"context"
	"time"
)

// WorkerConfig 控制本地异步 worker 的轮询、批量和重试节奏。
type WorkerConfig struct {
	PollIntervalMS   int
	BatchSize        int
	RetryBaseDelayMS int
	RunningTimeoutMS int
}

// WorkerRunResult 记录一轮 worker 执行结果，便于测试和后续诊断复用。
type WorkerRunResult struct {
	Claimed   int
	Succeeded int
	Retried   int
	Failed    int
	Recovered int
}

// Worker 负责领取 pending job、调用 automation service，并按失败次数决定 retry 或 failed。
type Worker struct {
	service *Service
	repo    Repository
	cfg     WorkerConfig
}

// NewWorker 创建本地 worker。配置缺省时使用 P3 设计文档中的保守默认值。
func NewWorker(service *Service, repo Repository, cfg WorkerConfig) *Worker {
	if cfg.PollIntervalMS <= 0 {
		cfg.PollIntervalMS = 1000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.RetryBaseDelayMS <= 0 {
		cfg.RetryBaseDelayMS = 1000
	}
	if cfg.RunningTimeoutMS <= 0 {
		cfg.RunningTimeoutMS = 300000
	}
	return &Worker{service: service, repo: repo, cfg: cfg}
}

// Run 持续轮询 pending job，直到 context 被取消。
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Duration(w.cfg.PollIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := w.RunOnce(ctx, time.Now().UTC()); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOnce 领取并执行一批到期任务；Provider 执行发生在 claim 事务之外。
func (w *Worker) RunOnce(ctx context.Context, now time.Time) (WorkerRunResult, error) {
	if err := ctx.Err(); err != nil {
		return WorkerRunResult{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	recovered, err := w.repo.RecoverStaleRunningJobs(ctx, now, time.Duration(w.cfg.RunningTimeoutMS)*time.Millisecond)
	if err != nil {
		return WorkerRunResult{}, err
	}
	jobs, err := w.repo.ClaimJobs(ctx, now, w.cfg.BatchSize)
	if err != nil {
		return WorkerRunResult{}, err
	}
	result := WorkerRunResult{Claimed: len(jobs), Recovered: recovered}
	for _, job := range jobs {
		if err := w.service.RunJob(ctx, job); err != nil {
			if w.shouldRetry(job) {
				retryCount := job.RetryCount + 1
				nextRunAt := now.Add(w.retryDelay(retryCount))
				if retryErr := w.repo.MarkJobRetry(ctx, job.ID, retryCount, nextRunAt, err.Error(), time.Now().UTC()); retryErr != nil {
					return result, retryErr
				}
				result.Retried++
				continue
			}
			if failErr := w.repo.MarkJobFailed(ctx, job.ID, err.Error(), time.Now().UTC()); failErr != nil {
				return result, failErr
			}
			result.Failed++
			continue
		}
		result.Succeeded++
	}
	return result, nil
}

func (w *Worker) shouldRetry(job AsyncJob) bool {
	maxRetries := job.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	return job.RetryCount+1 < maxRetries
}

func (w *Worker) retryDelay(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	multiplier := 1 << retryCount
	return time.Duration(w.cfg.RetryBaseDelayMS*multiplier) * time.Millisecond
}
