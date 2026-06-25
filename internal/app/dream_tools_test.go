package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/dream"
)

func TestAppRegistersDreamExportTool(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Dream.Enabled = true
	cfg.Dream.Vault.Root = filepath.Join(t.TempDir(), "vault")

	app, err := New(ctx, cfg, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	rawDryRun, toolErr := app.CallTool(ctx, "memory.dream.export", dream.RunRequest{DryRun: true, WorkspaceID: "ws", ProjectID: "the-one"})
	if toolErr != nil {
		t.Fatalf("memory.dream.export dry-run error = %v", toolErr)
	}
	dryRun := rawDryRun.(dream.RunResponse)
	if !dryRun.DryRun || dryRun.Written != 0 {
		t.Fatalf("dryRun = %+v, want dry run without writes", dryRun)
	}

	rawApply, toolErr := app.CallTool(ctx, "memory.dream.export", dream.RunRequest{WorkspaceID: "ws", ProjectID: "the-one"})
	if toolErr != nil {
		t.Fatalf("memory.dream.export apply error = %v", toolErr)
	}
	apply := rawApply.(dream.RunResponse)
	if apply.DryRun || apply.Written != 0 || apply.Planned != 0 {
		t.Fatalf("apply = %+v, want empty apply against empty repository", apply)
	}
}

func TestAppRejectsDreamCurationWithoutExternalModel(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "memory.db")
	cfg.Dream.Enabled = true
	cfg.Dream.Vault.Root = filepath.Join(t.TempDir(), "vault")
	cfg.Dream.Curation.Enabled = true
	cfg.Processor.Provider = "rule_based"

	if _, err := New(ctx, cfg, "test"); err == nil {
		t.Fatal("New() error = nil, want dream curation external model config error")
	}
}
