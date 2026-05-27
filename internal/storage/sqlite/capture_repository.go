package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/capture"
)

const defaultCaptureLimit = 50

// UpsertSession 写入或更新 agent_session。该方法只做单表短写入，不负责 observe 编排。
func (s *Store) UpsertSession(ctx context.Context, session capture.AgentSession) (capture.AgentSession, error) {
	if session.ID == "" {
		return capture.AgentSession{}, fmt.Errorf("VALIDATION_FAILED: session id is required")
	}
	now := time.Now().UTC()
	if session.StartedAt.IsZero() {
		session.StartedAt = now
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now
	// INSERT OR UPDATE 语义：冲突时更新非空字段，保留已有值
	// coalesce(nullif(excluded.x, ''), existing.x): 新值为空时保留旧值，避免覆盖
	_, err := s.db.ExecContext(ctx, `insert into agent_session(
		id, agent_type, workspace_id, project_id, repo_id, capture_level, capture_capabilities_json,
		capture_quality_json, started_at, ended_at, goal_summary, status, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(id) do update set
		agent_type = coalesce(nullif(excluded.agent_type, ''), agent_session.agent_type),
		workspace_id = coalesce(nullif(excluded.workspace_id, ''), agent_session.workspace_id),
		project_id = coalesce(excluded.project_id, agent_session.project_id),
		repo_id = coalesce(excluded.repo_id, agent_session.repo_id),
		capture_level = excluded.capture_level,
		capture_capabilities_json = coalesce(excluded.capture_capabilities_json, agent_session.capture_capabilities_json),
		capture_quality_json = coalesce(excluded.capture_quality_json, agent_session.capture_quality_json),
		ended_at = excluded.ended_at,
		goal_summary = coalesce(excluded.goal_summary, agent_session.goal_summary),
		status = coalesce(nullif(excluded.status, ''), agent_session.status),
		updated_at = excluded.updated_at`,
		session.ID, session.AgentType, session.WorkspaceID, nullString(session.ProjectID), nullString(session.RepoID),
		session.CaptureLevel, nullString(session.CaptureCapabilitiesJSON), nullString(session.CaptureQualityJSON),
		session.StartedAt.Format(time.RFC3339Nano), nullableTime(session.EndedAt), nullString(session.GoalSummary),
		session.Status, session.CreatedAt.Format(time.RFC3339Nano), session.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return capture.AgentSession{}, storageErr(err)
	}
	return s.getSession(ctx, session.ID)
}

// EndSession 标记 session 结束并保存最新 capture quality。
func (s *Store) EndSession(ctx context.Context, sessionID string, status string, endedAt time.Time, quality capture.CaptureQuality) (capture.AgentSession, error) {
	if sessionID == "" {
		return capture.AgentSession{}, fmt.Errorf("VALIDATION_FAILED: session id is required")
	}
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	qualityJSON, err := toJSONText(quality)
	if err != nil {
		return capture.AgentSession{}, err
	}
	_, err = s.db.ExecContext(ctx, `update agent_session
		set status = ?, ended_at = ?, capture_quality_json = ?, updated_at = ?
		where id = ?`,
		status, endedAt.Format(time.RFC3339Nano), nullString(qualityJSON), time.Now().UTC().Format(time.RFC3339Nano), sessionID,
	)
	if err != nil {
		return capture.AgentSession{}, storageErr(err)
	}
	return s.getSession(ctx, sessionID)
}

// UpsertTask 写入或更新 agent_task。任务摘要和结果必须是最小化后的摘要。
func (s *Store) UpsertTask(ctx context.Context, task capture.AgentTask) (capture.AgentTask, error) {
	if task.ID == "" {
		return capture.AgentTask{}, fmt.Errorf("VALIDATION_FAILED: task id is required")
	}
	now := time.Now().UTC()
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `insert into agent_task(
		id, session_id, workspace_id, project_id, repo_id, task_summary, status,
		started_at, ended_at, outcome_summary, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(id) do update set
		session_id = excluded.session_id,
		workspace_id = excluded.workspace_id,
		project_id = excluded.project_id,
		repo_id = excluded.repo_id,
		task_summary = excluded.task_summary,
		status = excluded.status,
		ended_at = excluded.ended_at,
		outcome_summary = excluded.outcome_summary,
		updated_at = excluded.updated_at`,
		task.ID, nullString(task.SessionID), task.WorkspaceID, nullString(task.ProjectID), nullString(task.RepoID),
		task.TaskSummary, task.Status, task.StartedAt.Format(time.RFC3339Nano), nullableTime(task.EndedAt),
		nullString(task.OutcomeSummary), task.CreatedAt.Format(time.RFC3339Nano), task.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return capture.AgentTask{}, storageErr(err)
	}
	return s.getTask(ctx, task.ID)
}

