package automation

import (
	"context"
	"strings"

	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
)

// DecideRemember 将 memory.remember 请求转换为候选记忆并执行准入控制。
func (s *Service) DecideRemember(ctx context.Context, req memory.RememberRequest) (memory.RememberAdmissionDecision, error) {
	candidate := rememberCandidateFromRequest(req)
	related, err := s.repo.FindRelatedMemory(ctx, RelatedMemoryRequest{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		Scope:       req.Scope,
		MemoryType:  req.MemoryType,
		Query:       req.Content,
		Limit:       10,
	})
	if err != nil {
		return memory.RememberAdmissionDecision{}, err
	}
	result := s.admission.Decide(AdmissionInput{Candidate: candidate, RelatedMemory: related})
	allowed := result.Decision != DecisionDrop && result.Decision != DecisionWriteRawOnly
	return memory.RememberAdmissionDecision{
		Allowed:        allowed,
		Decision:       result.Decision,
		InitialState:   result.InitialState,
		InitialTier:    result.InitialTier,
		UserConfirmed:  result.UserConfirmed,
		RetentionScore: result.AdmissionScore,
		DecayRate:      result.DecayRate,
		ReasonCodes:    result.ReasonCodes,
	}, nil
}

func rememberCandidateFromRequest(req memory.RememberRequest) processor.MemoryCandidate {
	candidate := processor.MemoryCandidate{
		MemoryType:      req.MemoryType,
		Scope:           req.Scope,
		WorkspaceID:     req.WorkspaceID,
		UserID:          req.UserID,
		ProjectID:       req.ProjectID,
		RepoID:          req.RepoID,
		SessionID:       req.SessionID,
		TaskID:          req.TaskID,
		SourceType:      req.SourceType,
		Title:           req.Title,
		Content:         req.Content,
		Keywords:        req.Keywords,
		Entities:        req.Entities,
		RetrievalCues:   req.RetrievalCues,
		Tags:            req.Tags,
		Confidence:      req.Confidence,
		Importance:      req.Importance,
		EncodingDepth:   2,
		CandidateReason: []string{"explicit_remember"},
	}
	if req.ReviewCheckpoint != nil {
		candidate.ReviewCheckpoint = &processor.ReviewCheckpointDraft{
			CheckpointType:    req.ReviewCheckpoint.CheckpointType,
			ReviewIntent:      req.ReviewCheckpoint.ReviewIntent,
			TargetDocs:        req.ReviewCheckpoint.TargetDocs,
			TargetSections:    req.ReviewCheckpoint.TargetSections,
			TargetHashes:      req.ReviewCheckpoint.TargetHashes,
			Conclusion:        req.ReviewCheckpoint.Conclusion,
			ConfirmedBaseline: req.ReviewCheckpoint.ConfirmedBaseline,
			IgnoredItems:      req.ReviewCheckpoint.IgnoredItems,
			DeferredItems:     req.ReviewCheckpoint.DeferredItems,
			OpenItems:         req.ReviewCheckpoint.OpenItems,
			NextReviewPolicy:  req.ReviewCheckpoint.NextReviewPolicy,
		}
	}
	if strings.TrimSpace(req.SourceType) == "user_declared" {
		candidate.CandidateReason = append(candidate.CandidateReason, "user_declaration")
	}
	return candidate
}
