package automation_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/docindex"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
	"github.com/zaneway/theone/internal/retrieval"
	"github.com/zaneway/theone/internal/storage/sqlite"
)

func TestServiceRunsEvidenceCandidateAdmissionChain(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; automation chain needs searchable automated memory")
	}

	now := time.Date(2026, 5, 24, 9, 30, 0, 0, time.UTC)
	session := capture.AgentSession{
		ID:           "sess_p3_c1",
		AgentType:    "cursor",
		WorkspaceID:  "ws",
		ProjectID:    "project_a",
		CaptureLevel: 3,
		StartedAt:    now,
		Status:       capture.StatusActive,
	}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{
		ID:          "task_p3_c1",
		SessionID:   session.ID,
		WorkspaceID: session.WorkspaceID,
		ProjectID:   session.ProjectID,
		TaskSummary: "推进 automation automation service",
		Status:      capture.StatusActive,
		StartedAt:   now,
	}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_p3_c1_preference",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "以后推进 automation 时先按详细设计拆分任务，再用测试验证。",
		SourceRefsJSON: `[{"source_type":"agent_session","capture_method":"adapter_hook","producer":"codex_hook:UserPromptSubmit"}]`,
		ContentHash:    "sha256:p3-c1-preference",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}

	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)
	runNextJob(t, ctx, store, service, automation.JobTypeGenerateMemoryCandidate, "")
	runNextJob(t, ctx, store, service, automation.JobTypeComputeAdmission, "")

	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{Status: automation.CandidateStatusAdmitted})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID == "" {
		t.Fatalf("admitted candidates = %+v, want one candidate with resulting memory", candidates)
	}

	written, err := store.Get(ctx, candidates[0].ResultingMemoryID)
	if err != nil {
		t.Fatalf("Get(resulting memory) error = %v", err)
	}
	if written.MemoryType != memory.TypePreference || written.State != memory.StateStable || !written.UserConfirmed {
		t.Fatalf("written memory = %+v, want stable user-confirmed preference", written)
	}
	provenance, found, err := store.GetMemoryProvenance(ctx, written.ID)
	if err != nil {
		t.Fatalf("GetMemoryProvenance() error = %v", err)
	}
	if !found {
		t.Fatalf("GetMemoryProvenance() found=false, want true")
	}
	if provenance.RawEventID != rawEvent.ID || provenance.EvidenceID == "" || provenance.CandidateID != candidates[0].ID {
		t.Fatalf("provenance = %+v, want raw event/evidence/candidate linkage", provenance)
	}
	if provenance.SourceProducer != "codex_hook:UserPromptSubmit" || provenance.HookPhase != automation.HookPhasePrePrompt || provenance.Provider != "rule_based" || provenance.DerivationStage != automation.JobTypeComputeAdmission {
		t.Fatalf("provenance = %+v, want codex pre-prompt compute_admission provenance", provenance)
	}
}

