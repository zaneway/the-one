package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zaneway/the-one/internal/config"
	"github.com/zaneway/the-one/internal/idgen"
	"github.com/zaneway/the-one/internal/ingest"
)

// Repository 定义 P1 Memory CRUD 所需的事务能力。SQLite 实现必须保证多表写入原子性。
type Repository interface {
	Remember(ctx context.Context, item MemoryItem, evidence Evidence, checkpoint *ReviewCheckpoint) error
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, SearchDiagnostics, error)
	Get(ctx context.Context, memoryID string) (MemoryItem, error)
	ListReview(ctx context.Context, req ReviewRequest) ([]MemoryItem, error)
	Approve(ctx context.Context, memoryID, reviewer, feedback string) (MemoryItem, error)
	RejectOrArchive(ctx context.Context, memoryID, action, reviewer, feedback string) (MemoryItem, error)
	Edit(ctx context.Context, memoryID, editContent, reviewer, feedback, searchText string) (MemoryItem, error)
	Delete(ctx context.Context, memoryID, reviewer, feedback string) (MemoryItem, error)
}

// Service 编排 P1 手动记忆写入、检索、上下文构建和 review 流转。
type Service struct {
	cfg  config.Config
	repo Repository
}

// NewService 创建 P1 Memory 服务。
func NewService(cfg config.Config, repo Repository) *Service {
	return &Service{cfg: cfg, repo: repo}
}

// Remember 实现 P1 显式写入闭环：校验、最小化、证据、FTS 文档和 checkpoint 同事务写入。
func (s *Service) Remember(ctx context.Context, req RememberRequest) (RememberResponse, error) {
	if err := NormalizeRemember(s.cfg.Memory, &req); err != nil {
		return RememberResponse{}, err
	}
	checkpointRaw := ""
	if req.ReviewCheckpoint != nil {
		raw, err := toJSON(req.ReviewCheckpoint)
		if err != nil {
			return RememberResponse{}, fmt.Errorf("VALIDATION_FAILED: invalid review_checkpoint: %w", err)
		}
		checkpointRaw = raw
	}
	if err := ingest.CheckMinimizedContent(s.cfg.Memory, ingest.MinimizationInput{
		Content:               req.Content,
		EvidenceStatement:     req.Evidence.InterpretedStatement,
		Keywords:              req.Keywords,
		SalientSpans:          req.Evidence.SalientSpans,
		ReviewCheckpointJSONs: []string{checkpointRaw},
	}); err != nil {
		return RememberResponse{}, err
	}
	if req.MemoryType == TypeReviewCheckpoint && req.ReviewCheckpoint == nil {
		return RememberResponse{}, fmt.Errorf("VALIDATION_FAILED: review_checkpoint is required")
	}
	state, tier := defaultStateAndTier(req)
	memoryID, err := idgen.New("mem")
	if err != nil {
		return RememberResponse{}, err
	}
	evidenceID, err := idgen.New("ev")
	if err != nil {
		return RememberResponse{}, err
	}
	now := time.Now().UTC()
	keywordsJSON, err := toJSON(req.Keywords)
	if err != nil {
		return RememberResponse{}, err
	}
	tagsJSON, err := toJSON(req.Tags)
	if err != nil {
		return RememberResponse{}, err
	}
	entitiesJSON, err := toJSON(req.Entities)
	if err != nil {
		return RememberResponse{}, err
	}
	retrievalCuesJSON, err := toJSON(req.RetrievalCues)
	if err != nil {
		return RememberResponse{}, err
	}
	evidenceKeywordsJSON, err := toJSON(req.Evidence.Keywords)
	if err != nil {
		return RememberResponse{}, err
	}
	salientSpansJSON, err := toJSON(req.Evidence.SalientSpans)
	if err != nil {
		return RememberResponse{}, err
	}
	sourceRefJSON, err := toJSON(req.Evidence.SourceRef)
	if err != nil {
		return RememberResponse{}, err
	}
	searchText := ingest.BuildSearchText(ingest.SearchTextInput{
		Title:             req.Title,
		Content:           req.Content,
		NormalizedContent: req.Content,
		Keywords:          req.Keywords,
		Tags:              req.Tags,
		RetrievalCues:     req.RetrievalCues,
		Entities:          req.Entities,
	})
	item := MemoryItem{
		ID:                memoryID,
		Scope:             req.Scope,
		WorkspaceID:       req.WorkspaceID,
		UserID:            req.UserID,
		ProjectID:         req.ProjectID,
		RepoID:            req.RepoID,
		SessionID:         req.SessionID,
		TaskID:            req.TaskID,
		MemoryType:        req.MemoryType,
		SourceType:        req.SourceType,
		SourceQuality:     sourceQuality(req.SourceType),
		Title:             req.Title,
		Content:           req.Content,
		NormalizedContent: req.Content,
		SearchText:        searchText,
		KeywordsJSON:      keywordsJSON,
		EntitiesJSON:      entitiesJSON,
		RetrievalCuesJSON: retrievalCuesJSON,
		TagsJSON:          tagsJSON,
		State:             state,
		Confidence:        req.Confidence,
		Importance:        req.Importance,
		EncodingDepth:     2,
		DecayRate:         defaultDecayRate(req.MemoryType),
		RetentionScore:    0,
		Tier:              tier,
		CreatedAt:         now,
		UpdatedAt:         now,
		Pinned:            req.Pinned,
		UserConfirmed:     req.SourceType == "user_declared" && state == StateStable,
		Version:           1,
	}
	evidence := Evidence{
		ID:                   evidenceID,
		SourceType:           req.SourceType,
		InterpretedStatement: firstNonEmpty(req.Evidence.InterpretedStatement, req.Content),
		KeywordsJSON:         evidenceKeywordsJSON,
		SalientSpansJSON:     salientSpansJSON,
		SourceRefJSON:        sourceRefJSON,
		Confidence:           req.Confidence,
		CreatedAt:            now,
	}
	var checkpoint *ReviewCheckpoint
	if req.ReviewCheckpoint != nil {
		checkpoint, err = buildCheckpoint(memoryID, item, *req.ReviewCheckpoint, now)
		if err != nil {
			return RememberResponse{}, err
		}
	}
	if err := s.repo.Remember(ctx, item, evidence, checkpoint); err != nil {
		return RememberResponse{}, err
	}
	return RememberResponse{MemoryID: memoryID, State: state, Tier: tier, Deduped: false}, nil
}

