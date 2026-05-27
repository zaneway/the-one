package sqlite

import (
	"context"
	"strings"

	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/retention"
)

// AggregateRelationSignals 批量聚合记忆关系计数，用于 relation_factor 与 conflict_penalty。
func (s *Store) AggregateRelationSignals(ctx context.Context, memoryIDs []string) (map[string]retention.RelationSignals, error) {
	result := make(map[string]retention.RelationSignals, len(memoryIDs))
	if len(memoryIDs) == 0 {
		return result, nil
	}
	ph := sqlPlaceholders(len(memoryIDs))
	args := duplicateIDs(memoryIDs)

	query := `
		select edge.memory_id,
			sum(case when edge.relation_type in ('supports', 'corrected_by') then 1 else 0 end) as supporting,
			sum(case when edge.relation_type = 'contradicts' then 1 else 0 end) as contradicting,
			sum(case when edge.relation_type in ('linked_to_long', 'supersedes') then 1 else 0 end) as linked_long,
			sum(case when edge.relation_type = 'superseded_by' then 1 else 0 end) as superseded
		from (
			select source_id as memory_id, relation_type from memory_relation where source_id in (` + ph + `)
			union all
			select target_id as memory_id, relation_type from memory_relation where target_id in (` + ph + `)
		) edge
		group by edge.memory_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()

	for rows.Next() {
		var memoryID string
		var supporting, contradicting, linkedLong, superseded int
		if err := rows.Scan(&memoryID, &supporting, &contradicting, &linkedLong, &superseded); err != nil {
			return nil, storageErr(err)
		}
		result[memoryID] = retention.RelationSignals{
			SupportingCount:     supporting,
			ContradictingCount:  contradicting,
			LinkedLongTermCount: linkedLong,
			IsSuperseded:        superseded > 0,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, storageErr(err)
	}

	unresolved, err := s.aggregateUnresolvedConflicts(ctx, memoryIDs)
	if err != nil {
		return nil, err
	}
	for memoryID, count := range unresolved {
		signals := result[memoryID]
		signals.UnresolvedConflictCount = count
		result[memoryID] = signals
	}
	return result, nil
}

func (s *Store) aggregateUnresolvedConflicts(ctx context.Context, memoryIDs []string) (map[string]int, error) {
	result := make(map[string]int)
	if len(memoryIDs) == 0 {
		return result, nil
	}
	ph := sqlPlaceholders(len(memoryIDs))
	args := make([]any, 0, len(memoryIDs)*2+2)
	for _, id := range memoryIDs {
		args = append(args, id)
	}
	args = append(args, memory.StateStable)
	for _, id := range memoryIDs {
		args = append(args, id)
	}
	args = append(args, memory.StateStable)

	query := `
		select memory_id, sum(cnt) from (
			select r.source_id as memory_id, count(*) as cnt
			from memory_relation r
			join memory_item m on m.id = r.target_id
			where r.relation_type = 'contradicts'
			  and r.source_id in (` + ph + `)
			  and m.state = ?
			group by r.source_id
			union all
			select r.target_id as memory_id, count(*) as cnt
			from memory_relation r
			join memory_item m on m.id = r.source_id
			where r.relation_type = 'contradicts'
			  and r.target_id in (` + ph + `)
			  and m.state = ?
			group by r.target_id
		) grouped
		group by memory_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var memoryID string
		var count int
		if err := rows.Scan(&memoryID, &count); err != nil {
			return nil, storageErr(err)
		}
		result[memoryID] = count
	}
	return result, storageErr(rows.Err())
}

// CountStaleCodeRefs 统计每条记忆关联的 stale/missing/ambiguous code_ref 数量。
func (s *Store) CountStaleCodeRefs(ctx context.Context, memoryIDs []string) (map[string]int, error) {
	result := make(map[string]int)
	if len(memoryIDs) == 0 {
		return result, nil
	}
	ph := sqlPlaceholders(len(memoryIDs))
	args := make([]any, len(memoryIDs))
	for i, memoryID := range memoryIDs {
		args[i] = memoryID
	}
	query := `select memory_id, count(*)
		from code_ref
		where memory_id in (` + ph + `)
		  and resolve_status in ('stale', 'missing', 'ambiguous')
		group by memory_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var memoryID string
		var count int
		if err := rows.Scan(&memoryID, &count); err != nil {
			return nil, storageErr(err)
		}
		result[memoryID] = count
	}
	return result, storageErr(rows.Err())
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

func duplicateIDs(ids []string) []any {
	args := make([]any, len(ids)*2)
	for i, id := range ids {
		args[i] = id
		args[i+len(ids)] = id
	}
	return args
}