func TestServiceEnqueuesAndComputesMemoryEmbeddingAfterAdmission(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Embedding.Provider = "external"
	cfg.Embedding.Model = "embedding-test"
	cfg.Embedding.MemoryEmbeddingEnabled = true
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; automation chain needs searchable automated memory")
	}

	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	session := capture.AgentSession{
		ID:           "sess_embedding",
		AgentType:    "codex",
		WorkspaceID:  "ws_embed",
		ProjectID:    "project_embed",
		CaptureLevel: 3,
		StartedAt:    now,
		Status:       capture.StatusActive,
	}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{
		ID:          "task_embedding",
		SessionID:   session.ID,
		WorkspaceID: session.WorkspaceID,
		ProjectID:   session.ProjectID,
		TaskSummary: "生成 memory embedding K",
		Status:      capture.StatusActive,
		StartedAt:   now,
	}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_embedding",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "项目记忆向量 K 必须在 memory_item 写入后异步生成。",
		ContentHash:    "sha256:embedding-event",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	embeddingProvider := &fakeTextEmbeddingProvider{vector: []float32{1, 0, 0}}
	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider(), automation.WithEmbeddingProvider(embeddingProvider))
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)
	runNextJob(t, ctx, store, service, automation.JobTypeGenerateMemoryCandidate, "")
	runNextJob(t, ctx, store, service, automation.JobTypeComputeAdmission, "")

	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{Status: automation.CandidateStatusAdmitted, Limit: 10})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID == "" {
		t.Fatalf("admitted candidates = %+v, want one resulting memory", candidates)
	}
	embeddingJobs, err := store.ListJobs(ctx, automation.ListJobsRequest{
		Status:   automation.JobStatusPending,
		JobType:  automation.JobTypeComputeEmbedding,
		TargetID: candidates[0].ResultingMemoryID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListJobs(compute_embedding) error = %v", err)
	}
	if len(embeddingJobs) != 1 {
		t.Fatalf("embedding jobs = %+v, want one pending compute_embedding job", embeddingJobs)
	}

	runNextJob(t, ctx, store, service, automation.JobTypeComputeEmbedding, candidates[0].ResultingMemoryID)
	if len(embeddingProvider.inputs) != 1 || !strings.Contains(embeddingProvider.inputs[0], "memory_type") || !strings.Contains(embeddingProvider.inputs[0], "异步生成") {
		t.Fatalf("embedding inputs = %+v, want structured memory text", embeddingProvider.inputs)
	}
	results, err := store.SearchVector(ctx, memory.SearchRequest{
		Query:       "memory embedding",
		WorkspaceID: session.WorkspaceID,
		ProjectID:   session.ProjectID,
		Scope:       []string{memory.ScopeProjectLocal},
		Limit:       10,
	}, cfg.Embedding.Model, []float32{1, 0, 0}, 10)
	if err != nil {
		t.Fatalf("SearchVector() error = %v", err)
	}
	if len(results) != 1 || results[0].MemoryID != candidates[0].ResultingMemoryID {
		t.Fatalf("vector results = %+v, want generated memory embedding", results)
	}
}

func TestMemoryEmbeddingRecomputesAfterRememberEdit(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Embedding.Provider = "external"
	cfg.Embedding.Model = "embedding-test"
	cfg.Embedding.MemoryEmbeddingEnabled = true
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; automation chain needs searchable automated memory")
	}

	embeddingProvider := &fakeTextEmbeddingProvider{vector: []float32{1, 0, 0}}
	automationService := automation.NewService(cfg, store, processor.NewRuleBasedProvider(), automation.WithEmbeddingProvider(embeddingProvider))
	memoryService := memory.NewService(cfg, store,
		memory.WithRememberAdmissionDecider(automationService),
		memory.WithEmbeddingJobEnqueuer(automationService),
	)
	rememberResp, err := memoryService.Remember(ctx, memory.RememberRequest{
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws_embed_edit",
		ProjectID:   "project_embed_edit",
		MemoryType:  memory.TypeDecision,
		SourceType:  "user_declared",
		Title:       "embedding edit lifecycle",
		Content:     "初始内容用于生成第一版 memory embedding。",
		Confidence:  0.9,
		Importance:  0.8,
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户声明需要生成第一版 K。",
		},
	})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	runNextJob(t, ctx, store, automationService, automation.JobTypeComputeEmbedding, rememberResp.MemoryID)

	if _, err := memoryService.Review(ctx, memory.ReviewRequest{
		Action:      "edit",
		MemoryID:    rememberResp.MemoryID,
		Reviewer:    "test",
		Feedback:    "refresh embedding",
		EditContent: "编辑后的内容必须重新生成第二版 memory embedding。",
	}); err != nil {
		t.Fatalf("Review(edit) error = %v", err)
	}
	runNextJob(t, ctx, store, automationService, automation.JobTypeComputeEmbedding, rememberResp.MemoryID)
	if len(embeddingProvider.inputs) != 2 {
		t.Fatalf("embedding provider calls = %d, want 2 after remember and edit; inputs=%+v", len(embeddingProvider.inputs), embeddingProvider.inputs)
	}
	if !strings.Contains(embeddingProvider.inputs[1], "第二版 memory embedding") {
		t.Fatalf("second embedding input = %q, want edited content", embeddingProvider.inputs[1])
	}
}

