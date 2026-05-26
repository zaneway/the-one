package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/zaneway/the-one/internal/config"
)

const runLimitMax = 100

type Repository interface {
	ListExpiredTemporaryMemories(ctx context.Context, req ListRequest) ([]MemoryRecord, error)
	ArchiveTemporaryMemory(ctx context.Context, memoryID string, now time.Time) error
	ListMemoriesForScoreRecalc(ctx context.Context, req ListRequest) ([]MemoryRecord, error)
	UpdateRetentionFields(ctx context.Context, memoryID string, update ScoreUpdate) error
}

type Service struct {
	cfg  config.Config
	repo Repository
}

func NewService(cfg config.Config, repo Repository) *Service {
	return &Service{cfg: cfg, repo: repo}
}

func (s *Service) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	mode := req.Mode
	if mode == "" {
		mode = ModeCleanupTemporary
	}
	switch mode {
	case ModeCleanupTemporary:
		return s.runCleanupTemporary(ctx, req)
	case ModeRecomputeScores:
		return s.runRecomputeScores(ctx, req)
	default:
		return RunResponse{}, fmt.Errorf("VALIDATION_FAILED: unsupported retention mode %q", mode)
	}
}

// runCleanupTemporary 清理过期的临时记忆。
// 流程：查询超过 TemporaryTTLDays 的 temporary 记忆 → 逐条归档（state=archived）。
// 支持 dry_run 模式：只返回将要归档的记忆列表，不实际执行。
func (s *Service) runCleanupTemporary(ctx context.Context, req RunRequest) (RunResponse, error) {
	now := time.Now().UTC()
	limit, diagnostics := normalizeRunLimit(req.Limit)
	records, err := s.repo.ListExpiredTemporaryMemories(ctx, ListRequest{
		WorkspaceID:      req.WorkspaceID,
		ProjectID:        req.ProjectID,
		Limit:            limit,
		TemporaryTTLDays: s.cfg.Retention.TemporaryTTLDays,
		Now:              now,
	})
	if err != nil {
		return RunResponse{}, err
	}
	resp := RunResponse{
		Mode:        ModeCleanupTemporary,
		DryRun:      req.DryRun,
		Items:       make([]ActionItem, 0, len(records)),
		Diagnostics: diagnostics,
	}
	for _, record := range records {
		resp.Items = append(resp.Items, ActionItem{
			MemoryID: record.ID,
			Action:   ActionArchive,
			Reason:   ReasonTemporaryExpired,
		})
		if req.DryRun {
			continue
		}
		if err := s.repo.ArchiveTemporaryMemory(ctx, record.ID, now); err != nil {
			return resp, err
		}
		resp.Processed++
	}
	return resp, nil
}

// runRecomputeScores 重新计算记忆的保留分数和 tier。
// 流程：查询需要重算的记忆 → 逐条计算 ComputeScore + ComputeTier → 更新 retention_score 和 tier。
// 设计约束：pinned 记忆跳过更新（但仍计算并返回诊断信息）。
// score < 0.30 的记忆标记为 archive_candidate（由上层决策是否归档）。
func (s *Service) runRecomputeScores(ctx context.Context, req RunRequest) (RunResponse, error) {
	now := time.Now().UTC()
	limit, diagnostics := normalizeRunLimit(req.Limit)
	records, err := s.repo.ListMemoriesForScoreRecalc(ctx, ListRequest{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		Limit:       limit,
		Now:         now,
	})
	if err != nil {
		return RunResponse{}, err
	}
	resp := RunResponse{
		Mode:        ModeRecomputeScores,
		DryRun:      req.DryRun,
		Items:       make([]ActionItem, 0, len(records)),
		Diagnostics: diagnostics,
	}
	for _, record := range records {
		score := ComputeScore(recordInput(record, now, 0))
		tier := ComputeTier(recordInput(record, now, score))
		reason := ReasonScoreRecomputed
		if score < 0.30 {
			reason = ReasonArchiveCandidate
		}
		item := ActionItem{
			MemoryID:       record.ID,
			Action:         ActionUpdateScore,
			Reason:         reason,
			Tier:           tier,
			RetentionScore: score,
		}
		resp.Items = append(resp.Items, item)
		if req.DryRun || record.Pinned {
			continue
		}
		if err := s.repo.UpdateRetentionFields(ctx, record.ID, ScoreUpdate{
			RetentionScore: score,
			Tier:           tier,
			UpdatedAt:      now,
		}); err != nil {
			return resp, err
		}
		resp.Processed++
	}
	return resp, nil
}

func normalizeRunLimit(limit int) (int, []string) {
	if limit <= 0 {
		return runLimitMax, nil
	}
	if limit > runLimitMax {
		return runLimitMax, []string{"limit_truncated"}
	}
	return limit, nil
}
