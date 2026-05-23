package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/zaneway/the-one/internal/idgen"
	"github.com/zaneway/the-one/internal/memory"
)

// Remember 在一个短事务内写入 memory、evidence、link、FTS 和可选 review_checkpoint。
func (s *Store) Remember(ctx context.Context, item memory.MemoryItem, evidence memory.Evidence, checkpoint *memory.ReviewCheckpoint) error {
	if !s.capabilities.FTS5 {
		return fmt.Errorf("FTS_UNAVAILABLE: sqlite fts5 is required for P1 remember")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageErr(err)
	}
	if err := insertMemoryItem(ctx, tx, item); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := insertEvidence(ctx, tx, evidence); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"insert into memory_evidence_link(memory_id, evidence_id, relation_type, weight) values (?, ?, ?, ?)",
		item.ID, evidence.ID, "derived_from", 1.0,
	); err != nil {
		_ = tx.Rollback()
		return storageErr(err)
	}
	if shouldIndex(item.State) {
		if err := upsertFTS(ctx, tx, item.ID, item.SearchText); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if checkpoint != nil {
		if err := insertReviewCheckpoint(ctx, tx, *checkpoint); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return storageErr(tx.Commit())
}

// Search 执行 P1 FTS + metadata 查询和简化排序。
func (s *Store) Search(ctx context.Context, req memory.SearchRequest) ([]memory.SearchResult, memory.SearchDiagnostics, error) {
	if !s.capabilities.FTS5 {
		return nil, memory.SearchDiagnostics{Fallback: "metadata"}, fmt.Errorf("FTS_UNAVAILABLE: sqlite fts5 is required for P1 search")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	matchQuery := buildFTSQuery(req.Query)
	results, diag, err := s.searchByFTS(ctx, req, matchQuery, limit)
	if err != nil {
		return nil, diag, err
	}
	if len(results) == 0 {
		fallbackResults, fallbackDiag, err := s.searchByLike(ctx, req, limit)
		fallbackDiag.FTSHits = diag.FTSHits
		fallbackDiag.FilteredCount += diag.FilteredCount
		if err != nil {
			return nil, fallbackDiag, err
		}
		return fallbackResults, fallbackDiag, nil
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
	item, err := getMemoryForUpdate(ctx, tx, memoryID)
	if err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	if item.State == memory.StateDeleted {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: deleted memory is terminal")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`update memory_item
		   set content = ?, normalized_content = ?, search_text = ?, version = version + 1, updated_at = ?
		 where id = ?`,
		editContent, editContent, searchText, now, memoryID,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
	if err := insertReviewRecord(ctx, tx, memoryID, "manual_review", "edited", reviewer, feedback, item.Content, editContent); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
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
	if item.State == memory.StateDeleted {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: deleted memory is terminal")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		"update memory_item set state = ?, updated_at = ? where id = ?",
		memory.StateDeleted, now, memoryID,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
	if _, err := tx.ExecContext(ctx,
		"insert or replace into memory_tombstone(memory_id, deleted_reason, deleted_by, content_hash, deleted_at) values (?, ?, ?, ?, ?)",
		memoryID, feedback, reviewer, "", now,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
	if err := deleteFTS(ctx, tx, memoryID); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
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

func (s *Store) transition(ctx context.Context, memoryID, action, newState, reviewer, feedback string, confirmed bool) (memory.MemoryItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.MemoryItem{}, storageErr(err)
	}
	item, err := getMemoryForUpdate(ctx, tx, memoryID)
	if err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, err
	}
	if item.State == memory.StateDeleted {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: deleted memory is terminal")
	}
	if action == "approve" && item.State != memory.StatePendingReview && item.State != memory.StateArchived {
		_ = tx.Rollback()
		return memory.MemoryItem{}, fmt.Errorf("STATE_CONFLICT: approve requires pending_review or archived")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		"update memory_item set state = ?, user_confirmed = ?, updated_at = ? where id = ?",
		newState, confirmed, now, memoryID,
	); err != nil {
		_ = tx.Rollback()
		return memory.MemoryItem{}, storageErr(err)
	}
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
		nullString(item.SourceType), nullString("memoryd"), item.SourceQuality, nullString(item.Title), item.Content,
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
		id, source_type, interpreted_statement, keywords_json, salient_spans_json, source_ref_json, confidence, created_at
	) values (?, ?, ?, ?, ?, ?, ?, ?)`,
		evidence.ID, evidence.SourceType, evidence.InterpretedStatement, nullString(evidence.KeywordsJSON),
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

func (s *Store) searchByFTS(ctx context.Context, req memory.SearchRequest, matchQuery string, limit int) ([]memory.SearchResult, memory.SearchDiagnostics, error) {
	query := `select m.id, m.memory_type, m.scope, coalesce(m.title, ''), m.content,
		m.confidence, m.importance, m.state, m.tier, bm25(memory_item_fts) as rank
		from memory_item_fts
		join memory_item m on m.id = memory_item_fts.memory_id
		where memory_item_fts match ?`
	args := []any{matchQuery}
	query, args = appendSearchFilters(query, args, req, false)
	query += " order by rank limit ?"
	args = append(args, limit*3)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
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
	results := s.rankSearchResults(ctx, raw, req, limit)
	return results, memory.SearchDiagnostics{
		FTSHits:       len(raw),
		FilteredCount: max(0, len(raw)-len(results)),
		Fallback:      "fts_metadata",
	}, nil
}

func (s *Store) searchByLike(ctx context.Context, req memory.SearchRequest, limit int) ([]memory.SearchResult, memory.SearchDiagnostics, error) {
	query := `select id, memory_type, scope, coalesce(title, ''), content,
		confidence, importance, state, tier, 0.0 as rank
		from memory_item
		where lower(search_text) like ?`
	args := []any{"%" + strings.ToLower(req.Query) + "%"}
	query, args = appendSearchFilters(query, args, req, true)
	query += " order by updated_at desc limit ?"
	args = append(args, limit*3)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
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
	return s.rankSearchResults(ctx, raw, req, limit), memory.SearchDiagnostics{Fallback: "metadata_like"}, nil
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

func (s *Store) rankSearchResults(ctx context.Context, raw []rankedMemory, req memory.SearchRequest, limit int) []memory.SearchResult {
	results := make([]memory.SearchResult, 0, len(raw))
	for _, item := range raw {
		bm25Norm := 1.0 / (1.0 + math.Abs(item.Rank))
		score := 0.55*bm25Norm + 0.20*scopeWeight(item.Scope) + 0.15*item.Confidence + 0.10*item.Importance
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

func appendSearchFilters(query string, args []any, req memory.SearchRequest, tableOnly bool) (string, []any) {
	prefix := "m."
	if tableOnly {
		prefix = ""
	}
	if !req.IncludeArchived {
		query += " and " + prefix + "state in ('stable', 'pending_review', 'provisional')"
	}
	if len(req.Scope) > 0 {
		query += " and " + prefix + "scope in (" + placeholders(len(req.Scope)) + ")"
		for _, scope := range req.Scope {
			args = append(args, scope)
		}
	}
	if len(req.MemoryTypes) > 0 {
		query += " and " + prefix + "memory_type in (" + placeholders(len(req.MemoryTypes)) + ")"
		for _, memoryType := range req.MemoryTypes {
			args = append(args, memoryType)
		}
	}
	query += " and (" + prefix + "scope != 'project_local' or " + prefix + "project_id = ?)"
	args = append(args, req.ProjectID)
	query += " and (" + prefix + "scope != 'repo_local' or " + prefix + "repo_id = ?)"
	args = append(args, req.RepoID)
	query += " and (" + prefix + "scope != 'session' or " + prefix + "session_id = ?)"
	args = append(args, req.SessionID)
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
		coalesce(source_type, ''), source_quality, coalesce(title, ''), content,
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
		&item.SourceQuality, &item.Title, &item.Content, &item.NormalizedContent, &item.SearchText,
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
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
	return storageErr(err)
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

func buildFTSQuery(query string) string {
	terms := make([]string, 0)
	var current []rune
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
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r > unicode.MaxASCII {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	if len(terms) == 0 {
		return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	}
	return strings.Join(terms, " OR ")
}

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
