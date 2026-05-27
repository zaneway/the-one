package memory

import (
	"testing"

	"github.com/zaneway/theone/internal/config"
)

func TestValidateScopeRejectsProjectLocalWithoutProject(t *testing.T) {
	err := ValidateScope(ScopeProjectLocal, "ws", "user", "", "", "")
	if err == nil {
		t.Fatal("ValidateScope() error = nil, want SCOPE_INVALID")
	}
}

func TestNormalizeRememberFillsUserGlobalDefaults(t *testing.T) {
	cfg := config.Default().Memory
	req := RememberRequest{
		Content:    "用户偏好先分析架构边界。",
		MemoryType: TypePreference,
		Scope:      ScopeUserGlobal,
	}
	if err := NormalizeRemember(cfg, &req); err != nil {
		t.Fatalf("NormalizeRemember() error = %v", err)
	}
	if req.UserID != cfg.DefaultUserID {
		t.Fatalf("user id = %q, want %q", req.UserID, cfg.DefaultUserID)
	}
	if req.SourceType != "user_declared" {
		t.Fatalf("source type = %q, want user_declared", req.SourceType)
	}
}
