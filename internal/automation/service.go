package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/capture"
	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/idgen"
	"github.com/zaneway/the-one/internal/ingest"
	"github.com/zaneway/the-one/internal/memory"
	"github.com/zaneway/the-one/internal/processor"
	"github.com/zaneway/the-one/internal/retention"
)

const (
	defaultRelatedEventsLimit = 20
	defaultMaxRetries         = 3
)

// Repository 定义 P3-C1 automation service 依赖的异步任务、事件、证据和自动写入能力。
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
	ListRelatedEvents(ctx context.Context, req RelatedEventsRequest) ([]capture.RawEvent, error)

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
	ListOrphanRawEvents(ctx context.Context, req OrphanRawEventRequest) ([]capture.RawEvent, error)

	ListExpiredTemporaryMemories(ctx context.Context, req retention.ListRequest) ([]retention.MemoryRecord, error)
	ArchiveTemporaryMemory(ctx context.Context, memoryID string, now time.Time) error
	ListMemoriesForScoreRecalc(ctx context.Context, req retention.ListRequest) ([]retention.MemoryRecord, error)
	UpdateRetentionFields(ctx context.Context, memoryID string, update retention.ScoreUpdate) error
}

// Service 编排 P3 自动记忆 job 链路。
// Provider 只负责生成 draft/candidate，最终写入和 Admission 均在该服务中统一执行。
type Service struct {
	cfg        config.Config
	repo       Repository
	provider   processor.Provider
	admission  AdmissionController
	dispatcher JobDispatcher
}

// NewService 创建自动记忆服务。provider 为空时使用 rule_based，保证 P3 默认本地可运行。
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
	}
	service.dispatcher = NewJobDispatcher(
		p3JobHandler{service: service},
		newP4JobHandler(cfg, repo),
	)
	return service
}

// EnqueueRawEvent 为 raw_event 创建 evidence 抽取任务。
func (s *Service) EnqueueRawEvent(ctx context.Context, rawEvent capture.RawEvent) error {
	if rawEvent.ID == "" {
		return fmt.Errorf("VALIDATION_FAILED: raw_event id is required")
	}
	if !s.cfg.Processor.EnableAutoProcessing || s.cfg.Processor.Provider == "none" {
		return nil
	}
	jobID, err := idgen.New("job")
	if err != nil {
		return err
	}
	_, _, err = s.repo.EnqueueJob(ctx, AsyncJob{
		ID:         jobID,
		JobType:    JobTypeExtractEvidence,
		TargetType: TargetTypeRawEvent,
		TargetID:   rawEvent.ID,
		Priority:   3,
		MaxRetries: defaultMaxRetries,
		DedupKey:   JobTypeExtractEvidence + ":" + rawEvent.ID,
	})
	return err
}

// RunJob 执行单个已领取 job，并负责把结果状态回写为 succeeded 或 failed。
func (s *Service) RunJob(ctx context.Context, job AsyncJob) error {
	now := time.Now().UTC()
	payload, err := s.dispatcher.RunJob(ctx, job)
	if err != nil {
		_ = s.repo.MarkJobFailed(ctx, job.ID, err.Error(), now)
		return err
	}
	payloadJSON, err := jsonText(payload)
	if err != nil {
		_ = s.repo.MarkJobFailed(ctx, job.ID, err.Error(), now)
		return err
	}
	return s.repo.MarkJobSucceeded(ctx, job.ID, payloadJSON, now)
}

