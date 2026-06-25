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
		InterpretedStatement: "用户要求 automation 自动写入必须可解释。",
		KeywordsJSON:         jsonArrayText("automation", "自动写入"),
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
		InterpretedStatement: "automation 只实现 rule_based Provider。",
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
		Title:         "automation Provider 决策",
		Content:       "automation 只实现 rule_based Provider，外部 LLM Provider 放到后续版本。",
		KeywordsJSON:  jsonArrayText("automation", "rule_based"),
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

func TestAutomatedMemoryRepositoryWritesMemoryKeyProjection(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	evidence := memory.Evidence{
		ID:                   "ev_key_auto",
		RawEventID:           "evt_key_auto",
		SourceType:           "agent_summary",
		InterpretedStatement: "自动记忆使用 QKV Retrieval Projection。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:          "mem_key_auto",
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_key_auto",
		MemoryType:  memory.TypeDecision,
		Content:     "自动记忆使用 QKV Retrieval Projection。",
		SearchText:  "retrieval: QKV Retrieval Projection",
		State:       memory.StatePendingReview,
		Tier:        memory.TierLongTerm,
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}

	resp, _, err := store.Search(ctx, memory.SearchRequest{
		Query:       "qkvretrievalprojection",
		WorkspaceID: "ws",
		ProjectID:   "project_key_auto",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(resp) != 1 || resp[0].MemoryID != item.ID {
		t.Fatalf("results = %+v, want automated memory_key fallback hit", resp)
	}
	if !containsString(resp[0].WhyIncluded, "memory_key_fallback") {
		t.Fatalf("why_included = %#v, want memory_key_fallback", resp[0].WhyIncluded)
	}
}

func TestSearchResultCarriesRetrievalProfile(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	evidence := memory.Evidence{
		ID:                   "ev_profile",
		RawEventID:           "evt_profile",
		SourceType:           "agent_summary",
		InterpretedStatement: "上下文注入应使用检索画像。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:                "mem_profile",
		Scope:             memory.ScopeProjectLocal,
		WorkspaceID:       "ws",
		ProjectID:         "project_profile",
		MemoryType:        memory.TypeDecision,
		Content:           "上下文注入前应比较 query profile 与 memory retrieval profile。",
		SearchText:        "上下文 注入 retrieval profile",
		KeywordsJSON:      jsonArrayText("上下文注入", "retrieval profile"),
		RetrievalCuesJSON: jsonArrayText("如何防止 FTS 宽召回污染上下文", "query profile 对比 memory profile"),
		State:             memory.StatePendingReview,
		Tier:              memory.TierLongTerm,
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}

	results, _, err := store.Search(ctx, memory.SearchRequest{
		Query:       "上下文 注入",
		WorkspaceID: "ws",
		ProjectID:   "project_profile",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeDecision},
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].MemoryID != item.ID {
		t.Fatalf("results = %+v, want mem_profile", results)
	}
	if results[0].KeywordsJSON == "" || results[0].RetrievalCuesJSON == "" {
		t.Fatalf("search result profile = keywords:%q cues:%q, want retrieval profile carried to rerank", results[0].KeywordsJSON, results[0].RetrievalCuesJSON)
	}
}

func TestFindRelatedMemoryUsesTokenOverlapInsteadOfFullSentenceLike(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	evidence := memory.Evidence{
		ID:                   "ev_related_token",
		RawEventID:           "evt_related_token",
		SourceType:           "agent_summary",
		InterpretedStatement: "数据库基线是 PostgreSQL。",
		Confidence:           0.9,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:            "mem_related_postgres",
		Scope:         memory.ScopeProjectLocal,
		WorkspaceID:   "ws",
		ProjectID:     "project_related",
		MemoryType:    memory.TypeProjectFact,
		Content:       "当前数据库使用 PostgreSQL。",
		SearchText:    "database PostgreSQL postgres 数据库",
		KeywordsJSON:  jsonArrayText("PostgreSQL", "database", "数据库"),
		State:         memory.StateStable,
		Confidence:    0.9,
		Importance:    0.8,
		EncodingDepth: 2,
		DecayRate:     0.3,
		Tier:          memory.TierLongTerm,
		Version:       1,
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}

	related, err := store.FindRelatedMemory(ctx, automation.RelatedMemoryRequest{
		WorkspaceID: "ws",
		ProjectID:   "project_related",
		Scope:       memory.ScopeProjectLocal,
		MemoryType:  memory.TypeProjectFact,
		Query:       "当前数据库不再使用 MySQL，已经改为 PostgreSQL。",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("FindRelatedMemory() error = %v", err)
	}
	if len(related) == 0 || related[0].ID != item.ID {
		t.Fatalf("related = %+v, want PostgreSQL baseline memory", related)
	}
}

func TestAutomatedMemoryRepositoryWritesProvenanceWithMemory(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()

	evidence := memory.Evidence{
		ID:                   "ev_provenance",
		RawEventID:           "evt_provenance",
		SourceType:           "tool_output",
		InterpretedStatement: "Codex PostToolUse 捕获到工具失败摘要。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:            "mem_provenance",
		Scope:         memory.ScopeSession,
		WorkspaceID:   "ws",
		SessionID:     "sess_provenance",
		MemoryType:    memory.TypeTemporaryState,
		SourceType:    "tool_output",
		CreatedBy:     "automation:rule_based",
		Content:       "Codex PostToolUse 捕获到工具失败摘要。",
		State:         memory.StateArchived,
		Confidence:    0.8,
		Importance:    0.5,
		EncodingDepth: 2,
		DecayRate:     0.8,
		Tier:          memory.TierArchived,
		Version:       1,
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
		Provenance: &automation.MemoryProvenance{
			RawEventID:        evidence.RawEventID,
			EvidenceID:        evidence.ID,
			CandidateID:       "cand_provenance",
			AgentType:         "codex",
			SourceChannel:     "agent_session",
			SourceProducer:    "codex_hook:PostToolUse",
			HookName:          "PostToolUse",
			HookPhase:         automation.HookPhasePostTool,
			EventType:         "tool.result.summary",
			CaptureMethod:     "adapter_hook",
			Pipeline:          "raw_event->evidence->candidate->admission->memory",
			Provider:          "rule_based",
			DerivationStage:   automation.JobTypeComputeAdmission,
			AdmissionDecision: automation.DecisionWriteProvisional,
			AdmissionScore:    0.74,
		},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	got, found, err := store.GetMemoryProvenance(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetMemoryProvenance() error = %v", err)
	}
	if !found {
		t.Fatalf("GetMemoryProvenance() found=false, want true")
	}
	if got.MemoryID != item.ID || got.RawEventID != evidence.RawEventID || got.EvidenceID != evidence.ID || got.CandidateID != "cand_provenance" {
		t.Fatalf("provenance ids = %+v, want memory/raw/evidence/candidate linkage", got)
	}
	if got.SourceProducer != "codex_hook:PostToolUse" || got.HookPhase != automation.HookPhasePostTool || got.Provider != "rule_based" || got.AdmissionDecision != automation.DecisionWriteProvisional {
		t.Fatalf("provenance = %+v, want codex post-tool rule_based write_provisional", got)
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
		InterpretedStatement: "automation 详细设计复查完成。",
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
		Title:         "automation 详细设计复查 checkpoint",
		Content:       "automation 详细设计复查完成，后续关注自动写入闭环。",
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
		TargetDocsJSON:       `[{"path":"doc/theone 长期记忆系统 automation 详细设计.md"}]`,
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

func TestAutomatedMemoryCorrectionRebuildsMemoryKeyProjection(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	evidence := memory.Evidence{
		ID:                   "ev_correction_key",
		RawEventID:           "evt_correction_key",
		SourceType:           "agent_summary",
		InterpretedStatement: "纠正前使用旧检索线索。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:          "mem_correction_key",
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_correction_key",
		MemoryType:  memory.TypeProjectFact,
		Content:     "旧线索是 Legacy Retrieval Projection。",
		SearchText:  "retrieval: Legacy Retrieval Projection",
		State:       memory.StateStable,
		Tier:        memory.TierLongTerm,
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	correctionEvidence := memory.Evidence{
		ID:                   "ev_correction_key_new",
		RawEventID:           "evt_correction_key_new",
		SourceType:           "user_confirmed",
		InterpretedStatement: "纠正后使用 QKV Retrieval Projection。",
		Confidence:           0.9,
	}
	if err := store.WriteEvidence(ctx, correctionEvidence); err != nil {
		t.Fatalf("WriteEvidence(correction) error = %v", err)
	}
	if _, err := store.OverwriteMemoryWithCorrection(ctx, automation.AutomatedMemoryCorrection{
		TargetMemoryID: item.ID,
		Item: memory.MemoryItem{
			Scope:       item.Scope,
			WorkspaceID: item.WorkspaceID,
			ProjectID:   item.ProjectID,
			MemoryType:  item.MemoryType,
			Content:     "新线索是 QKV Retrieval Projection。",
			SearchText:  "retrieval: QKV Retrieval Projection",
			State:       memory.StateStable,
			Tier:        memory.TierLongTerm,
		},
		EvidenceIDs: []string{correctionEvidence.ID},
	}); err != nil {
		t.Fatalf("OverwriteMemoryWithCorrection() error = %v", err)
	}

	oldResults, _, err := store.Search(ctx, memory.SearchRequest{
		Query:       "legacyretrievalprojection",
		WorkspaceID: "ws",
		ProjectID:   "project_correction_key",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeProjectFact},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(old key) error = %v", err)
	}
	if len(oldResults) != 0 {
		t.Fatalf("old key results = %+v, want no hit after correction", oldResults)
	}
	newResults, _, err := store.Search(ctx, memory.SearchRequest{
		Query:       "qkvretrievalprojection",
		WorkspaceID: "ws",
		ProjectID:   "project_correction_key",
		Scope:       []string{memory.ScopeProjectLocal},
		MemoryTypes: []string{memory.TypeProjectFact},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search(new key) error = %v", err)
	}
	if len(newResults) != 1 || newResults[0].MemoryID != item.ID {
		t.Fatalf("new key results = %+v, want corrected memory hit", newResults)
	}
}

func TestAutomatedArchivePathsDeleteMemoryKeyProjection(t *testing.T) {
	ctx := context.Background()
	store := newCaptureTestStore(t)
	defer store.Close()
	requireFTS5(t, store)

	evidence := memory.Evidence{
		ID:                   "ev_archive_key",
		RawEventID:           "evt_archive_key",
		SourceType:           "agent_summary",
		InterpretedStatement: "归档前存在 key 投影。",
		Confidence:           0.8,
	}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	item := memory.MemoryItem{
		ID:          "mem_archive_key",
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "project_archive_key",
		MemoryType:  memory.TypeProjectFact,
		Content:     "归档测试使用 Archive Key Projection。",
		SearchText:  "retrieval: Archive Key Projection",
		State:       memory.StateStable,
		Tier:        memory.TierLongTerm,
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item:        item,
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory() error = %v", err)
	}
	var keyCount int
	if err := store.db.QueryRowContext(ctx, "select count(*) from memory_key where memory_id = ?", item.ID).Scan(&keyCount); err != nil {
		t.Fatalf("query memory_key count before archive error = %v", err)
	}
	if keyCount == 0 {
		t.Fatal("memory_key count before archive = 0, want key projection to clean")
	}
	if err := store.UpsertMemoryEmbedding(ctx, item.ID, "embedding-test", []float32{1, 0}); err != nil {
		t.Fatalf("UpsertMemoryEmbedding() error = %v", err)
	}
	if err := store.ArchiveMemoryForSupersedes(ctx, item.ID, time.Now().UTC()); err != nil {
		t.Fatalf("ArchiveMemoryForSupersedes() error = %v", err)
	}
	if err := store.db.QueryRowContext(ctx, "select count(*) from memory_key where memory_id = ?", item.ID).Scan(&keyCount); err != nil {
		t.Fatalf("query memory_key count error = %v", err)
	}
	if keyCount != 0 {
		t.Fatalf("memory_key count = %d, want 0 after supersedes archive", keyCount)
	}
	var embeddingCount int
	if err := store.db.QueryRowContext(ctx, "select count(*) from memory_embedding where memory_id = ?", item.ID).Scan(&embeddingCount); err != nil {
		t.Fatalf("query memory_embedding count error = %v", err)
	}
	if embeddingCount != 0 {
		t.Fatalf("memory_embedding count = %d, want 0 after supersedes archive", embeddingCount)
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
