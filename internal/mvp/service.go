package mvp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const maxMVPJSONSummaryRunes = 4096

// Service 提供 P5 MVP 验收模型的应用层入口。
// P5-A 只负责 run/task 记录和基础模型，不在这里聚合 P2/P4 诊断或生成报告。
type Service struct {
	repo Repository
}

// NewService 创建 P5 MVP service。
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

type StartRunRequest struct {
	Name          string `json:"name"`
	Mode          string `json:"mode,omitempty"`
	WorkspaceID   string `json:"workspace_id"`
	ProjectID     string `json:"project_id,omitempty"`
	RepoID        string `json:"repo_id,omitempty"`
	BaselineType  string `json:"baseline_type,omitempty"`
	CandidateType string `json:"candidate_type,omitempty"`
}

type StartRunResponse struct {
	RequestID string `json:"request_id,omitempty"`
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
}

type RecordTaskRequest struct {
	RunID            string          `json:"run_id"`
	ScenarioID       string          `json:"scenario_id"`
	Round            int             `json:"round"`
	AgentType        string          `json:"agent_type"`
	Baseline         bool            `json:"baseline"`
	SessionID        string          `json:"session_id,omitempty"`
	TaskID           string          `json:"task_id,omitempty"`
	RetrievalTraceID string          `json:"retrieval_trace_id,omitempty"`
	Status           string          `json:"status,omitempty"`
	TaskSuccess      bool            `json:"task_success"`
	Expected         json.RawMessage `json:"expected,omitempty"`
	Observed         json.RawMessage `json:"observed,omitempty"`
	FailureReason    string          `json:"failure_reason,omitempty"`
}

type RecordTaskResponse struct {
	RequestID    string `json:"request_id,omitempty"`
	TaskResultID string `json:"task_result_id"`
	Accepted     bool   `json:"accepted"`
}

type RecordCapabilityRequest struct {
	RunID               string          `json:"run_id"`
	AgentType           string          `json:"agent_type"`
	AdapterName         string          `json:"adapter_name,omitempty"`
	AdapterVersion      string          `json:"adapter_version,omitempty"`
	CaptureLevel        int             `json:"capture_level"`
	ConversationCapture bool            `json:"conversation_capture"`
	ToolCallCapture     bool            `json:"tool_call_capture"`
	ToolOutputCapture   bool            `json:"tool_output_capture"`
	FileEditCapture     bool            `json:"file_edit_capture"`
	SessionLifecycle    bool            `json:"session_lifecycle"`
	MemoryObserve       bool            `json:"memory_observe"`
	Completeness        float64         `json:"completeness"`
	DegradationReasons  json.RawMessage `json:"degradation_reasons,omitempty"`
}

type RecordCapabilityResponse struct {
	RequestID          string  `json:"request_id,omitempty"`
	CapabilityID       string  `json:"capability_id"`
	AgentType          string  `json:"agent_type"`
	CapabilityCoverage float64 `json:"capability_coverage"`
	Completeness       float64 `json:"completeness"`
	Accepted           bool    `json:"accepted"`
}

// StartRun 创建一次 P5 验收 run。
func (s *Service) StartRun(ctx context.Context, req StartRunRequest) (StartRunResponse, error) {
	if s == nil || s.repo == nil {
		return StartRunResponse{}, fmt.Errorf("MVP_SERVICE_UNAVAILABLE: repository is nil")
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.WorkspaceID) == "" {
		return StartRunResponse{}, fmt.Errorf("VALIDATION_FAILED: name and workspace_id are required")
	}
	if !validRunMode(req.Mode) || !validBaselineType(req.BaselineType) || !validCandidateType(req.CandidateType) {
		return StartRunResponse{}, fmt.Errorf("VALIDATION_FAILED: invalid mvp run mode or baseline/candidate type")
	}
	run, err := s.repo.CreateRun(ctx, AcceptanceRun{
		Name:          req.Name,
		Mode:          req.Mode,
		WorkspaceID:   req.WorkspaceID,
		ProjectID:     req.ProjectID,
		RepoID:        req.RepoID,
		BaselineType:  req.BaselineType,
		CandidateType: req.CandidateType,
	})
	if err != nil {
		return StartRunResponse{}, err
	}
	return StartRunResponse{RunID: run.ID, Status: run.Status}, nil
}

