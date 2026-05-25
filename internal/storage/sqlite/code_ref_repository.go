package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/idgen"
	"github.com/zaneway/the-one/internal/memory"
)

const (
	defaultCodeRefLimit   = 100
	maxCodeRefSummaryRune = 512
)

// WriteCodeRef 写入或更新一条 code_ref。
// 设计约束：code_ref 只保存 repo/file/symbol/hash/摘要和解析状态，不保存源码、调用关系或完整 diff。
func (s *Store) WriteCodeRef(ctx context.Context, ref memory.CodeRef) (memory.CodeRef, error) {
	if err := validateCodeRef(ref); err != nil {
		return memory.CodeRef{}, err
	}
	if ref.ID == "" {
		id, err := idgen.New("cr")
		if err != nil {
			return memory.CodeRef{}, err
		}
		ref.ID = id
	}
	if ref.ResolveStatus == "" {
		ref.ResolveStatus = memory.CodeRefStatusUnresolved
	}
	ref.FilePath = normalizeCodeRefPath(ref.FilePath)
	ref.RefSummary = compactRetrievalText(ref.RefSummary, maxCodeRefSummaryRune)

	now := time.Now().UTC()
	resolvedAt := nullableResolvedAt(ref.ResolveStatus, now)
	_, err := s.db.ExecContext(ctx, `insert into code_ref(
		id, memory_id, repo_id, commit_hash, file_path, symbol, line_start, line_end,
		content_hash, ref_summary, resolve_status, resolved_at, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(id) do update set
		memory_id = excluded.memory_id,
		repo_id = excluded.repo_id,
		commit_hash = excluded.commit_hash,
		file_path = excluded.file_path,
		symbol = excluded.symbol,
		line_start = excluded.line_start,
		line_end = excluded.line_end,
		content_hash = excluded.content_hash,
		ref_summary = excluded.ref_summary,
		resolve_status = excluded.resolve_status,
		resolved_at = excluded.resolved_at,
		updated_at = excluded.updated_at`,
		ref.ID, ref.MemoryID, ref.RepoID, nullString(ref.CommitHash), nullString(ref.FilePath),
		nullString(ref.Symbol), nullablePositiveInt(ref.LineStart), nullablePositiveInt(ref.LineEnd),
		nullString(ref.ContentHash), nullString(ref.RefSummary), ref.ResolveStatus, resolvedAt,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return memory.CodeRef{}, storageErr(err)
	}
	return s.GetCodeRef(ctx, ref.ID)
}

// GetCodeRef 按 code_ref id 读取代码引用。
func (s *Store) GetCodeRef(ctx context.Context, id string) (memory.CodeRef, error) {
	if strings.TrimSpace(id) == "" {
		return memory.CodeRef{}, fmt.Errorf("VALIDATION_FAILED: code_ref id is required")
	}
	ref, err := scanCodeRef(s.db.QueryRowContext(ctx, baseCodeRefSelect()+" where id = ?", id))
	if err == sql.ErrNoRows {
		return memory.CodeRef{}, fmt.Errorf("CODE_REF_NOT_FOUND: %s", id)
	}
	return ref, storageErr(err)
}

// ListCodeRefs 查询 code_ref。
// 查询边界：必须提供 memory_id，或 repo_id + file_path；诊断工具不得无条件扫描 code_ref 表。
func (s *Store) ListCodeRefs(ctx context.Context, query memory.CodeRefQuery) ([]memory.CodeRef, error) {
	if strings.TrimSpace(query.MemoryID) == "" && (strings.TrimSpace(query.RepoID) == "" || strings.TrimSpace(query.FilePath) == "") {
		return nil, fmt.Errorf("VALIDATION_FAILED: memory_id or repo_id + file_path is required")
	}
	sqlText := baseCodeRefSelect() + " where 1 = 1"
	args := make([]any, 0, 6)
	if query.MemoryID != "" {
		sqlText += " and memory_id = ?"
		args = append(args, strings.TrimSpace(query.MemoryID))
	}
	if query.RepoID != "" {
		sqlText += " and repo_id = ?"
		args = append(args, strings.TrimSpace(query.RepoID))
	}
	if query.FilePath != "" {
		sqlText += " and coalesce(file_path, '') = ?"
		args = append(args, normalizeCodeRefPath(query.FilePath))
	}
	if query.Symbol != "" {
		sqlText += " and coalesce(symbol, '') = ?"
		args = append(args, strings.TrimSpace(query.Symbol))
	}
	if query.ResolveStatus != "" {
		if !isValidCodeRefStatus(query.ResolveStatus) {
			return nil, fmt.Errorf("VALIDATION_FAILED: invalid resolve_status %q", query.ResolveStatus)
		}
		sqlText += " and resolve_status = ?"
		args = append(args, query.ResolveStatus)
	}
	sqlText += " order by updated_at desc, id asc limit ?"
	args = append(args, codeRefLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanCodeRefRows(rows)
}

// ListCodeRefsForRefresh 按 repo_id 查询需要重新解析的 code_ref。
// 该入口仅供 refresh_code_ref_status job 使用，必须带 repo_id，避免刷新任务扫描全表。
func (s *Store) ListCodeRefsForRefresh(ctx context.Context, repoID string, limit int) ([]memory.CodeRef, error) {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: repo_id is required")
	}
	rows, err := s.db.QueryContext(ctx, baseCodeRefSelect()+`
		where repo_id = ?
		  and resolve_status in (?, ?, ?)
		order by updated_at asc, id asc
		limit ?`,
		repoID,
		memory.CodeRefStatusUnresolved,
		memory.CodeRefStatusResolved,
		memory.CodeRefStatusStale,
		codeRefLimit(limit),
	)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanCodeRefRows(rows)
}

