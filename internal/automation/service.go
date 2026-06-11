package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zaneway/theone/internal/capture"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/ingest"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
	"github.com/zaneway/theone/internal/retention"
	"github.com/zaneway/theone/internal/retrieval"
)

const (
	defaultMaxRetries = 3
)

// Repository 定义 automation service 依赖的异步任务、事件、证据和自动写入能力。
type Repository interface {
	EnqueueJob(ctx context.Context, job AsyncJob) (AsyncJob, bool, error)
	ClaimJobs(ctx context.Context, now time.Time, limit int) ([]AsyncJob, error)
	RecoverStaleRunningJobs(ctx context.Context, now time.Time, timeout time.Duration) (int, error)
	MarkJobSucceeded(ctx context.Context, jobID string, payload string, now time.Time) error
	MarkJobRetry(ctx context.Context, jobID string, retryCount int, nextRunAt time.Time, lastError string, now time.Time) error
	MarkJobFailed(ctx context.Context, jobID string, lastError string, now time.Time) error
	GetJob(ctx context.Context, jobID string) (AsyncJob, error)
	ListJobs(ctx context.Context, req ListJobsRequest) ([]AsyncJob, error)

	GetRawEvent(ctx context.Context, rawEventID string) (capture.RawEvent, error)
	GetSession(ctx context.Context, sessionID string) (capture.AgentSession, error)
	GetTask(ctx context.Context, taskID string) (capture.AgentTask, error)
	FindDuplicateEvidence(ctx context.Context, draft EvidenceDraftKey) (memory.Evidence, bool, error)
	WriteEvidence(ctx context.Context, evidence memory.Evidence) error
	GetEvidence(ctx context.Context, evidenceID string) (memory.Evidence, error)
	WriteCandidate(ctx context.Context, candidate MemoryCandidateRecord) error
	GetCandidate(ctx context.Context, candidateID string) (MemoryCandidateRecord, error)
	ListCandidates(ctx context.Context, req ListCandidatesRequest) ([]MemoryCandidateRecord, error)
	UpdateCandidateAdmission(ctx context.Context, candidateID string, admission AdmissionResult, status string, memoryID string) error
	FindRelatedMemory(ctx context.Context, req RelatedMemoryRequest) ([]memory.MemoryItem, error)
	WriteAutomatedMemory(ctx context.Context, input AutomatedMemoryWrite) (memory.MemoryItem, error)
	OverwriteMemoryWithCorrection(ctx context.Context, input AutomatedMemoryCorrection) (memory.MemoryItem, error)
	ResolveCorrectionTargetMemory(ctx context.Context, req CorrectionTargetRequest) (memory.MemoryItem, bool, error)
	WriteMemoryRelation(ctx context.Context, relation memory.MemoryRelation) error
	ArchiveMemoryForSupersedes(ctx context.Context, memoryID string, now time.Time) error
	UpdateMemorySupersedesID(ctx context.Context, memoryID, supersedesID string, now time.Time) error
	ListOrphanRawEvents(ctx context.Context, req OrphanRawEventRequest) ([]capture.RawEvent, error)

	WriteMemoryAccessLog(ctx context.Context, record retrieval.AccessLogRecord) (retrieval.AccessLogRecord, error)
	ListMemoryAccessLogs(ctx context.Context, query retrieval.AccessLogQuery) ([]retrieval.AccessLogRecord, error)

	ListExpiredTemporaryMemories(ctx context.Context, req retention.ListRequest) ([]retention.MemoryRecord, error)
	ListAccessEvents(ctx context.Context, memoryIDs []string) (map[string][]retention.AccessFeedbackEvent, error)
	AggregateRelationSignals(ctx context.Context, memoryIDs []string) (map[string]retention.RelationSignals, error)
	CountStaleCodeRefs(ctx context.Context, memoryIDs []string) (map[string]int, error)
	ArchiveTemporaryMemory(ctx context.Context, memoryID string, now time.Time) error
	ListMemoriesForScoreRecalc(ctx context.Context, req retention.ListRequest) ([]retention.MemoryRecord, error)
	UpdateRetentionFields(ctx context.Context, memoryID string, update retention.ScoreUpdate) error
	DeleteInvalidMemory(ctx context.Context, memoryID string, now time.Time, reason string) error
}

// Service 编排自动记忆 job 链路。
// Provider 只负责生成 draft/candidate，最终写入和 Admission 均在该服务中统一执行。
type Service struct {
	cfg        config.Config
	repo       Repository
	provider   processor.Provider
	admission  AdmissionController
	dispatcher JobDispatcher
	logger     *slog.Logger
}