// RecordTask 记录单个 scenario 的执行结果。
func (s *Service) RecordTask(ctx context.Context, req RecordTaskRequest) (RecordTaskResponse, error) {
	if s == nil || s.repo == nil {
		return RecordTaskResponse{}, fmt.Errorf("MVP_SERVICE_UNAVAILABLE: repository is nil")
	}
	if _, err := s.repo.GetRun(ctx, req.RunID); err != nil {
		return RecordTaskResponse{}, err
	}
	if _, ok := FindScenario(req.ScenarioID); !ok {
		return RecordTaskResponse{}, fmt.Errorf("VALIDATION_FAILED: unknown mvp scenario_id")
	}
	if !IsCertificationAgent(req.AgentType) {
		return RecordTaskResponse{}, fmt.Errorf("VALIDATION_FAILED: unsupported p5 agent_type")
	}
	if !IsTaskStatus(req.Status) {
		return RecordTaskResponse{}, fmt.Errorf("VALIDATION_FAILED: invalid task status")
	}
	expected, err := compactRawJSON(req.Expected)
	if err != nil {
		return RecordTaskResponse{}, err
	}
	observed, err := compactRawJSON(req.Observed)
	if err != nil {
		return RecordTaskResponse{}, err
	}
	task, err := s.repo.RecordTask(ctx, AcceptanceTask{
		RunID:            req.RunID,
		ScenarioID:       req.ScenarioID,
		Round:            req.Round,
		AgentType:        req.AgentType,
		Baseline:         req.Baseline,
		SessionID:        req.SessionID,
		TaskID:           req.TaskID,
		RetrievalTraceID: req.RetrievalTraceID,
		Status:           req.Status,
		TaskSuccess:      req.TaskSuccess,
		ExpectedJSON:     expected,
		ObservedJSON:     observed,
		FailureReason:    compactString(req.FailureReason, maxMVPJSONSummaryRunes),
	})
	if err != nil {
		return RecordTaskResponse{}, err
	}
	return RecordTaskResponse{TaskResultID: task.ID, Accepted: true}, nil
}

// RecordCapability 记录 P5-D 单个 Agent 的捕获能力快照。
func (s *Service) RecordCapability(ctx context.Context, req RecordCapabilityRequest) (RecordCapabilityResponse, error) {
	if s == nil || s.repo == nil {
		return RecordCapabilityResponse{}, fmt.Errorf("MVP_SERVICE_UNAVAILABLE: repository is nil")
	}
	if _, err := s.repo.GetRun(ctx, req.RunID); err != nil {
		return RecordCapabilityResponse{}, err
	}
	if !IsCertificationAgent(req.AgentType) {
		return RecordCapabilityResponse{}, fmt.Errorf("VALIDATION_FAILED: unsupported p5 agent_type")
	}
	if req.CaptureLevel < 1 || req.CaptureLevel > 4 {
		return RecordCapabilityResponse{}, fmt.Errorf("VALIDATION_FAILED: capture_level must be between 1 and 4")
	}
	if req.Completeness < 0 || req.Completeness > 1 {
		return RecordCapabilityResponse{}, fmt.Errorf("VALIDATION_FAILED: completeness must be between 0 and 1")
	}
	degradationReasons, err := compactRawJSON(req.DegradationReasons)
	if err != nil {
		return RecordCapabilityResponse{}, err
	}
	capability := AgentCapability{
		RunID:                  req.RunID,
		AgentType:              req.AgentType,
		AdapterName:            req.AdapterName,
		AdapterVersion:         req.AdapterVersion,
		CaptureLevel:           req.CaptureLevel,
		ConversationCapture:    req.ConversationCapture,
		ToolCallCapture:        req.ToolCallCapture,
		ToolOutputCapture:      req.ToolOutputCapture,
		FileEditCapture:        req.FileEditCapture,
		SessionLifecycle:       req.SessionLifecycle,
		MemoryObserve:          req.MemoryObserve,
		Completeness:           req.Completeness,
		DegradationReasonsJSON: degradationReasons,
	}
	coverage := CapabilityCoverage(capability)
	if (coverage < 1 || req.Completeness < 0.90) && degradationReasons == "" {
		return RecordCapabilityResponse{}, fmt.Errorf("VALIDATION_FAILED: degradation_reasons are required for degraded agent capability")
	}
	written, err := s.repo.UpsertAgentCapability(ctx, capability)
	if err != nil {
		return RecordCapabilityResponse{}, err
	}
	return RecordCapabilityResponse{
		CapabilityID:       written.ID,
		AgentType:          written.AgentType,
		CapabilityCoverage: written.CapabilityCoverage,
		Completeness:       written.Completeness,
		Accepted:           true,
	}, nil
}

