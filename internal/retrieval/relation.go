package retrieval

import "github.com/zaneway/the-one/internal/memory"

const (
	// RelationTypeSupersedes 表示 source 记忆替代 target 记忆。
	RelationTypeSupersedes = "supersedes"

	// RelationTypeSupersededBy 表示 source 记忆被 target 记忆替代。
	RelationTypeSupersededBy = "superseded_by"

	// RelationTypeSupports 表示 source 和 target 之间存在支持关系。
	RelationTypeSupports = "supports"

	// RelationTypeContradicts 表示 source 和 target 之间存在冲突关系。
	RelationTypeContradicts = "contradicts"

	// RelationDirectionOutgoing 表示 seed memory 是 relation.source_id。
	RelationDirectionOutgoing = "outgoing"

	// RelationDirectionIncoming 表示 seed memory 是 relation.target_id。
	RelationDirectionIncoming = "incoming"
)

// RelationExpansionQuery 描述 P4-C2 relation expansion 的查询边界。
// 设计约束：只允许从已召回的 seed memory 做 depth=1 扩展，避免在线检索退化为图遍历。
type RelationExpansionQuery struct {
	// SeedMemoryIDs FTS/vector 已召回的种子 memory_id。
	SeedMemoryIDs []string

	// WorkspaceID 工作空间 ID，用于 scope 隔离。
	WorkspaceID string

	// ProjectID 项目 ID，用于 project_local 隔离。
	ProjectID string

	// RepoID 仓库 ID，用于 repo_local 隔离。
	RepoID string

	// SessionID 会话 ID，用于 session 隔离。
	SessionID string

	// Scopes scope 过滤；为空时按 repository 默认隔离规则处理。
	Scopes []string

	// IncludeArchived 是否允许返回 archived 记忆。
	IncludeArchived bool

	// RelationTypes 允许参与在线检索的关系类型；为空时使用 P4 默认强关系集合。
	RelationTypes []string

	// Limit 单次 relation expansion 最大边数。
	Limit int
}

// RelationExpansion 是 relation repository 返回的单条 depth=1 扩展边。
// RelatedMemory 是边另一端的记忆，调用方根据 relation_type/direction 决定是否作为候选注入。
type RelationExpansion struct {
	// SeedMemoryID 触发扩展的种子 memory_id。
	SeedMemoryID string

	// Direction 表示 seed 在关系边中的方向。
	Direction string

	// RelationType 关系类型。
	RelationType string

	// Weight 关系权重，默认 1.0。
	Weight float64

	// RelatedMemory 关系另一端记忆。
	RelatedMemory memory.MemoryItem
}

// DefaultRelationTypes 返回 P4-C2 在线检索默认参与排序的关系类型。
func DefaultRelationTypes() []string {
	return []string{RelationTypeSupersedes, RelationTypeSupersededBy, RelationTypeSupports, RelationTypeContradicts}
}