func TestServiceCombinedProviderWritesCandidatesDuringExtractJob(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	session := capture.AgentSession{
		ID:          "sess_openai_combined",
		AgentType:   "codex",
		WorkspaceID: "ws",
		ProjectID:   "project_a",
		StartedAt:   now,
		Status:      capture.StatusActive,
	}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{
		ID:          "task_openai_combined",
		SessionID:   session.ID,
		WorkspaceID: session.WorkspaceID,
		ProjectID:   session.ProjectID,
		TaskSummary: "合并 OpenAI processor 输出",
		Status:      capture.StatusActive,
		StartedAt:   now,
	}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_openai_combined",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventUserDeclaration,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "processor.provider=openai 时 raw_event 自动处理只能调用一次外部模型，同时产出 evidence 和 candidate。",
		ContentHash:    "sha256:openai-combined",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	provider := &combinedProviderStub{}
	service := automation.NewService(cfg, store, provider)
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)

	if provider.processCalls != 1 || provider.extractCalls != 0 || provider.generateCalls != 0 {
		t.Fatalf("provider calls process=%d extract=%d generate=%d, want only one combined process call", provider.processCalls, provider.extractCalls, provider.generateCalls)
	}
	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{RawEventID: rawEvent.ID})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].Provider != processor.OpenAIProviderName || candidates[0].EvidenceID == "" {
		t.Fatalf("candidates = %+v, want one openai candidate linked to persisted evidence", candidates)
	}
	if !strings.Contains(candidates[0].SourceEvidenceIDsJSON, candidates[0].EvidenceID) {
		t.Fatalf("source evidence ids = %s, want materialized evidence id %s", candidates[0].SourceEvidenceIDsJSON, candidates[0].EvidenceID)
	}
	generateJobs, err := store.ListJobs(ctx, automation.ListJobsRequest{JobType: automation.JobTypeGenerateMemoryCandidate})
	if err != nil {
		t.Fatalf("ListJobs(generate) error = %v", err)
	}
	if len(generateJobs) != 0 {
		t.Fatalf("generate jobs = %+v, want none for combined provider", generateJobs)
	}
	pending, err := store.ListJobs(ctx, automation.ListJobsRequest{Status: automation.JobStatusPending})
	if err != nil {
		t.Fatalf("ListJobs(pending) error = %v", err)
	}
	if len(pending) != 1 || pending[0].JobType != automation.JobTypeComputeAdmission || pending[0].TargetID != candidates[0].ID {
		t.Fatalf("pending jobs = %+v, want compute_admission for combined candidate", pending)
	}
	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:         "job_stale_openai_generate",
		JobType:    automation.JobTypeGenerateMemoryCandidate,
		TargetType: automation.TargetTypeEvidence,
		TargetID:   candidates[0].EvidenceID,
		Priority:   1,
		MaxRetries: 3,
		DedupKey:   "stale_generate:" + candidates[0].EvidenceID,
		NextRunAt:  time.Now().UTC().Add(-time.Second),
	}); err != nil {
		t.Fatalf("EnqueueJob(stale generate) error = %v", err)
	}
	staleJobs, err := store.ClaimJobs(ctx, time.Now().UTC().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("ClaimJobs(stale generate) error = %v", err)
	}
	if len(staleJobs) != 1 || staleJobs[0].JobType != automation.JobTypeGenerateMemoryCandidate {
		t.Fatalf("stale jobs = %+v, want generate_memory_candidate", staleJobs)
	}
	if err := service.RunJob(ctx, staleJobs[0]); err != nil {
		t.Fatalf("RunJob(stale generate) error = %v", err)
	}
	if provider.generateCalls != 0 {
		t.Fatalf("generate calls = %d, want stale generate job skipped for combined provider", provider.generateCalls)
	}
}