// ComputeMetrics 聚合 P5 run 下的 task、trace 和 capability，生成可持久化指标样本。
func (s *Service) ComputeMetrics(ctx context.Context, req ComputeMetricsRequest) (ComputeMetricsResponse, error) {
	if s == nil || s.repo == nil {
		return ComputeMetricsResponse{}, fmt.Errorf("MVP_SERVICE_UNAVAILABLE: repository is nil")
	}
	run, err := s.repo.GetRun(ctx, req.RunID)
	if err != nil {
		return ComputeMetricsResponse{}, err
	}
	tasks, err := s.repo.ListAcceptanceTasks(ctx, TaskQuery{RunID: run.ID, Limit: 500})
	if err != nil {
		return ComputeMetricsResponse{}, err
	}
	capabilities, err := s.repo.ListAgentCapabilities(ctx, CapabilityQuery{RunID: run.ID, Limit: 20})
	if err != nil {
		return ComputeMetricsResponse{}, err
	}
	traceIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.RetrievalTraceID != "" {
			traceIDs = append(traceIDs, task.RetrievalTraceID)
		}
	}
	latencies, err := s.repo.ListRetrievalLatenciesByTraceIDs(ctx, traceIDs)
	if err != nil {
		return ComputeMetricsResponse{}, err
	}
	samples := buildMetricSamples(run.ID, tasks, capabilities, latencies)
	written, err := s.repo.UpsertMetricSamples(ctx, samples)
	if err != nil {
		return ComputeMetricsResponse{}, err
	}
	summary := summarizeMetrics(written)
	status := statusFromSummary(summary)
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return ComputeMetricsResponse{}, fmt.Errorf("VALIDATION_FAILED: encode mvp summary: %w", err)
	}
	if err := s.repo.UpdateRunStatus(ctx, AcceptanceRun{
		ID:          run.ID,
		Status:      status,
		SummaryJSON: string(summaryJSON),
		ReportPath:  run.ReportPath,
	}); err != nil {
		return ComputeMetricsResponse{}, err
	}
	return ComputeMetricsResponse{
		RunID:   run.ID,
		Status:  status,
		Metrics: metricDiagnostics(written),
		Summary: summary,
	}, nil
}

// Report 生成 P5-B 内存报告。文件写入由后续 P5-C 脚本或 CLI 负责，避免服务层写入任意路径。
func (s *Service) Report(ctx context.Context, req ReportRequest) (ReportResponse, error) {
	if s == nil || s.repo == nil {
		return ReportResponse{}, fmt.Errorf("MVP_SERVICE_UNAVAILABLE: repository is nil")
	}
	run, err := s.repo.GetRun(ctx, req.RunID)
	if err != nil {
		return ReportResponse{}, err
	}
	metrics, err := s.repo.ListMetricSamples(ctx, MetricQuery{RunID: run.ID, Limit: 500})
	if err != nil {
		return ReportResponse{}, err
	}
	capabilities, err := s.repo.ListAgentCapabilities(ctx, CapabilityQuery{RunID: run.ID, Limit: 20})
	if err != nil {
		return ReportResponse{}, err
	}
	var tasks []AcceptanceTask
	if req.IncludeFailures {
		tasks, err = s.repo.ListAcceptanceTasks(ctx, TaskQuery{RunID: run.ID, Limit: 500})
		if err != nil {
			return ReportResponse{}, err
		}
	}
	summary := summarizeMetrics(metrics)
	format := req.Format
	if format == "" {
		format = "markdown"
	}
	var report string
	switch format {
	case "markdown":
		report = renderMarkdownReport(run, summary, metrics, capabilities, tasks, req.IncludeFailures)
	case "json":
		report, err = renderJSONReport(run, summary, metrics, capabilities, tasks, req.IncludeFailures)
		if err != nil {
			return ReportResponse{}, err
		}
	default:
		return ReportResponse{}, fmt.Errorf("VALIDATION_FAILED: unsupported report format")
	}
	return ReportResponse{
		RunID:      run.ID,
		Status:     run.Status,
		ReportPath: run.ReportPath,
		Summary:    summary,
		Report:     report,
	}, nil
}

func validRunMode(value string) bool {
	switch value {
	case "", RunModeSynthetic, RunModeRealAgent, RunModeMixed:
		return true
	default:
		return false
	}
}