// NewService 创建自动记忆服务。provider 为空时使用 rule_based，保证默认本地可运行。
func NewService(cfg config.Config, repo Repository, provider processor.Provider) *Service {
	if provider == nil {
		defaultProvider := processor.NewRuleBasedProvider()
		provider = defaultProvider
	}
	service := &Service{
		cfg:       cfg,
		repo:      repo,
		provider:  provider,
		admission: NewAdmissionController(),
		logger:    slog.Default(),
	}
	service.dispatcher = NewJobDispatcher(
		p3JobHandler{service: service},
		newExtendedJobHandler(cfg, repo),
	)
	return service
}

// EnqueueRawEvent 为 raw_event 创建 evidence 抽取任务。
// capture service 在 raw_event 写入成功后调用此方法，触发自动记忆处理管道。
func (s *Service) EnqueueRawEvent(ctx context.Context, rawEvent capture.RawEvent) error {
	if rawEvent.ID == "" {
		return fmt.Errorf("VALIDATION_FAILED: raw_event id is required")
	}
	jobID, err := idgen.New("job")
	if err != nil {
		s.logger.Error("enqueue raw event job id generation failed", "error", err)
		return err
	}
	_, enqueued, err := s.repo.EnqueueJob(ctx, AsyncJob{
		ID:         jobID,
		JobType:    JobTypeExtractEvidence,
		TargetType: TargetTypeRawEvent,
		TargetID:   rawEvent.ID,
		Priority:   3,
		MaxRetries: defaultMaxRetries,
		DedupKey:   JobTypeExtractEvidence + ":" + rawEvent.ID,
	})
	if err != nil {
		s.logger.Error("enqueue raw event failed", "raw_event_id", rawEvent.ID, "error", err)
		return err
	}
	s.logger.Debug("enqueue raw event succeeded",
		"job_id", jobID,
		"raw_event_id", rawEvent.ID,
		"enqueued", enqueued,
	)
	return err
}

// RunJob 执行单个已领取 job，并负责把结果状态回写为 succeeded 或 failed。
// 处理流程：通过 JobDispatcher 分发到对应 handler → 成功则 MarkJobSucceeded，失败则 MarkJobFailed。
// 设计约束：Provider 执行发生在 claim 事务之外，避免长时间持锁。
func (s *Service) RunJob(ctx context.Context, job AsyncJob) error {
	now := time.Now()
	s.logger.Info("job started",
		"job_id", job.ID,
		"job_type", job.JobType,
		"target_type", job.TargetType,
		"target_id", job.TargetID,
		"retry_count", job.RetryCount,
	)
	payload, err := s.dispatcher.RunJob(ctx, job)
	if err != nil {
		s.logger.Error("job failed",
			"job_id", job.ID,
			"job_type", job.JobType,
			"error", err,
		)
		_ = s.repo.MarkJobFailed(ctx, job.ID, err.Error(), now)
		return err
	}
	payloadJSON, err := jsonText(payload)
	if err != nil {
		s.logger.Error("job payload marshal failed", "job_id", job.ID, "error", err)
		_ = s.repo.MarkJobFailed(ctx, job.ID, err.Error(), now)
		return err
	}
	s.logger.Info("job succeeded",
		"job_id", job.ID,
		"job_type", job.JobType,
	)
	return s.repo.MarkJobSucceeded(ctx, job.ID, payloadJSON, now)
}

// runExtractEvidence 执行管道第一步：从 raw_event 抽取 evidence。
// 处理流程：
//  1. 加载原始事件及其关联的 session 和 task
//  2. 调用 Provider 抽取 evidence drafts
//  4. 将 drafts 物化为 evidence 记录（去重检测 + 序列化 + 写入）
//  5. 为每条 evidence 入队下一步 job（generate_memory_candidate）
func (s *Service) runExtractEvidence(ctx context.Context, job AsyncJob) (map[string]any, error) {
	rawEvent, err := s.repo.GetRawEvent(ctx, job.TargetID)
	if err != nil {
		s.logger.Error("extract evidence get raw event failed", "job_id", job.ID, "target_id", job.TargetID, "error", err)
		return nil, err
	}
	s.logger.Debug("extract evidence loaded raw event",
		"job_id", job.ID,
		"event_type", rawEvent.EventType,
		"session_id", rawEvent.SessionID,
	)
	session, err := s.loadSession(ctx, rawEvent.SessionID)
	if err != nil {
		return nil, err
	}
	task, err := s.loadTask(ctx, rawEvent.TaskID)
	if err != nil {
		return nil, err
	}
	if provider, ok := s.provider.(processor.RawEventProcessor); ok {
		return s.runProcessRawEvent(ctx, job, provider, rawEvent, session, task)
	}
	drafts, err := s.provider.ExtractEvidence(processor.WithLogContext(ctx, processor.LogContext{JobID: job.ID}), processor.EvidenceInput{
		RawEvent: rawEvent,
		Session:  session,
		Task:     task,
		Now:      time.Now(),
	})
	if err != nil {
		s.logger.Error("extract evidence provider failed",
			"job_id", job.ID,
			"raw_event_id", rawEvent.ID,
			"event_type", rawEvent.EventType,
			"session_id", rawEvent.SessionID,
			"task_id", rawEvent.TaskID,
			"error", err,
		)
		return nil, err
	}
	s.logger.Debug("extract evidence provider returned",
		"job_id", job.ID,
		"draft_count", len(drafts),
	)
	written := 0
	for _, draft := range drafts {
		evidence, err := s.materializeEvidence(ctx, rawEvent.ID, draft)
		if err != nil {
			s.logger.Error("extract evidence materialize failed", "job_id", job.ID, "error", err)
			return nil, err
		}
		written++
		if err := s.enqueueNext(ctx, JobTypeGenerateMemoryCandidate, TargetTypeEvidence, evidence.ID, 4); err != nil {
			s.logger.Error("extract evidence enqueue next failed", "job_id", job.ID, "evidence_id", evidence.ID, "error", err)
			return nil, err
		}
	}
	s.logger.Info("extract evidence completed",
		"job_id", job.ID,
		"event_type", rawEvent.EventType,
		"draft_count", len(drafts),
		"written_count", written,
	)
	return map[string]any{"evidence_count": written}, nil
}