func TestServiceWritesReviewCheckpointFromCandidate(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; checkpoint chain needs searchable automated memory")
	}

	now := time.Date(2026, 5, 24, 10, 10, 0, 0, time.UTC)
	session := capture.AgentSession{ID: "sess_checkpoint", AgentType: "cursor", WorkspaceID: "ws", ProjectID: "project_a", CaptureLevel: 3, StartedAt: now, Status: capture.StatusActive}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{ID: "task_checkpoint", SessionID: session.ID, WorkspaceID: session.WorkspaceID, ProjectID: session.ProjectID, TaskSummary: "automation 详细设计复查", OutcomeSummary: "automation 详细设计复查完成。", Status: capture.StatusCompleted, StartedAt: now}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_checkpoint_chain",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventTaskResult,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorAgent,
		ContentSummary: "automation 详细设计复查完成。",
		SourceRefsJSON: `[{"checkpoint_type":"implementation_design_review","review_intent":["logic_consistency"],"target_docs":[{"path":"doc/The One 长期记忆系统 automation 详细设计.md"}],"conclusion":"supplemented"}]`,
		ContentHash:    "sha256:p3-checkpoint-chain",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)
	runNextJob(t, ctx, store, service, automation.JobTypeGenerateMemoryCandidate, "")
	runNextJob(t, ctx, store, service, automation.JobTypeComputeAdmission, "")

	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{MemoryType: memory.TypeReviewCheckpoint, Status: automation.CandidateStatusAdmitted})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID == "" {
		t.Fatalf("checkpoint candidates = %+v, want admitted checkpoint", candidates)
	}
	checkpoint, found, err := store.GetReviewCheckpoint(ctx, candidates[0].ResultingMemoryID)
	if err != nil {
		t.Fatalf("GetReviewCheckpoint() error = %v", err)
	}
	if !found || checkpoint.Conclusion != "supplemented" || checkpoint.CheckpointType != "implementation_design_review" {
		t.Fatalf("checkpoint = %+v found=%v, want persisted checkpoint", checkpoint, found)
	}
}

func TestServiceUsesRelatedMemoryForRepeatedFailureCandidate(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; related-memory chain needs searchable automated memory")
	}

	now := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
	evidence := memory.Evidence{ID: "ev_old_failure", RawEventID: "evt_old_failure", SourceType: "tool_output", InterpretedStatement: "token expiry boundary error", Confidence: 0.8}
	if err := store.WriteEvidence(ctx, evidence); err != nil {
		t.Fatalf("WriteEvidence() error = %v", err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item: memory.MemoryItem{
			ID:            "mem_old_failure",
			Scope:         memory.ScopeProjectLocal,
			WorkspaceID:   "ws",
			ProjectID:     "project_a",
			MemoryType:    memory.TypeFailure,
			SourceType:    "tool_output",
			Title:         "旧失败",
			Content:       "token expiry boundary error",
			State:         memory.StateProvisional,
			Confidence:    0.8,
			Importance:    0.7,
			EncodingDepth: 2,
			DecayRate:     0.4,
			Tier:          memory.TierShortTerm,
			CreatedAt:     now.Add(-time.Hour),
			UpdatedAt:     now.Add(-time.Hour),
		},
		EvidenceIDs: []string{evidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory(old failure) error = %v", err)
	}
	session := capture.AgentSession{ID: "sess_failure", AgentType: "cursor", WorkspaceID: "ws", ProjectID: "project_a", CaptureLevel: 3, StartedAt: now, Status: capture.StatusActive}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{ID: "task_failure", SessionID: session.ID, WorkspaceID: session.WorkspaceID, ProjectID: session.ProjectID, TaskSummary: "运行测试", Status: capture.StatusActive, StartedAt: now}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_repeated_failure",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventToolResultSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorTool,
		ToolName:       "go test",
		OutputSummary:  "token expiry boundary error",
		SourceRefsJSON: `[{"exit_code":1}]`,
		ContentHash:    "sha256:repeated-failure",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)
	runNextJob(t, ctx, store, service, automation.JobTypeGenerateMemoryCandidate, "")
	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{RawEventID: rawEvent.ID})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].MemoryType != memory.TypeFailure {
		t.Fatalf("candidates = %+v, want failure candidate from related memory", candidates)
	}
}