func validBaselineType(value string) bool {
	switch value {
	case "", BaselineNoMemory, BaselineFullChatHistory, BaselineSummaryOnly:
		return true
	default:
		return false
	}
}

func validCandidateType(value string) bool {
	switch value {
	case "", CandidateHybridMemory:
		return true
	default:
		return false
	}
}

func buildMetricSamples(runID string, tasks []AcceptanceTask, capabilities []AgentCapability, latencies []float64) []MetricSample {
	samples := make([]MetricSample, 0)
	for _, task := range tasks {
		observed := decodeObservedNumbers(task.ObservedJSON)
		scenario, _ := FindScenario(task.ScenarioID)
		taskSuccess := taskMetricPassed(task)
		samples = append(samples, MetricSample{
			RunID:             runID,
			ScenarioID:        task.ScenarioID,
			TaskResultID:      task.ID,
			AgentType:         task.AgentType,
			MetricName:        MetricTaskSuccessRate,
			MetricValue:       boolAsFloat(taskSuccess),
			Numerator:         boolAsFloat(taskSuccess),
			Denominator:       1,
			Unit:              MetricUnitRatio,
			ThresholdValue:    1,
			ThresholdOperator: ThresholdEqual,
			Passed:            taskSuccess,
			SourceJSON:        metricSourceJSON("mvp_acceptance_task", task.ID),
		})
		for _, threshold := range scenario.Metrics {
			if value, ok := observed[threshold.MetricName]; ok {
				samples = append(samples, MetricSample{
					RunID:             runID,
					ScenarioID:        task.ScenarioID,
					TaskResultID:      task.ID,
					AgentType:         task.AgentType,
					MetricName:        threshold.MetricName,
					MetricValue:       value,
					Numerator:         value,
					Denominator:       1,
					Unit:              threshold.Unit,
					ThresholdValue:    threshold.Value,
					ThresholdOperator: threshold.Operator,
					Passed:            taskSuccess && CompareThreshold(value, threshold.Operator, threshold.Value),
					SourceJSON:        metricSourceJSON("observed_json", task.ID),
				})
			}
		}
		if baseline, ok := observed["baseline_context_tokens"]; ok {
			if candidate, ok := observed["candidate_context_tokens"]; ok {
				sample := TokenSavings(runID, task.ScenarioID, baseline, candidate, 0.30)
				sample.TaskResultID = task.ID
				sample.AgentType = task.AgentType
				sample.SourceJSON = metricSourceJSON("observed_json", task.ID)
				sample.Passed = taskSuccess && sample.Passed
				samples = append(samples, sample)
			}
		}
		if baseline, ok := observed["review_baseline_context_tokens"]; ok {
			if candidate, ok := observed["review_candidate_context_tokens"]; ok {
				sample := TokenSavings(runID, task.ScenarioID, baseline, candidate, 0.60)
				sample.MetricName = MetricReviewContextTokenSavings
				sample.TaskResultID = task.ID
				sample.AgentType = task.AgentType
				sample.SourceJSON = metricSourceJSON("observed_json", task.ID)
				sample.Passed = taskSuccess && sample.Passed
				samples = append(samples, sample)
			}
		}
		if wrong, ok := observed["wrong_memory_injected_count"]; ok {
			if injected, ok := observed["injected_memory_count"]; ok {
				sample := WrongMemoryInjectionRate(runID, task.ScenarioID, wrong, injected)
				sample.TaskResultID = task.ID
				sample.AgentType = task.AgentType
				sample.SourceJSON = metricSourceJSON("observed_json", task.ID)
				sample.Passed = taskSuccess && sample.Passed
				samples = append(samples, sample)
			}
		}
	}
	samples = append(samples, RetrievalLatencyP95MS(runID, latencies))
	seenAgents := map[string]bool{}
	for _, capability := range capabilities {
		seenAgents[capability.AgentType] = true
		samples = append(samples, MetricSample{
			RunID:             runID,
			AgentType:         capability.AgentType,
			MetricName:        MetricLevel4CapabilityCoverage,
			MetricValue:       capability.CapabilityCoverage,
			Numerator:         capability.CapabilityCoverage,
			Denominator:       1,
			Unit:              MetricUnitRatio,
			ThresholdValue:    1,
			ThresholdOperator: ThresholdEqual,
			Passed:            CompareThreshold(capability.CapabilityCoverage, ThresholdEqual, 1),
			SourceJSON:        metricSourceJSON("mvp_agent_capability", capability.ID),
		})
		samples = append(samples, MetricSample{
			RunID:             runID,
			AgentType:         capability.AgentType,
			MetricName:        MetricEventCaptureCompleteness,
			MetricValue:       capability.Completeness,
			Numerator:         capability.Completeness,
			Denominator:       1,
			Unit:              MetricUnitRatio,
			ThresholdValue:    0.90,
			ThresholdOperator: ThresholdGreaterOrEqual,
			Passed:            CompareThreshold(capability.Completeness, ThresholdGreaterOrEqual, 0.90),
			SourceJSON:        metricSourceJSON("mvp_agent_capability", capability.ID),
		})
	}
	for _, agentType := range RequiredCertificationAgents() {
		if seenAgents[agentType] {
			continue
		}
		samples = append(samples, missingAgentMetric(runID, agentType, MetricLevel4CapabilityCoverage, 1, ThresholdEqual))
		samples = append(samples, missingAgentMetric(runID, agentType, MetricEventCaptureCompleteness, 0.90, ThresholdGreaterOrEqual))
	}
	return samples
}

