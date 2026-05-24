package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/automation"
	"github.com/zaneway/the-one/internal/memory"
)

// FindDuplicateEvidence 按 P3 evidence 幂等键查找已有证据。
// 去重规则：raw_event_id + source_type + interpreted_statement 三元组唯一。
// 设计说明：同一事件可能被多次处理（worker 重试），幂等写入避免重复 evidence。
func (s *Store) FindDuplicateEvidence(ctx context.Context, key automation.EvidenceDraftKey) (memory.Evidence, bool, error) {
	if key.RawEventID == "" || key.SourceType == "" || key.InterpretedStatement == "" {
		return memory.Evidence{}, false, fmt.Errorf("VALIDATION_FAILED: raw_event_id, source_type and interpreted_statement are required")
	}
	row := s.db.QueryRowContext(ctx, baseEvidenceSelect()+` where raw_event_id = ?
		and source_type = ?
		and interpreted_statement = ?
		order by created_at desc
		limit 1`, key.RawEventID, key.SourceType, key.InterpretedStatement)
	evidence, err := scanEvidence(row)
	if err == sql.ErrNoRows {
		return memory.Evidence{}, false, nil
	}
	if err != nil {
		return memory.Evidence{}, false, storageErr(err)
	}
	return evidence, true, nil
}

func (s *Store) WriteEvidence(ctx context.Context, evidence memory.Evidence) error {
	if evidence.ID == "" || evidence.RawEventID == "" || evidence.SourceType == "" || evidence.InterpretedStatement == "" {
		return fmt.Errorf("VALIDATION_FAILED: evidence id, raw_event_id, source_type and interpreted_statement are required")
	}
	_, found, err := s.FindDuplicateEvidence(ctx, automation.EvidenceDraftKey{
		RawEventID:           evidence.RawEventID,
		SourceType:           evidence.SourceType,
		InterpretedStatement: evidence.InterpretedStatement,
	})
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	if evidence.Confidence == 0 {
		evidence.Confidence = 0.7
	}
	if evidence.CreatedAt.IsZero() {
		evidence.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageErr(err)
	}
	if err := insertEvidence(ctx, tx, evidence); err != nil {
		_ = tx.Rollback()
		return err
	}
	return storageErr(tx.Commit())
}

// GetEvidence 按 evidence_id 读取证据详情，供 candidate generation 和 Admission 使用。
func (s *Store) GetEvidence(ctx context.Context, evidenceID string) (memory.Evidence, error) {
	evidence, err := scanEvidence(s.db.QueryRowContext(ctx, baseEvidenceSelect()+" where id = ?", evidenceID))
	if err == sql.ErrNoRows {
		return memory.Evidence{}, fmt.Errorf("EVIDENCE_NOT_FOUND: %s", evidenceID)
	}
	return evidence, storageErr(err)
}

func (s *Store) FindRelatedMemory(ctx context.Context, req automation.RelatedMemoryRequest) ([]memory.MemoryItem, error) {
	query := baseMemorySelect() + ` where state in (?, ?, ?)`
	args := []any{memory.StateStable, memory.StatePendingReview, memory.StateProvisional}
	if req.Scope != "" {
		query += " and scope = ?"
		args = append(args, req.Scope)
	}
	if req.MemoryType != "" {
		query += " and memory_type = ?"
		args = append(args, req.MemoryType)
	}
	if req.WorkspaceID != "" {
		query += " and coalesce(workspace_id, '') = ?"
		args = append(args, req.WorkspaceID)
	}
	if req.ProjectID != "" {
		query += " and coalesce(project_id, '') = ?"
		args = append(args, req.ProjectID)
	}
	if req.RepoID != "" {
		query += " and coalesce(repo_id, '') = ?"
		args = append(args, req.RepoID)
	}
	if strings.TrimSpace(req.Query) != "" {
		like := "%" + strings.TrimSpace(req.Query) + "%"
		query += " and (content like ? or coalesce(title, '') like ? or coalesce(keywords_json, '') like ?)"
		args = append(args, like, like, like)
	}
	query += " order by updated_at desc limit ?"
	args = append(args, automationLimit(req.Limit))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanMemoryRows(rows)
}

