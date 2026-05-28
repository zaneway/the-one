package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/mvp"
)

const defaultMVPLimit = 50

// CreateRun 创建一次 MVP 验收 run。
// 设计约束：run 只保存验收摘要和 scope，不保存完整 prompt、完整输出或完整对话。
func (s *Store) CreateRun(ctx context.Context, run mvp.AcceptanceRun) (mvp.AcceptanceRun, error) {
	if strings.TrimSpace(run.Name) == "" || strings.TrimSpace(run.WorkspaceID) == "" {
		return mvp.AcceptanceRun{}, fmt.Errorf("VALIDATION_FAILED: name and workspace_id are required")
	}
	if run.ID == "" {
		id, err := idgen.New("mvp_run")
		if err != nil {
			return mvp.AcceptanceRun{}, err
		}
		run.ID = id
	}
	if run.Mode == "" {
		run.Mode = mvp.RunModeSynthetic
	}
	if run.BaselineType == "" {
		run.BaselineType = mvp.BaselineSummaryOnly
	}
	if run.CandidateType == "" {
		run.CandidateType = mvp.CandidateHybridMemory
	}
	if run.Status == "" {
		run.Status = mvp.RunStatusRunning
	}
	now := time.Now().UTC()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `insert into mvp_acceptance_run(
		id, name, mode, workspace_id, project_id, repo_id, baseline_type,
		candidate_type, status, started_at, ended_at, summary_json, report_path,
		created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.Name, run.Mode, run.WorkspaceID, nullString(run.ProjectID), nullString(run.RepoID),
		run.BaselineType, run.CandidateType, run.Status, run.StartedAt.Format(time.RFC3339Nano),
		nullableTime(run.EndedAt), nullString(run.SummaryJSON), nullString(run.ReportPath),
		run.CreatedAt.Format(time.RFC3339Nano), run.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return mvp.AcceptanceRun{}, storageErr(err)
	}
	return run, nil
}

// GetRun 按 run_id 读取一次验收 run。
func (s *Store) GetRun(ctx context.Context, runID string) (mvp.AcceptanceRun, error) {
	if strings.TrimSpace(runID) == "" {
		return mvp.AcceptanceRun{}, fmt.Errorf("VALIDATION_FAILED: run_id is required")
	}
	row := s.db.QueryRowContext(ctx, baseMVPRunSelect()+` where id = ?`, runID)
	run, err := scanMVPRun(row)
	if err == sql.ErrNoRows {
		return mvp.AcceptanceRun{}, fmt.Errorf("MVP_RUN_NOT_FOUND: %s", runID)
	}
	return run, storageErr(err)
}

// UpdateRunStatus 更新验收 run 的收口状态和报告摘要。
func (s *Store) UpdateRunStatus(ctx context.Context, run mvp.AcceptanceRun) error {
	if strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.Status) == "" {
		return fmt.Errorf("VALIDATION_FAILED: run id and status are required")
	}
	now := time.Now().UTC()
	endedAt := run.EndedAt
	if endedAt.IsZero() && run.Status != mvp.RunStatusRunning {
		endedAt = now
	}
	result, err := s.db.ExecContext(ctx, `update mvp_acceptance_run set
		status = ?,
		ended_at = ?,
		summary_json = ?,
		report_path = ?,
		updated_at = ?
		where id = ?`,
		run.Status, nullableTime(endedAt), nullString(run.SummaryJSON), nullString(run.ReportPath),
		now.Format(time.RFC3339Nano), run.ID,
	)
	if err != nil {
		return storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageErr(err)
	}
	if affected == 0 {
		return fmt.Errorf("MVP_RUN_NOT_FOUND: %s", run.ID)
	}
	return nil
}

// ListRuns 按 workspace/scope 查询验收 run。WorkspaceID 必填，避免验收历史无边界扫描。
func (s *Store) ListRuns(ctx context.Context, query mvp.RunQuery) ([]mvp.AcceptanceRun, error) {
	if strings.TrimSpace(query.WorkspaceID) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: workspace_id is required")
	}
	sqlText := baseMVPRunSelect() + ` where workspace_id = ?`
	args := []any{query.WorkspaceID}
	if query.ProjectID != "" {
		sqlText += " and coalesce(project_id, '') = ?"
		args = append(args, query.ProjectID)
	}
	if query.RepoID != "" {
		sqlText += " and coalesce(repo_id, '') = ?"
		args = append(args, query.RepoID)
	}
	if query.Status != "" {
		sqlText += " and status = ?"
		args = append(args, query.Status)
	}
	sqlText += " order by started_at desc limit ?"
	args = append(args, mvpLimit(query.Limit))
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanMVPRunRows(rows)
}

// RecordTask 记录单个 MVP scenario 的一轮执行结果。
func (s *Store) RecordTask(ctx context.Context, task mvp.AcceptanceTask) (mvp.AcceptanceTask, error) {
	if strings.TrimSpace(task.RunID) == "" || strings.TrimSpace(task.ScenarioID) == "" || strings.TrimSpace(task.AgentType) == "" {
		return mvp.AcceptanceTask{}, fmt.Errorf("VALIDATION_FAILED: run_id, scenario_id and agent_type are required")
	}
	if _, err := s.GetRun(ctx, task.RunID); err != nil {
		return mvp.AcceptanceTask{}, err
	}
	if _, ok := mvp.FindScenario(task.ScenarioID); !ok {
		return mvp.AcceptanceTask{}, fmt.Errorf("VALIDATION_FAILED: unknown mvp scenario_id")
	}
	if !mvp.IsCertificationAgent(task.AgentType) {
		return mvp.AcceptanceTask{}, fmt.Errorf("VALIDATION_FAILED: unsupported p5 agent_type")
	}
	if !mvp.IsTaskStatus(task.Status) {
		return mvp.AcceptanceTask{}, fmt.Errorf("VALIDATION_FAILED: invalid task status")
	}
	if task.ID == "" {
		id, err := idgen.New("mvp_task")
		if err != nil {
			return mvp.AcceptanceTask{}, err
		}
		task.ID = id
	}
	if task.Round <= 0 {
		task.Round = 1
	}
	if task.Status == "" {
		if task.TaskSuccess {
			task.Status = mvp.TaskStatusPassed
		} else {
			task.Status = mvp.TaskStatusFailed
		}
	}
	now := time.Now().UTC()
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `insert into mvp_acceptance_task(
		id, run_id, scenario_id, round, agent_type, baseline, session_id, task_id,
		retrieval_trace_id, status, task_success, expected_json, observed_json,
		failure_reason, started_at, ended_at, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.RunID, task.ScenarioID, task.Round, task.AgentType, task.Baseline,
		nullString(task.SessionID), nullString(task.TaskID), nullString(task.RetrievalTraceID),
		task.Status, task.TaskSuccess, nullString(task.ExpectedJSON), nullString(task.ObservedJSON),
		nullString(task.FailureReason), task.StartedAt.Format(time.RFC3339Nano), nullableTime(task.EndedAt),
		task.CreatedAt.Format(time.RFC3339Nano), task.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return mvp.AcceptanceTask{}, storageErr(err)
	}
	return task, nil
}