func missingAgentMetric(runID, agentType, metricName string, threshold float64, operator string) MetricSample {
	return MetricSample{
		RunID:             runID,
		AgentType:         agentType,
		MetricName:        metricName,
		MetricValue:       0,
		Numerator:         0,
		Denominator:       1,
		Unit:              MetricUnitRatio,
		ThresholdValue:    threshold,
		ThresholdOperator: operator,
		Passed:            false,
		SourceJSON:        metricSourceJSON("missing_agent_capability", agentType),
	}
}

func taskMetricPassed(task AcceptanceTask) bool {
	if !task.TaskSuccess {
		return false
	}
	switch task.Status {
	case "", TaskStatusPassed:
		return true
	default:
		return false
	}
}

func boolAsFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func decodeObservedNumbers(value string) map[string]float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil
	}
	out := make(map[string]float64, len(raw))
	for key, item := range raw {
		switch typed := item.(type) {
		case float64:
			out[key] = typed
		case bool:
			if typed {
				out[key] = 1
			} else {
				out[key] = 0
			}
		}
	}
	return out
}

func summarizeMetrics(samples []MetricSample) MetricsSummary {
	summary := MetricsSummary{MetricCount: len(samples)}
	if len(samples) == 0 {
		return summary
	}
	enginePassed := true
	agentPassed := true
	seenEngine := false
	seenAgent := false
	for _, sample := range samples {
		if sample.Passed {
			summary.PassedMetrics++
		} else {
			summary.FailedMetrics++
		}
		if sample.MetricName == MetricLevel4CapabilityCoverage || sample.MetricName == MetricEventCaptureCompleteness {
			seenAgent = true
			if !sample.Passed {
				agentPassed = false
			}
			continue
		}
		seenEngine = true
		if !sample.Passed {
			enginePassed = false
		}
	}
	summary.EngineMVPPassed = seenEngine && enginePassed
	summary.AgentCertificationPassed = seenAgent && agentPassed
	return summary
}

func statusFromSummary(summary MetricsSummary) string {
	if summary.MetricCount == 0 {
		return RunStatusPartial
	}
	if summary.FailedMetrics == 0 && summary.EngineMVPPassed && summary.AgentCertificationPassed {
		return RunStatusPassed
	}
	if summary.EngineMVPPassed {
		return RunStatusPartial
	}
	return RunStatusFailed
}

func metricDiagnostics(samples []MetricSample) []MetricDiagnostic {
	diagnostics := make([]MetricDiagnostic, len(samples))
	for i, sample := range samples {
		diagnostics[i] = MetricDiagnostic{
			MetricName:        sample.MetricName,
			ScenarioID:        sample.ScenarioID,
			AgentType:         sample.AgentType,
			MetricValue:       sample.MetricValue,
			ThresholdOperator: sample.ThresholdOperator,
			ThresholdValue:    sample.ThresholdValue,
			Passed:            sample.Passed,
			Unit:              sample.Unit,
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].MetricName == diagnostics[j].MetricName {
			return diagnostics[i].ScenarioID < diagnostics[j].ScenarioID
		}
		return diagnostics[i].MetricName < diagnostics[j].MetricName
	})
	return diagnostics
}

