package retention

// AccessLogEventWeight 返回访问事件权重，与架构 §8.1 及 sqlite 写入保持一致。
func AccessLogEventWeight(eventType string) float64 {
	switch eventType {
	case "retrieved":
		return 0.2
	case "injected":
		return 0.5
	case "cited_by_agent":
		return 1.0
	case "user_confirmed":
		return 2.0
	case "user_declared":
		return 2.5
	case "task_success":
		return 1.5
	case "repeated_signal", "linked_to_long":
		return 1.0
	case "ignored":
		return -0.5
	case "task_failure":
		return -1.5
	case "user_rejected":
		return -3.0
	default:
		return 0.2
	}
}

// EventDecayModifier 返回事件类型对幂律衰减的修正系数（架构 §8.1）。
func EventDecayModifier(eventType string) float64 {
	switch eventType {
	case "user_declared":
		return 0.5
	case "user_confirmed":
		return 0.6
	case "task_success":
		return 0.8
	case "cited_by_agent":
		return 0.9
	case "injected":
		return 1.0
	case "retrieved":
		return 1.2
	case "ignored":
		return 1.4
	case "task_failure":
		return 1.5
	case "user_rejected":
		return 2.0
	default:
		return 1.0
	}
}
