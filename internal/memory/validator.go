package memory

import (
	"fmt"
	"strings"

	"github.com/zaneway/the-one/internal/config"
)

// NormalizeRemember 填充 P1 默认字段，并执行 memory_type/scope 的基础合法性检查。
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

// ValidateScope 执行 P1 scope validator，避免 user/project/repo 记忆互相污染。
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

// ValidateSearchScopes 校验检索请求中的 scope 与定位字段，避免跨项目、跨仓库或跨会话误召回。
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

func validMemoryType(memoryType string) bool {
	switch memoryType {
	case TypePreference, TypeDecision, TypeConstraint, TypeFailure, TypeProjectFact, TypeProcedure, TypeTemporaryState, TypeReviewCheckpoint:
		return true
	default:
		return false
	}
}

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
