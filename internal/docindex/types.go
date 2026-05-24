package docindex

import "time"

// DocumentSnapshot 表示一次 Markdown 文档快照。
// 设计约束：快照只保存路径、hash、章节元数据和摘要，不保存完整文档正文。
type DocumentSnapshot struct {
	// ID snapshot ID。
	ID string `json:"id,omitempty"`

	// WorkspaceID 工作空间 ID，必填。
	WorkspaceID string `json:"workspace_id"`

	// ProjectID 项目 ID，可为空，持久层使用空字符串表达不适用。
	ProjectID string `json:"project_id,omitempty"`

	// RepoID 仓库 ID，可为空，持久层使用空字符串表达不适用。
	RepoID string `json:"repo_id,omitempty"`

	// Path repo/workspace 内相对文档路径。
	Path string `json:"path"`

	// Role 文档角色，如 implementation_design、iteration_plan。
	Role string `json:"role,omitempty"`

	// ContentHash 文档内容 hash。
	ContentHash string `json:"content_hash"`

	// ModifiedAt 文档文件修改时间，可为空。
	ModifiedAt time.Time `json:"modified_at,omitempty"`

	// SectionCount 章节数量。
	SectionCount int `json:"section_count,omitempty"`

	// Sections 章节快照列表。
	Sections []DocumentSection `json:"sections,omitempty"`

	// CreatedAt 快照创建时间。
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// DocumentSection 表示 Markdown 章节快照。
// 设计约束：section 只保存标题路径、行号、hash 和简短摘要，不保存章节正文。
type DocumentSection struct {
	// ID section snapshot ID。
	ID string `json:"id,omitempty"`

	// SnapshotID 所属文档 snapshot ID。
	SnapshotID string `json:"snapshot_id,omitempty"`

	// SectionID 文档内稳定章节 ID。
	SectionID string `json:"section_id"`

	// HeadingPath 标题路径。
	HeadingPath []string `json:"heading_path,omitempty"`

	// Level Markdown 标题层级。
	Level int `json:"level,omitempty"`

	// StartLine 起始行号。
	StartLine int `json:"start_line,omitempty"`

	// EndLine 结束行号。
	EndLine int `json:"end_line,omitempty"`

	// ContentHash 章节内容 hash。
	ContentHash string `json:"content_hash"`

	// Summary 简短章节摘要。
	Summary string `json:"summary,omitempty"`

	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// SnapshotQuery 是 doc_snapshot 诊断查询条件。
// 查询必须按 workspace_id + doc_path 收敛，避免诊断入口退化为全库扫描。
type SnapshotQuery struct {
	// WorkspaceID 工作空间 ID，必填。
	WorkspaceID string

	// ProjectID 项目 ID，可选过滤条件。
	ProjectID string

	// RepoID 仓库 ID，可选过滤条件。
	RepoID string

	// Path 文档路径，必填。
	Path string

	// ContentHash 文档内容 hash，可选过滤条件。
	ContentHash string

	// IncludeSections 是否加载章节快照。
	IncludeSections bool

	// Limit 返回数量限制。
	Limit int
}
