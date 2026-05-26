package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/idgen"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retrieval"
)

const (
	defaultRetrievalTraceLimit = 50
	defaultAccessLogLimit      = 100
	maxTraceTextRunes          = 512
	maxAccessTextRunes         = 512
)

// CreateRetrievalTrace 创建一次 P4 检索追踪记录。
// 边界条件：query/task 会被裁剪为短摘要；调用方不得依赖 trace 保存完整 prompt、源码或工具输出。
func (s *Store) CreateRetrievalTrace(ctx context.Context, record retrieval.TraceRecord) (retrieval.TraceRecord, error) {
	if record.ID == "" {
		id, err := idgen.New("rt")
		if err != nil {
			return retrieval.TraceRecord{}, err
		}
		record.ID = id
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.Status == "" {
		record.Status = retrieval.TraceStarted
	}
	record.Query = compactRetrievalText(record.Query, maxTraceTextRunes)
	record.Task = compactRetrievalText(record.Task, maxTraceTextRunes)
	_, err := s.db.ExecContext(ctx, `insert into retrieval_trace(
		id, session_id, task_id, workspace_id, project_id, repo_id, query, task,
		retrieval_intent, retrieval_mode, used_fts, used_vector, used_relation,
		used_code_index, used_doc_index, fallback_reason, candidate_count,
		injected_count, latency_ms, status, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, nullString(record.SessionID), nullString(record.TaskID), nullString(record.WorkspaceID),
		nullString(record.ProjectID), nullString(record.RepoID), nullString(record.Query), nullString(record.Task),
		nullString(string(record.Intent)), nullString(string(record.Mode)), record.UsedFTS, record.UsedVector,
		record.UsedRelation, record.UsedCodeIndex, record.UsedDocIndex, nullString(record.FallbackReason),
		record.CandidateCount, record.InjectedCount, record.LatencyMS, string(record.Status),
		record.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return retrieval.TraceRecord{}, storageErr(err)
	}
	return record, nil
}

// UpdateRetrievalTrace 更新一次检索 trace 的完成状态和诊断字段。
// 设计约束：该方法只按 id 更新单条 trace；用于 started -> completed/failed/degraded 的短事务写入。
func (s *Store) UpdateRetrievalTrace(ctx context.Context, record retrieval.TraceRecord) error {
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("VALIDATION_FAILED: retrieval trace id is required")
	}
	if record.Status == "" {
		record.Status = retrieval.TraceCompleted
	}
	record.Query = compactRetrievalText(record.Query, maxTraceTextRunes)
	record.Task = compactRetrievalText(record.Task, maxTraceTextRunes)
	result, err := s.db.ExecContext(ctx, `update retrieval_trace set
		session_id = coalesce(?, session_id),
		task_id = coalesce(?, task_id),
		workspace_id = coalesce(?, workspace_id),
		project_id = coalesce(?, project_id),
		repo_id = coalesce(?, repo_id),
		query = coalesce(?, query),
		task = coalesce(?, task),
		retrieval_intent = coalesce(?, retrieval_intent),
		retrieval_mode = coalesce(?, retrieval_mode),
		used_fts = ?,
		used_vector = ?,
		used_relation = ?,
		used_code_index = ?,
		used_doc_index = ?,
		fallback_reason = ?,
		candidate_count = ?,
		injected_count = ?,
		latency_ms = ?,
		status = ?
		where id = ?`,
		nullString(record.SessionID), nullString(record.TaskID), nullString(record.WorkspaceID),
		nullString(record.ProjectID), nullString(record.RepoID), nullString(record.Query), nullString(record.Task),
		nullString(string(record.Intent)), nullString(string(record.Mode)), record.UsedFTS, record.UsedVector,
		record.UsedRelation, record.UsedCodeIndex, record.UsedDocIndex, nullString(record.FallbackReason),
		record.CandidateCount, record.InjectedCount, record.LatencyMS, string(record.Status), record.ID,
	)
	if err != nil {
		return storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageErr(err)
	}
	if affected == 0 {
		return fmt.Errorf("RETRIEVAL_TRACE_NOT_FOUND: %s", record.ID)
	}
	return nil
}

// ListRetrievalTraces 按 scope/task 条件查询 retrieval_trace 诊断记录。
// WorkspaceID 必填，避免诊断工具在高增长表上执行无边界全库扫描。
func (s *Store) ListRetrievalTraces(ctx context.Context, query retrieval.TraceQuery) ([]retrieval.TraceRecord, error) {
	if strings.TrimSpace(query.WorkspaceID) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: workspace_id is required")
	}
	sqlText := `select id, coalesce(session_id, ''), coalesce(task_id, ''), coalesce(workspace_id, ''),
		coalesce(project_id, ''), coalesce(repo_id, ''), coalesce(query, ''), coalesce(task, ''),
		coalesce(retrieval_intent, ''), coalesce(retrieval_mode, ''), used_fts, used_vector,
		used_relation, used_code_index, used_doc_index, coalesce(fallback_reason, ''),
		candidate_count, injected_count, coalesce(latency_ms, 0), status, created_at
		from retrieval_trace where coalesce(workspace_id, '') = ?`
	args := []any{strings.TrimSpace(query.WorkspaceID)}
	if query.ProjectID != "" {
		sqlText += " and coalesce(project_id, '') = ?"
		args = append(args, query.ProjectID)
	}
	if query.RepoID != "" {
		sqlText += " and coalesce(repo_id, '') = ?"
		args = append(args, query.RepoID)
	}
	if query.SessionID != "" {
		sqlText += " and coalesce(session_id, '') = ?"
		args = append(args, query.SessionID)
	}
	if query.TaskID != "" {
		sqlText += " and coalesce(task_id, '') = ?"
		args = append(args, query.TaskID)
	}
	if query.Status != "" {
		sqlText += " and status = ?"
		args = append(args, string(query.Status))
	}
	sqlText += " order by created_at desc limit ?"
	args = append(args, retrievalTraceLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanRetrievalTraceRows(rows)
}

// WriteMemoryAccessLog 写入单条 memory access log。
// 边界条件：query/feedback 只保存短摘要；score_breakdown 和 inclusion_reason 使用 JSON 存储，便于诊断解释。
func (s *Store) WriteMemoryAccessLog(ctx context.Context, record retrieval.AccessLogRecord) (retrieval.AccessLogRecord, error) {
	records, err := s.WriteMemoryAccessLogs(ctx, []retrieval.AccessLogRecord{record})
	if err != nil {
		return retrieval.AccessLogRecord{}, err
	}
	return records[0], nil
}

// WriteMemoryAccessLogs 批量写入 memory access log。
// 事务边界：同一批 access log 在一个短事务内写入；失败时整批回滚，避免 trace 下 rank 记录部分缺失。
func (s *Store) WriteMemoryAccessLogs(ctx context.Context, records []retrieval.AccessLogRecord) ([]retrieval.AccessLogRecord, error) {
	if len(records) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	prepared := make([]retrieval.AccessLogRecord, len(records))
	for i, record := range records {
		if strings.TrimSpace(record.MemoryID) == "" || strings.TrimSpace(record.EventType) == "" {
			return nil, fmt.Errorf("VALIDATION_FAILED: memory_id and event_type are required")
		}
		if record.ID == "" {
			id, err := idgen.New("mal")
			if err != nil {
				return nil, err
			}
			record.ID = id
		}
		if record.EventWeight == 0 {
			record.EventWeight = accessLogEventWeight(record.EventType)
		}
		if record.SourceQuality == 0 {
			record.SourceQuality = 0.7
		}
		if record.CreatedAt.IsZero() {
			record.CreatedAt = now
		}
		record.Query = compactRetrievalText(record.Query, maxAccessTextRunes)
		record.Feedback = compactRetrievalText(record.Feedback, maxAccessTextRunes)
		prepared[i] = record
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storageErr(err)
	}
	stmt, err := tx.PrepareContext(ctx, `insert into memory_access_log(
		id, memory_id, session_id, task_id, retrieval_trace_id, event_type, event_weight,
		source_type, source_quality, query, rank, score, score_breakdown_json,
		inclusion_reason_json, used_in_context, feedback, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return nil, storageErr(err)
	}
	defer stmt.Close()

	for _, record := range prepared {
		scoreBreakdownJSON, err := encodeScoreBreakdown(record.ScoreBreakdown)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		inclusionReasonJSON, err := toJSONText(record.InclusionReasons)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if _, err := stmt.ExecContext(ctx,
			record.ID, record.MemoryID, nullString(record.SessionID), nullString(record.TaskID),
			nullString(record.RetrievalTraceID), record.EventType, record.EventWeight,
			nullString(record.SourceType), record.SourceQuality, nullString(record.Query), record.Rank,
			record.Score, nullString(scoreBreakdownJSON), nullString(inclusionReasonJSON),
			record.UsedInContext, nullString(record.Feedback), record.CreatedAt.Format(time.RFC3339Nano),
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

// ListMemoryAccessLogs 按 trace 或 memory 查询 access log 诊断记录。
// RetrievalTraceID 和 MemoryID 至少提供一个，避免对高增长日志表做无条件扫描。
func (s *Store) ListMemoryAccessLogs(ctx context.Context, query retrieval.AccessLogQuery) ([]retrieval.AccessLogRecord, error) {
	if strings.TrimSpace(query.RetrievalTraceID) == "" && strings.TrimSpace(query.MemoryID) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: retrieval_trace_id or memory_id is required")
	}
	sqlText := `select id, memory_id, coalesce(session_id, ''), coalesce(task_id, ''),
		coalesce(retrieval_trace_id, ''), event_type, event_weight, coalesce(source_type, ''),
		source_quality, coalesce(query, ''), coalesce(rank, 0), coalesce(score, 0),
		coalesce(score_breakdown_json, ''), coalesce(inclusion_reason_json, ''),
		used_in_context, coalesce(feedback, ''), created_at
		from memory_access_log where 1 = 1`
	args := make([]any, 0, 4)
	if query.RetrievalTraceID != "" {
		sqlText += " and retrieval_trace_id = ?"
		args = append(args, query.RetrievalTraceID)
	}
	if query.MemoryID != "" {
		sqlText += " and memory_id = ?"
		args = append(args, query.MemoryID)
	}
	if query.EventType != "" {
		sqlText += " and event_type = ?"
		args = append(args, query.EventType)
	}
	sqlText += " order by created_at desc, rank asc limit ?"
	args = append(args, accessLogLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanAccessLogRows(rows)
}

// CleanupMemoryAccessLogs 删除指定事件类型在 cutoff 之前的 access log 明细。
// 设计约束：只清理低价值 retrieved/injected 等派生访问记录；用户反馈类事件应由调用方排除。
func (s *Store) CleanupMemoryAccessLogs(ctx context.Context, eventType string, before time.Time) (int, error) {
	if strings.TrimSpace(eventType) == "" || before.IsZero() {
		return 0, fmt.Errorf("VALIDATION_FAILED: event_type and before are required")
	}
	result, err := s.db.ExecContext(ctx, `delete from memory_access_log
		where event_type = ? and julianday(created_at) < julianday(?)`,
		eventType, before.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, storageErr(err)
	}
	return int(affected), nil
}

func scanRetrievalTraceRows(rows *sql.Rows) ([]retrieval.TraceRecord, error) {
	records := make([]retrieval.TraceRecord, 0)
	for rows.Next() {
		record, err := scanRetrievalTrace(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		records = append(records, record)
	}
	return records, storageErr(rows.Err())
}

func scanRetrievalTrace(row rowScanner) (retrieval.TraceRecord, error) {
	var record retrieval.TraceRecord
	var intent, mode, status, createdAt string
	err := row.Scan(&record.ID, &record.SessionID, &record.TaskID, &record.WorkspaceID,
		&record.ProjectID, &record.RepoID, &record.Query, &record.Task, &intent, &mode,
		&record.UsedFTS, &record.UsedVector, &record.UsedRelation, &record.UsedCodeIndex,
		&record.UsedDocIndex, &record.FallbackReason, &record.CandidateCount, &record.InjectedCount,
		&record.LatencyMS, &status, &createdAt)
	if err != nil {
		return retrieval.TraceRecord{}, err
	}
	record.Intent = retrieval.RetrievalIntent(intent)
	record.Mode = retrieval.RetrievalMode(mode)
	record.Status = retrieval.TraceStatus(status)
	record.CreatedAt = parseTime(createdAt)
	return record, nil
}

func scanAccessLogRows(rows *sql.Rows) ([]retrieval.AccessLogRecord, error) {
	records := make([]retrieval.AccessLogRecord, 0)
	for rows.Next() {
		record, err := scanAccessLog(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		records = append(records, record)
	}
	return records, storageErr(rows.Err())
}

func scanAccessLog(row rowScanner) (retrieval.AccessLogRecord, error) {
	var record retrieval.AccessLogRecord
	var scoreBreakdownJSON, inclusionReasonJSON, createdAt string
	err := row.Scan(&record.ID, &record.MemoryID, &record.SessionID, &record.TaskID,
		&record.RetrievalTraceID, &record.EventType, &record.EventWeight, &record.SourceType,
		&record.SourceQuality, &record.Query, &record.Rank, &record.Score, &scoreBreakdownJSON,
		&inclusionReasonJSON, &record.UsedInContext, &record.Feedback, &createdAt)
	if err != nil {
		return retrieval.AccessLogRecord{}, err
	}
	record.ScoreBreakdown = decodeScoreBreakdown(scoreBreakdownJSON)
	record.InclusionReasons = decodeStringSlice(inclusionReasonJSON)
	record.CreatedAt = parseTime(createdAt)
	return record, nil
}

func encodeScoreBreakdown(score memory.ScoreBreakdown) (string, error) {
	data, err := json.Marshal(score)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: encode score_breakdown: %w", err)
	}
	return string(data), nil
}

func decodeScoreBreakdown(value string) memory.ScoreBreakdown {
	if strings.TrimSpace(value) == "" {
		return memory.ScoreBreakdown{}
	}
	var score memory.ScoreBreakdown
	_ = json.Unmarshal([]byte(value), &score)
	return score
}

func decodeStringSlice(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(value), &out)
	return out
}

func retrievalTraceLimit(limit int) int {
	if limit <= 0 {
		return defaultRetrievalTraceLimit
	}
	return limit
}

func accessLogLimit(limit int) int {
	if limit <= 0 {
		return defaultAccessLogLimit
	}
	return limit
}

func compactRetrievalText(value string, maxRunes int) string {
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

// accessLogEventWeight 返回不同访问事件类型的权重。
// 权重用于 P4 retention score 重算中的 reinforcement 因子计算。
//
// 正向事件（记忆被有效使用）：
//   - retrieved (0.2)：被检索返回（最低正向）
//   - injected (0.5)：被注入上下文
//   - cited_by_agent (1.0)：被 Agent 引用
//   - task_success (1.5)：关联任务成功
//   - user_confirmed (2.0)：用户确认（最高正向）
//
// 负向事件（记忆价值存疑）：
//   - ignored (-0.5)：被忽略
//   - task_failure (-1.5)：关联任务失败
//   - user_rejected (-3.0)：用户拒绝（最高负向）
func accessLogEventWeight(eventType string) float64 {
	switch eventType {
	case "retrieved":
		return 0.2
	case "injected":
		return 0.5
	case "cited_by_agent":
		return 1.0
	case "user_confirmed":
		return 2.0
	case "task_success":
		return 1.5
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
