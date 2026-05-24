package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retention"
)

func (s *Store) ListExpiredTemporaryMemories(ctx context.Context, req retention.ListRequest) ([]retention.MemoryRecord, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttlDays := req.TemporaryTTLDays
	if ttlDays <= 0 {
		ttlDays = 5
	}
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
	if err := deleteFTS(ctx, tx, memoryID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return storageErr(tx.Commit())
}

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
	result, err := s.db.ExecContext(ctx, `update memory_item
		set retention_score = ?, tier = ?, updated_at = ?
		where id = ? and state not in (?, ?)`,
		update.RetentionScore, update.Tier, now.Format(time.RFC3339Nano),
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
	return `select id, coalesce(workspace_id, ''), coalesce(project_id, ''), state, tier, memory_type,
		confidence, importance, user_confirmed, pinned, effective_reinforcement, retention_score,
		valid_until, created_at, updated_at
		from memory_item`
}

func scanRetentionMemory(row rowScanner) (retention.MemoryRecord, error) {
	var record retention.MemoryRecord
	var validUntil sql.NullString
	var createdAt, updatedAt string
	err := row.Scan(&record.ID, &record.WorkspaceID, &record.ProjectID, &record.State, &record.Tier,
		&record.MemoryType, &record.Confidence, &record.Importance, &record.UserConfirmed, &record.Pinned,
		&record.EffectiveReinforcement, &record.RetentionScore, &validUntil, &createdAt, &updatedAt)
	if err != nil {
		return retention.MemoryRecord{}, err
	}
	if validUntil.Valid && validUntil.String != "" {
		record.HasValidUntil = true
		record.ValidUntil = parseTime(validUntil.String)
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
