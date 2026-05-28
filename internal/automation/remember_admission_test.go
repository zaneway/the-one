package automation_test

import (
	"context"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/zaneway/theone/internal/automation"
	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
	"github.com/zaneway/theone/internal/processor"
	"github.com/zaneway/theone/internal/storage/sqlite"
)

func TestDecideRememberAllowsUserDeclaredPreference(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	decision, err := service.DecideRemember(ctx, memory.RememberRequest{
		Content:    "用户偏好技术方案先分析架构边界与风险。",
		MemoryType: memory.TypePreference,
		Scope:      memory.ScopeUserGlobal,
		UserID:     "local_default_user",
		SourceType: "user_declared",
		Confidence: 0.9,
		Importance: 0.8,
		Evidence: memory.EvidenceInput{
			InterpretedStatement: "用户声明偏好先架构分析。",
		},
	})
	if err != nil {
		t.Fatalf("DecideRemember() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision = %+v, want allowed explicit user preference", decision)
	}
}

func TestDecideRememberRejectsEmptyScopeContent(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	store, err := sqlite.Open(ctx, cfg.Storage, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := automation.NewService(cfg, store, processor.NewRuleBasedProvider())
	decision, err := service.DecideRemember(ctx, memory.RememberRequest{
		Content:    "普通闲聊内容，没有工程记忆价值。",
		MemoryType: memory.TypeTemporaryState,
		Scope:      "",
		SourceType: "agent_inferred",
	})
	if err != nil {
		t.Fatalf("DecideRemember() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("decision = %+v, want rejected for invalid scope", decision)
	}
}