// WriteAutomatedMemory 写入 P3 自动生成的记忆。
// 与手动 Remember 的区别：
//   - 必须关联至少一个 evidence（自动化记忆必须有证据链）
//   - 自动设置默认值（confidence=0.7, importance=0.5, encoding_depth=2, decay_rate=0.8）
//   - 创建者标记为 "automation"（区别于手动写入的 "memoryd"）
//   - 支持批量写入 evidence 关联和可选 review_checkpoint
//
// 事务保证：memory_item + evidence_link + FTS + checkpoint 在同一事务中原子写入。
func (s *Store) WriteAutomatedMemory(ctx context.Context, input automation.AutomatedMemoryWrite) (memory.MemoryItem, error) {
	item := input.Item
	if item.ID == "" || item.Scope == "" || item.MemoryType == "" || item.Content == "" || item.State == "" || item.Tier == "" {
		return memory.MemoryItem{}, fmt.Errorf("VALIDATION_FAILED: memory id, scope, memory_type, content, state and tier are required")
	}
	// 自动化记忆必须关联至少一个 evidence（证据链要求）
	evidenceIDs := compactUniqueStrings(input.EvidenceIDs)
	if len(evidenceIDs) == 0 {
		return memory.MemoryItem{}, fmt.Errorf("VALIDATION_FAILED: automated memory requires at least one evidence id")
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	// 默认值填充：normalized_content 降级为 content，search_text 降级为 title+content
	if item.NormalizedContent == "" {
		item.NormalizedContent = item.Content
	}
	if item.SearchText == "" {
		item.SearchText = strings.TrimSpace(item.Title + "\n" + item.Content)
	}
	// 自动化记忆的默认评分：confidence=0.7, importance=0.5, encoding_depth=2, decay_rate=0.8
	if item.SourceQuality == 0 {
		item.SourceQuality = 0.7
	}
	if item.Confidence == 0 {
		item.Confidence = 0.7
	}
	if item.Importance == 0 {
		item.Importance = 0.5
	}
	if item.EncodingDepth == 0 {
		item.EncodingDepth = 2
	}
	if item.DecayRate == 0 {
		item.DecayRate = 0.8
	}
	if item.Version == 0 {
		item.Version = 1
	}
	// 创建者标记为 "automation"，区别于手动写入的 "memoryd"
	if item.CreatedBy == "" {
		item.CreatedBy = "automation"
	}
	relationType := firstNonEmpty(input.EvidenceRelation, "derived_from")

	// 事务保证：memory_item + evidence_link + FTS + checkpoint 原子写入
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	if err := insertMemoryItem(ctx, tx, item); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// 批量写入 evidence 关联（insert or ignore 保证幂等）
	for _, evidenceID := range evidenceIDs {
		if _, err := tx.ExecContext(ctx,
			"insert or ignore into memory_evidence_link(memory_id, evidence_id, relation_type, weight) values (?, ?, ?, ?)",
			item.ID, evidenceID, relationType, 1.0,
		); err != nil {
			_ = tx.Rollback()
			return memory.MemoryItem{}, storageErr(err)
		}
	}
	// 条件写入 FTS 索引
	if shouldIndex(item.State) {
		if err := upsertFTS(ctx, tx, item.ID, item.SearchText); err != nil {
			_ = tx.Rollback()
			return memory.MemoryItem{}, err
		}
	}
	// 可选写入 review_checkpoint
	if input.ReviewCheckpoint != nil {
		checkpoint := *input.ReviewCheckpoint
		if checkpoint.MemoryID == "" {
			checkpoint.MemoryID = item.ID
		}
		if checkpoint.CreatedAt.IsZero() {
			checkpoint.CreatedAt = now
		}
		if checkpoint.UpdatedAt.IsZero() {
			checkpoint.UpdatedAt = now
		}
		if err := insertReviewCheckpoint(ctx, tx, checkpoint); err != nil {
			_ = tx.Rollback()
			return memory.MemoryItem{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	return s.Get(ctx, item.ID)
}

// WriteMemoryRelation 写入记忆关系边（supports/contradicts/supersedes/superseded_by）。
// 幂等性：同一 (source_id, target_id, relation_type) 三元组只写入一次。
// 设计说明：P3 最小关系边集，后续 P4 可扩展 relation expansion。
func (s *Store) WriteMemoryRelation(ctx context.Context, relation memory.MemoryRelation) error {
	if relation.ID == "" || relation.SourceID == "" || relation.TargetID == "" || relation.RelationType == "" {
		return fmt.Errorf("VALIDATION_FAILED: relation id, source_id, target_id and relation_type are required")
	}
	var existing string
	err := s.db.QueryRowContext(ctx, `select id from memory_relation
		where source_id = ? and target_id = ? and relation_type = ?
		limit 1`, relation.SourceID, relation.TargetID, relation.RelationType).Scan(&existing)
	if err != nil && err != sql.ErrNoRows {
		return storageErr(err)
	}
	if existing != "" {
		return nil
	}
	now := time.Now().UTC()
	if relation.Weight == 0 {
		relation.Weight = 1.0
	}
	if relation.CreatedAt.IsZero() {
		relation.CreatedAt = now
	}
	if relation.UpdatedAt.IsZero() {
		relation.UpdatedAt = now
	}
	_, err = s.db.ExecContext(ctx, `insert into memory_relation(
		id, source_id, target_id, relation_type, weight, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?)`,
		relation.ID, relation.SourceID, relation.TargetID, relation.RelationType, relation.Weight,
		relation.CreatedAt.Format(time.RFC3339Nano), relation.UpdatedAt.Format(time.RFC3339Nano),
	)
	return storageErr(err)
}

func baseEvidenceSelect() string {
	return `select id, coalesce(raw_event_id, ''), source_type, interpreted_statement,
		coalesce(keywords_json, ''), coalesce(salient_spans_json, ''),
		coalesce(source_ref_json, ''), confidence, created_at
		from evidence`
}

func scanEvidence(row rowScanner) (memory.Evidence, error) {
	var evidence memory.Evidence
	var createdAt string
	err := row.Scan(&evidence.ID, &evidence.RawEventID, &evidence.SourceType, &evidence.InterpretedStatement,
		&evidence.KeywordsJSON, &evidence.SalientSpansJSON, &evidence.SourceRefJSON, &evidence.Confidence, &createdAt)
	if err != nil {
		return memory.Evidence{}, err
	}
	evidence.CreatedAt = parseTime(createdAt)
	return evidence, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	compact := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		compact = append(compact, trimmed)
	}
	return compact
}
