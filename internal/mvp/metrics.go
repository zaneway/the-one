package mvp

import (
	"math"
	"sort"
)

const level4CapabilityCount = 6

// TokenSavings 计算 token savings 指标。
// 边界条件：baseline <= 0 时无法形成有效分母，返回 passed=false 的样本，避免制造虚假的节省率。
func TokenSavings(runID, scenarioID string, baselineTokens, candidateTokens float64, threshold float64) MetricSample {
	value := 0.0
	passed := false
	if baselineTokens > 0 {
		value = (baselineTokens - candidateTokens) / baselineTokens
		passed = CompareThreshold(value, ThresholdGreaterOrEqual, threshold)
	}
	return MetricSample{
		RunID:             runID,
		ScenarioID:        scenarioID,
		MetricName:        MetricTokenSavings,
		MetricValue:       value,
		Numerator:         baselineTokens - candidateTokens,
		Denominator:       baselineTokens,
		Unit:              MetricUnitRatio,
		ThresholdValue:    threshold,
		ThresholdOperator: ThresholdGreaterOrEqual,
		Passed:            passed,
	}
}

// RatioMetric 生成通用比例类指标样本。
// 分母为 0 表示本轮没有有效统计样本，返回 0 且不通过，调用方应在报告中解释原因。
func RatioMetric(runID, scenarioID, agentType, name string, numerator, denominator float64, operator string, threshold float64) MetricSample {
	value := 0.0
	passed := false
	if denominator > 0 {
		value = numerator / denominator
		passed = CompareThreshold(value, operator, threshold)
	}
	return MetricSample{
		RunID:             runID,
		ScenarioID:        scenarioID,
		AgentType:         agentType,
		MetricName:        name,
		MetricValue:       value,
		Numerator:         numerator,
		Denominator:       denominator,
		Unit:              MetricUnitRatio,
		ThresholdValue:    threshold,
		ThresholdOperator: operator,
		Passed:            passed,
	}
}

// WrongMemoryInjectionRate 计算错误记忆注入率。
// injected 为 0 时不计入错误注入率分母，返回不通过样本用于暴露召回或注入缺失。
func WrongMemoryInjectionRate(runID, scenarioID string, wrong, injected float64) MetricSample {
	return RatioMetric(runID, scenarioID, "", MetricWrongMemoryInjectionRate, wrong, injected, ThresholdLessOrEqual, 0.05)
}

// RetrievalLatencyP95MS 计算 retrieval_trace.latency_ms 的 P95。
func RetrievalLatencyP95MS(runID string, latencies []float64) MetricSample {
	value := percentile(latencies, 0.95)
	return MetricSample{
		RunID:             runID,
		MetricName:        MetricRetrievalLatencyP95MS,
		MetricValue:       value,
		Numerator:         value,
		Denominator:       float64(len(latencies)),
		Unit:              MetricUnitMS,
		ThresholdValue:    100,
		ThresholdOperator: ThresholdLessOrEqual,
		Passed:            len(latencies) > 0 && CompareThreshold(value, ThresholdLessOrEqual, 100),
	}
}

// CapabilityCoverage 根据 Level4 六项能力计算覆盖率。
func CapabilityCoverage(capability AgentCapability) float64 {
	supported := 0
	for _, ok := range []bool{
		capability.ConversationCapture,
		capability.ToolCallCapture,
		capability.ToolOutputCapture,
		capability.FileEditCapture,
		capability.SessionLifecycle,
		capability.MemoryObserve,
	} {
		if ok {
			supported++
		}
	}
	return float64(supported) / level4CapabilityCount
}

// EventCaptureCompleteness 计算实际捕获事件完整度。
func EventCaptureCompleteness(captured, expected float64) float64 {
	if expected <= 0 {
		return 0
	}
	return captured / expected
}

// CompareThreshold 执行指标阈值判断。
func CompareThreshold(value float64, operator string, threshold float64) bool {
	switch operator {
	case ThresholdGreaterOrEqual:
		return value >= threshold
	case ThresholdLessOrEqual:
		return value <= threshold
	case ThresholdEqual:
		return math.Abs(value-threshold) < 0.000001
	default:
		return false
	}
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	index := int(math.Ceil(p*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
