package mvp

import "testing"

func TestTokenSavings(t *testing.T) {
	sample := TokenSavings("run_1", "mvp_01_task_continuation", 1000, 650, 0.30)
	if sample.MetricName != MetricTokenSavings || sample.Unit != MetricUnitRatio {
		t.Fatalf("sample identity = %+v, want token savings ratio", sample)
	}
	if sample.MetricValue != 0.35 || !sample.Passed {
		t.Fatalf("token savings = %+v, want 0.35 passed", sample)
	}

	invalid := TokenSavings("run_1", "mvp_01_task_continuation", 0, 100, 0.30)
	if invalid.Passed || invalid.MetricValue != 0 {
		t.Fatalf("invalid baseline sample = %+v, want not passed zero value", invalid)
	}
}

func TestWrongMemoryInjectionRate(t *testing.T) {
	passed := WrongMemoryInjectionRate("run_1", "mvp_03_decision_recall", 1, 40)
	if passed.MetricValue != 0.025 || !passed.Passed {
		t.Fatalf("wrong injection rate = %+v, want 0.025 passed", passed)
	}
	failed := WrongMemoryInjectionRate("run_1", "mvp_03_decision_recall", 2, 20)
	if failed.MetricValue != 0.1 || failed.Passed {
		t.Fatalf("wrong injection rate = %+v, want 0.1 failed", failed)
	}
}

func TestRetrievalLatencyP95MS(t *testing.T) {
	sample := RetrievalLatencyP95MS("run_1", []float64{10, 20, 30, 40, 50})
	if sample.MetricValue != 50 || !sample.Passed {
		t.Fatalf("p95 sample = %+v, want 50 passed", sample)
	}
	failed := RetrievalLatencyP95MS("run_1", []float64{10, 120, 130})
	if failed.MetricValue != 130 || failed.Passed {
		t.Fatalf("p95 sample = %+v, want 130 failed", failed)
	}
	missing := RetrievalLatencyP95MS("run_1", nil)
	if missing.Passed || missing.Denominator != 0 {
		t.Fatalf("missing p95 sample = %+v, want failed zero denominator", missing)
	}
}

func TestCapabilityCoverageAndCompleteness(t *testing.T) {
	coverage := CapabilityCoverage(AgentCapability{
		ConversationCapture: true,
		ToolCallCapture:     true,
		ToolOutputCapture:   true,
		FileEditCapture:     false,
		SessionLifecycle:    true,
		MemoryObserve:       true,
	})
	if coverage != 5.0/6.0 {
		t.Fatalf("coverage = %v, want 5/6", coverage)
	}
	if got := EventCaptureCompleteness(9, 10); got != 0.9 {
		t.Fatalf("completeness = %v, want 0.9", got)
	}
	if got := EventCaptureCompleteness(1, 0); got != 0 {
		t.Fatalf("zero denominator completeness = %v, want 0", got)
	}
}

func TestScenarioRegistry(t *testing.T) {
	scenarios := ScenarioRegistry()
	if len(scenarios) != 10 {
		t.Fatalf("scenario count = %d, want 10", len(scenarios))
	}
	seen := map[string]bool{}
	for _, scenario := range scenarios {
		if scenario.ID == "" || scenario.Name == "" || len(scenario.Metrics) == 0 {
			t.Fatalf("incomplete scenario = %+v", scenario)
		}
		if seen[scenario.ID] {
			t.Fatalf("duplicate scenario id %s", scenario.ID)
		}
		seen[scenario.ID] = true
	}
	if scenario, ok := FindScenario("mvp_06_cross_agent_sharing"); !ok || scenario.ID == "" {
		t.Fatalf("FindScenario(mvp_06_cross_agent_sharing) = %+v/%v, want found", scenario, ok)
	}
}

func TestBuildMetricSamplesFailsMissingCertificationAgents(t *testing.T) {
	samples := buildMetricSamples("run_1", nil, []AgentCapability{
		{
			ID:                  "cap_codex",
			RunID:               "run_1",
			AgentType:           AgentCodex,
			ConversationCapture: true,
			ToolCallCapture:     true,
			ToolOutputCapture:   true,
			FileEditCapture:     true,
			SessionLifecycle:    true,
			MemoryObserve:       true,
			CapabilityCoverage:  1,
			Completeness:        0.95,
		},
	}, nil)
	summary := summarizeMetrics(samples)
	if summary.AgentCertificationPassed {
		t.Fatalf("summary = %+v, want missing agents to fail certification", summary)
	}
	missingFailures := 0
	for _, sample := range samples {
		if (sample.AgentType == AgentClaudeCode || sample.AgentType == AgentCursor) && !sample.Passed {
			missingFailures++
		}
	}
	if missingFailures != 4 {
		t.Fatalf("missing failure samples = %d, want 4", missingFailures)
	}
}

func TestBuildMetricSamplesFailsWhenTaskFailedDespitePassingObservedMetrics(t *testing.T) {
	samples := buildMetricSamples("run_1", []AcceptanceTask{
		{
			ID:          "task_1",
			RunID:       "run_1",
			ScenarioID:  "mvp_01_task_continuation",
			AgentType:   AgentCodex,
			Status:      TaskStatusFailed,
			TaskSuccess: false,
			ObservedJSON: `{
				"repeated_explanation_count": 0,
				"task_state_recall": 1,
				"baseline_context_tokens": 1000,
				"candidate_context_tokens": 500
			}`,
		},
	}, fullTestCapabilities("run_1"), []float64{30})

	summary := summarizeMetrics(samples)
	if summary.EngineMVPPassed {
		t.Fatalf("summary = %+v, want failed task to fail engine mvp", summary)
	}
	var sawTaskSuccess, sawTokenSavings bool
	for _, sample := range samples {
		switch sample.MetricName {
		case MetricTaskSuccessRate:
			sawTaskSuccess = true
			if sample.Passed {
				t.Fatalf("task success sample = %+v, want failed", sample)
			}
		case MetricTokenSavings:
			sawTokenSavings = true
			if sample.Passed {
				t.Fatalf("token savings sample = %+v, want failed because task failed", sample)
			}
		}
	}
	if !sawTaskSuccess || !sawTokenSavings {
		t.Fatalf("samples = %+v, want task_success_rate and token_savings", samples)
	}
}

func fullTestCapabilities(runID string) []AgentCapability {
	out := make([]AgentCapability, 0, len(RequiredCertificationAgents()))
	for _, agentType := range RequiredCertificationAgents() {
		out = append(out, AgentCapability{
			ID:                  "cap_" + agentType,
			RunID:               runID,
			AgentType:           agentType,
			ConversationCapture: true,
			ToolCallCapture:     true,
			ToolOutputCapture:   true,
			FileEditCapture:     true,
			SessionLifecycle:    true,
			MemoryObserve:       true,
			CapabilityCoverage:  1,
			Completeness:        0.95,
		})
	}
	return out
}
