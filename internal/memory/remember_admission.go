package memory

import "context"

// RememberAdmissionDecider 对显式 remember 请求执行统一准入规则。
// 设计约束：memory.Service 不得在未经过准入决策时直接持久化 memory_item。
type RememberAdmissionDecider interface {
	DecideRemember(ctx context.Context, req RememberRequest) (RememberAdmissionDecision, error)
}

// RememberAdmissionDecision 是 remember 路径上的准入结果。
type RememberAdmissionDecision struct {
	Allowed        bool
	Decision       string
	InitialState   string
	InitialTier    string
	UserConfirmed  bool
	RetentionScore float64
	DecayRate      float64
	ReasonCodes    []string
}