// EndTask 标记任务结束并写入结果摘要。
func (s *Store) EndTask(ctx context.Context, taskID string, status string, outcome string, endedAt time.Time) (capture.AgentTask, error) {
	if taskID == "" {
		return capture.AgentTask{}, fmt.Errorf("VALIDATION_FAILED: task id is required")
	}
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `update agent_task
		set status = ?, ended_at = ?, outcome_summary = ?, updated_at = ?
		where id = ?`,
		status, endedAt.Format(time.RFC3339Nano), nullString(outcome), time.Now().UTC().Format(time.RFC3339Nano), taskID,
	)
	if err != nil {
		return capture.AgentTask{}, storageErr(err)
	}
	return s.getTask(ctx, taskID)
}

// GetDefaultTask 读取 session 下的 default task。P2-B2 暂按固定摘要识别，C1 负责创建策略。
func (s *Store) GetDefaultTask(ctx context.Context, sessionID string) (capture.AgentTask, bool, error) {
	row := s.db.QueryRowContext(ctx, baseTaskSelect()+` where coalesce(session_id, '') = ? and task_summary = ?
		order by created_at asc limit 1`, sessionID, "default task")
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return capture.AgentTask{}, false, nil
	}
	if err != nil {
		return capture.AgentTask{}, false, storageErr(err)
	}
	return task, true, nil
}

// FindDuplicateEvent 按 P2 幂等规则查询已有 raw_event。
func (s *Store) FindDuplicateEvent(ctx context.Context, dedup capture.EventDedupKey) (capture.RawEvent, bool, error) {
	if dedup.ContentHash == "" || dedup.EventType == "" {
		return capture.RawEvent{}, false, fmt.Errorf("VALIDATION_FAILED: content_hash and event_type are required")
	}
	// 幂等键核心：content_hash + event_type
	query := baseEventSelect() + ` where content_hash = ? and event_type = ?`
	args := []any{dedup.ContentHash, dedup.EventType}
	// 隔离策略：有 session_id 时按 session 隔离（会话内去重）
	if dedup.SessionID != "" {
		query += " and session_id = ?"
		args = append(args, dedup.SessionID)
	} else {
		// 无 session_id 时按 source_channel + workspace/project/repo 隔离（跨会话去重）
		query += ` and coalesce(source_channel, '') = ?
			and coalesce(workspace_id, '') = ?
			and coalesce(project_id, '') = ?
			and coalesce(repo_id, '') = ?`
		args = append(args, dedup.SourceChannel, dedup.WorkspaceID, dedup.ProjectID, dedup.RepoID)
	}
	query += " order by occurred_at desc limit 1"
	event, err := scanEvent(s.db.QueryRowContext(ctx, query, args...))
	if err == sql.ErrNoRows {
		return capture.RawEvent{}, false, nil
	}
	if err != nil {
		return capture.RawEvent{}, false, storageErr(err)
	}
	return event, true, nil
}

