package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/docindex"
	"github.com/zaneway/the-one/internal/idgen"
)

const (
	defaultDocSnapshotLimit = 20
	maxDocSectionSummary    = 512
)

// WriteDocSnapshot 原子写入文档 snapshot 和章节 snapshot。
// 幂等性：相同 workspace/project/repo/doc_path/content_hash 已存在时，直接返回既有 snapshot。
func (s *Store) WriteDocSnapshot(ctx context.Context, snapshot docindex.DocumentSnapshot) (docindex.DocumentSnapshot, error) {
	if err := validateDocSnapshot(snapshot); err != nil {
		return docindex.DocumentSnapshot{}, err
	}
	snapshot.Path = normalizeDocPath(snapshot.Path)
	snapshot.ProjectID = strings.TrimSpace(snapshot.ProjectID)
	snapshot.RepoID = strings.TrimSpace(snapshot.RepoID)
	snapshot.SectionCount = len(snapshot.Sections)

	if existing, found, err := s.findDocSnapshotByDedup(ctx, snapshot); err != nil {
		return docindex.DocumentSnapshot{}, err
	} else if found {
		return s.GetDocSnapshot(ctx, existing.ID, true)
	}

	now := time.Now().UTC()
	if snapshot.ID == "" {
		id, err := idgen.New("doc")
		if err != nil {
			return docindex.DocumentSnapshot{}, err
		}
		snapshot.ID = id
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	for i := range snapshot.Sections {
		section := &snapshot.Sections[i]
		if err := validateDocSection(*section); err != nil {
			return docindex.DocumentSnapshot{}, err
		}
		if section.ID == "" {
			id, err := idgen.New("dsec")
			if err != nil {
				return docindex.DocumentSnapshot{}, err
			}
			section.ID = id
		}
		section.SnapshotID = snapshot.ID
		section.Summary = compactRetrievalText(section.Summary, maxDocSectionSummary)
		if section.CreatedAt.IsZero() {
			section.CreatedAt = snapshot.CreatedAt
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return docindex.DocumentSnapshot{}, storageErr(err)
	}
	if _, err := tx.ExecContext(ctx, `insert into doc_snapshot(
		id, workspace_id, project_id, repo_id, doc_path, doc_role, content_hash,
		modified_at, section_count, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.ID, strings.TrimSpace(snapshot.WorkspaceID), snapshot.ProjectID, snapshot.RepoID,
		snapshot.Path, nullString(snapshot.Role), strings.TrimSpace(snapshot.ContentHash),
		nullableTime(snapshot.ModifiedAt), snapshot.SectionCount, snapshot.CreatedAt.Format(time.RFC3339Nano),
	); err != nil {
		_ = tx.Rollback()
		return docindex.DocumentSnapshot{}, storageErr(err)
	}
	for _, section := range snapshot.Sections {
		headingPathJSON, err := toJSONText(section.HeadingPath)
		if err != nil {
			_ = tx.Rollback()
			return docindex.DocumentSnapshot{}, err
		}
		if _, err := tx.ExecContext(ctx, `insert into doc_section_snapshot(
			id, snapshot_id, section_id, heading_path_json, level, start_line, end_line,
			content_hash, summary, created_at
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			section.ID, snapshot.ID, section.SectionID, nullString(headingPathJSON),
			nullablePositiveInt(section.Level), nullablePositiveInt(section.StartLine),
			nullablePositiveInt(section.EndLine), strings.TrimSpace(section.ContentHash),
			nullString(section.Summary), section.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			_ = tx.Rollback()
			return docindex.DocumentSnapshot{}, storageErr(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return docindex.DocumentSnapshot{}, storageErr(err)
	}
	return s.GetDocSnapshot(ctx, snapshot.ID, true)
}

// GetDocSnapshot 按 snapshot ID 读取文档快照。
func (s *Store) GetDocSnapshot(ctx context.Context, id string, includeSections bool) (docindex.DocumentSnapshot, error) {
	if strings.TrimSpace(id) == "" {
		return docindex.DocumentSnapshot{}, fmt.Errorf("VALIDATION_FAILED: snapshot id is required")
	}
	snapshot, err := scanDocSnapshot(s.db.QueryRowContext(ctx, baseDocSnapshotSelect()+" where id = ?", id))
	if err == sql.ErrNoRows {
		return docindex.DocumentSnapshot{}, fmt.Errorf("DOC_SNAPSHOT_NOT_FOUND: %s", id)
	}
	if err != nil {
		return docindex.DocumentSnapshot{}, storageErr(err)
	}
	if includeSections {
		sections, err := s.ListDocSections(ctx, id)
		if err != nil {
			return docindex.DocumentSnapshot{}, err
		}
		snapshot.Sections = sections
	}
	return snapshot, nil
}

// ListDocSnapshots 按 workspace + doc_path 查询文档快照。
// 查询边界：workspace_id 和 doc_path 必填，避免 docindex 诊断工具扫描全部文档历史。
func (s *Store) ListDocSnapshots(ctx context.Context, query docindex.SnapshotQuery) ([]docindex.DocumentSnapshot, error) {
	if strings.TrimSpace(query.WorkspaceID) == "" || strings.TrimSpace(query.Path) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: workspace_id and doc_path are required")
	}
	docPath := normalizeDocPath(query.Path)
	if err := validateDocPath(docPath); err != nil {
		return nil, err
	}
	sqlText := baseDocSnapshotSelect() + ` where workspace_id = ? and doc_path = ?`
	args := []any{strings.TrimSpace(query.WorkspaceID), docPath}
	if query.ProjectID != "" {
		sqlText += " and project_id = ?"
		args = append(args, strings.TrimSpace(query.ProjectID))
	}
	if query.RepoID != "" {
		sqlText += " and repo_id = ?"
		args = append(args, strings.TrimSpace(query.RepoID))
	}
	if query.ContentHash != "" {
		sqlText += " and content_hash = ?"
		args = append(args, strings.TrimSpace(query.ContentHash))
	}
	sqlText += " order by created_at desc limit ?"
	args = append(args, docSnapshotLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	snapshots, err := scanDocSnapshotRows(rows)
	if err != nil {
		return nil, err
	}
	if query.IncludeSections {
		for i := range snapshots {
			sections, err := s.ListDocSections(ctx, snapshots[i].ID)
			if err != nil {
				return nil, err
			}
			snapshots[i].Sections = sections
		}
	}
	return snapshots, nil
}

// ListDocSections 查询某个 snapshot 下的章节快照。
func (s *Store) ListDocSections(ctx context.Context, snapshotID string) ([]docindex.DocumentSection, error) {
	if strings.TrimSpace(snapshotID) == "" {
		return nil, fmt.Errorf("VALIDATION_FAILED: snapshot_id is required")
	}
	rows, err := s.db.QueryContext(ctx, baseDocSectionSelect()+` where snapshot_id = ?
		order by start_line asc, section_id asc`, snapshotID)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanDocSectionRows(rows)
}

// DeleteDocSnapshot 删除文档 snapshot，并同步删除其 section snapshot。
func (s *Store) DeleteDocSnapshot(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("VALIDATION_FAILED: snapshot id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageErr(err)
	}
	if _, err := tx.ExecContext(ctx, "delete from doc_section_snapshot where snapshot_id = ?", id); err != nil {
		_ = tx.Rollback()
		return storageErr(err)
	}
	result, err := tx.ExecContext(ctx, "delete from doc_snapshot where id = ?", id)
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
		return fmt.Errorf("DOC_SNAPSHOT_NOT_FOUND: %s", id)
	}
	return storageErr(tx.Commit())
}

func (s *Store) findDocSnapshotByDedup(ctx context.Context, snapshot docindex.DocumentSnapshot) (docindex.DocumentSnapshot, bool, error) {
	row := s.db.QueryRowContext(ctx, baseDocSnapshotSelect()+` where workspace_id = ?
		and project_id = ? and repo_id = ? and doc_path = ? and content_hash = ?
		limit 1`, strings.TrimSpace(snapshot.WorkspaceID), snapshot.ProjectID, snapshot.RepoID,
		snapshot.Path, strings.TrimSpace(snapshot.ContentHash))
	existing, err := scanDocSnapshot(row)
	if err == sql.ErrNoRows {
		return docindex.DocumentSnapshot{}, false, nil
	}
	if err != nil {
		return docindex.DocumentSnapshot{}, false, storageErr(err)
	}
	return existing, true, nil
}

func validateDocSnapshot(snapshot docindex.DocumentSnapshot) error {
	if strings.TrimSpace(snapshot.WorkspaceID) == "" || strings.TrimSpace(snapshot.Path) == "" || strings.TrimSpace(snapshot.ContentHash) == "" {
		return fmt.Errorf("VALIDATION_FAILED: workspace_id, doc_path and content_hash are required")
	}
	docPath := normalizeDocPath(snapshot.Path)
	if err := validateDocPath(docPath); err != nil {
		return err
	}
	seenSections := make(map[string]struct{}, len(snapshot.Sections))
	for _, section := range snapshot.Sections {
		if err := validateDocSection(section); err != nil {
			return err
		}
		if _, ok := seenSections[section.SectionID]; ok {
			return fmt.Errorf("VALIDATION_FAILED: duplicate section_id %q", section.SectionID)
		}
		seenSections[section.SectionID] = struct{}{}
	}
	return nil
}

func validateDocSection(section docindex.DocumentSection) error {
	if strings.TrimSpace(section.SectionID) == "" || strings.TrimSpace(section.ContentHash) == "" {
		return fmt.Errorf("VALIDATION_FAILED: section_id and content_hash are required")
	}
	if section.Level < 0 || section.StartLine < 0 || section.EndLine < 0 ||
		(section.StartLine > 0 && section.EndLine > 0 && section.EndLine < section.StartLine) {
		return fmt.Errorf("VALIDATION_FAILED: invalid section line range")
	}
	return nil
}

func validateDocPath(docPath string) error {
	if docPath == "" || docPath == "." {
		return fmt.Errorf("VALIDATION_FAILED: doc_path is required")
	}
	if filepath.IsAbs(docPath) || docPath == ".." || strings.HasPrefix(docPath, "../") {
		return fmt.Errorf("VALIDATION_FAILED: doc_path must be workspace-relative")
	}
	ext := strings.ToLower(filepath.Ext(docPath))
	if ext != ".md" && ext != ".markdown" {
		return fmt.Errorf("VALIDATION_FAILED: doc_path must be markdown")
	}
	return nil
}

func baseDocSnapshotSelect() string {
	return `select id, workspace_id, project_id, repo_id, doc_path, coalesce(doc_role, ''),
		content_hash, coalesce(modified_at, ''), section_count, created_at
		from doc_snapshot`
}

func baseDocSectionSelect() string {
	return `select id, snapshot_id, section_id, coalesce(heading_path_json, ''), coalesce(level, 0),
		coalesce(start_line, 0), coalesce(end_line, 0), content_hash, coalesce(summary, ''), created_at
		from doc_section_snapshot`
}

func scanDocSnapshotRows(rows *sql.Rows) ([]docindex.DocumentSnapshot, error) {
	snapshots := make([]docindex.DocumentSnapshot, 0)
	for rows.Next() {
		snapshot, err := scanDocSnapshot(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, storageErr(rows.Err())
}

func scanDocSnapshot(row rowScanner) (docindex.DocumentSnapshot, error) {
	var snapshot docindex.DocumentSnapshot
	var modifiedAt, createdAt string
	err := row.Scan(&snapshot.ID, &snapshot.WorkspaceID, &snapshot.ProjectID, &snapshot.RepoID,
		&snapshot.Path, &snapshot.Role, &snapshot.ContentHash, &modifiedAt, &snapshot.SectionCount, &createdAt)
	if err != nil {
		return docindex.DocumentSnapshot{}, err
	}
	snapshot.ModifiedAt = parseTime(modifiedAt)
	snapshot.CreatedAt = parseTime(createdAt)
	return snapshot, nil
}

func scanDocSectionRows(rows *sql.Rows) ([]docindex.DocumentSection, error) {
	sections := make([]docindex.DocumentSection, 0)
	for rows.Next() {
		section, err := scanDocSection(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		sections = append(sections, section)
	}
	return sections, storageErr(rows.Err())
}

func scanDocSection(row rowScanner) (docindex.DocumentSection, error) {
	var section docindex.DocumentSection
	var headingPathJSON, createdAt string
	err := row.Scan(&section.ID, &section.SnapshotID, &section.SectionID, &headingPathJSON,
		&section.Level, &section.StartLine, &section.EndLine, &section.ContentHash, &section.Summary, &createdAt)
	if err != nil {
		return docindex.DocumentSection{}, err
	}
	section.HeadingPath = decodeStringSlice(headingPathJSON)
	section.CreatedAt = parseTime(createdAt)
	return section, nil
}

func normalizeDocPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(value))
}

func docSnapshotLimit(limit int) int {
	if limit <= 0 {
		return defaultDocSnapshotLimit
	}
	return limit
}
