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
		TaskSummary: "推进 P3-C1 automation service",
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
		ContentSummary: "以后推进 P3 时先按详细设计拆分任务，再用测试验证。",
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
	task := capture.AgentTask{ID: "task_checkpoint", SessionID: session.ID, WorkspaceID: session.WorkspaceID, ProjectID: session.ProjectID, TaskSummary: "P3 详细设计复查", OutcomeSummary: "P3 详细设计复查完成。", Status: capture.StatusCompleted, StartedAt: now}
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
		ContentSummary: "P3 详细设计复查完成。",
		SourceRefsJSON: `[{"checkpoint_type":"implementation_design_review","review_intent":["logic_consistency"],"target_docs":[{"path":"doc/The One 长期记忆系统 P3 详细设计.md"}],"conclusion":"supplemented"}]`,
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
		ID:          "job_p4_code_ref",
		JobType:     automation.JobTypeResolveCodeRef,
		TargetType:  automation.TargetTypeMemoryItem,
		TargetID:    "mem_p4_code",
		PayloadJSON: `{"repo_id":"repo_p4","file_path":"internal/memory/service.go","symbol":"Service.Search","content_hash":"sha256:code","resolve_mode":"adapter"}`,
		NextRunAt:   now,
	}); err != nil {
		t.Fatalf("EnqueueJob(job_p4_code_ref) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeResolveCodeRef, "mem_p4_code")
	refs, err := store.ListCodeRefs(ctx, memory.CodeRefQuery{MemoryID: "mem_p4_code"})
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
		ID:          "job_p4_refresh_code_ref",
		JobType:     automation.JobTypeRefreshCodeRefStatus,
		TargetType:  automation.TargetTypeRepo,
		TargetID:    repoRoot,
		PayloadJSON: `{"repo_root":"` + repoRoot + `"}`,
		NextRunAt:   now.Add(time.Second),
	}); err != nil {
		t.Fatalf("EnqueueJob(job_p4_refresh_code_ref) error = %v", err)
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
		ID:          "job_p4_doc_snapshot",
		JobType:     automation.JobTypeBuildDocSnapshot,
		TargetType:  automation.TargetTypeDocPath,
		TargetID:    "doc/P4.md",
		PayloadJSON: `{"workspace_id":"ws_p4","project_id":"proj_p4","repo_id":"repo_p4","content_hash":"sha256:doc","sections":[{"section_id":"8.3","heading_path":["8. Doc Index","8.3 doc_index 数据模型"],"level":3,"start_line":42,"end_line":80,"content_hash":"sha256:section"}]}`,
		NextRunAt:   now,
	}); err != nil {
		t.Fatalf("EnqueueJob(job_p4_doc_snapshot) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeBuildDocSnapshot, "doc/P4.md")
	snapshots, err := store.ListDocSnapshots(ctx, docindex.SnapshotQuery{WorkspaceID: "ws_p4", ProjectID: "proj_p4", RepoID: "repo_p4", Path: "doc/P4.md", IncludeSections: true})
	if err != nil {
		t.Fatalf("ListDocSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].SectionCount != 1 || len(snapshots[0].Sections) != 1 {
		t.Fatalf("snapshots = %+v, want one snapshot with section", snapshots)
	}

	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_p4_embedding",
		JobType:     automation.JobTypeComputeEmbedding,
		TargetType:  automation.TargetTypeMemoryItem,
		TargetID:    "mem_p4_code",
		PayloadJSON: `{"embedding_model":"local_stub","embedding":[0.1,0.2,0.3]}`,
		NextRunAt:   now,
	}); err != nil {
		t.Fatalf("EnqueueJob(job_p4_embedding) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeComputeEmbedding, "mem_p4_code")
	embeddingJob, err := store.GetJob(ctx, "job_p4_embedding")
	if err != nil {
		t.Fatalf("GetJob(job_p4_embedding) error = %v", err)
	}
	if !strings.Contains(embeddingJob.PayloadJSON, `"embedding_dim":3`) {
		t.Fatalf("embedding payload = %s, want embedding_dim=3", embeddingJob.PayloadJSON)
	}

	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:        "mal_old_retrieved",
		MemoryID:  "mem_p4_code",
		EventType: "retrieved",
		CreatedAt: now.AddDate(0, 0, -31),
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog(retrieved) error = %v", err)
	}
	if _, err := store.WriteMemoryAccessLog(ctx, retrieval.AccessLogRecord{
		ID:            "mal_old_injected",
		MemoryID:      "mem_p4_code",
		EventType:     "injected",
		UsedInContext: true,
		CreatedAt:     now.AddDate(0, 0, -181),
	}); err != nil {
		t.Fatalf("WriteMemoryAccessLog(injected) error = %v", err)
	}
	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_p4_cleanup",
		JobType:     automation.JobTypeCleanupAccessLog,
		TargetType:  automation.TargetTypeWorkspace,
		TargetID:    "ws_p4",
		PayloadJSON: `{"now":"2026-05-24T12:00:00Z"}`,
		NextRunAt:   now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("EnqueueJob(cleanup) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeCleanupAccessLog, "ws_p4")
	logs, err := store.ListMemoryAccessLogs(ctx, retrieval.AccessLogQuery{MemoryID: "mem_p4_code"})
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
	if err := os.WriteFile(filepath.Join(repoRoot, "design.md"), []byte("# Design\n\n## Scope\nP4-C4 review strategy\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	if _, _, err := store.EnqueueJob(ctx, automation.AsyncJob{
		ID:          "job_p4_doc_snapshot_build",
		JobType:     automation.JobTypeBuildDocSnapshot,
		TargetType:  automation.TargetTypeDocPath,
		TargetID:    "design.md",
		PayloadJSON: `{"workspace_id":"ws_p4_build","project_id":"proj_p4","repo_id":"` + repoRoot + `"}`,
		NextRunAt:   time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("EnqueueJob(job_p4_doc_snapshot_build) error = %v", err)
	}
	runNextJob(t, ctx, store, service, automation.JobTypeBuildDocSnapshot, "design.md")
	snapshots, err := store.ListDocSnapshots(ctx, docindex.SnapshotQuery{WorkspaceID: "ws_p4_build", ProjectID: "proj_p4", RepoID: repoRoot, Path: "design.md", IncludeSections: true})
	if err != nil {
		t.Fatalf("ListDocSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].ContentHash == "" || snapshots[0].SectionCount != 2 {
		t.Fatalf("snapshots = %+v, want computed markdown snapshot", snapshots)
	}
	if strings.Contains(snapshots[0].Sections[1].Summary, "P4-C4 review strategy") {
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
