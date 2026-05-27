package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/memory"
)

func TestAutomatedMemoryRepositoryEvidenceDedup(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	evidence := memory.Evidence{
		ID:                   "ev_001",
		RawEventID:           "evt_001",
		SourceType:           "user_declared",
		InterpretedStatement: "用户要求 P3 自动写入必须可解释。",
		KeywordsJSON:         jsonArrayText("P3", "自动写入"),
		SourceRefJSON:        `{"raw_event_id":"evt_001"}`,
		Confidence:           0.9,
		CreatedAt:            time.Now().UTC(),
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	if err := store.WriteEvidence(ctx, memory.Evidence{
		ID:                   "ev_duplicate",
		RawEventID:           evidence.RawEventID,
		SourceType:           evidence.SourceType,
		InterpretedStatement: evidence.InterpretedStatement,
		Confidence:           0.9,
	}); err != nil {
		t.Fatalf("WriteEvidence duplicate error = %v", err)
	}
	duplicate, found, err := store.FindDuplicateEvidence(ctx, automation.EvidenceDraftKey{
		RawEventID:           evidence.RawEventID,
		SourceType:           evidence.SourceType,
		InterpretedStatement: evidence.InterpretedStatement,
	})
	if err != nil {
		t.Fatalf("FindDuplicateEvidence() error = %v", err)
	}
	if !found || duplicate.ID != evidence.ID || duplicate.RawEventID != evidence.RawEventID {
		t.Fatalf("duplicate = %+v found=%v, want ev_001", duplicate, found)
	}
	var count int
	if err := store.db.QueryRow("select count(*) from evidence where raw_event_id = ?", evidence.RawEventID).Scan(&count); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if count != 1 {
		t.Fatalf("evidence count = %d, want 1", count)
	}
}

func TestAutomatedMemoryRepositoryWriteMemoryAndSearch(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	evidence := memory.Evidence{
		ID:                   "ev_decision",
		RawEventID:           "evt_decision",
		SourceType:           "agent_summary",
		InterpretedStatement: "P3 只实现 rule_based Provider。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:            "mem_decision",
		Scope:         memory.ScopeProjectLocal,
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		MemoryType:    memory.TypeDecision,
		SourceType:    "agent_summary",
		CreatedBy:     "automation:rule_based",
		Title:         "P3 Provider 决策",
		Content:       "P3 只实现 rule_based Provider，外部 LLM Provider 放到二期。",
		KeywordsJSON:  jsonArrayText("P3", "rule_based"),
		State:         memory.StatePendingReview,
		Confidence:    0.8,
		Importance:    0.8,
		EncodingDepth: 2,
		DecayRate:     0.3,
		Tier:          memory.TierLongTerm,
		UserConfirmed: false,
		Version:       1,
	}
	written, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	})
	if err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	if written.ID != item.ID || written.CreatedBy != "automation:rule_based" {
		t.Fatalf("written = %+v, want automated memory", written)
	}
	var linkCount int
	if err := store.db.QueryRow("select count(*) from memory_evidence_link where memory_id = ? and evidence_id = ?", item.ID, evidence.ID).Scan(&linkCount); err != nil {
		t.Fatalf("count evidence link: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("link count = %d, want 1", linkCount)
	}
	results, _, err := store.Search(ctx, memory.SearchRequest{
		Query:       "rule_based Provider",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Scope:       []string{memory.ScopeProjectLocal},
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || results[0].MemoryID != item.ID {
		t.Fatalf("results = %+v, want mem_decision", results)
	}
}

func TestAutomatedMemoryRepositoryReviewCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	evidence := memory.Evidence{
		ID:                   "ev_checkpoint",
		RawEventID:           "evt_checkpoint",
		SourceType:           "task_result",
		InterpretedStatement: "P3 详细设计复查完成。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	now := time.Now().UTC()
	item := memory.MemoryItem{
		ID:            "mem_checkpoint",
		Scope:         memory.ScopeProjectLocal,
		WorkspaceID:   "ws",
		ProjectID:     "project_a",
		MemoryType:    memory.TypeReviewCheckpoint,
		SourceType:    "task_result",
		CreatedBy:     "automation:rule_based",
		Title:         "P3 详细设计复查 checkpoint",
		Content:       "P3 详细设计复查完成，后续关注自动写入闭环。",
		State:         memory.StatePendingReview,
		Confidence:    0.8,
		Importance:    0.8,
		EncodingDepth: 2,
		DecayRate:     0.3,
		Tier:          memory.TierLongTerm,
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       1,
	}
	checkpoint := &memory.ReviewCheckpoint{
		ID:                   "chk_001",
		MemoryID:             item.ID,
		WorkspaceID:          item.WorkspaceID,
		ProjectID:            item.ProjectID,
		CheckpointType:       "implementation_design_review",
		ReviewIntentJSON:     jsonArrayText("logic_consistency"),
		TargetDocsJSON:       `[{"path":"doc/The One 长期记忆系统 P3 详细设计.md"}]`,
		Conclusion:           "supplemented",
		NextReviewPolicyJSON: `{"focus":"major_logic_gap"}`,
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:             item,
		EvidenceIDs:      []string{evidence.ID},
		ReviewCheckpoint: checkpoint,
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory checkpoint error = %v", err)
	}
	got, found, err := store.GetReviewCheckpoint(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetReviewCheckpoint() error = %v", err)
	}
	if !found || got.ID != checkpoint.ID || got.Conclusion != "supplemented" {
		t.Fatalf("checkpoint = %+v found=%v, want chk_001 supplemented", got, found)
	}
}

func TestAutomatedMemoryRepositoryResolvesCorrectionTargetByEvent(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	evidence := memory.Evidence{
		ID:                   "ev_target_event",
		RawEventID:           "evt_target_event",
		SourceType:           "agent_summary",
		InterpretedStatement: "旧事实来自目标事件。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item: memory.MemoryItem{
			ID:            "mem_target_event",
			Scope:         memory.ScopeProjectLocal,
			WorkspaceID:   "ws",
			ProjectID:     "project_a",
			MemoryType:    memory.TypeProjectFact,
			Content:       "旧事实。",
			State:         memory.StateArchived,
			Confidence:    0.8,
			Importance:    0.5,
			EncodingDepth: 2,
			DecayRate:     0.4,
			Tier:          memory.TierArchived,
			Version:       1,
		},
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	target, found, err := store.ResolveCorrectionTargetMemory(ctx, automation.CorrectionTargetRequest{TargetEventID: evidence.RawEventID})
	if err != nil {
		t.Fatalf("ResolveCorrectionTargetMemory() error = %v", err)
	}
	if !found || target.ID != "mem_target_event" {
		t.Fatalf("target = %+v found=%v, want mem_target_event", target, found)
	}
}

func TestAutomatedMemoryRepositoryRelationAndRelatedMemory(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	ev := memory.Evidence{
		ID:                   "ev_relation",
		RawEventID:           "evt_relation",
		SourceType:           "user_confirmed",
		InterpretedStatement: "数据库改为 PostgreSQL。",
		Confidence:           0.9,
	}
	if err := store.WriteEvidence(ctx, ev); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	for _, item := range []memory.MemoryItem{
		{
			ID:            "mem_old",
			Scope:         memory.ScopeProjectLocal,
			WorkspaceID:   "ws",
			ProjectID:     "project_a",
			MemoryType:    memory.TypeProjectFact,
			Title:         "数据库事实",
			Content:       "当前数据库使用 MySQL。",
			State:         memory.StateArchived,
			Confidence:    0.8,
			Importance:    0.7,
			EncodingDepth: 2,
			DecayRate:     0.4,
			Tier:          memory.TierArchived,
			Version:       1,
		},
		{
			ID:            "mem_new",
			Scope:         memory.ScopeProjectLocal,
			WorkspaceID:   "ws",
			ProjectID:     "project_a",
			MemoryType:    memory.TypeProjectFact,
			Title:         "数据库事实",
			Content:       "当前数据库使用 PostgreSQL。",
			State:         memory.StateStable,
			Confidence:    0.9,
			Importance:    0.8,
			EncodingDepth: 2,
			DecayRate:     0.4,
			Tier:          memory.TierLongTerm,
			SupersedesID:  "mem_old",
			Version:       1,
		},
	} {
		if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{Item: item, EvidenceIDs: []string{ev.ID}}); err != nil {
			t.Fatalf("WriteAutomatedMemory(%s) error = %v", item.ID, err)
		}
	}
	relation := memory.MemoryRelation{
		ID:           "rel_001",
		SourceID:     "mem_new",
		TargetID:     "mem_old",
		RelationType: "supersedes",
	}
	if err := store.WriteMemoryRelation(ctx, relation); err != nil {
		t.Fatalf("WriteMemoryRelation() error = %v", err)
	}
	if err := store.WriteMemoryRelation(ctx, memory.MemoryRelation{
		ID:           "rel_duplicate",
		SourceID:     relation.SourceID,
		TargetID:     relation.TargetID,
		RelationType: relation.RelationType,
	}); err != nil {
		t.Fatalf("WriteMemoryRelation duplicate error = %v", err)
	}
	var relationCount int
	if err := store.db.QueryRow("select count(*) from memory_relation where source_id = ? and target_id = ? and relation_type = ?", relation.SourceID, relation.TargetID, relation.RelationType).Scan(&relationCount); err != nil {
		t.Fatalf("count relation: %v", err)
	}
	if relationCount != 1 {
		t.Fatalf("relation count = %d, want 1", relationCount)
	}
	related, err := store.FindRelatedMemory(ctx, automation.RelatedMemoryRequest{
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		Scope:       memory.ScopeProjectLocal,
		MemoryType:  memory.TypeProjectFact,
		Query:       "PostgreSQL",
	})
	if err != nil {
		t.Fatalf("FindRelatedMemory() error = %v", err)
	}
	if len(related) != 1 || related[0].ID != "mem_new" || strings.Contains(related[0].Content, "MySQL") {
		t.Fatalf("related = %+v, want active PostgreSQL memory", related)
	}
}

func TestAutomatedMemoryRepositoryValidation(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	if err := store.WriteEvidence(ctx, memory.Evidence{ID: "ev_bad"}); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatalf("WriteEvidence validation error = %v, want VALIDATION_FAILED", err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{Item: memory.MemoryItem{ID: "mem_bad"}}); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatalf("WriteAutomatedMemory validation error = %v, want VALIDATION_FAILED", err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item: memory.MemoryItem{
			ID:         "mem_without_evidence",
			Scope:      memory.ScopeSession,
			MemoryType: memory.TypeTemporaryState,
			Content:    "临时状态不能脱离 evidence 自动写入。",
			State:      memory.StateArchived,
			Tier:       memory.TierArchived,
		},
		EvidenceIDs: []string{" "},
	}); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatalf("WriteAutomatedMemory empty evidence error = %v, want VALIDATION_FAILED", err)
	}
	if err := store.WriteMemoryRelation(ctx, memory.MemoryRelation{ID: "rel_bad"}); err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
		t.Fatalf("WriteMemoryRelation validation error = %v, want VALIDATION_FAILED", err)
	}
}

func requireFTS5(t *testing.T, store *Store) {
	t.Helper()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; run with -tags sqlite_fts5 to verify automated memory FTS sync")
	}
}