// ListAcceptanceTasks 按 run 查询 scenario 结果。RunID 必填，避免跨验收 run 混算指标。
func (s *Store) ListAcceptanceTasks(ctx context.Context, query mvp.TaskQuery) ([]mvp.AcceptanceTask, error) {
	if strings.TrimSpace(query.RunID) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: run_id is required")
	}
	sqlText := baseMVPTaskSelect() + ` where run_id = ?`
	args := []any{query.RunID}
	if query.ScenarioID != "" {
		sqlText += " and scenario_id = ?"
		args = append(args, query.ScenarioID)
	}
	if query.AgentType != "" {
		sqlText += " and agent_type = ?"
		args = append(args, query.AgentType)
	}
	if query.Baseline != nil {
		sqlText += " and baseline = ?"
		args = append(args, *query.Baseline)
	}
	sqlText += " order by scenario_id asc, round asc, created_at asc limit ?"
	args = append(args, mvpLimit(query.Limit))
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanMVPTaskRows(rows)
}

// UpsertMetricSamples 写入或替换同一 run/scenario/task/agent/metric 的指标样本。
func (s *Store) UpsertMetricSamples(ctx context.Context, samples []mvp.MetricSample) ([]mvp.MetricSample, error) {
	if len(samples) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	prepared := make([]mvp.MetricSample, len(samples))
	for i, sample := range samples {
		if strings.TrimSpace(sample.RunID) == "" || strings.TrimSpace(sample.MetricName) == "" || strings.TrimSpace(sample.Unit) == "" {
			return nil, fmt.Errorf("VALIDATION_FAILED: run_id, metric_name and unit are required")
		}
		if sample.ID == "" {
			id, err := idgen.New("mvp_metric")
			if err != nil {
				return nil, err
			}
			sample.ID = id
		}
		if sample.CreatedAt.IsZero() {
			sample.CreatedAt = now
		}
		prepared[i] = sample
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storageErr(err)
	}
	for _, sample := range prepared {
		if _, err := tx.ExecContext(ctx, `delete from mvp_metric_sample
			where run_id = ?
			  and coalesce(scenario_id, '') = ?
			  and coalesce(task_result_id, '') = ?
			  and coalesce(agent_type, '') = ?
			  and metric_name = ?`,
			sample.RunID, sample.ScenarioID, sample.TaskResultID, sample.AgentType, sample.MetricName,
		); err != nil {
			_ = tx.Rollback()
			return nil, storageErr(err)
		}
		if _, err := tx.ExecContext(ctx, `insert into mvp_metric_sample(
			id, run_id, scenario_id, task_result_id, agent_type, metric_name,
			metric_value, numerator, denominator, unit, threshold_value,
			threshold_operator, passed, source_json, created_at
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sample.ID, sample.RunID, nullString(sample.ScenarioID), nullString(sample.TaskResultID),
			nullString(sample.AgentType), sample.MetricName, sample.MetricValue, sample.Numerator,
			sample.Denominator, sample.Unit, sample.ThresholdValue, nullString(sample.ThresholdOperator),
			sample.Passed, nullString(sample.SourceJSON), sample.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			_ = tx.Rollback()
			return nil, storageErr(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, storageErr(err)
	}
	return prepared, nil
}

// ListMetricSamples 按 run 查询指标样本。RunID 必填。
func (s *Store) ListMetricSamples(ctx context.Context, query mvp.MetricQuery) ([]mvp.MetricSample, error) {
	if strings.TrimSpace(query.RunID) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: run_id is required")
	}
	sqlText := baseMVPMetricSelect() + ` where run_id = ?`
	args := []any{query.RunID}
	if query.ScenarioID != "" {
		sqlText += " and coalesce(scenario_id, '') = ?"
		args = append(args, query.ScenarioID)
	}
	if query.AgentType != "" {
		sqlText += " and coalesce(agent_type, '') = ?"
		args = append(args, query.AgentType)
	}
	if query.MetricName != "" {
		sqlText += " and metric_name = ?"
		args = append(args, query.MetricName)
	}
	sqlText += " order by metric_name asc, scenario_id asc, created_at desc limit ?"
	args = append(args, mvpLimit(query.Limit))
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanMVPMetricRows(rows)
}

// UpsertAgentCapability 写入或更新某个 Agent 在本次验收中的 capability 快照。
func (s *Store) UpsertAgentCapability(ctx context.Context, capability mvp.AgentCapability) (mvp.AgentCapability, error) {
	if strings.TrimSpace(capability.RunID) == "" || strings.TrimSpace(capability.AgentType) == "" {
		return mvp.AgentCapability{}, fmt.Errorf("VALIDATION_FAILED: run_id and agent_type are required")
	}
	if _, err := s.GetRun(ctx, capability.RunID); err != nil {
		return mvp.AgentCapability{}, err
	}
	if !mvp.IsCertificationAgent(capability.AgentType) {
		return mvp.AgentCapability{}, fmt.Errorf("VALIDATION_FAILED: unsupported p5 agent_type")
	}
	if capability.Completeness < 0 || capability.Completeness > 1 {
		return mvp.AgentCapability{}, fmt.Errorf("VALIDATION_FAILED: completeness must be between 0 and 1")
	}
	if capability.ID == "" {
		id, err := idgen.New("mvp_cap")
		if err != nil {
			return mvp.AgentCapability{}, err
		}
		capability.ID = id
	}
	if capability.CaptureLevel <= 0 {
		capability.CaptureLevel = 1
	}
	capability.CapabilityCoverage = mvp.CapabilityCoverage(capability)
	if capability.CreatedAt.IsZero() {
		capability.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `insert into mvp_agent_capability(
		id, run_id, agent_type, adapter_name, adapter_version, capture_level,
		conversation_capture, tool_call_capture, tool_output_capture, file_edit_capture,
		session_lifecycle, memory_observe, capability_coverage, completeness,
		degradation_reasons_json, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(run_id, agent_type) do update set
		adapter_name = excluded.adapter_name,
		adapter_version = excluded.adapter_version,
		capture_level = excluded.capture_level,
		conversation_capture = excluded.conversation_capture,
		tool_call_capture = excluded.tool_call_capture,
		tool_output_capture = excluded.tool_output_capture,
		file_edit_capture = excluded.file_edit_capture,
		session_lifecycle = excluded.session_lifecycle,
		memory_observe = excluded.memory_observe,
		capability_coverage = excluded.capability_coverage,
		completeness = excluded.completeness,
		degradation_reasons_json = excluded.degradation_reasons_json,
		created_at = excluded.created_at`,
		capability.ID, capability.RunID, capability.AgentType, nullString(capability.AdapterName),
		nullString(capability.AdapterVersion), capability.CaptureLevel, capability.ConversationCapture,
		capability.ToolCallCapture, capability.ToolOutputCapture, capability.FileEditCapture,
		capability.SessionLifecycle, capability.MemoryObserve, capability.CapabilityCoverage,
		capability.Completeness, nullString(capability.DegradationReasonsJSON),
		capability.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return mvp.AgentCapability{}, storageErr(err)
	}
	capabilities, err := s.ListAgentCapabilities(ctx, mvp.CapabilityQuery{RunID: capability.RunID, AgentType: capability.AgentType, Limit: 1})
	if err != nil {
		return mvp.AgentCapability{}, err
	}
	if len(capabilities) == 0 {
		return mvp.AgentCapability{}, fmt.Errorf("MVP_AGENT_CAPABILITY_NOT_FOUND: %s/%s", capability.RunID, capability.AgentType)
	}
	return capabilities[0], nil
}

// ListAgentCapabilities 按 run 查询 Agent capability。RunID 必填。
func (s *Store) ListAgentCapabilities(ctx context.Context, query mvp.CapabilityQuery) ([]mvp.AgentCapability, error) {
	if strings.TrimSpace(query.RunID) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: run_id is required")
	}
	sqlText := baseMVPCapabilitySelect() + ` where run_id = ?`
	args := []any{query.RunID}
	if query.AgentType != "" {
		sqlText += " and agent_type = ?"
		args = append(args, query.AgentType)
	}
	sqlText += " order by agent_type asc limit ?"
	args = append(args, mvpLimit(query.Limit))
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanMVPCapabilityRows(rows)
}

// ListRetrievalLatenciesByTraceIDs 查询 task 绑定的 retrieval_trace 延迟样本。
// 设计约束：必须由明确 trace_id 列表驱动，避免指标计算扫描全部 retrieval_trace。
func (s *Store) ListRetrievalLatenciesByTraceIDs(ctx context.Context, traceIDs []string) ([]float64, error) {
	cleaned := make([]string, 0, len(traceIDs))
	seen := map[string]bool{}
	for _, traceID := range traceIDs {
		traceID = strings.TrimSpace(traceID)
		if traceID == "" || seen[traceID] {
			continue
		}
		seen[traceID] = true
		cleaned = append(cleaned, traceID)
	}
	if len(cleaned) == 0 {
		return nil, nil
	}
	args := make([]any, len(cleaned))
	for i, traceID := range cleaned {
		args[i] = traceID
	}
	rows, err := s.db.QueryContext(ctx, `select coalesce(latency_ms, 0)
		from retrieval_trace
		where id in (`+placeholders(len(args))+`)
		order by created_at asc`, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	latencies := make([]float64, 0, len(cleaned))
	for rows.Next() {
		var latency float64
		if err := rows.Scan(&latency); err != nil {
			return nil, storageErr(err)
		}
		latencies = append(latencies, latency)
	}
	return latencies, storageErr(rows.Err())
}

func baseMVPRunSelect() string {
	return `select id, name, mode, workspace_id, coalesce(project_id, ''), coalesce(repo_id, ''),
		baseline_type, candidate_type, status, started_at, coalesce(ended_at, ''),
		coalesce(summary_json, ''), coalesce(report_path, ''), created_at, updated_at
		from mvp_acceptance_run`
}

func baseMVPTaskSelect() string {
	return `select id, run_id, scenario_id, round, agent_type, baseline, coalesce(session_id, ''),
		coalesce(task_id, ''), coalesce(retrieval_trace_id, ''), status, task_success,
		coalesce(expected_json, ''), coalesce(observed_json, ''), coalesce(failure_reason, ''),
		started_at, coalesce(ended_at, ''), created_at, updated_at
		from mvp_acceptance_task`
}

func baseMVPMetricSelect() string {
	return `select id, run_id, coalesce(scenario_id, ''), coalesce(task_result_id, ''),
		coalesce(agent_type, ''), metric_name, metric_value, coalesce(numerator, 0),
		coalesce(denominator, 0), unit, coalesce(threshold_value, 0),
		coalesce(threshold_operator, ''), passed, coalesce(source_json, ''), created_at
		from mvp_metric_sample`
}

func baseMVPCapabilitySelect() string {
	return `select id, run_id, agent_type, coalesce(adapter_name, ''), coalesce(adapter_version, ''),
		capture_level, conversation_capture, tool_call_capture, tool_output_capture,
		file_edit_capture, session_lifecycle, memory_observe, capability_coverage,
		completeness, coalesce(degradation_reasons_json, ''), created_at
		from mvp_agent_capability`
}

func scanMVPRunRows(rows *sql.Rows) ([]mvp.AcceptanceRun, error) {
	items := make([]mvp.AcceptanceRun, 0)
	for rows.Next() {
		item, err := scanMVPRun(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func scanMVPRun(row rowScanner) (mvp.AcceptanceRun, error) {
	var item mvp.AcceptanceRun
	var startedAt, endedAt, createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.Name, &item.Mode, &item.WorkspaceID, &item.ProjectID,
		&item.RepoID, &item.BaselineType, &item.CandidateType, &item.Status, &startedAt,
		&endedAt, &item.SummaryJSON, &item.ReportPath, &createdAt, &updatedAt)
	if err != nil {
		return mvp.AcceptanceRun{}, err
	}
	item.StartedAt = parseTime(startedAt)
	item.EndedAt = parseTime(endedAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanMVPTaskRows(rows *sql.Rows) ([]mvp.AcceptanceTask, error) {
	items := make([]mvp.AcceptanceTask, 0)
	for rows.Next() {
		item, err := scanMVPTask(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func scanMVPTask(row rowScanner) (mvp.AcceptanceTask, error) {
	var item mvp.AcceptanceTask
	var startedAt, endedAt, createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.RunID, &item.ScenarioID, &item.Round, &item.AgentType,
		&item.Baseline, &item.SessionID, &item.TaskID, &item.RetrievalTraceID, &item.Status,
		&item.TaskSuccess, &item.ExpectedJSON, &item.ObservedJSON, &item.FailureReason,
		&startedAt, &endedAt, &createdAt, &updatedAt)
	if err != nil {
		return mvp.AcceptanceTask{}, err
	}
	item.StartedAt = parseTime(startedAt)
	item.EndedAt = parseTime(endedAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanMVPMetricRows(rows *sql.Rows) ([]mvp.MetricSample, error) {
	items := make([]mvp.MetricSample, 0)
	for rows.Next() {
		item, err := scanMVPMetric(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func scanMVPMetric(row rowScanner) (mvp.MetricSample, error) {
	var item mvp.MetricSample
	var createdAt string
	err := row.Scan(&item.ID, &item.RunID, &item.ScenarioID, &item.TaskResultID,
		&item.AgentType, &item.MetricName, &item.MetricValue, &item.Numerator,
		&item.Denominator, &item.Unit, &item.ThresholdValue, &item.ThresholdOperator,
		&item.Passed, &item.SourceJSON, &createdAt)
	if err != nil {
		return mvp.MetricSample{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func scanMVPCapabilityRows(rows *sql.Rows) ([]mvp.AgentCapability, error) {
	items := make([]mvp.AgentCapability, 0)
	for rows.Next() {
		item, err := scanMVPCapability(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func scanMVPCapability(row rowScanner) (mvp.AgentCapability, error) {
	var item mvp.AgentCapability
	var createdAt string
	err := row.Scan(&item.ID, &item.RunID, &item.AgentType, &item.AdapterName,
		&item.AdapterVersion, &item.CaptureLevel, &item.ConversationCapture,
		&item.ToolCallCapture, &item.ToolOutputCapture, &item.FileEditCapture,
		&item.SessionLifecycle, &item.MemoryObserve, &item.CapabilityCoverage,
		&item.Completeness, &item.DegradationReasonsJSON, &createdAt)
	if err != nil {
		return mvp.AgentCapability{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func mvpLimit(limit int) int {
	if limit <= 0 {
		return defaultMVPLimit
	}
	return limit
}
