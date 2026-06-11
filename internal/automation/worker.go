package automation

import (
	"context"
	"time"
)

// WorkerConfig 控制本地异步 worker 的轮询、批量和重试节奏。
// 字段含义：
//   - PollIntervalMS：pending job 队列为空时的等待间隔；
//   - BatchSize：单次 RunOnce 最多领取的 job 数；
//   - RetryBaseDelayMS：失败重试的指数退避基数（毫秒）；
//   - RunningTimeoutMS：把长时间 running 状态判为 stale 并回收的阈值。
type WorkerConfig struct {
	PollIntervalMS   int
	BatchSize        int
	RetryBaseDelayMS int
	RunningTimeoutMS int
}

// WorkerRunResult 记录一轮 worker 执行结果，便于测试和后续诊断复用。
// Recovered 统计从 running 回收回 pending 的 job 数；其它字段按状态归类。
type WorkerRunResult struct {
	Claimed   int
	Succeeded int
	Retried   int
	Failed    int
	Recovered int
}

// Worker 负责领取 pending job、调用 automation service，并按失败次数决定 retry 或 failed。
// 设计要点：单进程内只跑一个 worker，避免 SQLite 写锁竞争；Provider 调用发生在
// ClaimJobs 事务之外，避免长耗时调用持有短事务。
type Worker struct {
	service *Service
	repo    Repository
	cfg     WorkerConfig
}

// NewWorker 创建本地 worker。配置缺省时使用保守默认值。
// 默认值与 config.Automation 字段一致，便于单测注入不同的边界值。
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
// 处理流程：每轮先做 ctx 取消检查，然后调用 RunOnce 处理一批任务，最后
// 通过 ticker 等待 PollIntervalMS 间隔；ctx 取消时立即退出并返回 ctx.Err()。
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Duration(w.cfg.PollIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		// 入口处先检查 ctx，避免已经取消后还触发一次 RunOnce
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := w.RunOnce(ctx, time.Now()); err != nil {
			return err
		}
		select {
		// 优先响应 ctx 取消，再等待 ticker 触发下一轮
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOnce 领取并执行一批到期任务；Provider 执行发生在 claim 事务之外。
// 处理流程：
//  1. ctx 取消检查；
//  2. 回收超时 running 状态的 job（处理上次进程崩溃遗留）；
//  3. 一次性 ClaimJobs 拉一批 pending 任务；
//  4. 逐个执行 service.RunJob，成功计 Succeeded；
//  5. 失败时根据 shouldRetry 决定是 MarkJobRetry（指数退避）还是 MarkJobFailed；
//  6. 任意一步数据库失败立即终止本轮，返回部分结果 + 错误，便于上层日志定位。
func (w *Worker) RunOnce(ctx context.Context, now time.Time) (WorkerRunResult, error) {
	if err := ctx.Err(); err != nil {
		return WorkerRunResult{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	// 优先回收上次崩溃遗留的 running 任务，避免它们永远卡在 running
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
				// 退避后写回 retry 状态；写入失败直接返回，让上层感知数据库异常
				if retryErr := w.repo.MarkJobRetry(ctx, job.ID, retryCount, nextRunAt, err.Error(), time.Now()); retryErr != nil {
					return result, retryErr
				}
				result.Retried++
				continue
			}
			// 超过最大重试次数：标记为 failed，不再重试
			if failErr := w.repo.MarkJobFailed(ctx, job.ID, err.Error(), time.Now()); failErr != nil {
				return result, failErr
			}
			result.Failed++
			continue
		}
		result.Succeeded++
	}
	return result, nil
}

// shouldRetry 决定本次失败是否还能再重试。
// job.MaxRetries <= 0 退回到 defaultMaxRetries；只有"下一轮重试次数仍小于上限"时才允许重试。
func (w *Worker) shouldRetry(job AsyncJob) bool {
	maxRetries := job.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	return job.RetryCount+1 < maxRetries
}

// retryDelay 按指数退避计算下一次重试的等待时间：base * 2^retryCount。
// retryCount 防御性取 0，避免负值导致左移越界。
func (w *Worker) retryDelay(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	multiplier := 1 << retryCount
	return time.Duration(w.cfg.RetryBaseDelayMS*multiplier) * time.Millisecond
}
