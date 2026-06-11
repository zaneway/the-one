package automation

import (
	"context"
	"time"

	"github.com/zaneway/theone/internal/idgen"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
)

// applyCorrectionSupersedes 在无 target_memory_id 的用户纠正写入后，归档冲突 stable 记忆并建立 supersedes 边。
// 入参：
//   - written：本次写入的新记忆；
//   - candidate：触发本次写入的候选；
//   - evidence：候选对应的证据；
//   - related：admission 阶段查到的同 scope 相关历史记忆。
//
// 处理流程：
//  1. 不是用户纠正则直接返回；
//  2. 遍历 related，只对处于 stable 状态且 LikelyConflict 判定为冲突的旧记忆执行：
//     a. 归档旧记忆；
//     b. 记录第一条作为主 supersedes 目标；
//     c. 写入双向 supersedes/superseded_by 关系；
//  3. 写入新记忆上的 SupersedesID（取第一条作为主链），便于后续 recall 直接定位被替代记忆。
//
// 设计约束：归档失败/关系写入失败立即返回错误，由外层 job 标记为失败并触发重试。
func (s *Service) applyCorrectionSupersedes(ctx context.Context, written memory.MemoryItem, candidate processor.MemoryCandidate, evidence memory.Evidence, related []memory.MemoryItem) error {
	if !isUserCorrection(candidate, evidence) {
		return nil
	}
	now := time.Now()
	var primarySupersedes string
	for _, item := range related {
		// 跳过自己（极端情况下 related 也会包含本次写入的目标）
		if item.ID == written.ID {
			continue
		}
		// 只处理 stable 状态：避免误归档临时/待复核记忆
		if item.State != memory.StateStable {
			continue
		}
		// 借助同 type/scope 下的语义冲突判定避免误伤相似但不同的记忆
		if !memory.LikelyConflict(candidate.MemoryType, candidate.Scope, candidate.Content, item) {
			continue
		}
		if err := s.repo.ArchiveMemoryForSupersedes(ctx, item.ID, now); err != nil {
			return err
		}
		// 只把第一条命中的旧记忆作为新记忆的主 supersedes 目标
		if primarySupersedes == "" {
			primarySupersedes = item.ID
		}
		if err := s.writeSupersedesRelation(ctx, written.ID, item.ID, now); err != nil {
			return err
		}
	}
	// 只有当外层没有显式指定 supersedes 时才回填，避免覆盖调用方提供的更准确的目标
	if primarySupersedes != "" && written.SupersedesID == "" {
		return s.repo.UpdateMemorySupersedesID(ctx, written.ID, primarySupersedes, now)
	}
	return nil
}

// writeSupersedesRelation 写入一对 supersedes/superseded_by 关系边。
// 双向写入是为了一致性：后续检索在任一方向（向前 / 向后）查询都能命中；
// 关系 ID 用 idgen.New("rel") 独立生成，保证两条边的 ID 都唯一。
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
