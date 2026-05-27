package retention

import "math"

// RelationFactor 计算关系因子（架构 §8.2.2）。
func RelationFactor(signals RelationSignals) float64 {
	factor := 1.0 +
		math.Min(0.3, 0.05*float64(signals.SupportingCount)) +
		math.Min(0.2, 0.08*float64(signals.LinkedLongTermCount)) -
		math.Min(0.4, 0.15*float64(signals.ContradictingCount))
	if factor < 0.6 {
		return 0.6
	}
	return factor
}

// ConflictPenalty 计算冲突惩罚（架构 §8.2.3）。
func ConflictPenalty(signals RelationSignals) float64 {
	count := signals.UnresolvedConflictCount
	if count == 0 && signals.ContradictingCount > 0 {
		count = signals.ContradictingCount
	}
	return math.Min(0.6, 0.2*float64(count))
}
