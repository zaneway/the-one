package automation

import (
	"context"
	"time"

	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
)

// applyCorrectionSupersedes 在无 target_memory_id 的用户纠正写入后，归档冲突 stable 记忆并建立 supersedes 边。
func (s *Service) applyCorrectionSupersedes(ctx context.Context, written memory.MemoryItem, candidate processor.MemoryCandidate, evidence memory.Evidence, related []memory.MemoryItem) error {
	if !isUserCorrection(candidate, evidence) {
		return nil
	}
	now := time.Now().UTC()
	var primarySupersedes string
	for _, item := range related {
		if item.ID == written.ID {
			continue
		}
		if item.State != memory.StateStable {
			continue
		}
		if !memory.LikelyConflict(candidate.MemoryType, candidate.Scope, candidate.Content, item) {
			continue
		}
		if err := s.repo.ArchiveMemoryForSupersedes(ctx, item.ID, now); err != nil {
			return err
		}
		if primarySupersedes == "" {
			primarySupersedes = item.ID
		}
		if err := s.writeSupersedesRelation(ctx, written.ID, item.ID, now); err != nil {
			return err
		}
	}
	if primarySupersedes != "" && written.SupersedesID == "" {
		return s.repo.UpdateMemorySupersedesID(ctx, written.ID, primarySupersedes, now)
	}
	return nil
}

func (s *Service) writeSupersedesRelation(ctx context.Context, newMemoryID, oldMemoryID string, now time.Time) error {
	forwardID, err := idgen.New("rel")
	if err != nil {
		return err
	}
	if err := s.repo.WriteMemoryRelation(ctx, memory.MemoryRelation{
		ID:           forwardID,
		SourceID:     newMemoryID,
		TargetID:     oldMemoryID,
		RelationType: "supersedes",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return err
	}
	reverseID, err := idgen.New("rel")
	if err != nil {
		return err
	}
	return s.repo.WriteMemoryRelation(ctx, memory.MemoryRelation{
		ID:           reverseID,
		SourceID:     oldMemoryID,
		TargetID:     newMemoryID,
		RelationType: "superseded_by",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
}
