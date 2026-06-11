package processor

import "context"

// logContextKey 是 context.Value 的私有键类型，避免与其它包产生冲突。
type logContextKey struct{}

// LogContext 携带 automation 等上层调用方可注入的日志关联字段。
// 当前字段：JobID（worker 在 RunJob 时填入，provider 内部日志自动带上）。
type LogContext struct {
	JobID string
}

// WithLogContext 把 LogContext 写入 ctx，provider 内部从 ctx 读出后写入每条日志。
// 设计约束：键采用未导出 struct{}，外部包无法伪造/读取，避免与 caller 自己的 ctx 冲突。
func WithLogContext(ctx context.Context, lc LogContext) context.Context {
	return context.WithValue(ctx, logContextKey{}, lc)
}

// LogContextFrom 从 ctx 中读出 LogContext；nil ctx 或未携带时返回零值。
// 用于 provider 内部把所有日志字段补齐 JobID，让失败日志可与 worker 任务关联。
func LogContextFrom(ctx context.Context) LogContext {
	if ctx == nil {
		return LogContext{}
	}
	lc, _ := ctx.Value(logContextKey{}).(LogContext)
	return lc
}