// InsertRawEvent 追加写入 raw_event。raw_event 是事实层，更新应通过新事件表达。
func (s *Store) InsertRawEvent(ctx context.Context, event capture.RawEvent) error {
	if event.ID == "" {
		return fmt.Errorf("VALIDATION_FAILED: raw_event id is required")
	}
	now := time.Now().UTC()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `insert into raw_event(
		id, session_id, task_id, workspace_id, project_id, repo_id, agent_type, event_type,
		source_channel, occurred_at, actor, tool_name, input_summary, output_summary, content_summary,
		keywords_json, salient_spans_json, source_refs_json, content_hash, sensitivity, retention_hint, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, nullString(event.SessionID), nullString(event.TaskID), nullString(event.WorkspaceID),
		nullString(event.ProjectID), nullString(event.RepoID), nullString(event.AgentType), event.EventType,
		nullString(event.SourceChannel), event.OccurredAt.Format(time.RFC3339Nano), nullString(event.Actor),
		nullString(event.ToolName), nullString(event.InputSummary), nullString(event.OutputSummary),
		nullString(event.ContentSummary), nullString(event.KeywordsJSON), nullString(event.SalientSpansJSON),
		nullString(event.SourceRefsJSON), nullString(event.ContentHash), nullString(event.Sensitivity),
		nullString(event.RetentionHint), event.CreatedAt.Format(time.RFC3339Nano),
	)
	return storageErr(err)
}

// ListSessions 按 scope/agent/status 查询 capture sessions，默认限制 50 条。
func (s *Store) ListSessions(ctx context.Context, req capture.ListSessionsRequest) ([]capture.AgentSession, error) {
	query := baseSessionSelect() + " where 1 = 1"
	args := make([]any, 0)
	if req.WorkspaceID != "" {
		query += " and workspace_id = ?"
		args = append(args, req.WorkspaceID)
	}
	if req.ProjectID != "" {
		query += " and project_id = ?"
		args = append(args, req.ProjectID)
	}
	if req.RepoID != "" {
		query += " and repo_id = ?"
		args = append(args, req.RepoID)
	}
	if req.AgentType != "" {
		query += " and agent_type = ?"
		args = append(args, req.AgentType)
	}
	if req.Status != "" {
		query += " and status = ?"
		args = append(args, req.Status)
	}
	query += " order by started_at desc limit ?"
	args = append(args, queryLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// ListTasks 按 session/scope/status 查询 capture tasks。
func (s *Store) ListTasks(ctx context.Context, req capture.ListTasksRequest) ([]capture.AgentTask, error) {
	query := baseTaskSelect() + " where 1 = 1"
	args := make([]any, 0)
	if req.SessionID != "" {
		query += " and session_id = ?"
		args = append(args, req.SessionID)
	}
	if req.WorkspaceID != "" {
		query += " and workspace_id = ?"
		args = append(args, req.WorkspaceID)
	}
	if req.ProjectID != "" {
		query += " and project_id = ?"
		args = append(args, req.ProjectID)
	}
	if req.RepoID != "" {
		query += " and repo_id = ?"
		args = append(args, req.RepoID)
	}
	if req.Status != "" {
		query += " and status = ?"
		args = append(args, req.Status)
	}
	query += " order by started_at desc limit ?"
	args = append(args, queryLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanTaskRows(rows)
}

// ListEvents 按 session/task/scope/event_type 查询 raw_event。
func (s *Store) ListEvents(ctx context.Context, req capture.ListEventsRequest) ([]capture.RawEvent, error) {
	query := baseEventSelect() + " where 1 = 1"
	args := make([]any, 0)
	if req.SessionID != "" {
		query += " and session_id = ?"
		args = append(args, req.SessionID)
	}
	if req.TaskID != "" {
		query += " and task_id = ?"
		args = append(args, req.TaskID)
	}
	if req.WorkspaceID != "" {
		query += " and workspace_id = ?"
		args = append(args, req.WorkspaceID)
	}
	if req.ProjectID != "" {
		query += " and project_id = ?"
		args = append(args, req.ProjectID)
	}
	if req.RepoID != "" {
		query += " and repo_id = ?"
		args = append(args, req.RepoID)
	}
	if req.AgentType != "" {
		query += " and agent_type = ?"
		args = append(args, req.AgentType)
	}
	if req.SourceChannel != "" {
		query += " and source_channel = ?"
		args = append(args, req.SourceChannel)
	}
	if req.EventType != "" {
		query += " and event_type = ?"
		args = append(args, req.EventType)
	}
	if len(req.EventTypes) > 0 {
		query += " and event_type in (" + placeholders(len(req.EventTypes)) + ")"
		for _, eventType := range req.EventTypes {
			args = append(args, eventType)
		}
	}
	query += " order by occurred_at desc limit ?"
	args = append(args, queryLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// GetCaptureQuality 读取 session 当前捕获等级、capability 和 quality 摘要。
func (s *Store) GetCaptureQuality(ctx context.Context, sessionID string) (capture.CaptureQualityReport, error) {
	row := s.db.QueryRowContext(ctx, `select id, capture_level, coalesce(capture_capabilities_json, ''),
		coalesce(capture_quality_json, '') from agent_session where id = ?`, sessionID)
	var report capture.CaptureQualityReport
	err := row.Scan(&report.SessionID, &report.CaptureLevel, &report.CaptureCapabilitiesJSON, &report.CaptureQualityJSON)
	if err == sql.ErrNoRows {
		return capture.CaptureQualityReport{}, fmt.Errorf("SESSION_NOT_FOUND: %s", sessionID)
	}
	if err != nil {
		return capture.CaptureQualityReport{}, storageErr(err)
	}
	return report, nil
}

// GetRawEvent 按 raw_event id 读取事件事实，供 P3 automation worker 使用。
func (s *Store) GetRawEvent(ctx context.Context, rawEventID string) (capture.RawEvent, error) {
	event, err := scanEvent(s.db.QueryRowContext(ctx, baseEventSelect()+" where id = ?", rawEventID))
	if err == sql.ErrNoRows {
		return capture.RawEvent{}, fmt.Errorf("RAW_EVENT_NOT_FOUND: %s", rawEventID)
	}
	return event, storageErr(err)
}

// GetSession 按 session_id 读取 Agent 会话。
func (s *Store) GetSession(ctx context.Context, sessionID string) (capture.AgentSession, error) {
	return s.getSession(ctx, sessionID)
}

// GetTask 按 task_id 读取 Agent 任务。
func (s *Store) GetTask(ctx context.Context, taskID string) (capture.AgentTask, error) {
	return s.getTask(ctx, taskID)
}

// ListOrphanRawEvents 查找没有 extract_evidence job 且没有 evidence 的 raw_event，供 reconcile 使用。
func (s *Store) ListOrphanRawEvents(ctx context.Context, req automation.OrphanRawEventRequest) ([]capture.RawEvent, error) {
	if req.WorkspaceID == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: workspace_id is required")
	}
	// 孤儿事件定义：没有 extract_evidence job 且没有 evidence 的 raw_event
	// 双重 NOT EXISTS 确保：既没有排队的处理任务，也没有已完成的证据
	query := baseEventSelect() + `
		where workspace_id = ?
		  and not exists (
			select 1 from async_job j
			where j.job_type = ?
			  and j.target_type = ?
			  and j.target_id = raw_event.id
		  )
		  and not exists (
			select 1 from evidence e where e.raw_event_id = raw_event.id
		  )`
	args := []any{req.WorkspaceID, automation.JobTypeExtractEvidence, automation.TargetTypeRawEvent}
	if req.ProjectID != "" {
		query += " and project_id = ?"
		args = append(args, req.ProjectID)
	}
	if req.RepoID != "" {
		query += " and repo_id = ?"
		args = append(args, req.RepoID)
	}
	query += " order by created_at desc limit ?"
	args = append(args, automationLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// ListRelatedEvents 读取同 session/task 的近邻事件，用于 P3 Provider 抽取上下文。
func (s *Store) ListRelatedEvents(ctx context.Context, req automation.RelatedEventsRequest) ([]capture.RawEvent, error) {
	query := baseEventSelect() + " where 1 = 1"
	args := make([]any, 0)
	if req.SessionID != "" {
		query += " and session_id = ?"
		args = append(args, req.SessionID)
	}
	if req.TaskID != "" {
		query += " and task_id = ?"
		args = append(args, req.TaskID)
	}
	query += " order by occurred_at desc limit ?"
	args = append(args, automationLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanEventRows(rows)
}

func (s *Store) getSession(ctx context.Context, sessionID string) (capture.AgentSession, error) {
	session, err := scanSession(s.db.QueryRowContext(ctx, baseSessionSelect()+" where id = ?", sessionID))
	if err == sql.ErrNoRows {
		return capture.AgentSession{}, fmt.Errorf("SESSION_NOT_FOUND: %s", sessionID)
	}
	return session, storageErr(err)
}

func (s *Store) getTask(ctx context.Context, taskID string) (capture.AgentTask, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, baseTaskSelect()+" where id = ?", taskID))
	if err == sql.ErrNoRows {
		return capture.AgentTask{}, fmt.Errorf("TASK_NOT_FOUND: %s", taskID)
	}
	return task, storageErr(err)
}

func baseSessionSelect() string {
	return `select id, agent_type, workspace_id, coalesce(project_id, ''), coalesce(repo_id, ''),
		capture_level, coalesce(capture_capabilities_json, ''), coalesce(capture_quality_json, ''),
		started_at, coalesce(ended_at, ''), coalesce(goal_summary, ''), status, created_at, updated_at
		from agent_session`
}

func baseTaskSelect() string {
	return `select id, coalesce(session_id, ''), workspace_id, coalesce(project_id, ''), coalesce(repo_id, ''),
		task_summary, status, started_at, coalesce(ended_at, ''), coalesce(outcome_summary, ''),
		created_at, updated_at
		from agent_task`
}

func baseEventSelect() string {
	return `select id, coalesce(session_id, ''), coalesce(task_id, ''), coalesce(workspace_id, ''),
		coalesce(project_id, ''), coalesce(repo_id, ''), coalesce(agent_type, ''), event_type,
		coalesce(source_channel, ''), occurred_at, coalesce(actor, ''), coalesce(tool_name, ''),
		coalesce(input_summary, ''), coalesce(output_summary, ''), coalesce(content_summary, ''),
		coalesce(keywords_json, ''), coalesce(salient_spans_json, ''), coalesce(source_refs_json, ''),
		coalesce(content_hash, ''), coalesce(sensitivity, ''), coalesce(retention_hint, ''), created_at
		from raw_event`
}

func scanSession(row rowScanner) (capture.AgentSession, error) {
	var session capture.AgentSession
	var startedAt, endedAt, createdAt, updatedAt string
	err := row.Scan(&session.ID, &session.AgentType, &session.WorkspaceID, &session.ProjectID, &session.RepoID,
		&session.CaptureLevel, &session.CaptureCapabilitiesJSON, &session.CaptureQualityJSON,
		&startedAt, &endedAt, &session.GoalSummary, &session.Status, &createdAt, &updatedAt)
	if err != nil {
		return capture.AgentSession{}, err
	}
	session.StartedAt = parseTime(startedAt)
	session.EndedAt = parseTime(endedAt)
	session.CreatedAt = parseTime(createdAt)
	session.UpdatedAt = parseTime(updatedAt)
	return session, nil
}

func scanTask(row rowScanner) (capture.AgentTask, error) {
	var task capture.AgentTask
	var startedAt, endedAt, createdAt, updatedAt string
	err := row.Scan(&task.ID, &task.SessionID, &task.WorkspaceID, &task.ProjectID, &task.RepoID,
		&task.TaskSummary, &task.Status, &startedAt, &endedAt, &task.OutcomeSummary, &createdAt, &updatedAt)
	if err != nil {
		return capture.AgentTask{}, err
	}
	task.StartedAt = parseTime(startedAt)
	task.EndedAt = parseTime(endedAt)
	task.CreatedAt = parseTime(createdAt)
	task.UpdatedAt = parseTime(updatedAt)
	return task, nil
}

func scanEvent(row rowScanner) (capture.RawEvent, error) {
	var event capture.RawEvent
	var occurredAt, createdAt string
	err := row.Scan(&event.ID, &event.SessionID, &event.TaskID, &event.WorkspaceID, &event.ProjectID,
		&event.RepoID, &event.AgentType, &event.EventType, &event.SourceChannel, &occurredAt,
		&event.Actor, &event.ToolName, &event.InputSummary, &event.OutputSummary, &event.ContentSummary,
		&event.KeywordsJSON, &event.SalientSpansJSON, &event.SourceRefsJSON, &event.ContentHash,
		&event.Sensitivity, &event.RetentionHint, &createdAt)
	if err != nil {
		return capture.RawEvent{}, err
	}
	event.OccurredAt = parseTime(occurredAt)
	event.CreatedAt = parseTime(createdAt)
	return event, nil
}

func scanSessionRows(rows *sql.Rows) ([]capture.AgentSession, error) {
	items := make([]capture.AgentSession, 0)
	for rows.Next() {
		item, err := scanSession(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func scanTaskRows(rows *sql.Rows) ([]capture.AgentTask, error) {
	items := make([]capture.AgentTask, 0)
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func scanEventRows(rows *sql.Rows) ([]capture.RawEvent, error) {
	items := make([]capture.RawEvent, 0)
	for rows.Next() {
		item, err := scanEvent(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func queryLimit(limit int) int {
	if limit <= 0 {
		return defaultCaptureLimit
	}
	return limit
}

func toJSONText(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: encode json: %w", err)
	}
	return string(data), nil
}