func (s *Service) runExtractEvidence(ctx context.Context, job AsyncJob) (map[string]any, error) {
	rawEvent, err := s.repo.GetRawEvent(ctx, job.TargetID)
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
	related, err := s.repo.ListRelatedEvents(ctx, RelatedEventsRequest{
		SessionID: rawEvent.SessionID,
		TaskID:    rawEvent.TaskID,
		Limit:     defaultRelatedEventsLimit,
	})
	if err != nil {
		return nil, err
	}
	drafts, err := s.provider.ExtractEvidence(ctx, processor.EvidenceInput{
		RawEvent:      rawEvent,
		Session:       session,
		Task:          task,
		RelatedEvents: related,
		Now:           time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	written := 0
	for _, draft := range drafts {
		evidence, err := s.materializeEvidence(ctx, rawEvent.ID, draft)
		if err != nil {
			return nil, err
		}
		written++
		if err := s.enqueueNext(ctx, JobTypeGenerateMemoryCandidate, TargetTypeEvidence, evidence.ID, 4); err != nil {
			return nil, err
		}
	}
	return map[string]any{"evidence_count": written}, nil
}

func (s *Service) runGenerateMemoryCandidate(ctx context.Context, job AsyncJob) (map[string]any, error) {
	evidence, err := s.repo.GetEvidence(ctx, job.TargetID)
	if err != nil {
		return nil, err
	}
	rawEvent, err := s.repo.GetRawEvent(ctx, evidence.RawEventID)
	if err != nil {
		return nil, err
	}
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
	candidates, err := s.provider.GenerateCandidates(ctx, processor.CandidateInput{
		Evidence:      evidence,
		RawEvent:      rawEvent,
		Session:       session,
		Task:          task,
		RelatedMemory: related,
		Now:           time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	written := 0
	for _, candidate := range candidates {
		record, err := s.materializeCandidate(candidate, evidence, rawEvent)
		if err != nil {
			return nil, err
		}
		if err := s.repo.WriteCandidate(ctx, record); err != nil {
			return nil, err
		}
		written++
		if err := s.enqueueNext(ctx, JobTypeComputeAdmission, TargetTypeMemoryCandidate, record.ID, 5); err != nil {
			return nil, err
		}
	}
	return map[string]any{"candidate_count": written}, nil
}

func (s *Service) runComputeAdmission(ctx context.Context, job AsyncJob) (map[string]any, error) {
	record, err := s.repo.GetCandidate(ctx, job.TargetID)
	if err != nil {
		return nil, err
	}
	evidence, err := s.repo.GetEvidence(ctx, record.EvidenceID)
	if err != nil {
		return nil, err
	}
	candidate := candidateFromRecord(record, evidence.SourceType)
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
		return nil, err
	}
	admission := s.admission.Decide(AdmissionInput{Candidate: candidate, RelatedMemory: related})
	switch admission.Decision {
	case DecisionDrop, DecisionWriteRawOnly:
		if err := s.repo.UpdateCandidateAdmission(ctx, record.ID, admission, CandidateStatusDropped, ""); err != nil {
			return nil, err
		}
		return map[string]any{"admission_decision": admission.Decision}, nil
	case DecisionWriteTemporary, DecisionWriteProvisional, DecisionWritePendingReview, DecisionWriteStable:
		item, err := s.memoryItemFromAdmission(record, candidate, admission, evidence)
		if err != nil {
			return nil, err
		}
		written, err := s.writeAdmittedMemory(ctx, record, candidate, admission, evidence, item)
		if err != nil {
			return nil, err
		}
		if err := s.repo.UpdateCandidateAdmission(ctx, record.ID, admission, CandidateStatusAdmitted, written.ID); err != nil {
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

func (s *Service) writeAdmittedMemory(ctx context.Context, record MemoryCandidateRecord, candidate processor.MemoryCandidate, admission AdmissionResult, evidence memory.Evidence, item memory.MemoryItem) (memory.MemoryItem, error) {
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
	return s.repo.WriteAutomatedMemory(ctx, AutomatedMemoryWrite{
		Item:             item,
		EvidenceIDs:      candidate.SourceEvidenceIDs,
		ReviewCheckpoint: checkpoint,
	})
}

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
		CreatedAt:            time.Now().UTC(),
	}
	if err := s.repo.WriteEvidence(ctx, evidence); err != nil {
		return memory.Evidence{}, err
	}
	return evidence, nil
}

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
		CandidateReasonJSON:   reasonsJSON,
		Status:                CandidateStatusGenerated,
		DedupKey:              strings.Join([]string{s.provider.Name(), evidence.ID, candidate.MemoryType, candidate.Scope, candidate.Content}, ":"),
	}, nil
}

func (s *Service) memoryItemFromAdmission(record MemoryCandidateRecord, candidate processor.MemoryCandidate, admission AdmissionResult, evidence memory.Evidence) (memory.MemoryItem, error) {
	memoryID, err := idgen.New("mem")
	if err != nil {
		return memory.MemoryItem{}, err
	}
	now := time.Now().UTC()
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
		CandidateReason:   decodeStringSlice(record.CandidateReasonJSON),
		SourceEvidenceIDs: decodeStringSlice(record.SourceEvidenceIDsJSON),
	}
}

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