// UpdateCodeRefResolveStatus 更新 code_ref 解析状态。
// 状态为 resolved 时会刷新 resolved_at；其他状态保留为空，用于后续 staleness penalty 和诊断展示。
func (s *Store) UpdateCodeRefResolveStatus(ctx context.Context, id, status, contentHash, refSummary string) (memory.CodeRef, error) {
	if strings.TrimSpace(id) == "" {
		return memory.CodeRef{}, fmt.Errorf("VALIDATION_FAILED: code_ref id is required")
	}
	if !isValidCodeRefStatus(status) {
		return memory.CodeRef{}, fmt.Errorf("VALIDATION_FAILED: invalid resolve_status %q", status)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `update code_ref set
		resolve_status = ?,
		resolved_at = ?,
		content_hash = coalesce(?, content_hash),
		ref_summary = coalesce(?, ref_summary),
		updated_at = ?
		where id = ?`,
		status, nullableResolvedAt(status, now), nullString(strings.TrimSpace(contentHash)),
		nullString(compactRetrievalText(refSummary, maxCodeRefSummaryRune)), now.Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return memory.CodeRef{}, storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return memory.CodeRef{}, storageErr(err)
	}
	if affected == 0 {
		return memory.CodeRef{}, fmt.Errorf("CODE_REF_NOT_FOUND: %s", id)
	}
	return s.GetCodeRef(ctx, id)
}

// DeleteCodeRef 删除单条 code_ref。
func (s *Store) DeleteCodeRef(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("VALIDATION_FAILED: code_ref id is required")
	}
	result, err := s.db.ExecContext(ctx, "delete from code_ref where id = ?", id)
	if err != nil {
		return storageErr(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageErr(err)
	}
	if affected == 0 {
		return fmt.Errorf("CODE_REF_NOT_FOUND: %s", id)
	}
	return nil
}

// DeleteCodeRefsByMemory 删除某条 memory 下的所有 code_ref。
// 该方法供删除一致性和未来敏感删除流程复用。
func (s *Store) DeleteCodeRefsByMemory(ctx context.Context, memoryID string) error {
	if strings.TrimSpace(memoryID) == "" {
		return fmt.Errorf("VALIDATION_FAILED: memory_id is required")
	}
	_, err := s.db.ExecContext(ctx, "delete from code_ref where memory_id = ?", memoryID)
	return storageErr(err)
}

func validateCodeRef(ref memory.CodeRef) error {
	if strings.TrimSpace(ref.MemoryID) == "" || strings.TrimSpace(ref.RepoID) == "" {
		return fmt.Errorf("VALIDATION_FAILED: memory_id and repo_id are required")
	}
	if strings.TrimSpace(ref.FilePath) == "" && strings.TrimSpace(ref.Symbol) == "" {
		return fmt.Errorf("VALIDATION_FAILED: file_path or symbol is required")
	}
	if ref.ResolveStatus != "" && !isValidCodeRefStatus(ref.ResolveStatus) {
		return fmt.Errorf("VALIDATION_FAILED: invalid resolve_status %q", ref.ResolveStatus)
	}
	if ref.LineStart < 0 || ref.LineEnd < 0 || (ref.LineStart > 0 && ref.LineEnd > 0 && ref.LineEnd < ref.LineStart) {
		return fmt.Errorf("VALIDATION_FAILED: invalid line range")
	}
	normalizedPath := normalizeCodeRefPath(ref.FilePath)
	if strings.HasPrefix(normalizedPath, "../") || normalizedPath == ".." || filepath.IsAbs(normalizedPath) {
		return fmt.Errorf("VALIDATION_FAILED: file_path must be repo-relative")
	}
	return nil
}

func baseCodeRefSelect() string {
	return `select id, memory_id, repo_id, coalesce(commit_hash, ''), coalesce(file_path, ''),
		coalesce(symbol, ''), coalesce(line_start, 0), coalesce(line_end, 0),
		coalesce(content_hash, ''), coalesce(ref_summary, ''), resolve_status
		from code_ref`
}

func scanCodeRefRows(rows *sql.Rows) ([]memory.CodeRef, error) {
	refs := make([]memory.CodeRef, 0)
	for rows.Next() {
		ref, err := scanCodeRef(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		refs = append(refs, ref)
	}
	return refs, storageErr(rows.Err())
}

func scanCodeRef(row rowScanner) (memory.CodeRef, error) {
	var ref memory.CodeRef
	err := row.Scan(&ref.ID, &ref.MemoryID, &ref.RepoID, &ref.CommitHash, &ref.FilePath,
		&ref.Symbol, &ref.LineStart, &ref.LineEnd, &ref.ContentHash, &ref.RefSummary, &ref.ResolveStatus)
	if err != nil {
		return memory.CodeRef{}, err
	}
	return ref, nil
}

func nullablePositiveInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableResolvedAt(status string, now time.Time) any {
	if status != memory.CodeRefStatusResolved {
		return nil
	}
	return now.Format(time.RFC3339Nano)
}

func normalizeCodeRefPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(value))
}

func isValidCodeRefStatus(status string) bool {
	switch status {
	case memory.CodeRefStatusUnresolved,
		memory.CodeRefStatusResolved,
		memory.CodeRefStatusStale,
		memory.CodeRefStatusMissing,
		memory.CodeRefStatusAmbiguous:
		return true
	default:
		return false
	}
}

func codeRefLimit(limit int) int {
	if limit <= 0 {
		return defaultCodeRefLimit
	}
	return limit
}