func metricSourceJSON(sourceType, sourceID string) string {
	data, _ := json.Marshal(map[string]string{"source_type": sourceType, "source_id": sourceID})
	return string(data)
}

func renderMarkdownReport(run AcceptanceRun, summary MetricsSummary, metrics []MetricSample, capabilities []AgentCapability, tasks []AcceptanceTask, includeFailures bool) string {
	var buf bytes.Buffer
	buf.WriteString("# P5 MVP Acceptance Report\n\n")
	buf.WriteString(fmt.Sprintf("- run_id: `%s`\n", run.ID))
	buf.WriteString(fmt.Sprintf("- status: `%s`\n", run.Status))
	buf.WriteString(fmt.Sprintf("- mode: `%s`\n", run.Mode))
	buf.WriteString(fmt.Sprintf("- scope: `%s/%s/%s`\n\n", run.WorkspaceID, run.ProjectID, run.RepoID))
	buf.WriteString("## Summary\n\n")
	buf.WriteString(fmt.Sprintf("- metrics: %d\n", summary.MetricCount))
	buf.WriteString(fmt.Sprintf("- passed: %d\n", summary.PassedMetrics))
	buf.WriteString(fmt.Sprintf("- failed: %d\n", summary.FailedMetrics))
	buf.WriteString(fmt.Sprintf("- engine_mvp_passed: `%t`\n", summary.EngineMVPPassed))
	buf.WriteString(fmt.Sprintf("- agent_certification_passed: `%t`\n\n", summary.AgentCertificationPassed))
	buf.WriteString("## Metrics\n\n")
	buf.WriteString("| Metric | Scenario | Agent | Value | Threshold | Passed |\n")
	buf.WriteString("|---|---|---|---:|---|---|\n")
	for _, metric := range sortedMetrics(metrics) {
		buf.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | %.4f | `%s %.4f` | `%t` |\n",
			metric.MetricName, metric.ScenarioID, metric.AgentType, metric.MetricValue,
			metric.ThresholdOperator, metric.ThresholdValue, metric.Passed))
	}
	buf.WriteString("\n## Agent Capability\n\n")
	buf.WriteString("| Agent | Level | Coverage | Completeness | Degradation |\n")
	buf.WriteString("|---|---:|---:|---:|---|\n")
	for _, capability := range capabilities {
		buf.WriteString(fmt.Sprintf("| `%s` | %d | %.4f | %.4f | `%s` |\n",
			capability.AgentType, capability.CaptureLevel, capability.CapabilityCoverage,
			capability.Completeness, compactString(capability.DegradationReasonsJSON, 240)))
	}
	if includeFailures {
		buf.WriteString("\n## Failure Details\n\n")
		buf.WriteString("| Scenario | Agent | Status | Task Success | Reason |\n")
		buf.WriteString("|---|---|---|---:|---|\n")
		for _, task := range failedTasks(tasks) {
			buf.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | `%t` | `%s` |\n",
				task.ScenarioID, task.AgentType, task.Status, task.TaskSuccess, compactString(task.FailureReason, 240)))
		}
	}
	return buf.String()
}

func renderJSONReport(run AcceptanceRun, summary MetricsSummary, metrics []MetricSample, capabilities []AgentCapability, tasks []AcceptanceTask, includeFailures bool) (string, error) {
	payload := map[string]any{
		"run":          run,
		"summary":      summary,
		"metrics":      metrics,
		"capabilities": capabilities,
	}
	if includeFailures {
		payload["failures"] = failedTasks(tasks)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: encode mvp report: %w", err)
	}
	return string(data), nil
}

func sortedMetrics(metrics []MetricSample) []MetricSample {
	out := append([]MetricSample(nil), metrics...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].MetricName == out[j].MetricName {
			return out[i].ScenarioID < out[j].ScenarioID
		}
		return out[i].MetricName < out[j].MetricName
	})
	return out
}

func failedTasks(tasks []AcceptanceTask) []AcceptanceTask {
	out := make([]AcceptanceTask, 0)
	for _, task := range tasks {
		if !taskMetricPassed(task) || task.FailureReason != "" {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ScenarioID == out[j].ScenarioID {
			return out[i].AgentType < out[j].AgentType
		}
		return out[i].ScenarioID < out[j].ScenarioID
	})
	return out
}

func compactRawJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("VALIDATION_FAILED: invalid json summary")
	}
	return compactString(string(raw), maxMVPJSONSummaryRunes), nil
}

func compactString(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