// runProcessRawEvent 执行联合处理路径：一次 Provider 调用同时得到 evidence 与 candidate。
// 该路径用于 openai provider；持久化仍然写既有 evidence / memory_candidate 表，
// 后续准入仍通过 compute_admission job 完成。
func (s *Service) runProcessRawEvent(ctx context.Context, job AsyncJob, provider processor.RawEventProcessor, rawEvent capture.RawEvent, session capture.AgentSession, task capture.AgentTask) (map[string]any, error) {
	processed, err := provider.ProcessRawEvent(processor.WithLogContext(ctx, processor.LogContext{JobID: job.ID}), processor.EvidenceInput{
		RawEvent: rawEvent,
		Session:  session,
		Task:     task,
		Now:      time.Now(),
	})
	if err != nil {
		s.logger.Error("process raw event provider failed",
			"job_id", job.ID,
			"raw_event_id", rawEvent.ID,
			"event_type", rawEvent.EventType,
			"session_id", rawEvent.SessionID,
			"task_id", rawEvent.TaskID,
			"error", err,
		)
		return nil, err
	}
	evidenceWritten := 0
	candidatesWritten := 0
	for _, item := range processed {
		evidence, err := s.materializeEvidence(ctx, rawEvent.ID, item.Evidence)
		if err != nil {
			s.logger.Error("process raw event evidence materialize failed", "job_id", job.ID, "error", err)
			return nil, err
		}
		evidenceWritten++
		for _, candidate := range item.Candidates {
			candidate.SourceEvidenceIDs = []string{evidence.ID}
			record, err := s.materializeCandidate(candidate, evidence, rawEvent)
			if err != nil {
				s.logger.Error("process raw event candidate materialize failed", "job_id", job.ID, "error", err)
				return nil, err
			}
			if err := s.repo.WriteCandidate(ctx, record); err != nil {
				s.logger.Error("process raw event candidate write failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
				return nil, err
			}
			candidatesWritten++
			if err := s.enqueueNext(ctx, JobTypeComputeAdmission, TargetTypeMemoryCandidate, record.ID, 5); err != nil {
				s.logger.Error("process raw event enqueue admission failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
				return nil, err
			}
		}
	}
	s.logger.Info("process raw event completed",
		"job_id", job.ID,
		"event_type", rawEvent.EventType,
		"evidence_count", evidenceWritten,
		"candidate_count", candidatesWritten,
	)
	return map[string]any{"evidence_count": evidenceWritten, "candidate_count": candidatesWritten}, nil
}

// runGenerateMemoryCandidate 执行管道第二步：从 evidence 生成候选记忆。
// 处理流程：
//  1. 加载 evidence 及其关联的 raw_event
//  2. 查找同 scope 的相关已有记忆（rule_based 用于重复失败检测）
//  3. 加载 session 和 task 上下文（rule_based 与候选 lineage 回填）
//  4. 调用 Provider 生成 memory candidates
//  5. 将 candidates 物化为 MemoryCandidateRecord（序列化所有 JSON 字段）
//  6. 为每个 candidate 入队下一步 job（compute_admission）
func (s *Service) runGenerateMemoryCandidate(ctx context.Context, job AsyncJob) (map[string]any, error) {
	if _, ok := s.provider.(processor.RawEventProcessor); ok {
		s.logger.Info("generate candidate skipped for combined provider",
			"job_id", job.ID,
			"target_id", job.TargetID,
			"provider", s.provider.Name(),
		)
		return map[string]any{"candidate_count": 0, "skipped": "combined_provider"}, nil
	}
	evidence, err := s.repo.GetEvidence(ctx, job.TargetID)
	if err != nil {
		s.logger.Error("generate candidate get evidence failed", "job_id", job.ID, "target_id", job.TargetID, "error", err)
		return nil, err
	}
	rawEvent, err := s.repo.GetRawEvent(ctx, evidence.RawEventID)
	if err != nil {
		s.logger.Error("generate candidate get raw event failed", "job_id", job.ID, "evidence_id", evidence.ID, "error", err)
		return nil, err
	}
	s.logger.Debug("generate candidate loaded context",
		"job_id", job.ID,
		"evidence_id", evidence.ID,
		"source_type", evidence.SourceType,
		"event_type", rawEvent.EventType,
	)
	related, err := s.repo.FindRelatedMemory(ctx, RelatedMemoryRequest{
		WorkspaceID: rawEvent.WorkspaceID,
		ProjectID:   rawEvent.ProjectID,
		RepoID:      rawEvent.RepoID,
		Query:       evidence.InterpretedStatement,
		Limit:       10,
	})
	if err != nil {
		return nil, err
	}
	session, err := s.loadSession(ctx, rawEvent.SessionID)
	if err != nil {
		return nil, err
	}
	task, err := s.loadTask(ctx, rawEvent.TaskID)
	if err != nil {
		return nil, err
	}
	candidates, err := s.provider.GenerateCandidates(processor.WithLogContext(ctx, processor.LogContext{JobID: job.ID}), processor.CandidateInput{
		Evidence:      evidence,
		RawEvent:      rawEvent,
		Session:       session,
		Task:          task,
		RelatedMemory: related,
		Now:           time.Now(),
	})
	if err != nil {
		s.logger.Error("generate candidate provider failed",
			"job_id", job.ID,
			"evidence_id", evidence.ID,
			"raw_event_id", rawEvent.ID,
			"event_type", rawEvent.EventType,
			"error", err,
		)
		return nil, err
	}
	s.logger.Debug("generate candidate provider returned",
		"job_id", job.ID,
		"candidate_count", len(candidates),
	)
	written := 0
	for _, candidate := range candidates {
		record, err := s.materializeCandidate(candidate, evidence, rawEvent)
		if err != nil {
			s.logger.Error("generate candidate materialize failed", "job_id", job.ID, "error", err)
			return nil, err
		}
		if err := s.repo.WriteCandidate(ctx, record); err != nil {
			s.logger.Error("generate candidate write failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
			return nil, err
		}
		written++
		if err := s.enqueueNext(ctx, JobTypeComputeAdmission, TargetTypeMemoryCandidate, record.ID, 5); err != nil {
			s.logger.Error("generate candidate enqueue next failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
			return nil, err
		}
	}
	s.logger.Info("generate candidate completed",
		"job_id", job.ID,
		"evidence_id", evidence.ID,
		"candidate_count", len(candidates),
		"written_count", written,
	)
	return map[string]any{"candidate_count": written}, nil
}

// runComputeAdmission 执行管道第三步：对候选记忆执行准入控制。
// 处理流程：
//  1. 加载 candidate 记录和关联的 evidence
//  2. 查找同 scope 的相关已有记忆（用于冲突检测）
//  3. 调用 AdmissionController.Decide 计算准入决策
//  4. 根据决策结果：
//     - drop/write_raw_only → 更新 candidate 状态为 dropped
//     - write_temporary/provisional/pending_review/stable → 构建 memory_item 并写入
//  5. 写入时如果是用户纠正（user_correction），执行原地覆盖语义
//  6. 更新 candidate 的 admission 结果和 resulting_memory_id
func (s *Service) runComputeAdmission(ctx context.Context, job AsyncJob) (map[string]any, error) {
	record, err := s.repo.GetCandidate(ctx, job.TargetID)
	if err != nil {
		s.logger.Error("compute admission get candidate failed", "job_id", job.ID, "target_id", job.TargetID, "error", err)
		return nil, err
	}
	evidence, err := s.repo.GetEvidence(ctx, record.EvidenceID)
	if err != nil {
		s.logger.Error("compute admission get evidence failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
		return nil, err
	}
	rawEvent, err := s.repo.GetRawEvent(ctx, evidence.RawEventID)
	if err != nil {
		s.logger.Error("compute admission get raw event failed", "job_id", job.ID, "candidate_id", record.ID, "raw_event_id", evidence.RawEventID, "error", err)
		return nil, err
	}
	candidate := candidateFromRecord(record, evidence.SourceType)
	s.logger.Debug("compute admission loaded candidate",
		"job_id", job.ID,
		"candidate_id", record.ID,
		"memory_type", candidate.MemoryType,
		"scope", candidate.Scope,
	)
	related, err := s.repo.FindRelatedMemory(ctx, RelatedMemoryRequest{
		WorkspaceID: candidate.WorkspaceID,
		ProjectID:   candidate.ProjectID,
		RepoID:      candidate.RepoID,
		Scope:       candidate.Scope,
		MemoryType:  candidate.MemoryType,
		Query:       candidate.Content,
		Limit:       10,
	})
	if err != nil {
		s.logger.Error("compute admission find related memory failed", "job_id", job.ID, "error", err)
		return nil, err
	}
	admission := s.admission.Decide(AdmissionInput{Candidate: candidate, RelatedMemory: related})
	s.logger.Info("compute admission decided",
		"job_id", job.ID,
		"candidate_id", record.ID,
		"decision", admission.Decision,
		"admission_score", admission.AdmissionScore,
		"memory_type", candidate.MemoryType,
		"scope", candidate.Scope,
	)
	switch admission.Decision {
	case DecisionDrop, DecisionWriteRawOnly:
		if err := s.repo.UpdateCandidateAdmission(ctx, record.ID, admission, CandidateStatusDropped, ""); err != nil {
			s.logger.Error("compute admission update dropped failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
			return nil, err
		}
		return map[string]any{"admission_decision": admission.Decision}, nil
	case DecisionWriteTemporary, DecisionWriteProvisional, DecisionWritePendingReview, DecisionWriteStable:
		item, err := s.memoryItemFromAdmission(record, candidate, admission, evidence)
		if err != nil {
			s.logger.Error("compute admission build memory item failed", "job_id", job.ID, "error", err)
			return nil, err
		}
		written, err := s.writeAdmittedMemory(ctx, record, candidate, admission, evidence, rawEvent, item, related)
		if err != nil {
			s.logger.Error("compute admission write memory failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
			return nil, err
		}
		if err := s.applyCorrectionSupersedes(ctx, written, candidate, evidence, related); err != nil {
			s.logger.Error("compute admission apply correction failed", "job_id", job.ID, "memory_id", written.ID, "error", err)
			return nil, err
		}
		if err := s.repo.UpdateCandidateAdmission(ctx, record.ID, admission, CandidateStatusAdmitted, written.ID); err != nil {
			s.logger.Error("compute admission update admitted failed", "job_id", job.ID, "candidate_id", record.ID, "error", err)
			return nil, err
		}
		return map[string]any{"admission_decision": admission.Decision, "memory_id": written.ID}, nil
	default:
		if err := s.repo.UpdateCandidateAdmission(ctx, record.ID, admission, CandidateStatusDropped, ""); err != nil {
			return nil, err
		}
		return map[string]any{"admission_decision": admission.Decision}, nil
	}
}

// writeAdmittedMemory 写入通过准入控制的记忆。
// 特殊处理：如果候选记忆是用户纠正（user_correction），执行原地覆盖语义——
// 保留旧 memory_id，更新内容和检索字段，并追加新 evidence/review 轨迹。
// 非纠正场景：写入新的 memory_item + evidence 关联 + 可选的 review_checkpoint。
func (s *Service) writeAdmittedMemory(ctx context.Context, record MemoryCandidateRecord, candidate processor.MemoryCandidate, admission AdmissionResult, evidence memory.Evidence, rawEvent capture.RawEvent, item memory.MemoryItem, related []memory.MemoryItem) (memory.MemoryItem, error) {
	if isUserCorrection(candidate, evidence) {
		target, found, err := s.resolveCorrectionTarget(ctx, evidence)
		if err != nil {
			return memory.MemoryItem{}, err
		}
		if found {
			item.ID = target.ID
			item.Scope = target.Scope
			item.WorkspaceID = target.WorkspaceID
			item.UserID = target.UserID
			item.ProjectID = target.ProjectID
			item.RepoID = target.RepoID
			item.SessionID = target.SessionID
			item.TaskID = target.TaskID
			return s.repo.OverwriteMemoryWithCorrection(ctx, AutomatedMemoryCorrection{
				TargetMemoryID:   target.ID,
				Item:             item,
				EvidenceIDs:      candidate.SourceEvidenceIDs,
				EvidenceRelation: "corrected_by",
				ReviewFeedback:   "automated user correction",
			})
		}
	}
	checkpoint, err := reviewCheckpointFromRecord(record, item)
	if err != nil {
		return memory.MemoryItem{}, err
	}
	written, err := s.repo.WriteAutomatedMemory(ctx, AutomatedMemoryWrite{
		Item:             item,
		EvidenceIDs:      candidate.SourceEvidenceIDs,
		ReviewCheckpoint: checkpoint,
		Provenance:       buildMemoryProvenance(record, evidence, rawEvent, admission),
	})
	return written, err
}

// resolveCorrectionTarget 从 evidence 的 source_ref 中解析纠正目标 memory。
// 纠正场景：用户通过 user.correction 事件声明某条记忆需要修正，
// source_ref 中携带 target_memory_id 或 target_event_id，用于定位被纠正的旧记忆。
func (s *Service) resolveCorrectionTarget(ctx context.Context, evidence memory.Evidence) (memory.MemoryItem, bool, error) {
	ref := decodeObject(evidence.SourceRefJSON)
	req := CorrectionTargetRequest{
		TargetMemoryID: stringValue(ref, "target_memory_id"),
		TargetEventID:  stringValue(ref, "target_event_id"),
	}
	if req.TargetMemoryID == "" && req.TargetEventID == "" {
		return memory.MemoryItem{}, false, nil
	}
	return s.repo.ResolveCorrectionTargetMemory(ctx, req)
}

func isUserCorrection(candidate processor.MemoryCandidate, evidence memory.Evidence) bool {
	return evidence.SourceType == "user_confirmed" || candidate.SourceType == "user_confirmed" || hasString(candidate.CandidateReason, "user_correction")
}

func reviewCheckpointFromRecord(record MemoryCandidateRecord, item memory.MemoryItem) (*memory.ReviewCheckpoint, error) {
	if strings.TrimSpace(record.ReviewCheckpointJSON) == "" {
		return nil, nil
	}
	var draft processor.ReviewCheckpointDraft
	if err := json.Unmarshal([]byte(record.ReviewCheckpointJSON), &draft); err != nil {
		return nil, fmt.Errorf("VALIDATION_FAILED: invalid review_checkpoint_json: %w", err)
	}
	checkpointID, err := idgen.New("rcp")
	if err != nil {
		return nil, err
	}
	reviewIntent, err := jsonText(draft.ReviewIntent)
	if err != nil {
		return nil, err
	}
	targetDocs, err := jsonText(draft.TargetDocs)
	if err != nil {
		return nil, err
	}
	targetSections, err := jsonText(draft.TargetSections)
	if err != nil {
		return nil, err
	}
	targetHashes, err := jsonText(draft.TargetHashes)
	if err != nil {
		return nil, err
	}
	confirmedBaseline, err := jsonText(draft.ConfirmedBaseline)
	if err != nil {
		return nil, err
	}
	ignoredItems, err := jsonText(draft.IgnoredItems)
	if err != nil {
		return nil, err
	}
	deferredItems, err := jsonText(draft.DeferredItems)
	if err != nil {
		return nil, err
	}
	openItems, err := jsonText(draft.OpenItems)
	if err != nil {
		return nil, err
	}
	nextPolicy, err := jsonText(draft.NextReviewPolicy)
	if err != nil {
		return nil, err
	}
	return &memory.ReviewCheckpoint{
		ID:                    checkpointID,
		MemoryID:              item.ID,
		WorkspaceID:           item.WorkspaceID,
		ProjectID:             item.ProjectID,
		RepoID:                item.RepoID,
		SessionID:             item.SessionID,
		TaskID:                item.TaskID,
		CheckpointType:        draft.CheckpointType,
		ReviewIntentJSON:      reviewIntent,
		TargetDocsJSON:        targetDocs,
		TargetSectionsJSON:    targetSections,
		TargetHashesJSON:      targetHashes,
		Conclusion:            draft.Conclusion,
		ConfirmedBaselineJSON: confirmedBaseline,
		IgnoredItemsJSON:      ignoredItems,
		DeferredItemsJSON:     deferredItems,
		OpenItemsJSON:         openItems,
		NextReviewPolicyJSON:  nextPolicy,
	}, nil
}

// materializeEvidence 将 Provider 生成的 EvidenceDraft 物化为持久化的 Evidence 记录。
// 处理流程：校验必填字段 → 幂等检测（按 rawEventID + sourceType + interpretedStatement 去重）→
// 生成 evidence_id → 序列化 JSON 字段 → 写入 repository。
func (s *Service) materializeEvidence(ctx context.Context, rawEventID string, draft processor.EvidenceDraft) (memory.Evidence, error) {
	if strings.TrimSpace(draft.SourceType) == "" || strings.TrimSpace(draft.InterpretedStatement) == "" {
		return memory.Evidence{}, fmt.Errorf("VALIDATION_FAILED: evidence source_type and interpreted_statement are required")
	}
	key := EvidenceDraftKey{RawEventID: rawEventID, SourceType: draft.SourceType, InterpretedStatement: draft.InterpretedStatement}
	if existing, found, err := s.repo.FindDuplicateEvidence(ctx, key); err != nil {
		return memory.Evidence{}, err
	} else if found {
		return existing, nil
	}
	evidenceID, err := idgen.New("ev")
	if err != nil {
		return memory.Evidence{}, err
	}
	keywordsJSON, err := jsonText(draft.Keywords)
	if err != nil {
		return memory.Evidence{}, err
	}
	spansJSON, err := jsonText(draft.SalientSpans)
	if err != nil {
		return memory.Evidence{}, err
	}
	sourceRefJSON, err := jsonText(draft.SourceRef)
	if err != nil {
		return memory.Evidence{}, err
	}
	evidence := memory.Evidence{
		ID:                   evidenceID,
		RawEventID:           rawEventID,
		SourceType:           draft.SourceType,
		InterpretedStatement: draft.InterpretedStatement,
		KeywordsJSON:         keywordsJSON,
		SalientSpansJSON:     spansJSON,
		SourceRefJSON:        sourceRefJSON,
		Confidence:           defaultFloat(draft.Confidence, 0.7),
		CreatedAt:            time.Now(),
	}
	if err := s.repo.WriteEvidence(ctx, evidence); err != nil {
		return memory.Evidence{}, err
	}
	return evidence, nil
}

// materializeCandidate 将 Provider 生成的 MemoryCandidate 物化为 MemoryCandidateRecord。
// 处理流程：生成 candidate_id → 序列化所有 JSON 字段（keywords、entities、tags 等）→
// 填充默认值（confidence、importance、encoding_depth）→ 构建 dedup_key → 返回记录。
// dedup_key 由 provider + evidence_id + memory_type + scope + content 拼接，用于幂等检测。
func (s *Service) materializeCandidate(candidate processor.MemoryCandidate, evidence memory.Evidence, rawEvent capture.RawEvent) (MemoryCandidateRecord, error) {
	candidateID := candidate.CandidateID
	if candidateID == "" {
		generated, err := idgen.New("cand")
		if err != nil {
			return MemoryCandidateRecord{}, err
		}
		candidateID = generated
	}
	keywordsJSON, err := jsonText(candidate.Keywords)
	if err != nil {
		return MemoryCandidateRecord{}, err
	}
	entitiesJSON, err := jsonText(candidate.Entities)
	if err != nil {
		return MemoryCandidateRecord{}, err
	}
	cuesJSON, err := jsonText(candidate.RetrievalCues)
	if err != nil {
		return MemoryCandidateRecord{}, err
	}
	tagsJSON, err := jsonText(candidate.Tags)
	if err != nil {
		return MemoryCandidateRecord{}, err
	}
	evidenceIDsJSON, err := jsonText(candidate.SourceEvidenceIDs)
	if err != nil {
		return MemoryCandidateRecord{}, err
	}
	reasonsJSON, err := jsonText(candidate.CandidateReason)
	if err != nil {
		return MemoryCandidateRecord{}, err
	}
	checkpointJSON := ""
	if candidate.ReviewCheckpoint != nil {
		checkpointJSON, err = jsonText(candidate.ReviewCheckpoint)
		if err != nil {
			return MemoryCandidateRecord{}, err
		}
	}
	return MemoryCandidateRecord{
		ID:                    candidateID,
		RawEventID:            rawEvent.ID,
		EvidenceID:            evidence.ID,
		Provider:              s.provider.Name(),
		MemoryType:            candidate.MemoryType,
		Scope:                 candidate.Scope,
		WorkspaceID:           candidate.WorkspaceID,
		UserID:                firstNonEmpty(candidate.UserID, defaultUserForScope(s.cfg, candidate.Scope)),
		ProjectID:             candidate.ProjectID,
		RepoID:                candidate.RepoID,
		SessionID:             candidate.SessionID,
		TaskID:                candidate.TaskID,
		Title:                 candidate.Title,
		Content:               candidate.Content,
		KeywordsJSON:          keywordsJSON,
		EntitiesJSON:          entitiesJSON,
		RetrievalCuesJSON:     cuesJSON,
		TagsJSON:              tagsJSON,
		SourceEvidenceIDsJSON: evidenceIDsJSON,
		ReviewCheckpointJSON:  checkpointJSON,
		Confidence:            defaultFloat(candidate.Confidence, evidence.Confidence),
		Importance:            defaultFloat(candidate.Importance, 0.5),
		EncodingDepth:         defaultInt(candidate.EncodingDepth, 2),
		EventScore:            defaultFloat(candidate.EventScore, 0),
		CandidateReasonJSON:   reasonsJSON,
		Status:                CandidateStatusGenerated,
		DedupKey:              strings.Join([]string{s.provider.Name(), evidence.ID, candidate.MemoryType, candidate.Scope, candidate.Content}, ":"),
	}, nil
}

// memoryItemFromAdmission 根据准入结果构建 MemoryItem 领域对象。
// 将 candidate record 的字段映射到 memory_item 结构，并设置准入决定的 state、tier、decay_rate 和 retention_score。
func (s *Service) memoryItemFromAdmission(record MemoryCandidateRecord, candidate processor.MemoryCandidate, admission AdmissionResult, evidence memory.Evidence) (memory.MemoryItem, error) {
	memoryID, err := idgen.New("mem")
	if err != nil {
		return memory.MemoryItem{}, err
	}
	now := time.Now()
	searchText := ingest.BuildSearchText(ingest.SearchTextInput{
		Title:             record.Title,
		Content:           record.Content,
		NormalizedContent: record.Content,
		Keywords:          candidate.Keywords,
		Tags:              candidate.Tags,
		RetrievalCues:     candidate.RetrievalCues,
		Entities:          candidate.Entities,
	})
	return memory.MemoryItem{
		ID:                memoryID,
		Scope:             record.Scope,
		WorkspaceID:       record.WorkspaceID,
		UserID:            record.UserID,
		ProjectID:         record.ProjectID,
		RepoID:            record.RepoID,
		SessionID:         record.SessionID,
		TaskID:            record.TaskID,
		MemoryType:        record.MemoryType,
		SourceType:        evidence.SourceType,
		CreatedBy:         "automation:" + s.provider.Name(),
		SourceQuality:     defaultFloat(evidence.Confidence, 0.7),
		Title:             record.Title,
		Content:           record.Content,
		NormalizedContent: record.Content,
		SearchText:        searchText,
		KeywordsJSON:      record.KeywordsJSON,
		EntitiesJSON:      record.EntitiesJSON,
		RetrievalCuesJSON: record.RetrievalCuesJSON,
		TagsJSON:          record.TagsJSON,
		State:             admission.InitialState,
		Confidence:        defaultFloat(record.Confidence, evidence.Confidence),
		Importance:        defaultFloat(record.Importance, 0.5),
		EncodingDepth:     defaultInt(record.EncodingDepth, 2),
		DecayRate:         defaultFloat(admission.DecayRate, defaultDecayRate(record.MemoryType)),
		RetentionScore:    admission.AdmissionScore,
		Tier:              admission.InitialTier,
		CreatedAt:         now,
		UpdatedAt:         now,
		UserConfirmed:     admission.UserConfirmed,
		Version:           1,
	}, nil
}

func candidateFromRecord(record MemoryCandidateRecord, sourceType string) processor.MemoryCandidate {
	return processor.MemoryCandidate{
		CandidateID:       record.ID,
		MemoryType:        record.MemoryType,
		Scope:             record.Scope,
		WorkspaceID:       record.WorkspaceID,
		UserID:            record.UserID,
		ProjectID:         record.ProjectID,
		RepoID:            record.RepoID,
		SessionID:         record.SessionID,
		TaskID:            record.TaskID,
		SourceType:        sourceType,
		Title:             record.Title,
		Content:           record.Content,
		Keywords:          decodeStringSlice(record.KeywordsJSON),
		Entities:          decodeStringSlice(record.EntitiesJSON),
		RetrievalCues:     decodeStringSlice(record.RetrievalCuesJSON),
		Tags:              decodeStringSlice(record.TagsJSON),
		Confidence:        record.Confidence,
		Importance:        record.Importance,
		EncodingDepth:     record.EncodingDepth,
		EventScore:        record.EventScore,
		CandidateReason:   decodeStringSlice(record.CandidateReasonJSON),
		SourceEvidenceIDs: decodeStringSlice(record.SourceEvidenceIDsJSON),
	}
}

// enqueueNext 为当前管道步骤的输出入队下一步 job。
// 用于管道衔接：extract_evidence → generate_memory_candidate → compute_admission。
// dedup_key 由 jobType + targetID 拼接，确保同一 target 不会重复入队。
func (s *Service) enqueueNext(ctx context.Context, jobType, targetType, targetID string, priority int) error {
	jobID, err := idgen.New("job")
	if err != nil {
		return err
	}
	_, _, err = s.repo.EnqueueJob(ctx, AsyncJob{
		ID:         jobID,
		JobType:    jobType,
		TargetType: targetType,
		TargetID:   targetID,
		Priority:   priority,
		MaxRetries: defaultMaxRetries,
		DedupKey:   jobType + ":" + targetID,
	})
	return err
}

func (s *Service) loadSession(ctx context.Context, sessionID string) (capture.AgentSession, error) {
	if sessionID == "" {
		return capture.AgentSession{}, nil
	}
	return s.repo.GetSession(ctx, sessionID)
}

func (s *Service) loadTask(ctx context.Context, taskID string) (capture.AgentTask, error) {
	if taskID == "" {
		return capture.AgentTask{}, nil
	}
	return s.repo.GetTask(ctx, taskID)
}

func jsonText(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("VALIDATION_FAILED: encode json: %w", err)
	}
	return string(data), nil
}

func defaultFloat(value float64, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultUserForScope(cfg config.Config, scope string) string {
	if scope != memory.ScopeUserGlobal {
		return ""
	}
	return firstNonEmpty(cfg.Memory.DefaultUserID, "local_default_user")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodeObject(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		return object
	}
	var objects []map[string]any
	if err := json.Unmarshal([]byte(raw), &objects); err == nil && len(objects) > 0 {
		return objects[0]
	}
	return nil
}

func stringValue(object map[string]any, key string) string {
	if object == nil {
		return ""
	}
	value, ok := object[key]
	if !ok || value == nil {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
