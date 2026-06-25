package dream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zaneway/theone/internal/memory"
)

func TestServiceSkipsStandaloneMemoriesInConsolidation(t *testing.T) {
	root := t.TempDir()
	curator := CuratorFunc(func(ctx context.Context, input CurationInput) (CurationResult, error) {
		return CurationResult{Groups: []CurationGroup{{
			ProjectionID:    "topic_should_not_merge",
			Title:           "Should not merge decision",
			Summary:         "Decision must stay standalone.",
			SourceMemoryIDs: []string{"mem_decision_001", "mem_fact_001"},
		}}}, nil
	})
	service := NewService(Config{
		Enabled: true,
		Vault:   DefaultVaultConfig(root),
		Curation: CurationConfig{
			Enabled:      true,
			MinGroupSize: 2,
		},
	}, fakeRepository{memories: []MemoryRecord{
		{ID: "mem_decision_001", Title: "Decision", Content: "Standalone decision.", MemoryType: memory.TypeDecision, ProjectID: "the-one", State: memory.StateStable},
		{ID: "mem_fact_001", Title: "Fact", Content: "Only one consolidatable fact.", MemoryType: memory.TypeProjectFact, ProjectID: "the-one", State: memory.StateStable},
	}}, curator)

	resp, err := service.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, item := range resp.Items {
		if item.Mode == NoteModeConsolidated {
			t.Fatalf("items = %+v, did not expect consolidated note", resp.Items)
		}
	}
}

func TestServiceRefusesToOverwriteNonSystemFile(t *testing.T) {
	root := t.TempDir()
	vault := DefaultVaultConfig(root)
	repo := fakeRepository{memories: []MemoryRecord{{
		ID:         "mem_collision",
		Title:      "User note",
		Content:    "Would collide with user file.",
		MemoryType: memory.TypeProjectFact,
		ProjectID:  "the-one",
		State:      memory.StateStable,
	}}}
	service := NewService(Config{Enabled: true, Vault: vault}, repo, nil)
	preview, err := service.Run(context.Background(), RunRequest{DryRun: true})
	if err != nil {
		t.Fatalf("dry Run() error = %v", err)
	}
	var targetPath string
	for _, item := range preview.Items {
		if item.MemoryID == "mem_collision" {
			targetPath = item.Path
			break
		}
	}
	if targetPath == "" {
		t.Fatalf("preview items = %+v, want memory export path", preview.Items)
	}
	fullPath := filepath.Join(root, filepath.FromSlash(targetPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("user content"), 0o644); err != nil {
		t.Fatalf("write user file: %v", err)
	}
	_, err = service.Run(context.Background(), RunRequest{})
	if err == nil || !strings.Contains(err.Error(), "non-system file") {
		t.Fatalf("Run() error = %v, want non-system file refusal", err)
	}
}
