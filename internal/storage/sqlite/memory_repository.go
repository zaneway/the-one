package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/memory"
)

// FindDuplicate 按幂等键查找同 scope/type/content 的现有记忆。
func (s *Store) FindDuplicate(ctx context.Context, item memory.MemoryItem) (memory.MemoryItem, bool, error) {
	query := baseMemorySelect() + ` where scope = ?
		and memory_type = ?
		and content = ?
		and coalesce(workspace_id, '') = ?
		and coalesce(user_id, '') = ?
		and coalesce(project_id, '') = ?
		and coalesce(repo_id, '') = ?
		and coalesce(session_id, '') = ?
		and coalesce(task_id, '') = ?
		and state != ?
		order by updated_at desc
		limit 1`
	row := s.db.QueryRowContext(ctx, query,
		item.Scope,
		item.MemoryType,
		item.Content,
		item.WorkspaceID,
		item.UserID,
		item.ProjectID,
		item.RepoID,
		item.SessionID,
		item.TaskID,
		memory.StateDeleted,
	)
	existing, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return memory.MemoryItem{}, false, nil
	}
	if err != nil {
		return memory.MemoryItem{}, false, storageErr(err)
	}
	return existing, true, nil
}

// Remember 在一个短事务内写入 memory、evidence、link、FTS 和可选 review_checkpoint。
func (s *Store) Remember(ctx context.Context, item memory.MemoryItem, evidence memory.Evidence, checkpoint *memory.ReviewCheckpoint) error {
	// FTS5 是记忆写入的硬依赖，不可用时直接拒绝
	if !s.capabilities.FTS5 {
		return fmt.Errorf("FTS_UNAVAILABLE: sqlite fts5 is required for remember")
	}
	// 事务保证：memory_item + evidence + link + FTS + checkpoint 原子写入
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageErr(err)
	}
	// 写入 memory_item 主表
	if err := insertMemoryItem(ctx, tx, item); err != nil {
		_ = tx.Rollback()
		return err
	}
	// 写入 evidence 证据表
	if err := insertEvidence(ctx, tx, evidence); err != nil {
		_ = tx.Rollback()
		return err
	}
	// 写入 memory_evidence_link 关联表（relation_type=derived_from, weight=1.0）
	if _, err := tx.ExecContext(ctx,
		"insert into memory_evidence_link(memory_id, evidence_id, relation_type, weight) values (?, ?, ?, ?)",
		item.ID, evidence.ID, "derived_from", 1.0,
	); err != nil {
		_ = tx.Rollback()
		return storageErr(err)
	}
	// 条件写入 FTS 索引：只有 stable/pending_review/provisional 状态才纳入全文检索
	if shouldIndex(item.State) {
		if err := upsertFTS(ctx, tx, item.ID, item.SearchText); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	// 可选写入 review_checkpoint（设计复查检查点）
	if checkpoint != nil {
		if err := insertReviewCheckpoint(ctx, tx, *checkpoint); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	provenanceID, err := idgen.New("prov")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := insertMemoryProvenance(ctx, tx, automation.MemoryProvenance{
		ID:                provenanceID,
		MemoryID:          item.ID,
		EvidenceID:        evidence.ID,
		SourceChannel:     "memory_remember",
		HookPhase:         automation.HookPhaseManualObserve,
		EventType:         "memory.remember",
		Pipeline:          "memory_remember->memory",
		Provider:          "memory.remember",
		DerivationStage:   "memory_remember",
		AdmissionDecision: item.State,
		AdmissionScore:    item.RetentionScore,
		CreatedAt:         time.Now(),
	}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return storageErr(tx.Commit())
}

// GetReviewCheckpoint 按 memory_id 读取手动复查 checkpoint 的结构化上下文。
func (s *Store) GetReviewCheckpoint(ctx context.Context, memoryID string) (memory.ReviewCheckpoint, bool, error) {
	row := s.db.QueryRowContext(ctx, `select id, memory_id, coalesce(workspace_id, ''), coalesce(project_id, ''),
		coalesce(repo_id, ''), coalesce(session_id, ''), coalesce(task_id, ''), checkpoint_type,
		review_intent_json, target_docs_json, coalesce(target_sections_json, ''), coalesce(target_hashes_json, ''),
		conclusion, coalesce(confirmed_baseline_json, ''), coalesce(ignored_items_json, ''),
		coalesce(deferred_items_json, ''), coalesce(open_items_json, ''), coalesce(next_review_policy_json, ''),
		created_at, updated_at
		from review_checkpoint
		where memory_id = ?
		order by updated_at desc
		limit 1`, memoryID)
	var checkpoint memory.ReviewCheckpoint
	var createdAt, updatedAt string
	err := row.Scan(&checkpoint.ID, &checkpoint.MemoryID, &checkpoint.WorkspaceID, &checkpoint.ProjectID,
		&checkpoint.RepoID, &checkpoint.SessionID, &checkpoint.TaskID, &checkpoint.CheckpointType,
		&checkpoint.ReviewIntentJSON, &checkpoint.TargetDocsJSON, &checkpoint.TargetSectionsJSON,
		&checkpoint.TargetHashesJSON, &checkpoint.Conclusion, &checkpoint.ConfirmedBaselineJSON,
		&checkpoint.IgnoredItemsJSON, &checkpoint.DeferredItemsJSON, &checkpoint.OpenItemsJSON,
		&checkpoint.NextReviewPolicyJSON, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return memory.ReviewCheckpoint{}, false, nil
	}
	if err != nil {
		return memory.ReviewCheckpoint{}, false, storageErr(err)
	}
	checkpoint.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	checkpoint.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return checkpoint, true, nil
}

// Search 执行 FTS + metadata 查询和简化排序。
// 检索策略：先尝试 FTS5 全文检索，无结果时降级为 LIKE 模糊匹配。
// 排序公式：0.55*bm25 + 0.20*scope + 0.15*confidence + 0.10*importance
// 过滤规则：排除 deleted 状态，默认排除 archived；按 scope 隔离（project_local/repo_local/session 必须匹配对应 ID）。
func (s *Store) Search(ctx context.Context, req memory.SearchRequest) ([]memory.SearchResult, memory.SearchDiagnostics, error) {
	startedAt := time.Now()
	if !s.capabilities.FTS5 {
		return nil, memory.SearchDiagnostics{Fallback: "metadata"}, fmt.Errorf("FTS_UNAVAILABLE: sqlite fts5 is required for search")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	matchQuery := buildFTSQuery(req.Query)

	// 查询前日志：打印检索条件
	s.logger.Debug("memory.search 开始",
		"query", req.Query,
		"match_query", matchQuery,
		"limit", limit,
		"scope", req.Scope,
		"memory_types", req.MemoryTypes,
		"workspace_id", req.WorkspaceID,
		"project_id", req.ProjectID,
		"repo_id", req.RepoID,
		"session_id", req.SessionID,
		"include_archived", req.IncludeArchived,
	)

	results, diag, err := s.searchByFTS(ctx, req, matchQuery, limit)
	if err != nil {
		s.logger.Error("memory.search FTS 查询失败",
			"query", req.Query,
			"error", err,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		return nil, diag, err
	}

	// FTS 无结果时降级为 LIKE
	usedFallback := false
	if len(results) == 0 {
		usedFallback = true
		s.logger.Debug("memory.search FTS 无命中，降级为 LIKE 查询",
			"query", req.Query,
			"fts_hits", diag.FTSHits,
		)
		fallbackResults, fallbackDiag, err := s.searchByLike(ctx, req, limit)
		fallbackDiag.FTSHits = diag.FTSHits
		fallbackDiag.FilteredCount += diag.FilteredCount
		if err != nil {
			s.logger.Error("memory.search LIKE 降级查询失败",
				"query", req.Query,
				"error", err,
				"duration_ms", time.Since(startedAt).Milliseconds(),
			)
			return nil, fallbackDiag, err
		}
		results = fallbackResults
		diag = fallbackDiag
	}

	// 查询后日志：打印结果摘要
	duration := time.Since(startedAt)
	logLevel := slog.LevelDebug
	if duration > 100*time.Millisecond {
		logLevel = slog.LevelWarn // 慢查询告警
	}
	s.logger.Log(ctx, logLevel, "memory.search 完成",
		"query", req.Query,
		"result_count", len(results),
		"fts_hits", diag.FTSHits,
		"filtered_count", diag.FilteredCount,
		"fallback", diag.Fallback,
		"used_like_fallback", usedFallback,
		"duration_ms", duration.Milliseconds(),
	)

	// 打印 top-N 结果摘要（最多 5 条）
	topN := 5
	if len(results) < topN {
		topN = len(results)
	}
	for i := 0; i < topN; i++ {
		r := results[i]
		contentPreview := truncateString(r.Content, 80)
		s.logger.Debug("memory.search 结果",
			"rank", i+1,
			"memory_id", r.MemoryID,
			"memory_type", r.MemoryType,
			"scope", r.Scope,
			"score", fmt.Sprintf("%.4f", r.Score),
			"state", r.State,
			"tier", r.Tier,
			"content_preview", contentPreview,
		)
	}

	return results, diag, nil
}

// Get 按 memory_id 读取一条记忆。
func (s *Store) Get(ctx context.Context, memoryID string) (memory.MemoryItem, error) {
	row := s.db.QueryRowContext(ctx, baseMemorySelect()+" where id = ?", memoryID)
	item, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return memory.MemoryItem{}, fmt.Errorf("MEMORY_NOT_FOUND: %s", memoryID)
	}
	return item, storageErr(err)
}

// ListReview 查询待 review 或指定状态记忆。
func (s *Store) ListReview(ctx context.Context, req memory.ReviewRequest) ([]memory.MemoryItem, error) {
	state := req.State
	if state == "" {
		state = memory.StatePendingReview
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	query := baseMemorySelect() + " where state = ?"
	args := []any{state}
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
	query += " order by updated_at desc limit ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageErr(err)
	}
	defer rows.Close()
	return scanMemoryRows(rows)
}

// Approve 将 pending_review 记忆确认为 stable，并恢复/更新 FTS。
func (s *Store) Approve(ctx context.Context, memoryID, reviewer, feedback string) (memory.MemoryItem, error) {
	return s.transition(ctx, memoryID, "approve", memory.StateStable, reviewer, feedback, true)
}

// RejectOrArchive 将记忆归档并移除 FTS，避免默认检索继续注入。
func (s *Store) RejectOrArchive(ctx context.Context, memoryID, action, reviewer, feedback string) (memory.MemoryItem, error) {
	if action != "reject" && action != "archive" {
		return memory.MemoryItem{}, fmt.Errorf("VALIDATION_FAILED: unsupported action %q", action)
	}
	return s.transition(ctx, memoryID, action, memory.StateArchived, reviewer, feedback, false)
}

// Edit 原地更新 stable/pending/archived 记忆内容，version+1，并同步 FTS。
func (s *Store) Edit(ctx context.Context, memoryID, editContent, reviewer, feedback, searchText string) (memory.MemoryItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	// SELECT FOR UPDATE：获取原记忆内容（用于 review 记录的 original_content）
	item, err := getMemoryForUpdate(ctx, tx, memoryID)
	if err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// 终态保护：deleted 记忆不可编辑
	if item.State == memory.StateDeleted {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: deleted memory is terminal")
	}
	// 更新 content/normalized_content/search_text，version 自增
	now := time.Now().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`update memory_item
		   set content = ?, normalized_content = ?, search_text = ?, version = version + 1, updated_at = ?
		 where id = ?`,
		editContent, editContent, searchText, now, memoryID,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
	// 记录编辑历史：original_content = 旧内容，edited_content = 新内容
	if err := insertReviewRecord(ctx, tx, memoryID, "manual_review", "edited", reviewer, feedback, item.Content, editContent); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// 同步 FTS 索引：可检索状态的记忆需要重建 FTS 条目
	if shouldIndex(item.State) {
		if err := upsertFTS(ctx, tx, memoryID, searchText); err != nil {
			_ = tx.Rollback()
			return memory.MemoryItem{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	return s.Get(ctx, memoryID)
}

// Delete 标记记忆为 deleted，写 tombstone，并清理 FTS。
func (s *Store) Delete(ctx context.Context, memoryID, reviewer, feedback string) (memory.MemoryItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	item, err := getMemoryForUpdate(ctx, tx, memoryID)
	if err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// 终态保护：已删除的记忆不可再次删除
	if item.State == memory.StateDeleted {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: deleted memory is terminal")
	}
	// 标记为 deleted 状态
	now := time.Now().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		"update memory_item set state = ?, updated_at = ? where id = ?",
		memory.StateDeleted, now, memoryID,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
	// 写入 tombstone：记录删除原因、删除人，用于审计和可能的恢复
	if _, err := tx.ExecContext(ctx,
		"insert or replace into memory_tombstone(memory_id, deleted_reason, deleted_by, content_hash, deleted_at) values (?, ?, ?, ?, ?)",
		memoryID, feedback, reviewer, "", now,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
	// 清理 FTS 索引：删除的记忆不应出现在全文检索结果中
	if err := deleteFTS(ctx, tx, memoryID); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// 删除一致性：清理依赖该 memory 的关系、代码引用和 embedding。
	// access log 默认作为最小统计保留，不包含 memory content；敏感删除的脱敏入口由诊断/清理能力单独承载。
	if err := deleteMemoryArtifacts(ctx, tx, memoryID); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// 记录审核历史
	if err := insertReviewRecord(ctx, tx, memoryID, "manual_review", "deleted", reviewer, feedback, item.Content, ""); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	item.State = memory.StateDeleted
	item.UserConfirmed = false
	return item, nil
}

// transition 是记忆状态流转的核心事务方法。
// 支持的操作：approve（pending_review/archived -> stable）、reject/archive（-> archived）。
// 事务内完成：状态更新 -> review 记录写入 -> FTS 索引同步（新状态可检索则 upsert，否则 delete）。
// 状态约束：deleted 是终态，不可再流转；approve 只允许从 pending_review 或 archived 转出。
func (s *Store) transition(ctx context.Context, memoryID, action, newState, reviewer, feedback string, confirmed bool) (memory.MemoryItem, error) {
	// 开启事务：状态更新 + review 记录 + FTS 同步必须原子完成
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	// SELECT FOR UPDATE：获取当前状态，防止并发状态流转冲突
	item, err := getMemoryForUpdate(ctx, tx, memoryID)
	if err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// 终态保护：deleted 状态不可再流转
	if item.State == memory.StateDeleted {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: deleted memory is terminal")
	}
	// approve 前置条件：只允许从 pending_review 或 archived 转出
	if action == "approve" && item.State != memory.StatePendingReview && item.State != memory.StateArchived {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: approve requires pending_review or archived")
	}
	// 更新 memory_item 状态和 user_confirmed 标记
	now := time.Now().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		"update memory_item set state = ?, user_confirmed = ?, updated_at = ? where id = ?",
		newState, confirmed, now, memoryID,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
	// 根据操作类型确定 review 记录的 status 字段
	status := "approved"
	if newState == memory.StateArchived {
		status = "archived"
		if action == "reject" {
			status = "rejected"
		}
	}
	if err := insertReviewRecord(ctx, tx, memoryID, "manual_review", status, reviewer, feedback, item.Content, ""); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	// FTS 索引同步：新状态可检索则 upsert，否则 delete（避免 archived/deleted 出现在搜索结果中）
	if shouldIndex(newState) {
		if err := upsertFTS(ctx, tx, memoryID, item.SearchText); err != nil {
			_ = tx.Rollback()
			return memory.MemoryItem{}, err
		}
	} else {
		if err := deleteFTS(ctx, tx, memoryID); err != nil {
			_ = tx.Rollback()
			return memory.MemoryItem{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	return s.Get(ctx, memoryID)
}

func insertMemoryItem(ctx context.Context, tx *sql.Tx, item memory.MemoryItem) error {
	_, err := tx.ExecContext(ctx, `insert into memory_item(
		id, scope, workspace_id, user_id, project_id, repo_id, session_id, task_id,
		memory_type, source_type, created_by, source_quality, title, content, normalized_content, search_text,
		keywords_json, entities_json, retrieval_cues_json, tags_json, state, confidence, importance,
		encoding_depth, decay_rate, retention_score, tier, created_at, updated_at, pinned, user_confirmed,
		version, supersedes_id
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Scope, nullString(item.WorkspaceID), nullString(item.UserID), nullString(item.ProjectID),
		nullString(item.RepoID), nullString(item.SessionID), nullString(item.TaskID), item.MemoryType,
		nullString(item.SourceType), nullString(firstNonEmpty(item.CreatedBy, "theone")), item.SourceQuality, nullString(item.Title), item.Content,
		nullString(item.NormalizedContent), nullString(item.SearchText), nullString(item.KeywordsJSON),
		nullString(item.EntitiesJSON), nullString(item.RetrievalCuesJSON), nullString(item.TagsJSON), item.State,
		item.Confidence, item.Importance, item.EncodingDepth, item.DecayRate, item.RetentionScore, item.Tier,
		item.CreatedAt.Format(time.RFC3339Nano), item.UpdatedAt.Format(time.RFC3339Nano), item.Pinned,
		item.UserConfirmed, item.Version, nullString(item.SupersedesID),
	)
	return storageErr(err)
}

func insertEvidence(ctx context.Context, tx *sql.Tx, evidence memory.Evidence) error {
	_, err := tx.ExecContext(ctx, `insert into evidence(
		id, raw_event_id, source_type, interpreted_statement, keywords_json, salient_spans_json, source_ref_json, confidence, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evidence.ID, nullString(evidence.RawEventID), evidence.SourceType, evidence.InterpretedStatement, nullString(evidence.KeywordsJSON),
		nullString(evidence.SalientSpansJSON), nullString(evidence.SourceRefJSON), evidence.Confidence,
		evidence.CreatedAt.Format(time.RFC3339Nano),
	)
	return storageErr(err)
}

func insertReviewCheckpoint(ctx context.Context, tx *sql.Tx, checkpoint memory.ReviewCheckpoint) error {
	_, err := tx.ExecContext(ctx, `insert into review_checkpoint(
		id, memory_id, workspace_id, project_id, repo_id, session_id, task_id, checkpoint_type,
		review_intent_json, target_docs_json, target_sections_json, target_hashes_json, conclusion,
		confirmed_baseline_json, ignored_items_json, deferred_items_json, open_items_json,
		next_review_policy_json, created_at, updated_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		checkpoint.ID, checkpoint.MemoryID, nullString(checkpoint.WorkspaceID), nullString(checkpoint.ProjectID),
		nullString(checkpoint.RepoID), nullString(checkpoint.SessionID), nullString(checkpoint.TaskID),
		checkpoint.CheckpointType, checkpoint.ReviewIntentJSON, checkpoint.TargetDocsJSON,
		nullString(checkpoint.TargetSectionsJSON), nullString(checkpoint.TargetHashesJSON), checkpoint.Conclusion,
		nullString(checkpoint.ConfirmedBaselineJSON), nullString(checkpoint.IgnoredItemsJSON),
		nullString(checkpoint.DeferredItemsJSON), nullString(checkpoint.OpenItemsJSON),
		nullString(checkpoint.NextReviewPolicyJSON), checkpoint.CreatedAt.Format(time.RFC3339Nano),
		checkpoint.UpdatedAt.Format(time.RFC3339Nano),
	)
	return storageErr(err)
}

// searchByFTS 使用 FTS5 虚表执行全文检索。
// 查询流程：FTS5 match -> scope/state/type 过滤 -> BM25 排序 -> 取 top N*3 候选 -> 内存中二次排序裁剪。
// 多取 3 倍候选是为了在二次排序后仍有足够的结果。
func (s *Store) searchByFTS(ctx context.Context, req memory.SearchRequest, matchQuery string, limit int) ([]memory.SearchResult, memory.SearchDiagnostics, error) {
	startedAt := time.Now()
	// FTS5 虚表查询：通过 memory_item_fts match 匹配，JOIN memory_item 获取完整字段
	// bm25() 返回负值（越小越相关），用于后续排序
	query := `select m.id, m.memory_type, m.scope, coalesce(m.title, ''), m.content,
		m.confidence, m.importance, m.state, m.tier, bm25(memory_item_fts) as rank
		from memory_item_fts
		join memory_item m on m.id = memory_item_fts.memory_id
		where memory_item_fts match ?`
	args := []any{matchQuery}
	// 追加 scope/state/type 过滤和 scope 隔离条件
	query, args = appendSearchFilters(query, args, req, false)
	// BM25 排序后多取 3 倍候选，确保二次排序后仍有足够结果
	query += " order by rank limit ?"
	args = append(args, limit*3)

	s.logger.Debug("searchByFTS 执行查询",
		"match_query", matchQuery,
		"fetch_limit", limit*3,
		"args_count", len(args),
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		s.logger.Error("searchByFTS SQL 执行失败",
			"error", err,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		return nil, memory.SearchDiagnostics{Fallback: "fts_metadata"}, storageErr(err)
	}
	defer rows.Close()
	var raw []rankedMemory
	for rows.Next() {
		var item rankedMemory
		if err := rows.Scan(&item.ID, &item.MemoryType, &item.Scope, &item.Title, &item.Content, &item.Confidence, &item.Importance, &item.State, &item.Tier, &item.Rank); err != nil {
			return nil, memory.SearchDiagnostics{Fallback: "fts_metadata"}, storageErr(err)
		}
		raw = append(raw, item)
	}
	if err := rows.Err(); err != nil {
		return nil, memory.SearchDiagnostics{Fallback: "fts_metadata"}, storageErr(err)
	}

	s.logger.Debug("searchByFTS 原始召回完成",
		"raw_count", len(raw),
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)

	// 二次排序：BM25 归一化 + scope 权重 + confidence + importance 综合评分
	results := s.rankSearchResults(ctx, raw, req, limit)

	s.logger.Debug("searchByFTS 二次排序完成",
		"raw_count", len(raw),
		"result_count", len(results),
		"filtered_count", max(0, len(raw)-len(results)),
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)

	return results, memory.SearchDiagnostics{
		FTSHits:       len(raw),
		FilteredCount: max(0, len(raw)-len(results)),
		Fallback:      "fts_metadata",
	}, nil
}

// searchByLike 是 FTS5 不可用或无命中时的降级检索路径。
// 使用 LIKE 模糊匹配 search_text 字段，按 updated_at 降序排列。
// 降级路径不提供 BM25 相关性排序，只能依赖 metadata 权重。
func (s *Store) searchByLike(ctx context.Context, req memory.SearchRequest, limit int) ([]memory.SearchResult, memory.SearchDiagnostics, error) {
	startedAt := time.Now()
	// LIKE 降级路径：FTS5 无命中或不可用时，用 LIKE 模糊匹配 search_text
	// rank 固定为 0.0（无 BM25 分数），排序依赖 metadata 权重
	likePattern := "%" + strings.ToLower(req.Query) + "%"
	query := `select id, memory_type, scope, coalesce(title, ''), content,
		confidence, importance, state, tier, 0.0 as rank
		from memory_item
		where lower(search_text) like ?`
	args := []any{likePattern}
	// tableOnly=true：直接查 memory_item 表，列名不加 "m." 前缀
	query, args = appendSearchFilters(query, args, req, true)
	// 无 BM25 时按 updated_at 降序，优先返回最近更新的记忆
	query += " order by updated_at desc limit ?"
	args = append(args, limit*3)

	s.logger.Debug("searchByLike 执行查询",
		"query", req.Query,
		"like_pattern", likePattern,
		"fetch_limit", limit*3,
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		s.logger.Error("searchByLike SQL 执行失败",
			"error", err,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		return nil, memory.SearchDiagnostics{Fallback: "metadata_like"}, storageErr(err)
	}
	defer rows.Close()
	var raw []rankedMemory
	for rows.Next() {
		var item rankedMemory
		if err := rows.Scan(&item.ID, &item.MemoryType, &item.Scope, &item.Title, &item.Content, &item.Confidence, &item.Importance, &item.State, &item.Tier, &item.Rank); err != nil {
			return nil, memory.SearchDiagnostics{Fallback: "metadata_like"}, storageErr(err)
		}
		raw = append(raw, item)
	}
	if err := rows.Err(); err != nil {
		return nil, memory.SearchDiagnostics{Fallback: "metadata_like"}, storageErr(err)
	}

	results := s.rankSearchResults(ctx, raw, req, limit)

	s.logger.Debug("searchByLike 完成",
		"query", req.Query,
		"raw_count", len(raw),
		"result_count", len(results),
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)

	return results, memory.SearchDiagnostics{Fallback: "metadata_like"}, nil
}

type rankedMemory struct {
	ID         string
	MemoryType string
	Scope      string
	Title      string
	Content    string
	Confidence float64
	Importance float64
	State      string
	Tier       string
	Rank       float64
}

// rankSearchResults 对检索结果执行二次排序和裁剪。
// 排序公式：0.55*bm25 + 0.20*scope + 0.15*confidence + 0.10*importance
// BM25 归一化：将 FTS5 返回的负 BM25 分数转换为 0-1 范围（1/(1+|rank|)）。
// 惩罚：archived 状态的记忆额外扣 0.4 分。
// 可选：加载 evidence_refs 用于检索结果的可解释性。
func (s *Store) rankSearchResults(ctx context.Context, raw []rankedMemory, req memory.SearchRequest, limit int) []memory.SearchResult {
	results := make([]memory.SearchResult, 0, len(raw))
	for _, item := range raw {
		// BM25 归一化：FTS5 返回负值（越小越相关），转换为 0-1 范围
		// 公式：1/(1+|rank|)，rank 越大（绝对值）-> bm25Norm 越小 -> 相关性越低
		bm25Norm := 1.0 / (1.0 + math.Abs(item.Rank))
		// 综合评分公式：0.55*BM25 + 0.20*scope权重 + 0.15*置信度 + 0.10*重要度
		score := 0.55*bm25Norm + 0.20*scopeWeight(item.Scope) + 0.15*item.Confidence + 0.10*item.Importance
		// archived 状态惩罚：已归档记忆扣 0.4 分，降低其在检索结果中的排名
		if item.State == memory.StateArchived {
			score -= 0.4
		}
		result := memory.SearchResult{
			MemoryID:   item.ID,
			MemoryType: item.MemoryType,
			Scope:      item.Scope,
			Title:      item.Title,
			Content:    item.Content,
			Score:      clamp(score),
			Confidence: item.Confidence,
			State:      item.State,
			Tier:       item.Tier,
		}
		if req.IncludeEvidence {
			result.EvidenceRefs = s.loadEvidenceRefs(ctx, item.ID)
		}
		results = append(results, result)
	}
	sortSearchResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// appendSearchFilters 为 SQL 查询追加通用过滤条件。
// 过滤逻辑：
//   - 排除 deleted 状态
//   - 默认只返回 stable/pending_review/provisional（IncludeArchived=true 时放宽）
//   - 按 scope 过滤（如指定）
//   - 按 memory_type 过滤（如指定）
//   - scope 隔离：project_local 必须匹配 workspace_id+project_id
//   - scope 隔离：repo_local 必须匹配 workspace_id+repo_id
//   - scope 隔离：session 必须匹配 workspace_id+session_id
func appendSearchFilters(query string, args []any, req memory.SearchRequest, tableOnly bool) (string, []any) {
	// tableOnly=true 时列名不加表别名前缀（用于 searchByLike 直接查 memory_item 表）
	prefix := "m."
	if tableOnly {
		prefix = ""
	}
	// 永远排除 deleted 状态的记忆
	query += " and " + prefix + "state != 'deleted'"
	// 默认只返回 stable/pending_review/provisional，IncludeArchived=true 时放宽到所有非 deleted
	if !req.IncludeArchived {
		query += " and " + prefix + "state in ('stable', 'pending_review', 'provisional')"
	}
	// scope 过滤：按请求指定的 scope 列表筛选
	if len(req.Scope) > 0 {
		query += " and " + prefix + "scope in (" + placeholders(len(req.Scope)) + ")"
		for _, scope := range req.Scope {
			args = append(args, scope)
		}
	}
	// memory_type 过滤：按请求指定的记忆类型列表筛选
	if len(req.MemoryTypes) > 0 {
		query += " and " + prefix + "memory_type in (" + placeholders(len(req.MemoryTypes)) + ")"
		for _, memoryType := range req.MemoryTypes {
			args = append(args, memoryType)
		}
	}
	// scope 隔离：project_local 记忆必须匹配 workspace_id + project_id，否则不返回
	query += " and (" + prefix + "scope != 'project_local' or (" + prefix + "workspace_id = ? and " + prefix + "project_id = ?))"
	args = append(args, req.WorkspaceID, req.ProjectID)
	// scope 隔离：repo_local 记忆必须匹配 workspace_id + repo_id
	query += " and (" + prefix + "scope != 'repo_local' or (" + prefix + "workspace_id = ? and " + prefix + "repo_id = ?))"
	args = append(args, req.WorkspaceID, req.RepoID)
	// scope 隔离：session 记忆必须匹配 workspace_id + session_id
	query += " and (" + prefix + "scope != 'session' or (" + prefix + "workspace_id = ? and " + prefix + "session_id = ?))"
	args = append(args, req.WorkspaceID, req.SessionID)
	return query, args
}

func getMemoryForUpdate(ctx context.Context, tx *sql.Tx, memoryID string) (memory.MemoryItem, error) {
	row := tx.QueryRowContext(ctx, baseMemorySelect()+" where id = ?", memoryID)
	item, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return memory.MemoryItem{}, fmt.Errorf("MEMORY_NOT_FOUND: %s", memoryID)
	}
	return item, storageErr(err)
}

func baseMemorySelect() string {
	return `select id, scope, coalesce(workspace_id, ''), coalesce(user_id, ''), coalesce(project_id, ''),
		coalesce(repo_id, ''), coalesce(session_id, ''), coalesce(task_id, ''), memory_type,
		coalesce(source_type, ''), coalesce(created_by, ''), source_quality, coalesce(title, ''), content,
		coalesce(normalized_content, ''), coalesce(search_text, ''), coalesce(keywords_json, ''),
		coalesce(entities_json, ''), coalesce(retrieval_cues_json, ''), coalesce(tags_json, ''),
		state, confidence, importance, encoding_depth, decay_rate, retention_score, tier,
		created_at, updated_at, pinned, user_confirmed, version, coalesce(supersedes_id, '')
		from memory_item`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemory(row rowScanner) (memory.MemoryItem, error) {
	var item memory.MemoryItem
	var createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.Scope, &item.WorkspaceID, &item.UserID, &item.ProjectID,
		&item.RepoID, &item.SessionID, &item.TaskID, &item.MemoryType, &item.SourceType,
		&item.CreatedBy, &item.SourceQuality, &item.Title, &item.Content, &item.NormalizedContent, &item.SearchText,
		&item.KeywordsJSON, &item.EntitiesJSON, &item.RetrievalCuesJSON, &item.TagsJSON,
		&item.State, &item.Confidence, &item.Importance, &item.EncodingDepth, &item.DecayRate,
		&item.RetentionScore, &item.Tier, &createdAt, &updatedAt, &item.Pinned,
		&item.UserConfirmed, &item.Version, &item.SupersedesID)
	if err != nil {
		return memory.MemoryItem{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return item, nil
}

func scanMemoryRows(rows *sql.Rows) ([]memory.MemoryItem, error) {
	items := make([]memory.MemoryItem, 0)
	for rows.Next() {
		item, err := scanMemory(rows)
		if err != nil {
			return nil, storageErr(err)
		}
		items = append(items, item)
	}
	return items, storageErr(rows.Err())
}

func insertReviewRecord(ctx context.Context, tx *sql.Tx, memoryID, reviewType, status, reviewer, feedback, originalContent, editedContent string) error {
	reviewID, err := idgen.New("rev")
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `insert into memory_review(
		id, memory_id, review_type, status, reviewer, feedback, original_content, edited_content, created_at, reviewed_at
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reviewID, memoryID, reviewType, status, nullString(reviewer), nullString(feedback),
		nullString(originalContent), nullString(editedContent), now, now,
	)
	return storageErr(err)
}

func upsertFTS(ctx context.Context, tx *sql.Tx, memoryID, searchText string) error {
	if err := deleteFTS(ctx, tx, memoryID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "insert into memory_item_fts(memory_id, search_text) values (?, ?)", memoryID, searchText)
	return storageErr(err)
}

func deleteFTS(ctx context.Context, tx *sql.Tx, memoryID string) error {
	_, err := tx.ExecContext(ctx, "delete from memory_item_fts where memory_id = ?", memoryID)
	if err != nil && (strings.Contains(err.Error(), "no such table: memory_item_fts") || strings.Contains(err.Error(), "no such module: fts5")) {
		return nil
	}
	return storageErr(err)
}

func deleteMemoryArtifacts(ctx context.Context, tx *sql.Tx, memoryID string) error {
	statements := []string{
		"delete from memory_relation where source_id = ? or target_id = ?",
		"delete from code_ref where memory_id = ?",
		"delete from memory_embedding where memory_id = ?",
	}
	for _, statement := range statements {
		var err error
		if strings.Contains(statement, " or target_id") {
			_, err = tx.ExecContext(ctx, statement, memoryID, memoryID)
		} else {
			_, err = tx.ExecContext(ctx, statement, memoryID)
		}
		if err != nil {
			return storageErr(err)
		}
	}
	return nil
}

func (s *Store) loadEvidenceRefs(ctx context.Context, memoryID string) []string {
	rows, err := s.db.QueryContext(ctx, "select evidence_id from memory_evidence_link where memory_id = ? order by weight desc", memoryID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	refs := make([]string, 0)
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err == nil {
			refs = append(refs, ref)
		}
	}
	return refs
}

// buildFTSQuery 将用户查询文本转换为 FTS5 查询表达式。
// 分词策略：按非字母数字字符切分，每个词用双引号包裹（精确匹配），词间用 OR 连接。
// 设计说明：使用 OR 而非 AND 是为了提高召回率，避免一个词不匹配就丢失整条记忆。
func buildFTSQuery(query string) string {
	terms := make([]string, 0)
	var current []rune
	// 将累积的字符作为完整词输出：双引号包裹实现精确匹配，内部双引号转义
	flush := func() {
		if len(current) == 0 {
			return
		}
		term := strings.TrimSpace(string(current))
		if term != "" {
			terms = append(terms, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
		}
		current = nil
	}
	// 按非字母数字字符分词：字母/数字/非 ASCII（中文等）作为词的一部分
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r > unicode.MaxASCII {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	// 无有效词时将整个查询作为单个精确匹配词
	if len(terms) == 0 {
		return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	}
	// 词间用 OR 连接：提高召回率，避免一个词不匹配就丢失整条记忆
	return strings.Join(terms, " OR ")
}

// shouldIndex 判断指定状态的记忆是否应纳入 FTS 索引。
// stable/pending_review/provisional 状态可被检索；archived/deleted 不纳入索引。
func shouldIndex(state string) bool {
	return state == memory.StateStable || state == memory.StatePendingReview || state == memory.StateProvisional
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

func storageErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "database table is locked") {
		return fmt.Errorf("STORAGE_BUSY: %w", err)
	}
	if strings.Contains(err.Error(), "no such table: memory_item_fts") || strings.Contains(err.Error(), "no such module: fts5") {
		return fmt.Errorf("FTS_UNAVAILABLE: %w", err)
	}
	return err
}

// scopeWeight 返回不同 scope 在检索排序中的权重。
// project_local（0.90）> user_global（0.85）> repo_local（0.80）> session（0.35）。
// 设计说明：project_local 权重最高，因为项目级记忆与当前任务最相关；
// session 权重最低，因为会话级记忆通常是临时的、未巩固的。
func scopeWeight(scope string) float64 {
	switch scope {
	case memory.ScopeProjectLocal:
		return 0.90
	case memory.ScopeUserGlobal:
		return 0.85
	case memory.ScopeRepoLocal:
		return 0.80
	case memory.ScopeSession:
		return 0.35
	default:
		return 0.5
	}
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func sortSearchResults(results []memory.SearchResult) {
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncateString 截断字符串到指定 rune 长度，超出部分用 "..." 替代。
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
