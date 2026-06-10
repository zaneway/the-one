package processor

import "context"

type logContextKey struct{}

// LogContext 携带 automation 等上层调用方可注入的日志关联字段。
type LogContext struct {
	JobID string
}

func WithLogContext(ctx context.Context, lc LogContext) context.Context {
	return context.WithValue(ctx, logContextKey{}, lc)
}

func LogContextFrom(ctx context.Context) LogContext {
	if ctx == nil {
		return LogContext{}
	}
	lc, _ := ctx.Value(logContextKey{}).(LogContext)
	return lc
}