func TestServiceOverwritesTargetMemoryForUserCorrection(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if !store.Status().Capabilities.FTS5 {
		t.Skip("sqlite FTS5 unavailable; correction overwrite needs FTS sync")
	}

	now := time.Date(2026, 5, 24, 10, 50, 0, 0, time.UTC)
	oldEvidence := memory.Evidence{ID: "ev_old_db", RawEventID: "evt_old_db", SourceType: "agent_summary", InterpretedStatement: "当前数据库使用 MySQL。", Confidence: 0.8}
	if err := store.WriteEvidence(ctx, oldEvidence); err != nil {
		t.Fatalf("WriteEvidence(old) error = %v", err)
	}
	if _, err := store.WriteAutomatedMemory(ctx, automation.AutomatedMemoryWrite{
		Item: memory.MemoryItem{
			ID:            "mem_db_fact",
			Scope:         memory.ScopeProjectLocal,
			WorkspaceID:   "ws",
			ProjectID:     "project_a",
			MemoryType:    memory.TypeProjectFact,
			SourceType:    "agent_summary",
			Title:         "数据库事实",
			Content:       "当前数据库使用 MySQL。",
			State:         memory.StateStable,
			Confidence:    0.8,
			Importance:    0.7,
			EncodingDepth: 2,
			DecayRate:     0.4,
			Tier:          memory.TierLongTerm,
			CreatedAt:     now.Add(-time.Hour),
			UpdatedAt:     now.Add(-time.Hour),
		},
		EvidenceIDs: []string{oldEvidence.ID},
	}); err != nil {
		t.Fatalf("WriteAutomatedMemory(old db) error = %v", err)
	}
	session := capture.AgentSession{ID: "sess_correction", AgentType: "cursor", WorkspaceID: "ws", ProjectID: "project_a", CaptureLevel: 3, StartedAt: now, Status: capture.StatusActive}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{ID: "task_correction", SessionID: session.ID, WorkspaceID: session.WorkspaceID, ProjectID: session.ProjectID, TaskSummary: "修正数据库事实", Status: capture.StatusActive, StartedAt: now}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_correct_db",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventUserCorrection,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorUser,
		ContentSummary: "纠正：当前数据库使用 PostgreSQL。",
		SourceRefsJSON: `[{"target_memory_id":"mem_db_fact","target_memory_type":"project_fact","target_memory_scope":"project_local"}]`,
		ContentHash:    "sha256:correct-db",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)
	runNextJob(t, ctx, store, service, automation.JobTypeGenerateMemoryCandidate, "")
	runNextJob(t, ctx, store, service, automation.JobTypeComputeAdmission, "")

	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{RawEventID: rawEvent.ID, Status: automation.CandidateStatusAdmitted})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].ResultingMemoryID != "mem_db_fact" {
		t.Fatalf("candidates = %+v, want correction to reuse old memory id", candidates)
	}
	updated, err := store.Get(ctx, "mem_db_fact")
	if err != nil {
		t.Fatalf("Get(updated memory) error = %v", err)
	}
	if !strings.Contains(updated.Content, "PostgreSQL") || updated.State != memory.StateStable || !updated.UserConfirmed || updated.Version != 2 {
		t.Fatalf("updated memory = %+v, want overwritten stable confirmed memory", updated)
	}
	results, _, err := store.Search(ctx, memory.SearchRequest{
		Query:           "PostgreSQL",
		WorkspaceID:     "ws",
		ProjectID:       "project_a",
		Scope:           []string{memory.ScopeProjectLocal},
		IncludeEvidence: true,
		Limit:           5,
	})
	if err != nil {
		t.Fatalf("Search(updated memory) error = %v", err)
	}
	if len(results) == 0 || results[0].MemoryID != "mem_db_fact" || len(results[0].EvidenceRefs) != 2 {
		t.Fatalf("search results = %+v, want updated memory with old + correction evidence refs", results)
	}
}

