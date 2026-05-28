package mvp

import (
	"encoding/json"
	"fmt"
)

// SyntheticScenarioFixture 是 synthetic 验收的标准任务样本。
// 该 fixture 只包含结构化计数、比例和摘要，不包含完整对话、完整工具输出或完整 diff。
type SyntheticScenarioFixture struct {
	ScenarioID string
	AgentType  string
	Round      int
	LatencyMS  int64
	Expected   map[string]any
	Observed   map[string]any
}

// ExpectedJSON 返回可写入 mvp_acceptance_task.expected_json 的压缩 JSON。
func (f SyntheticScenarioFixture) ExpectedJSON() ([]byte, error) {
	return json.Marshal(f.Expected)
}

// ObservedJSON 返回可写入 mvp_acceptance_task.observed_json 的压缩 JSON。
func (f SyntheticScenarioFixture) ObservedJSON() ([]byte, error) {
	return json.Marshal(f.Observed)
}

// SyntheticScenarioFixtures 返回 synthetic Engine MVP 的 10 个标准 scenario 样本。
// 设计说明：目标是验证验收链路可执行，不模拟真实 Agent 能力；真实 Agent certification 由认证流程处理。
func SyntheticScenarioFixtures() []SyntheticScenarioFixture {
	return []SyntheticScenarioFixture{
		{
			ScenarioID: "mvp_01_task_continuation",
			AgentType:  AgentCodex,
			Round:      2,
			LatencyMS:  35,
			Expected:   map[string]any{"memory_types": []string{"session_summary", "constraint"}, "required_scope": "project_local"},
			Observed: map[string]any{
				"repeated_explanation_count": 0,
				"task_state_recall":          1,
				"baseline_context_tokens":    1200,
				"candidate_context_tokens":   720,
			},
		},
		{
			ScenarioID: "mvp_02_user_preference",
			AgentType:  AgentClaudeCode,
			Round:      2,
			LatencyMS:  28,
			Expected:   map[string]any{"memory_types": []string{"preference"}, "required_scope": "user_global"},
			Observed: map[string]any{
				"preference_recall_accuracy":  1,
				"repeated_explanation_count":  0,
				"wrong_scope_injection_count": 0,
			},
		},
		{
			ScenarioID: "mvp_03_decision_recall",
			AgentType:  AgentCursor,
			Round:      2,
			LatencyMS:  42,
			Expected:   map[string]any{"memory_types": []string{"decision"}, "required_evidence": true},
			Observed: map[string]any{
				MetricDecisionRecallAccuracy:  0.90,
				"evidence_faithfulness":       0.95,
				"pending_state_mark_rate":     1,
				"wrong_memory_injected_count": 0,
				"injected_memory_count":       4,
			},
		},
		{
			ScenarioID: "mvp_04_failure_recall",
			AgentType:  AgentCodex,
			Round:      2,
			LatencyMS:  38,
			Expected:   map[string]any{"memory_types": []string{"failure", "procedure"}},
			Observed: map[string]any{
				"failure_memory_recall":          1,
				"user_correction_reduction":      0.75,
				"old_wrong_strategy_reuse_count": 0,
			},
		},
		{
			ScenarioID: "mvp_05_temporal_validity",
			AgentType:  AgentClaudeCode,
			Round:      2,
			LatencyMS:  40,
			Expected:   map[string]any{"current_fact": "PostgreSQL", "stale_fact": "MySQL"},
			Observed: map[string]any{
				"temporal_correctness":      1,
				"stale_memory_misuse_count": 0,
				"supersedes_link_present":   1,
			},
		},
		{
			ScenarioID: "mvp_06_cross_agent_sharing",
			AgentType:  AgentCursor,
			Round:      2,
			LatencyMS:  45,
			Expected:   map[string]any{"agents": []string{AgentCodex, AgentClaudeCode, AgentCursor}},
			Observed: map[string]any{
				MetricCrossAgentRecallSuccessRate: 1,
				"scope_error_count":               0,
			},
		},
		{
			ScenarioID: "mvp_07_no_tool_output_pollution",
			AgentType:  AgentCodex,
			Round:      2,
			LatencyMS:  31,
			Expected:   map[string]any{"full_output_storage": false},
			Observed: map[string]any{
				"full_output_storage_count":       0,
				"temporary_output_long_term_rate": 0.02,
				"error_signature_accuracy":        0.90,
			},
		},
		{
			ScenarioID: "mvp_08_code_index_boundary",
			AgentType:  AgentClaudeCode,
			Round:      2,
			LatencyMS:  48,
			Expected:   map[string]any{"code_ref_required": true, "memory_code_structure_fact": false},
			Observed: map[string]any{
				"code_structure_fact_memory_count": 0,
				"code_ref_completeness":            0.95,
				"design_reason_memory_accuracy":    0.90,
			},
		},
		{
			ScenarioID: "mvp_09_user_correction",
			AgentType:  AgentCursor,
			Round:      2,
			LatencyMS:  33,
			Expected:   map[string]any{"old_preference": "Redis first", "new_preference": "local cache first when enough"},
			Observed: map[string]any{
				"corrected_preference_hit_rate":  1,
				"old_preference_misuse_count":    0,
				"supersedes_or_override_present": 1,
			},
		},
		{
			ScenarioID: "mvp_10_review_checkpoint_compression",
			AgentType:  AgentCodex,
			Round:      2,
			LatencyMS:  52,
			Expected:   map[string]any{"memory_types": []string{"review_checkpoint"}, "doc_hash_strategy": true},
			Observed: map[string]any{
				"review_baseline_context_tokens":  2400,
				"review_candidate_context_tokens": 720,
				"ignored_issue_repeated_count":    0,
				"checkpoint_recall_accuracy":      0.95,
				"unchanged_doc_full_read_rate":    0.20,
			},
		},
	}
}

// SyntheticTraceID 为 fixture 生成稳定 trace id，便于测试和报告定位。
func SyntheticTraceID(index int) string {
	return fmt.Sprintf("rt_p5_synthetic_%02d", index+1)
}
