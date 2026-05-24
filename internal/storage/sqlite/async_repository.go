package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/automation"
)

const defaultAutomationLimit = 50

// EnqueueJob 写入一条异步任务，并通过 dedup_key 保证同一目标任务不会重复入队。
func (s *Store) EnqueueJob(ctx context.Context, job automation.AsyncJob) (automation.AsyncJob, bool, error) {
	if job.ID == "" || job.JobType == "" || job.TargetType == "" || job.TargetID == "" {
		return automation.AsyncJob{}, false, fmt.Errorf("VALIDATION_FAILED: job id, job_type, target_type and target_id are required")
	}
	if job.DedupKey != "" {
		existing, found, err := s.getJobByDedupKey(ctx, job.DedupKey)
		if err != nil {
			return automation.AsyncJob{}, false, err
		}
		if found {
			return existing, true, nil
		}
	}
	now := time.Now().UTC()
	if job.Status == "" {
		job.Status = automation.JobStatusPending
	}
	if job.Priority == 0 {
		job.Priority = 5
	}
	if job.MaxRetries == 0 {
		job.MaxRetries = 3
	}
	if job.NextRunAt.IsZero() {
		job.NextRunAt = now
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `insert into async_job(
		id, job_type, target_type, target_id, status, priority, retry_count, max_retries,
		next_run_at, last_error, dedup_key, payload_json, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.JobType, job.TargetType, job.TargetID, job.Status, job.Priority, job.RetryCount, job.MaxRetries,
		job.NextRunAt.Format(time.RFC3339Nano), nullString(job.LastError), nullString(job.DedupKey),
		nullString(job.PayloadJSON), job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return automation.AsyncJob{}, false, storageErr(err)
	}
	created, err := s.GetJob(ctx, job.ID)
	return created, false, err
}

// ClaimJobs 按优先级和创建时间领取可运行任务，并在同一短事务内标记为 running。
func (s *Store) ClaimJobs(ctx context.Context, now time.Time, limit int) ([]automation.AsyncJob, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 {
		limit = 10
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storageErr(err)
	}
	rows, err := tx.QueryContext(ctx, baseJobSelect()+` where status = ? and julianday(next_run_at) <= julianday(?)
		order by priority asc, created_at asc
		limit ?`, automation.JobStatusPending, now.Format(time.RFC3339Nano), limit)
	if err != nil {
		_ = tx.Rollback()
		return nil, storageErr(err)
	}
	jobs, err := scanJobRows(rows)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	claimed := make([]automation.AsyncJob, 0, len(jobs))
	for _, job := range jobs {
		result, err := tx.ExecContext(ctx, `update async_job
			set status = ?, updated_at = ?
			where id = ? and status = ?`,
			automation.JobStatusRunning, now.Format(time.RFC3339Nano), job.ID, automation.JobStatusPending,
		)
		if err != nil {
			_ = tx.Rollback()
			return nil, storageErr(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return nil, storageErr(err)
		}
		if affected == 0 {
			continue
		}
		job.Status = automation.JobStatusRunning
		job.UpdatedAt = now
		claimed = append(claimed, job)
	}
	if err := tx.Commit(); err != nil {
		return nil, storageErr(err)
	}
	return claimed, nil
}

// RecoverStaleRunningJobs 恢复进程崩溃遗留的 running job。
// 未超过最大重试次数的任务恢复为 pending，超过次数的任务标记 failed，避免永久卡在 running。
func (s *Store) RecoverStaleRunningJobs(ctx context.Context, now time.Time, timeout time.Duration) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	cutoff := now.Add(-timeout).Format(time.RFC3339Nano)
	nowText := now.Format(time.RFC3339Nano)
	pending, err := s.db.ExecContext(ctx, `update async_job
		set status = ?, next_run_at = ?, last_error = ?, updated_at = ?
		where status = ?
		  and julianday(updated_at) < julianday(?)
		  and retry_count < max_retries`,
		automation.JobStatusPending, nowText, "WORKER_INTERRUPTED: stale running job", nowText,
		automation.JobStatusRunning, cutoff,
	)
	if err != nil {
		return 0, storageErr(err)
	}
	failed, err := s.db.ExecContext(ctx, `update async_job
		set status = ?, last_error = ?, updated_at = ?
		where status = ?
		  and julianday(updated_at) < julianday(?)
		  and retry_count >= max_retries`,
		automation.JobStatusFailed, "WORKER_INTERRUPTED: stale running job", nowText,
		automation.JobStatusRunning, cutoff,
	)
	if err != nil {
		return 0, storageErr(err)
	}
	pendingCount, err := pending.RowsAffected()
	if err != nil {
		return 0, storageErr(err)
	}
	failedCount, err := failed.RowsAffected()
	if err != nil {
		return 0, storageErr(err)
	}
	return int(pendingCount + failedCount), nil
}

// MarkJobSucceeded 将任务标记为 succeeded，并清理 last_error。
func (s *Store) MarkJobSucceeded(ctx context.Context, jobID string, payload string, now time.Time) error {
	if jobID == "" {
		return fmt.Errorf("VALIDATION_FAILED: job id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `update async_job
		set status = ?, payload_json = ?, last_error = null, updated_at = ?
		where id = ?`,
		automation.JobStatusSucceeded, nullString(payload), now.Format(time.RFC3339Nano), jobID,
	)
	return updateJobResult(jobID, result, err)
}

// MarkJobRetry 将任务放回 pending，并记录下一次运行时间和最近错误。
func (s *Store) MarkJobRetry(ctx context.Context, jobID string, retryCount int, nextRunAt time.Time, lastError string, now time.Time) error {
	if jobID == "" {
		return fmt.Errorf("VALIDATION_FAILED: job id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if nextRunAt.IsZero() {
		nextRunAt = now
	}
	result, err := s.db.ExecContext(ctx, `update async_job
		set status = ?, retry_count = ?, next_run_at = ?, last_error = ?, updated_at = ?
		where id = ?`,
		automation.JobStatusPending, retryCount, nextRunAt.Format(time.RFC3339Nano), nullString(lastError),
		now.Format(time.RFC3339Nano), jobID,
	)
	return updateJobResult(jobID, result, err)
}

// MarkJobFailed 将任务标记为 failed，保留错误摘要供诊断工具查询。
func (s *Store) MarkJobFailed(ctx context.Context, jobID string, lastError string, now time.Time) error {
	if jobID == "" {
		return fmt.Errorf("VALIDATION_FAILED: job id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `update async_job
		set status = ?, last_error = ?, updated_at = ?
		where id = ?`,
		automation.JobStatusFailed, nullString(lastError), now.Format(time.RFC3339Nano), jobID,
	)
	return updateJobResult(jobID, result, err)
}

// GetJob 按 job_id 读取异步任务详情。
func (s *Store) GetJob(ctx context.Context, jobID string) (automation.AsyncJob, error) {
	row := s.db.QueryRowContext(ctx, baseJobSelect()+" where id = ?", jobID)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return automation.AsyncJob{}, fmt.Errorf("JOB_NOT_FOUND: %s", jobID)
	}
	return job, storageErr(err)
}

// ListJobs 按状态、类型和目标过滤异步任务，用于 P3 诊断接口。
func (s *Store) ListJobs(ctx context.Context, req automation.ListJobsRequest) ([]automation.AsyncJob, error) {
	query := baseJobSelect() + " where 1 = 1"
	args := make([]any, 0)
	if req.Status != "" {
		query += " and status = ?"
		args = append(args, req.Status)
	}
	if req.JobType != "" {
		query += " and job_type = ?"
		args = append(args, req.JobType)
	}
	if req.TargetType != "" {
		query += " and target_type = ?"
		args = append(args, req.TargetType)
	}
	if req.TargetID != "" {
		query += " and target_id = ?"
		args = append(args, req.TargetID)
	}
	if req.WorkspaceID != "" || req.ProjectID != "" || req.RepoID != "" {
		scopeWhere, scopeArgs := jobScopeWhere(req)
		query += " and (" + scopeWhere + ")"
		args = append(args, scopeArgs...)
	}
	query += " order by created_at desc limit ?"
	args = append(args, automationLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	return scanJobRows(rows)
}

func jobScopeWhere(req automation.ListJobsRequest) (string, []any) {
	rawEventWhere, rawEventArgs := rawEventScopeWhere("re", req.WorkspaceID, req.ProjectID, req.RepoID)
	candidateWhere, candidateArgs := candidateScopeWhere("mc", req.WorkspaceID, req.ProjectID, req.RepoID)
	evidenceWhere, evidenceArgs := rawEventScopeWhere("re", req.WorkspaceID, req.ProjectID, req.RepoID)
	where := `(target_type = 'raw_event' and exists (
			select 1 from raw_event re where re.id = async_job.target_id and ` + rawEventWhere + `
		))
		or (target_type = 'memory_candidate' and exists (
			select 1 from memory_candidate mc where mc.id = async_job.target_id and ` + candidateWhere + `
		))
		or (target_type = 'evidence' and exists (
			select 1 from evidence e join raw_event re on re.id = e.raw_event_id
			where e.id = async_job.target_id and ` + evidenceWhere + `
		))`
	args := make([]any, 0, len(rawEventArgs)+len(candidateArgs)+len(evidenceArgs))
	args = append(args, rawEventArgs...)
	args = append(args, candidateArgs...)
	args = append(args, evidenceArgs...)
	return where, args
}

func rawEventScopeWhere(alias string, workspaceID string, projectID string, repoID string) (string, []any) {
	parts := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if workspaceID != "" {
		parts = append(parts, alias+".workspace_id = ?")
		args = append(args, workspaceID)
	}
	if projectID != "" {
		parts = append(parts, alias+".project_id = ?")
		args = append(args, projectID)
	}
	if repoID != "" {
		parts = append(parts, alias+".repo_id = ?")
		args = append(args, repoID)
	}
	if len(parts) == 0 {
		return "1 = 1", args
	}
	return strings.Join(parts, " and "), args
}

func candidateScopeWhere(alias string, workspaceID string, projectID string, repoID string) (string, []any) {
	return rawEventScopeWhere(alias, workspaceID, projectID, repoID)
}

// WriteCandidate 写入 Provider 生成的候选记忆记录，重复 dedup_key 会被视为幂等命中。
func (s *Store) WriteCandidate(ctx context.Context, candidate automation.MemoryCandidateRecord) error {
	if candidate.ID == "" || candidate.Provider == "" || candidate.MemoryType == "" || candidate.Scope == "" || candidate.Content == "" {
		return fmt.Errorf("VALIDATION_FAILED: candidate id, provider, memory_type, scope and content are required")
	}
	if candidate.DedupKey != "" {
		_, found, err := s.getCandidateByDedupKey(ctx, candidate.DedupKey)
		if err != nil {
			return err
		}
		if found {
			return nil
		}
	}
	now := time.Now().UTC()
	if candidate.Status == "" {
		candidate.Status = automation.CandidateStatusGenerated
	}
	if candidate.Confidence == 0 {
		candidate.Confidence = 0.7
	}
	if candidate.Importance == 0 {
		candidate.Importance = 0.5
	}
	if candidate.EncodingDepth == 0 {
		candidate.EncodingDepth = 2
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = now
	}
	candidate.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `insert into memory_candidate(
		id, raw_event_id, evidence_id, provider, memory_type, scope, workspace_id, user_id,
		project_id, repo_id, session_id, task_id, title, content, keywords_json, entities_json,
		retrieval_cues_json, tags_json, source_evidence_ids_json, review_checkpoint_json,
		confidence, importance, encoding_depth, candidate_reason_json, admission_score,
		admission_decision, admission_reason_json, resulting_memory_id, status, dedup_key,
		created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		candidate.ID, nullString(candidate.RawEventID), nullString(candidate.EvidenceID), candidate.Provider,
		candidate.MemoryType, candidate.Scope, nullString(candidate.WorkspaceID), nullString(candidate.UserID),
		nullString(candidate.ProjectID), nullString(candidate.RepoID), nullString(candidate.SessionID),
		nullString(candidate.TaskID), nullString(candidate.Title), candidate.Content, nullString(candidate.KeywordsJSON),
		nullString(candidate.EntitiesJSON), nullString(candidate.RetrievalCuesJSON), nullString(candidate.TagsJSON),
		nullString(candidate.SourceEvidenceIDsJSON), nullString(candidate.ReviewCheckpointJSON), candidate.Confidence,
		candidate.Importance, candidate.EncodingDepth, nullString(candidate.CandidateReasonJSON),
		nullableFloat(candidate.AdmissionScore), nullString(candidate.AdmissionDecision),
		nullString(candidate.AdmissionReasonJSON), nullString(candidate.ResultingMemoryID), candidate.Status,
		nullString(candidate.DedupKey), candidate.CreatedAt.Format(time.RFC3339Nano), candidate.UpdatedAt.Format(time.RFC3339Nano),
	)
	return storageErr(err)
}

// UpdateCandidateAdmission 回填 Admission 结果和最终 memory_id。
func (s *Store) UpdateCandidateAdmission(ctx context.Context, candidateID string, admission automation.AdmissionResult, status string, memoryID string) error {
	if candidateID == "" {
		return fmt.Errorf("VALIDATION_FAILED: candidate id is required")
	}
	if status == "" {
		status = automation.CandidateStatusGenerated
	}
	reasons, err := toJSONText(admission.ReasonCodes)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `update memory_candidate
		set admission_score = ?, admission_decision = ?, admission_reason_json = ?,
		    resulting_memory_id = ?, status = ?, updated_at = ?
		where id = ?`,
		admission.AdmissionScore, nullString(admission.Decision), nullString(reasons), nullString(memoryID),
		status, time.Now().UTC().Format(time.RFC3339Nano), candidateID,
	)
	if err != nil {
		return storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageErr(err)
	}
	if affected == 0 {
		return fmt.Errorf("CANDIDATE_NOT_FOUND: %s", candidateID)
	}
	return nil
}

