package memory

import (
	"fmt"
	"strings"

	"github.com/zaneway/the-one/internal/config"
)

// NormalizeRemember 归一化并校验 remember 请求
// 处理流程：
// 1. 去除空白字符
// 2. 填充默认值（user_id、workspace_id、source_type、confidence、importance）
// 3. 校验必填字段（content）
// 4. 校验memory_type合法性
// 5. 执行scope validator，避免user/project/repo记忆互相污染
func NormalizeRemember(cfg config.MemoryConfig, req *RememberRequest) error {
	req.Content = strings.TrimSpace(req.Content)
	req.Title = strings.TrimSpace(req.Title)
	req.Scope = strings.TrimSpace(req.Scope)
	req.MemoryType = strings.TrimSpace(req.MemoryType)
	if req.UserID == "" {
		req.UserID = cfg.DefaultUserID
	}
	if req.WorkspaceID == "" {
		req.WorkspaceID = cfg.DefaultWorkspace
	}
	if req.SourceType == "" {
		req.SourceType = "user_declared"
	}
	if req.Confidence == 0 {
		req.Confidence = 0.7
	}
	if req.Importance == 0 {
		req.Importance = 0.5
	}
	if req.Content == "" {
		return fmt.Errorf("VALIDATION_FAILED: content is required")
	}
	if !validMemoryType(req.MemoryType) {
		return fmt.Errorf("VALIDATION_FAILED: unsupported memory_type %q", req.MemoryType)
	}
	return ValidateScope(req.Scope, req.WorkspaceID, req.UserID, req.ProjectID, req.RepoID, req.SessionID)
}

// ValidateScope 执行 P1 scope validator
// 校验规则：
// - user_global: 必须有user_id，不能有project_id或repo_id
// - project_local: 必须有workspace_id和project_id
// - repo_local: 必须有workspace_id和repo_id
// - session: 必须有workspace_id和session_id
// 设计目的：避免user/project/repo记忆互相污染
func ValidateScope(scope, workspaceID, userID, projectID, repoID, sessionID string) error {
	switch scope {
	case ScopeUserGlobal:
		if userID == "" {
			return fmt.Errorf("SCOPE_INVALID: user_global memory requires user_id")
		}
		if projectID != "" || repoID != "" {
			return fmt.Errorf("SCOPE_INVALID: user_global memory must not include project_id or repo_id")
		}
	case ScopeProjectLocal:
		if workspaceID == "" || projectID == "" {
			return fmt.Errorf("SCOPE_INVALID: project_local memory requires workspace_id and project_id")
		}
	case ScopeRepoLocal:
		if workspaceID == "" || repoID == "" {
			return fmt.Errorf("SCOPE_INVALID: repo_local memory requires workspace_id and repo_id")
		}
	case ScopeSession:
		if workspaceID == "" || sessionID == "" {
			return fmt.Errorf("SCOPE_INVALID: session memory requires workspace_id and session_id")
		}
	default:
		return fmt.Errorf("SCOPE_INVALID: unsupported scope %q", scope)
	}
	return nil
}

// ValidateSearchScopes 校验检索请求中的 scope 与定位字段
// 校验规则：
// - user_global: 无需额外校验
// - project_local: 必须有workspace_id和project_id
// - repo_local: 必须有workspace_id和repo_id
// - session: 必须有workspace_id和session_id
// 设计目的：避免跨项目、跨仓库或跨会话误召回
func ValidateSearchScopes(scopes []string, workspaceID, projectID, repoID, sessionID string) error {
	for _, scope := range scopes {
		switch scope {
		case ScopeUserGlobal:
		case ScopeProjectLocal:
			if workspaceID == "" || projectID == "" {
				return fmt.Errorf("SCOPE_INVALID: project_local search requires workspace_id and project_id")
			}
		case ScopeRepoLocal:
			if workspaceID == "" || repoID == "" {
				return fmt.Errorf("SCOPE_INVALID: repo_local search requires workspace_id and repo_id")
			}
		case ScopeSession:
			if workspaceID == "" || sessionID == "" {
				return fmt.Errorf("SCOPE_INVALID: session search requires workspace_id and session_id")
			}
		default:
			return fmt.Errorf("SCOPE_INVALID: unsupported scope %q", scope)
		}
	}
	return nil
}

// validMemoryType 校验记忆类型是否合法
// 合法值：preference、decision、constraint、failure、project_fact、procedure、temporary_state、review_checkpoint
func validMemoryType(memoryType string) bool {
	switch memoryType {
	case TypePreference, TypeDecision, TypeConstraint, TypeFailure, TypeProjectFact, TypeProcedure, TypeTemporaryState, TypeReviewCheckpoint:
		return true
	default:
		return false
	}
}

// defaultStateAndTier 根据记忆类型和来源类型确定默认状态和层级
// 规则：
// - session作用域: stable + temporary
// - review_checkpoint: stable + long_term
// - decision/constraint: pending_review + long_term
// - failure且importance>=0.8: pending_review + long_term
// - user_declared: stable + durable
// - 其他: stable + long_term
func defaultStateAndTier(req RememberRequest) (string, string) {
	switch {
	case req.Scope == ScopeSession:
		return StateStable, TierTemporary
	case req.MemoryType == TypeReviewCheckpoint:
		return StateStable, TierLongTerm
	case req.MemoryType == TypeDecision || req.MemoryType == TypeConstraint:
		return StatePendingReview, TierLongTerm
	case req.MemoryType == TypeFailure && req.Importance >= 0.8:
		return StatePendingReview, TierLongTerm
	case req.SourceType == "user_declared":
		return StateStable, TierDurable
	default:
		return StateStable, TierLongTerm
	}
}