// Search 执行 P1 FTS + metadata 检索。
func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if strings.TrimSpace(req.Query) == "" {
		return SearchResponse{}, fmt.Errorf("VALIDATION_FAILED: query is required")
	}
	if req.Limit <= 0 {
		req.Limit = s.cfg.Retrieval.DefaultLimit
	}
	startedAt := time.Now()
	results, diag, err := s.repo.Search(ctx, req)
	if err != nil {
		return SearchResponse{}, err
	}
	diag.LatencyMS = time.Since(startedAt).Milliseconds()
	return SearchResponse{Results: results, Diagnostics: diag}, nil
}

// Context 构造 P1 压缩上下文包。P1 使用字符预算近似 token budget，后续可替换为 tokenizer。
func (s *Service) Context(ctx context.Context, req ContextRequest) (ContextResponse, error) {
	startedAt := time.Now()
	if strings.TrimSpace(req.Task) == "" {
		return ContextResponse{}, fmt.Errorf("VALIDATION_FAILED: task is required")
	}
	if req.TokenBudget <= 0 {
		req.TokenBudget = s.cfg.Retrieval.DefaultTokenBudget
	}
	searchTypes := []string{TypeConstraint, TypeDecision, TypeFailure, TypePreference, TypeProjectFact, TypeProcedure, TypeTemporaryState}
	if isDesignReviewTask(req.Task) {
		searchTypes = append([]string{TypeReviewCheckpoint}, searchTypes...)
	}
	searchResp, err := s.Search(ctx, SearchRequest{
		Query:           req.Task,
		WorkspaceID:     req.WorkspaceID,
		ProjectID:       req.ProjectID,
		RepoID:          req.RepoID,
		SessionID:       req.SessionID,
		Scope:           []string{ScopeProjectLocal, ScopeRepoLocal, ScopeUserGlobal, ScopeSession},
		MemoryTypes:     searchTypes,
		Limit:           s.cfg.Retrieval.DefaultLimit,
		IncludeArchived: false,
		IncludeEvidence: req.IncludeEvidenceSummary,
	})
	if err != nil {
		return ContextResponse{}, err
	}
	if !containsMemoryType(searchResp.Results, TypePreference) {
		preferenceResp, prefErr := s.Search(ctx, SearchRequest{
			Query:           "用户偏好 架构 风险 工程落地 preference",
			WorkspaceID:     req.WorkspaceID,
			ProjectID:       req.ProjectID,
			RepoID:          req.RepoID,
			SessionID:       req.SessionID,
			Scope:           []string{ScopeUserGlobal},
			MemoryTypes:     []string{TypePreference},
			Limit:           3,
			IncludeArchived: false,
			IncludeEvidence: req.IncludeEvidenceSummary,
		})
		if prefErr == nil {
			searchResp.Results = append(searchResp.Results, preferenceResp.Results...)
		}
	}
	if isDesignReviewTask(req.Task) && !containsMemoryType(searchResp.Results, TypeReviewCheckpoint) {
		checkpointResp, checkpointErr := s.Search(ctx, SearchRequest{
			Query:           "设计复查 架构评审 文档完整性 review_checkpoint",
			WorkspaceID:     req.WorkspaceID,
			ProjectID:       req.ProjectID,
			RepoID:          req.RepoID,
			SessionID:       req.SessionID,
			Scope:           []string{ScopeProjectLocal, ScopeRepoLocal},
			MemoryTypes:     []string{TypeReviewCheckpoint},
			Limit:           3,
			IncludeArchived: false,
			IncludeEvidence: req.IncludeEvidenceSummary,
		})
		if checkpointErr == nil {
			searchResp.Results = append(checkpointResp.Results, searchResp.Results...)
		}
	}
	memories := make([]ContextMemory, 0, len(searchResp.Results))
	usedIDs := make([]string, 0, len(searchResp.Results))
	constraints := make([]string, 0)
	remaining := req.TokenBudget
	for _, result := range searchResp.Results {
		compressed := compress(result.Content, remaining)
		if compressed == "" {
			break
		}
		remaining -= len([]rune(compressed))
		why := []string{result.Scope, "task_fit", result.State}
		if result.MemoryType == TypeReviewCheckpoint {
			why = append(why, "review_checkpoint")
		}
		memories = append(memories, ContextMemory{
			MemoryID:    result.MemoryID,
			Type:        result.MemoryType,
			Compressed:  compressed,
			WhyIncluded: why,
		})
		usedIDs = append(usedIDs, result.MemoryID)
		if result.MemoryType == TypeConstraint {
			constraints = append(constraints, compressed)
		}
	}
	summary := ""
	if len(memories) > 0 {
		summary = memories[0].Compressed
	}
	return ContextResponse{
		ContextPack: ContextPack{
			Summary:     summary,
			Memories:    memories,
			Constraints: constraints,
			CodeRefs:    []any{},
		},
		UsedMemoryIDs: usedIDs,
		LatencyMS:     time.Since(startedAt).Milliseconds(),
	}, nil
}

