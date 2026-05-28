package mvp

// Scenario 定义 MVP 验收任务的稳定结构化元数据。
type Scenario struct {
	ID          string
	Name        string
	Description string
	Metrics     []MetricThreshold
}

// MetricThreshold 定义 scenario 的指标门槛。
type MetricThreshold struct {
	MetricName string
	Operator   string
	Value      float64
	Unit       string
}

// ScenarioRegistry 返回十个 MVP scenario。
// 设计约束：这里仅定义验收元数据，事件 fixture 和执行编排由其他模块负责。
func ScenarioRegistry() []Scenario {
	return []Scenario{
		{
			ID:          "mvp_01_task_continuation",
			Name:        "跨 Session 继续同一项目任务",
			Description: "验证长期记忆能恢复项目上下文和任务状态。",
			Metrics: []MetricThreshold{
				{MetricName: "repeated_explanation_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: "task_state_recall", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitCount},
				{MetricName: MetricTokenSavings, Operator: ThresholdGreaterOrEqual, Value: 0.30, Unit: MetricUnitRatio},
			},
		},
		{
			ID:          "mvp_02_user_preference",
			Name:        "用户架构偏好应用",
			Description: "验证 user_global 偏好跨项目生效且不污染项目 scope。",
			Metrics: []MetricThreshold{
				{MetricName: "preference_recall_accuracy", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitRatio},
				{MetricName: "repeated_explanation_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: "wrong_scope_injection_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
			},
		},
		{
			ID:          "mvp_03_decision_recall",
			Name:        "历史架构决策召回",
			Description: "验证 decision 类型记忆和 evidence 能正确召回。",
			Metrics: []MetricThreshold{
				{MetricName: MetricDecisionRecallAccuracy, Operator: ThresholdGreaterOrEqual, Value: 0.80, Unit: MetricUnitRatio},
				{MetricName: "evidence_faithfulness", Operator: ThresholdGreaterOrEqual, Value: 0.90, Unit: MetricUnitRatio},
				{MetricName: "pending_state_mark_rate", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitRatio},
			},
		},
		{
			ID:          "mvp_04_failure_recall",
			Name:        "避免重复踩坑",
			Description: "验证 failure 和 procedure 记忆能改变后续行为。",
			Metrics: []MetricThreshold{
				{MetricName: "failure_memory_recall", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitCount},
				{MetricName: "user_correction_reduction", Operator: ThresholdGreaterOrEqual, Value: 0.50, Unit: MetricUnitRatio},
				{MetricName: "old_wrong_strategy_reuse_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
			},
		},
		{
			ID:          "mvp_05_temporal_validity",
			Name:        "识别过期项目事实",
			Description: "验证 temporal validity 和 supersedes/staleness 处理。",
			Metrics: []MetricThreshold{
				{MetricName: "temporal_correctness", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitRatio},
				{MetricName: "stale_memory_misuse_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: "supersedes_link_present", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitCount},
			},
		},
		{
			ID:          "mvp_06_cross_agent_sharing",
			Name:        "多 Agent 共享同一项目上下文",
			Description: "验证 Codex、Claude Code、Cursor 共享同一 Memory Daemon。",
			Metrics: []MetricThreshold{
				{MetricName: MetricCrossAgentRecallSuccessRate, Operator: ThresholdGreaterOrEqual, Value: 0.80, Unit: MetricUnitRatio},
				{MetricName: "scope_error_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: MetricLevel4CapabilityCoverage, Operator: ThresholdEqual, Value: 1, Unit: MetricUnitRatio},
				{MetricName: MetricEventCaptureCompleteness, Operator: ThresholdGreaterOrEqual, Value: 0.90, Unit: MetricUnitRatio},
			},
		},
		{
			ID:          "mvp_07_no_tool_output_pollution",
			Name:        "临时工具输出不污染长期记忆",
			Description: "验证 Admission 和 Retention 对工具输出的控制。",
			Metrics: []MetricThreshold{
				{MetricName: "full_output_storage_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: "temporary_output_long_term_rate", Operator: ThresholdLessOrEqual, Value: 0.05, Unit: MetricUnitRatio},
				{MetricName: "error_signature_accuracy", Operator: ThresholdGreaterOrEqual, Value: 0.80, Unit: MetricUnitRatio},
			},
		},
		{
			ID:          "mvp_08_code_index_boundary",
			Name:        "源码结构事实不混入普通 Memory",
			Description: "验证 Code Index 和 Memory 职责边界。",
			Metrics: []MetricThreshold{
				{MetricName: "code_structure_fact_memory_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: "code_ref_completeness", Operator: ThresholdGreaterOrEqual, Value: 0.90, Unit: MetricUnitRatio},
				{MetricName: "design_reason_memory_accuracy", Operator: ThresholdGreaterOrEqual, Value: 0.80, Unit: MetricUnitRatio},
			},
		},
		{
			ID:          "mvp_09_user_correction",
			Name:        "用户纠正后后续行为改变",
			Description: "验证纠错、负强化和版本化。",
			Metrics: []MetricThreshold{
				{MetricName: "corrected_preference_hit_rate", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitRatio},
				{MetricName: "old_preference_misuse_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: "supersedes_or_override_present", Operator: ThresholdEqual, Value: 1, Unit: MetricUnitCount},
			},
		},
		{
			ID:          "mvp_10_review_checkpoint_compression",
			Name:        "重复设计复查上下文压缩",
			Description: "验证 review_checkpoint 和文档 hash/diff-aware 复查。",
			Metrics: []MetricThreshold{
				{MetricName: MetricReviewContextTokenSavings, Operator: ThresholdGreaterOrEqual, Value: 0.60, Unit: MetricUnitRatio},
				{MetricName: "ignored_issue_repeated_count", Operator: ThresholdEqual, Value: 0, Unit: MetricUnitCount},
				{MetricName: "checkpoint_recall_accuracy", Operator: ThresholdGreaterOrEqual, Value: 0.90, Unit: MetricUnitRatio},
				{MetricName: "unchanged_doc_full_read_rate", Operator: ThresholdLessOrEqual, Value: 0.30, Unit: MetricUnitRatio},
			},
		},
	}
}

// FindScenario 按 ID 返回 scenario 定义。
func FindScenario(id string) (Scenario, bool) {
	for _, scenario := range ScenarioRegistry() {
		if scenario.ID == id {
			return scenario, true
		}
	}
	return Scenario{}, false
}
