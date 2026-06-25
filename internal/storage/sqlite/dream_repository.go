package sqlite

import (
	"context"

	"github.com/zaneway/theone/internal/dream"
	"github.com/zaneway/theone/internal/memory"
)

// ListMemoriesForDream 返回可投影到 Obsidian Dream Vault 的记忆。
// 设计约束：deleted 记忆不导出；archived 是否导出由上层策略决定，仓储保留其状态供上层路由。
func (s *Store) ListMemoriesForDream(ctx context.Context, req dream.ListRequest) ([]dream.MemoryRecord, error) {
	query := `select id, scope, coalesce(workspace_id, ''), coalesce(project_id, ''), coalesce(repo_id, ''),
		memory_type, coalesce(title, ''), content, coalesce(keywords_json, ''), coalesce(entities_json, ''),
		coalesce(tags_json, ''), state, tier, coalesce(version, 0), coalesce(confidence, 0),
		coalesce(importance, 0), created_at, updated_at
		from memory_item
		where state != ?`
	args := []any{memory.StateDeleted}
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
	query += " order by updated_at asc"
	if req.Limit > 0 {
		query += " limit ?"
		args = append(args, req.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	out := make([]dream.MemoryRecord, 0)
	for rows.Next() {
		var record dream.MemoryRecord
		var createdAt, updatedAt string
		if err := rows.Scan(&record.ID, &record.Scope, &record.WorkspaceID, &record.ProjectID, &record.RepoID,
			&record.MemoryType, &record.Title, &record.Content, &record.KeywordsJSON, &record.EntitiesJSON,
			&record.TagsJSON, &record.State, &record.Tier, &record.Version, &record.Confidence,
			&record.Importance, &createdAt, &updatedAt); err != nil {
			return nil, storageErr(err)
		}
		record.CreatedAt = parseTime(createdAt)
		record.UpdatedAt = parseTime(updatedAt)
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageErr(err)
	}
	return out, nil
}

// ListRelationsForDream 返回 Dream 投影可用的强关系边。
// 只返回两端均未 deleted 的边，避免 Obsidian 图谱链接到已删除记忆。
func (s *Store) ListRelationsForDream(ctx context.Context, req dream.ListRequest) ([]dream.RelationRecord, error) {
	query := `select r.source_id, r.target_id, r.relation_type, r.weight, r.updated_at
		from memory_relation r
		join memory_item src on src.id = r.source_id
		join memory_item dst on dst.id = r.target_id
		where src.state != ? and dst.state != ?`
	args := []any{memory.StateDeleted, memory.StateDeleted}
	if req.WorkspaceID != "" {
		query += " and src.workspace_id = ? and dst.workspace_id = ?"
		args = append(args, req.WorkspaceID, req.WorkspaceID)
	}
	if req.ProjectID != "" {
		query += " and src.project_id = ? and dst.project_id = ?"
		args = append(args, req.ProjectID, req.ProjectID)
	}
	if req.RepoID != "" {
		query += " and src.repo_id = ? and dst.repo_id = ?"
		args = append(args, req.RepoID, req.RepoID)
	}
	query += " order by r.updated_at asc"
	if req.Limit > 0 {
		query += " limit ?"
		args = append(args, req.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	out := make([]dream.RelationRecord, 0)
	for rows.Next() {
		var record dream.RelationRecord
		var updatedAt string
		if err := rows.Scan(&record.SourceID, &record.TargetID, &record.RelationType, &record.Weight, &updatedAt); err != nil {
			return nil, storageErr(err)
		}
		record.UpdatedAt = parseTime(updatedAt)
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageErr(err)
	}
	return out, nil
}