func containsMemoryType(results []SearchResult, memoryType string) bool {
	for _, result := range results {
		if result.MemoryType == memoryType {
			return true
		}
	}
	return false
}

// Review 执行 P1 pending memory 查询和 approve/reject/edit/archive/delete 状态流转。
func (s *Service) Review(ctx context.Context, req ReviewRequest) (ReviewResponse, error) {
	switch req.Action {
	case "list":
		if req.Limit <= 0 {
			req.Limit = s.cfg.Retrieval.DefaultLimit
		}
		items, err := s.repo.ListReview(ctx, req)
		return ReviewResponse{Results: items}, err
	case "approve":
		item, err := s.repo.Approve(ctx, req.MemoryID, req.Reviewer, req.Feedback)
		return ReviewResponse{MemoryID: item.ID, State: item.State, UserConfirmed: item.UserConfirmed}, err
	case "reject", "archive":
		item, err := s.repo.RejectOrArchive(ctx, req.MemoryID, req.Action, req.Reviewer, req.Feedback)
		return ReviewResponse{MemoryID: item.ID, State: item.State, UserConfirmed: item.UserConfirmed}, err
	case "edit":
		if strings.TrimSpace(req.EditContent) == "" {
			return ReviewResponse{}, fmt.Errorf("VALIDATION_FAILED: edit_content is required")
		}
		item, err := s.repo.Get(ctx, req.MemoryID)
		if err != nil {
			return ReviewResponse{}, err
		}
		searchText := ingest.BuildSearchText(ingest.SearchTextInput{
			Title:             item.Title,
			Content:           req.EditContent,
			NormalizedContent: req.EditContent,
		})
		updated, err := s.repo.Edit(ctx, req.MemoryID, req.EditContent, req.Reviewer, req.Feedback, searchText)
		return ReviewResponse{MemoryID: updated.ID, State: updated.State, UserConfirmed: updated.UserConfirmed}, err
	case "delete":
		item, err := s.repo.Delete(ctx, req.MemoryID, req.Reviewer, req.Feedback)
		return ReviewResponse{MemoryID: item.ID, State: item.State, UserConfirmed: item.UserConfirmed}, err
	default:
		return ReviewResponse{}, fmt.Errorf("VALIDATION_FAILED: unsupported review action %q", req.Action)
	}
}