func TestServiceSkipsOrdinaryToolSuccessWithoutCandidate(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 24, 9, 40, 0, 0, time.UTC)
	session := capture.AgentSession{
		ID:           "sess_tool_success",
		AgentType:    "cursor",
		WorkspaceID:  "ws",
		ProjectID:    "project_a",
		CaptureLevel: 3,
		StartedAt:    now,
		Status:       capture.StatusActive,
	}
	if _, err := store.UpsertSession(ctx, session); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	task := capture.AgentTask{
		ID:          "task_tool_success",
		SessionID:   session.ID,
		WorkspaceID: session.WorkspaceID,
		ProjectID:   session.ProjectID,
		TaskSummary: "运行测试",
		Status:      capture.StatusActive,
		StartedAt:   now,
	}
	if _, err := store.UpsertTask(ctx, task); err != nil {
		t.Fatalf("UpsertTask() error = %v", err)
	}
	rawEvent := capture.RawEvent{
		ID:             "evt_tool_success",
		SessionID:      session.ID,
		TaskID:         task.ID,
		WorkspaceID:    session.WorkspaceID,
		ProjectID:      session.ProjectID,
		AgentType:      session.AgentType,
		EventType:      capture.EventToolResultSummary,
		SourceChannel:  capture.SourceChannelAgentSession,
		OccurredAt:     now,
		Actor:          capture.ActorTool,
		ToolName:       "go test",
		OutputSummary:  "ok github.com/zaneway/theone/internal/automation",
		SourceRefsJSON: `[{"exit_code":0,"command_hash":"sha256:tool-success"}]`,
		ContentHash:    "sha256:tool-success",
		CreatedAt:      now,
	}
	if err := store.InsertRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("InsertRawEvent() error = %v", err)
	}

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if err := service.EnqueueRawEvent(ctx, rawEvent); err != nil {
		t.Fatalf("EnqueueRawEvent() error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeExtractEvidence, rawEvent.ID)

	jobs, err := store.ListJobs(ctx, automation.ListJobsRequest{Status: automation.JobStatusPending})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("pending jobs = %+v, want none after ordinary successful tool output", jobs)
	}
	candidates, err := store.ListCandidates(ctx, automation.ListCandidatesRequest{})
	if err != nil {
		t.Fatalf("ListCandidates() error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none for ordinary successful tool output", candidates)
	}
}

