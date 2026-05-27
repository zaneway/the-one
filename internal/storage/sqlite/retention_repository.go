package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retention"
)

// ListExpiredTemporaryMemories 查询已过期的临时记忆。
// 过期条件：tier=temporary 且（valid_until 已过期 或 创建时间超过 TTL 天数）。
// 排除条件：archived/deleted 状态、pinned 记忆、user_confirmed 记忆不会被自动清理。
// 设计说明：pinned 和 user_confirmed 的记忆由用户显式管理，不应被自动归档。
func (s *Store) ListExpiredTemporaryMemories(ctx context.Context, req retention.ListRequest) ([]retention.MemoryRecord, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttlDays := req.TemporaryTTLDays
	if ttlDays <= 0 {
		ttlDays = 5
	}
	// 过期条件：tier=temporary 且（valid_until 已过期 或 创建时间超过 TTL 天数）
	// 排除：archived/deleted、pinned、user_confirmed 的记忆不会被自动清理
	query := baseRetentionMemorySelect() + `
		where tier = ?
		  and state not in (?, ?)
		  and pinned = 0
		  and user_confirmed = 0
		  and (
			(valid_until is not null and julianday(valid_until) < julianday(?))
			or (valid_until is null and julianday(datetime(created_at, printf('+%d days', ?))) < julianday(?))
		  )`
	args := []any{
		memory.TierTemporary, memory.StateArchived, memory.StateDeleted,
		now.Format(time.RFC3339Nano), ttlDays, now.Format(time.RFC3339Nano),
	}
	if req.WorkspaceID != "" {
		query += " and workspace_id = ?"
		args = append(args, req.WorkspaceID)
	}
	if req.ProjectID != "" {
		query += " and project_id = ?"
		args = append(args, req.ProjectID)
	}
	query += " order by created_at asc limit ?"
	args = append(args, retentionLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanRetentionMemoryRows(rows)
}

// ArchiveTemporaryMemory 将过期的临时记忆归档。
// 操作：state -> archived, tier -> archived，并删除 FTS 索引。
// 事务保证：状态更新和 FTS 清理在同一事务中完成。
// 安全检查：已删除或已归档的记忆不会被重复处理。
func (s *Store) ArchiveTemporaryMemory(ctx context.Context, memoryID string, now time.Time) error {
	if memoryID == "" {
		return fmt.Errorf("VALIDATION_FAILED: memory id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageErr(err)
	}
	// 原子操作：state -> archived, tier -> archived
	// WHERE state not in (deleted, archived)：防止重复处理已终态的记忆
	result, err := tx.ExecContext(ctx, `update memory_item
		set state = ?, tier = ?, updated_at = ?
		where id = ? and state not in (?, ?)`,
		memory.StateArchived, memory.TierArchived, now.Format(time.RFC3339Nano),
		memoryID, memory.StateDeleted, memory.StateArchived,
	)
	if err != nil {
		_ = tx.Rollback()
		return storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return storageErr(err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("MEMORY_NOT_FOUND: %s", memoryID)
	}
	// 清理 FTS 索引：归档后的记忆不应出现在全文检索结果中
	if err := deleteFTS(ctx, tx, memoryID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return storageErr(tx.Commit())
}

// ListMemoriesForScoreRecalc 查询需要重算保留分数的记忆。
// 选择范围：provisional/pending_review/stable 状态，按 updated_at 升序（最久未更新的优先）。
// 设计说明：分数重算是批量异步任务，每次处理一批，避免长时间占用数据库连接。
func (s *Store) ListMemoriesForScoreRecalc(ctx context.Context, req retention.ListRequest) ([]retention.MemoryRecord, error) {
	query := baseRetentionMemorySelect() + `
		where state in (?, ?, ?)`
	args := []any{memory.StateProvisional, memory.StatePendingReview, memory.StateStable}
	if req.WorkspaceID != "" {
		query += " and workspace_id = ?"
		args = append(args, req.WorkspaceID)
	}
	if req.ProjectID != "" {
		query += " and project_id = ?"
		args = append(args, req.ProjectID)
	}
	query += " order by updated_at asc limit ?"
	args = append(args, retentionLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanRetentionMemoryRows(rows)
}

func (s *Store) UpdateRetentionFields(ctx context.Context, memoryID string, update retention.ScoreUpdate) error {
	if memoryID == "" {
		return fmt.Errorf("VALIDATION_FAILED: memory id is required")
	}
	now := update.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	lastReinforced := sql.NullString{}
	if update.HasLastReinforcedAt && !update.LastReinforcedAt.IsZero() {
		lastReinforced = sql.NullString{String: update.LastReinforcedAt.Format(time.RFC3339Nano), Valid: true}
	}
	result, err := s.db.ExecContext(ctx, `update memory_item
		set retention_score = ?, tier = ?, effective_reinforcement = ?,
		    reinforcement_count = ?, last_reinforced_at = ?, updated_at = ?
		where id = ? and state not in (?, ?)`,
		update.RetentionScore, update.Tier, update.EffectiveReinforcement, update.ReinforcementCount,
		lastReinforced, now.Format(time.RFC3339Nano),
		memoryID, memory.StateDeleted, memory.StateArchived,
	)
	if err != nil {
		return storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageErr(err)
	}
	if affected == 0 {
		return fmt.Errorf("MEMORY_NOT_FOUND: %s", memoryID)
	}
	return nil
}

func baseRetentionMemorySelect() string {
	return `select id, coalesce(workspace_id, ''), coalesce(project_id, ''), coalesce(scope, ''),
		coalesce(source_type, ''), state, tier, memory_type, confidence, importance,
		coalesce(source_quality, 0.7), coalesce(encoding_depth, 2), coalesce(decay_rate, 0.8),
		user_confirmed, pinned, coalesce(supersedes_id, ''), effective_reinforcement, retention_score,
		valid_until, last_validated_at, last_accessed_at, created_at, updated_at
		from memory_item`
}

func scanRetentionMemory(row rowScanner) (retention.MemoryRecord, error) {
	var record retention.MemoryRecord
	var validUntil, lastValidated, lastAccessed sql.NullString
	var createdAt, updatedAt string
	err := row.Scan(
		&record.ID, &record.WorkspaceID, &record.ProjectID, &record.Scope, &record.SourceType,
		&record.State, &record.Tier, &record.MemoryType, &record.Confidence, &record.Importance,
		&record.SourceQuality, &record.EncodingDepth, &record.DecayRate,
		&record.UserConfirmed, &record.Pinned, &record.SupersedesID,
		&record.EffectiveReinforcement, &record.RetentionScore,
		&validUntil, &lastValidated, &lastAccessed, &createdAt, &updatedAt,
	)
	if err != nil {
		return retention.MemoryRecord{}, err
	}
	if validUntil.Valid && validUntil.String != "" {
		record.HasValidUntil = true
		record.ValidUntil = parseTime(validUntil.String)
	}
	if lastValidated.Valid && lastValidated.String != "" {
		record.HasLastValidatedAt = true
		record.LastValidatedAt = parseTime(lastValidated.String)
	}
	if lastAccessed.Valid && lastAccessed.String != "" {
		record.HasLastAccessedAt = true
		record.LastAccessedAt = parseTime(lastAccessed.String)
	}
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

func scanRetentionMemoryRows(rows *sql.Rows) ([]retention.MemoryRecord, error) {
	items := make([]retention.MemoryRecord, 0)
	for rows.Next() {
		item, err := scanRetentionMemory(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func retentionLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}