func buildCheckpoint(memoryID string, item MemoryItem, input ReviewCheckpointInput, now time.Time) (*ReviewCheckpoint, error) {
	if input.CheckpointType == "" || input.Conclusion == "" || len(input.TargetDocs) == 0 {
		return nil, fmt.Errorf("VALIDATION_FAILED: checkpoint_type, conclusion and target_docs are required")
	}
	id, err := idgen.New("rcp")
	if err != nil {
		return nil, err
	}
	reviewIntent, err := toJSON(input.ReviewIntent)
	if err != nil {
		return nil, err
	}
	targetDocs, err := toJSON(input.TargetDocs)
	if err != nil {
		return nil, err
	}
	targetSections, err := toJSON(input.TargetSections)
	if err != nil {
		return nil, err
	}
	targetHashes, err := toJSON(input.TargetHashes)
	if err != nil {
		return nil, err
	}
	confirmedBaseline, err := toJSON(input.ConfirmedBaseline)
	if err != nil {
		return nil, err
	}
	ignoredItems, err := toJSON(input.IgnoredItems)
	if err != nil {
		return nil, err
	}
	deferredItems, err := toJSON(input.DeferredItems)
	if err != nil {
		return nil, err
	}
	openItems, err := toJSON(input.OpenItems)
	if err != nil {
		return nil, err
	}
	nextPolicy, err := toJSON(input.NextReviewPolicy)
	if err != nil {
		return nil, err
	}
	return &ReviewCheckpoint{
		ID:                    id,
		MemoryID:              memoryID,
		WorkspaceID:           item.WorkspaceID,
		ProjectID:             item.ProjectID,
		RepoID:                item.RepoID,
		SessionID:             item.SessionID,
		TaskID:                item.TaskID,
		CheckpointType:        input.CheckpointType,
		ReviewIntentJSON:      reviewIntent,
		TargetDocsJSON:        targetDocs,
		TargetSectionsJSON:    targetSections,
		TargetHashesJSON:      targetHashes,
		Conclusion:            input.Conclusion,
		ConfirmedBaselineJSON: confirmedBaseline,
		IgnoredItemsJSON:      ignoredItems,
		DeferredItemsJSON:     deferredItems,
		OpenItemsJSON:         openItems,
		NextReviewPolicyJSON:  nextPolicy,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}

func sourceQuality(sourceType string) float64 {
	switch sourceType {
	case "user_declared", "user_confirmed":
		return 1.0
	case "manual_review":
		return 0.8
	default:
		return 0.7
	}
}

func defaultDecayRate(memoryType string) float64 {
	switch memoryType {
	case TypeDecision, TypeConstraint, TypePreference:
		return 0.3
	case TypeFailure, TypeProcedure:
		return 0.45
	case TypeTemporaryState:
		return 1.2
	default:
		return 0.8
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isDesignReviewTask(task string) bool {
	task = strings.ToLower(task)
	return strings.Contains(task, "设计复查") ||
		strings.Contains(task, "架构评审") ||
		strings.Contains(task, "文档完整性") ||
		strings.Contains(task, "review")
}

func compress(content string, budget int) string {
	content = strings.TrimSpace(content)
	if budget <= 0 || content == "" {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= budget {
		return content
	}
	if budget <= 3 {
		return string(runes[:budget])
	}
	return string(runes[:budget-3]) + "..."
}

func decodeStringSlice(raw string) []string {
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