func TestServiceRunsP4JobsThroughDispatcher(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_retrieval_code_ref",
		JobType:     automation.JobTypeResolveCodeRef,
		TargetType:  automation.TargetTypeMemoryItem,
		TargetID:    "mem_retrieval_code",
		PayloadJSON: `{"repo_id":"repo_p4","file_path":"internal/memory/service.go","symbol":"Service.Search","content_hash":"sha256:code","resolve_mode":"adapter"}`,
		NextRunAt:   now,
	}); err != nil {
		t.Fatalf("EnqueueJob(job_retrieval_code_ref) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeResolveCodeRef, "mem_retrieval_code")
	refs, err := store.ListCodeRefs(ctx, memory.CodeRefQuery{MemoryID: "mem_retrieval_code"})
	if err != nil {
		t.Fatalf("ListCodeRefs() error = %v", err)
	}
	if len(refs) != 1 || refs[0].RepoID != "repo_p4" || refs[0].ResolveStatus != memory.CodeRefStatusStale || refs[0].ContentHash == "sha256:code" {
		t.Fatalf("code refs = %+v, want one stale ref with refreshed hash", refs)
	}

	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "worker.go"), []byte("package demo\n\nfunc RefreshMe() {}\n"), 0o644); err != nil {
		t.Fatalf("write worker.go: %v", err)
	}
	if _, err := store.WriteCodeRef(ctx, memory.CodeRef{
		ID:            "cr_refresh",
		MemoryID:      "mem_refresh",
		RepoID:        repoRoot,
		FilePath:      "worker.go",
		Symbol:        "RefreshMe",
		ResolveStatus: memory.CodeRefStatusUnresolved,
	}); err != nil {
		t.Fatalf("WriteCodeRef(refresh) error = %v", err)
	}
	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_retrieval_refresh_code_ref",
		JobType:     automation.JobTypeRefreshCodeRefStatus,
		TargetType:  automation.TargetTypeRepo,
		TargetID:    repoRoot,
		PayloadJSON: `{"repo_root":"` + repoRoot + `"}`,
		NextRunAt:   now.Add(time.Second),
	}); err != nil {
		t.Fatalf("EnqueueJob(job_retrieval_refresh_code_ref) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeRefreshCodeRefStatus, repoRoot)
	refreshed, err := store.GetCodeRef(ctx, "cr_refresh")
	if err != nil {
		t.Fatalf("GetCodeRef(cr_refresh) error = %v", err)
	}
	if refreshed.ResolveStatus != memory.CodeRefStatusResolved || refreshed.ContentHash == "" {
		t.Fatalf("refreshed code ref = %+v, want resolved with hash", refreshed)
	}

	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_retrieval_doc_snapshot",
		JobType:     automation.JobTypeBuildDocSnapshot,
		TargetType:  automation.TargetTypeDocPath,
		TargetID:    "doc/retrieval.md",
		PayloadJSON: `{"workspace_id":"ws_p4","project_id":"proj_p4","repo_id":"repo_p4","content_hash":"sha256:doc","sections":[{"section_id":"8.3","heading_path":["8. Doc Index","8.3 doc_index 数据模型"],"level":3,"start_line":42,"end_line":80,"content_hash":"sha256:section"}]}`,
		NextRunAt:   now,
	}); err != nil {
		t.Fatalf("EnqueueJob(job_retrieval_doc_snapshot) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeBuildDocSnapshot, "doc/retrieval.md")
	snapshots, err := store.ListDocSnapshots(ctx, docindex.SnapshotQuery{WorkspaceID: "ws_p4", ProjectID: "proj_p4", RepoID: "repo_p4", Path: "doc/retrieval.md", IncludeSections: true})
	if err != nil {
		t.Fatalf("ListDocSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].SectionCount != 1 || len(snapshots[0].Sections) != 1 {
		t.Fatalf("snapshots = %+v, want one snapshot with section", snapshots)
	}

	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_retrieval_embedding",
		JobType:     automation.JobTypeComputeEmbedding,
		TargetType:  automation.TargetTypeMemoryItem,
		TargetID:    "mem_retrieval_code",
		PayloadJSON: `{"embedding_model":"local_stub","embedding":[0.1,0.2,0.3]}`,
		NextRunAt:   now,
	}); err != nil {
		t.Fatalf("EnqueueJob(job_retrieval_embedding) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeComputeEmbedding, "mem_retrieval_code")
	embeddingJob, err := store.GetJob(ctx, "job_retrieval_embedding")
	if err != nil {
		t.Fatalf("GetJob(job_retrieval_embedding) error = %v", err)
	}
	if !strings.Contains(embeddingJob.PayloadJSON, `"embedding_dim":3`) {
		t.Fatalf("embedding payload = %s, want embedding_dim=3", embeddingJob.PayloadJSON)
	}

	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:        "mal_old_retrieved",
		MemoryID:  "mem_retrieval_code",
		EventType: "retrieved",
		CreatedAt: now.AddDate(0, 0, -31),
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog(retrieved) error = %v", err)
	}
	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:            "mal_old_injected",
		MemoryID:      "mem_retrieval_code",
		EventType:     "injected",
		UsedInContext: true,
		CreatedAt:     now.AddDate(0, 0, -181),
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog(injected) error = %v", err)
	}
	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_retrieval_cleanup",
		JobType:     automation.JobTypeCleanupAccessLog,
		TargetType:  automation.TargetTypeWorkspace,
		TargetID:    "ws_p4",
		PayloadJSON: `{"now":"2026-05-24T12:00:00Z"}`,
		NextRunAt:   now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("EnqueueJob(cleanup) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeCleanupAccessLog, "ws_p4")
	logs, err := store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{MemoryID: "mem_retrieval_code"})
	if err != nil {
		t.Fatalf("ListMemoryAccessLogs() error = %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("access logs after cleanup = %+v, want old retrieved/injected removed", logs)
	}
}

func TestServiceBuildsDocSnapshotFromMarkdownPath(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "design.md"), []byte("# Design\n\n## Scope\nretrieval review strategy\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_retrieval_doc_snapshot_build",
		JobType:     automation.JobTypeBuildDocSnapshot,
		TargetType:  automation.TargetTypeDocPath,
		TargetID:    "design.md",
		PayloadJSON: `{"workspace_id":"ws_retrieval_build","project_id":"proj_p4","repo_id":"` + repoRoot + `"}`,
		NextRunAt:   time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("EnqueueJob(job_retrieval_doc_snapshot_build) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeBuildDocSnapshot, "design.md")
	snapshots, err := store.ListDocSnapshots(ctx, docindex.SnapshotQuery{WorkspaceID: "ws_retrieval_build", ProjectID: "proj_p4", RepoID: repoRoot, Path: "design.md", IncludeSections: true})
	if err != nil {
		t.Fatalf("ListDocSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].ContentHash == "" || snapshots[0].SectionCount != 2 {
		t.Fatalf("snapshots = %+v, want computed markdown snapshot", snapshots)
	}
	if strings.Contains(snapshots[0].Sections[1].Summary, "retrieval review strategy") {
		t.Fatalf("section summary persisted body: %q", snapshots[0].Sections[1].Summary)
	}
}

func runNextJob(t *testing.T, ctx context.Context, store *sqlite.Store, service *automation.Service, wantType string, wantTarget string) {
	t.Helper()
	jobs, err := store.ClaimJobs(ctx, time.Now().UTC().Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ClaimJobs() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("claimed jobs = %+v, want exactly one", jobs)
	}
	if jobs[0].JobType != wantType {
		t.Fatalf("job type = %q, want %q", jobs[0].JobType, wantType)
	}
	if wantTarget != "" && jobs[0].TargetID != wantTarget {
		t.Fatalf("job target = %q, want %q", jobs[0].TargetID, wantTarget)
	}
	if err := service.RunJob(ctx, jobs[0]); err != nil {
		t.Fatalf("RunJob(%s) error = %v", jobs[0].JobType, err)
	}
	updated, err := store.GetJob(ctx, jobs[0].ID)
	if err != nil {
		t.Fatalf("GetJob(%s) error = %v", jobs[0].ID, err)
	}
	if updated.Status != automation.JobStatusSucceeded {
		t.Fatalf("job after RunJob = %+v, want succeeded", updated)
	}
}

type combinedProviderStub struct {
	processCalls  int
	extractCalls  int
	generateCalls int
}

type fakeTextEmbeddingProvider struct {
	vector []float32
	inputs []string
}

func (p *fakeTextEmbeddingProvider) EmbedText(ctx context.Context, text string) ([]float32, error) {
	p.inputs = append(p.inputs, text)
	return append([]float32(nil), p.vector...), nil
}

func (p *combinedProviderStub) Name() string {
	return processor.OpenAIProviderName
}

func (p *combinedProviderStub) ProcessRawEvent(ctx context.Context, input processor.EvidenceInput) ([]processor.ProcessedEvidence, error) {
	p.processCalls++
	return []processor.ProcessedEvidence{
		{
			Evidence: processor.EvidenceDraft{
				SourceType:           "user_declared",
				InterpretedStatement: "processor.provider=openai 时 raw_event 自动处理只能调用一次外部模型，同时产出 evidence 和 candidate。",
				Keywords:             []string{"processor.provider", "openai", "raw_event"},
				SalientSpans:         []string{"只能调用一次外部模型"},
				SourceRef:            map[string]any{"producer": "test"},
				Confidence:           0.93,
			},
			Candidates: []processor.MemoryCandidate{
				{
					MemoryType:      memory.TypeConstraint,
					Scope:           memory.ScopeProjectLocal,
					WorkspaceID:     input.RawEvent.WorkspaceID,
					ProjectID:       input.RawEvent.ProjectID,
					SessionID:       input.RawEvent.SessionID,
					TaskID:          input.RawEvent.TaskID,
					Title:           "OpenAI processor 单次调用约束",
					Content:         "processor.provider=openai 时每个 raw_event 自动处理只能调用一次外部模型，并同时产出 evidence 和 memory candidate。",
					Keywords:        []string{"processor.provider", "openai", "单次调用"},
					RetrievalCues:   []string{"OpenAI raw_event 自动处理"},
					Confidence:      0.93,
					Importance:      0.8,
					EncodingDepth:   2,
					CandidateReason: []string{"constraint_declared"},
				},
			},
		},
	}, nil
}

func (p *combinedProviderStub) ExtractEvidence(ctx context.Context, input processor.EvidenceInput) ([]processor.EvidenceDraft, error) {
	p.extractCalls++
	return nil, nil
}

func (p *combinedProviderStub) GenerateCandidates(ctx context.Context, input processor.CandidateInput) ([]processor.MemoryCandidate, error) {
	p.generateCalls++
	return nil, nil
}
