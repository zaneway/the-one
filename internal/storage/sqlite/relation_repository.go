package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/retrieval"
)

const defaultRelationExpansionLimit = 20

// ListRelationExpansions 查询 P4-C2 depth=1 关系扩展边。
// 设计约束：必须提供 seed memory；只读取持久化强关系边，不在线构建新关系，不做多跳图遍历。
func (s *Store) ListRelationExpansions(ctx context.Context, query retrieval.RelationExpansionQuery) ([]retrieval.RelationExpansion, error) {
	seedIDs := compactUniqueStrings(query.SeedMemoryIDs)
	if len(seedIDs) == 0 {
		return nil, fmt.Errorf("VALIDATION_FAILED: seed_memory_ids is required")
	}
	relationTypes := compactUniqueStrings(query.RelationTypes)
	if len(relationTypes) == 0 {
		relationTypes = retrieval.DefaultRelationTypes()
	}
	sqlText := `select source_id, target_id, relation_type, weight
		from memory_relation
		where (source_id in (` + placeholders(len(seedIDs)) + `) or target_id in (` + placeholders(len(seedIDs)) + `))
		and relation_type in (` + placeholders(len(relationTypes)) + `)
		order by updated_at desc
		limit ?`
	args := make([]any, 0, len(seedIDs)*2+len(relationTypes)+1)
	for _, id := range seedIDs {
		args = append(args, id)
	}
	for _, id := range seedIDs {
		args = append(args, id)
	}
	for _, relationType := range relationTypes {
		args = append(args, relationType)
	}
	args = append(args, relationExpansionLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()

	seedSet := make(map[string]bool, len(seedIDs))
	for _, id := range seedIDs {
		seedSet[id] = true
	}
	out := make([]retrieval.RelationExpansion, 0)
	seen := map[string]bool{}
	for rows.Next() {
		var sourceID, targetID, relationType string
		var weight float64
		if err := rows.Scan(&sourceID, &targetID, &relationType, &weight); err != nil {
			return nil, storageErr(err)
		}
		if seedSet[sourceID] {
			expansion, ok, err := s.relationExpansionForMemory(ctx, query, sourceID, targetID, relationType, retrieval.RelationDirectionOutgoing, weight)
			if err != nil {
				return nil, err
			}
			if ok && !seen[relationExpansionKey(expansion)] {
				seen[relationExpansionKey(expansion)] = true
				out = append(out, expansion)
			}
		}
		if seedSet[targetID] {
			expansion, ok, err := s.relationExpansionForMemory(ctx, query, targetID, sourceID, relationType, retrieval.RelationDirectionIncoming, weight)
			if err != nil {
				return nil, err
			}
			if ok && !seen[relationExpansionKey(expansion)] {
				seen[relationExpansionKey(expansion)] = true
				out = append(out, expansion)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, storageErr(err)
	}
	return out, nil
}

func (s *Store) relationExpansionForMemory(ctx context.Context, query retrieval.RelationExpansionQuery, seedID, relatedID, relationType, direction string, weight float64) (retrieval.RelationExpansion, bool, error) {
	if seedID == relatedID || strings.TrimSpace(relatedID) == "" {
		return retrieval.RelationExpansion{}, false, nil
	}
	req := memory.SearchRequest{
		WorkspaceID:     query.WorkspaceID,
		ProjectID:       query.ProjectID,
		RepoID:          query.RepoID,
		SessionID:       query.SessionID,
		Scope:           append([]string(nil), query.Scopes...),
		IncludeArchived: query.IncludeArchived,
	}
	sqlText := baseMemorySelect() + " where id = ?"
	args := []any{relatedID}
	sqlText, args = appendSearchFilters(sqlText, args, req, true)
	item, err := scanMemory(s.db.QueryRowContext(ctx, sqlText, args...))
	if err == sql.ErrNoRows {
		return retrieval.RelationExpansion{}, false, nil
	}
	if err != nil {
		return retrieval.RelationExpansion{}, false, storageErr(err)
	}
	if weight == 0 {
		weight = 1.0
	}
	return retrieval.RelationExpansion{
		SeedMemoryID:  seedID,
		Direction:     direction,
		RelationType:  relationType,
		Weight:        weight,
		RelatedMemory: item,
	}, true, nil
}

func relationExpansionLimit(limit int) int {
	if limit <= 0 {
		return defaultRelationExpansionLimit
	}
	if limit > defaultRelationExpansionLimit {
		return defaultRelationExpansionLimit
	}
	return limit
}

func relationExpansionKey(expansion retrieval.RelationExpansion) string {
	return expansion.SeedMemoryID + "\x00" + expansion.Direction + "\x00" + expansion.RelationType + "\x00" + expansion.RelatedMemory.ID
}