// GetCandidate 按 candidate_id 读取候选记忆诊断记录。
func (s *Store) GetCandidate(ctx context.Context, candidateID string) (automation.MemoryCandidateRecord, error) {
	row := s.db.QueryRowContext(ctx, baseCandidateSelect()+" where id = ?", candidateID)
	candidate, err := scanCandidate(row)
	if err == sql.ErrNoRows {
		return automation.MemoryCandidateRecord{}, fmt.Errorf("CANDIDATE_NOT_FOUND: %s", candidateID)
	}
	return candidate, storageErr(err)
}

// ListCandidates 按来源、状态、scope 和类型过滤候选记忆记录。
func (s *Store) ListCandidates(ctx context.Context, req automation.ListCandidatesRequest) ([]automation.MemoryCandidateRecord, error) {
	query := baseCandidateSelect() + " where 1 = 1"
	args := make([]any, 0)
	if req.Status != "" {
		query += " and status = ?"
		args = append(args, req.Status)
	}
	if req.MemoryType != "" {
		query += " and memory_type = ?"
		args = append(args, req.MemoryType)
	}
	if req.Provider != "" {
		query += " and provider = ?"
		args = append(args, req.Provider)
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
	if req.RawEventID != "" {
		query += " and raw_event_id = ?"
		args = append(args, req.RawEventID)
	}
	if req.EvidenceID != "" {
		query += " and evidence_id = ?"
		args = append(args, req.EvidenceID)
	}
	query += " order by created_at desc limit ?"
	args = append(args, automationLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	return scanCandidateRows(rows)
}

func (s *Store) getJobByDedupKey(ctx context.Context, dedupKey string) (automation.AsyncJob, bool, error) {
	row := s.db.QueryRowContext(ctx, baseJobSelect()+" where dedup_key = ? order by created_at desc limit 1", dedupKey)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return automation.AsyncJob{}, false, nil
	}
	if err != nil {
		return automation.AsyncJob{}, false, storageErr(err)
	}
	return job, true, nil
}

func (s *Store) getCandidateByDedupKey(ctx context.Context, dedupKey string) (automation.MemoryCandidateRecord, bool, error) {
	row := s.db.QueryRowContext(ctx, baseCandidateSelect()+" where dedup_key = ? order by created_at desc limit 1", dedupKey)
	candidate, err := scanCandidate(row)
	if err == sql.ErrNoRows {
		return automation.MemoryCandidateRecord{}, false, nil
	}
	if err != nil {
		return automation.MemoryCandidateRecord{}, false, storageErr(err)
	}
	return candidate, true, nil
}

func baseJobSelect() string {
	return `select id, job_type, target_type, target_id, status, priority, retry_count, max_retries,
		next_run_at, coalesce(last_error, ''), coalesce(dedup_key, ''), coalesce(payload_json, ''),
		created_at, updated_at
		from async_job`
}

func scanJob(row rowScanner) (automation.AsyncJob, error) {
	var job automation.AsyncJob
	var nextRunAt, createdAt, updatedAt string
	err := row.Scan(&job.ID, &job.JobType, &job.TargetType, &job.TargetID, &job.Status, &job.Priority,
		&job.RetryCount, &job.MaxRetries, &nextRunAt, &job.LastError, &job.DedupKey, &job.PayloadJSON,
		&createdAt, &updatedAt)
	if err != nil {
		return automation.AsyncJob{}, err
	}
	job.NextRunAt = parseTime(nextRunAt)
	job.CreatedAt = parseTime(createdAt)
	job.UpdatedAt = parseTime(updatedAt)
	return job, nil
}

func scanJobRows(rows *sql.Rows) ([]automation.AsyncJob, error) {
	defer rows.Close()
	items := make([]automation.AsyncJob, 0)
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func baseCandidateSelect() string {
	return `select id, coalesce(raw_event_id, ''), coalesce(evidence_id, ''), provider, memory_type,
		scope, coalesce(workspace_id, ''), coalesce(user_id, ''), coalesce(project_id, ''),
		coalesce(repo_id, ''), coalesce(session_id, ''), coalesce(task_id, ''), coalesce(title, ''),
		content, coalesce(keywords_json, ''), coalesce(entities_json, ''), coalesce(retrieval_cues_json, ''),
		coalesce(tags_json, ''), coalesce(source_evidence_ids_json, ''), coalesce(review_checkpoint_json, ''),
		confidence, importance, encoding_depth, coalesce(candidate_reason_json, ''),
		admission_score, coalesce(admission_decision, ''), coalesce(admission_reason_json, ''),
		coalesce(resulting_memory_id, ''), status, coalesce(dedup_key, ''), created_at, updated_at
		from memory_candidate`
}

func scanCandidate(row rowScanner) (automation.MemoryCandidateRecord, error) {
	var candidate automation.MemoryCandidateRecord
	var score sql.NullFloat64
	var createdAt, updatedAt string
	err := row.Scan(&candidate.ID, &candidate.RawEventID, &candidate.EvidenceID, &candidate.Provider,
		&candidate.MemoryType, &candidate.Scope, &candidate.WorkspaceID, &candidate.UserID, &candidate.ProjectID,
		&candidate.RepoID, &candidate.SessionID, &candidate.TaskID, &candidate.Title, &candidate.Content,
		&candidate.KeywordsJSON, &candidate.EntitiesJSON, &candidate.RetrievalCuesJSON, &candidate.TagsJSON,
		&candidate.SourceEvidenceIDsJSON, &candidate.ReviewCheckpointJSON, &candidate.Confidence,
		&candidate.Importance, &candidate.EncodingDepth, &candidate.CandidateReasonJSON, &score,
		&candidate.AdmissionDecision, &candidate.AdmissionReasonJSON, &candidate.ResultingMemoryID,
		&candidate.Status, &candidate.DedupKey, &createdAt, &updatedAt)
	if err != nil {
		return automation.MemoryCandidateRecord{}, err
	}
	if score.Valid {
		candidate.AdmissionScore = score.Float64
	}
	candidate.CreatedAt = parseTime(createdAt)
	candidate.UpdatedAt = parseTime(updatedAt)
	return candidate, nil
}

func scanCandidateRows(rows *sql.Rows) ([]automation.MemoryCandidateRecord, error) {
	defer rows.Close()
	items := make([]automation.MemoryCandidateRecord, 0)
	for rows.Next() {
		item, err := scanCandidate(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func nullableFloat(value float64) any {
	if value == 0 {
		return nil
	}
	return value
}

func automationLimit(limit int) int {
	if limit <= 0 {
		return defaultAutomationLimit
	}
	return limit
}

func updateJobResult(jobID string, result sql.Result, err error) error {
	if err != nil {
		return storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageErr(err)
	}
	if affected == 0 {
		return fmt.Errorf("JOB_NOT_FOUND: %s", jobID)
	}
	return nil
}
