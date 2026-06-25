package dream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

type fakeRepository struct {
	memories  []MemoryRecord
	relations []RelationRecord
}

func (r fakeRepository) ListMemoriesForDream(ctx context.Context, req ListRequest) ([]MemoryRecord, error) {
	return append([]MemoryRecord(nil), r.memories...), nil
}

func (r fakeRepository) ListRelationsForDream(ctx context.Context, req ListRequest) ([]RelationRecord, error) {
	return append([]RelationRecord(nil), r.relations...), nil
}

func TestServiceDryRunPlansConfiguredProjectPathWithoutWritingFiles(t *testing.T) {
	root := t.TempDir()
	service := NewService(Config{
		Enabled: true,
		Vault: VaultConfig{
			Root:      root,
			SystemDir: ".theone",
			Directories: DirectoryConfig{
				Projects:  "engineering",
				Knowledge: "domains",
				MOC:       "maps",
				Inbox:     "inbox",
				Archive:   "archive",
			},
			MemoryTypeDirs: map[string]string{
				memory.TypeDecision: "decisions",
			},
		},
	}, fakeRepository{
		memories: []MemoryRecord{{
			ID:          "mem_decision_001",
			Title:       "Relation expansion depth one",
			Content:     "Relation expansion must stay depth=1.",
			MemoryType:  memory.TypeDecision,
			Scope:       memory.ScopeProjectLocal,
			WorkspaceID: "ws",
			ProjectID:   "the-one",
			State:       memory.StateStable,
			Tier:        memory.TierDurable,
			Importance:  0.9,
			Confidence:  0.8,
			UpdatedAt:   time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC),
		}},
	}, nil)

	resp, err := service.Run(context.Background(), RunRequest{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.DryRun != true || resp.Planned != 2 || resp.Written != 0 {
		t.Fatalf("Run() = %+v, want dry-run planned=2 written=0", resp)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(resp.Items))
	}
	wantPath := filepath.ToSlash(filepath.Join("engineering", "the-one", "decisions", "relation-expansion-depth-one--mem_decision_001.md"))
	var found bool
	for _, item := range resp.Items {
		if item.Path == wantPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("planned items = %+v, want path %q", resp.Items, wantPath)
	}
	if _, err := os.Stat(filepath.Join(root, wantPath)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote file or unexpected stat err: %v", err)
	}
}

func TestServiceWritesReadonlyMarkdownWithRelationsAndManifest(t *testing.T) {
	root := t.TempDir()
	repo := fakeRepository{
		memories: []MemoryRecord{
			{
				ID:          "mem_decision_001",
				Title:       "Relation expansion depth one",
				Content:     "Relation expansion must stay depth=1.",
				MemoryType:  memory.TypeDecision,
				Scope:       memory.ScopeProjectLocal,
				WorkspaceID: "ws",
				ProjectID:   "the-one",
				State:       memory.StateStable,
				Tier:        memory.TierDurable,
				Importance:  0.9,
				Confidence:  0.8,
				UpdatedAt:   time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC),
			},
			{
				ID:          "mem_constraint_001",
				Title:       "No Vault online read",
				Content:     "Online retrieval must not read Obsidian Vault.",
				MemoryType:  memory.TypeConstraint,
				Scope:       memory.ScopeProjectLocal,
				WorkspaceID: "ws",
				ProjectID:   "the-one",
				State:       memory.StateStable,
				Tier:        memory.TierDurable,
				Importance:  0.8,
				Confidence:  0.8,
				UpdatedAt:   time.Date(2026, 6, 24, 8, 1, 0, 0, time.UTC),
			},
		},
		relations: []RelationRecord{{
			SourceID:     "mem_decision_001",
			TargetID:     "mem_constraint_001",
			RelationType: "supports",
			Weight:       1,
			UpdatedAt:    time.Date(2026, 6, 24, 8, 2, 0, 0, time.UTC),
		}},
	}
	service := NewService(Config{Enabled: true, Vault: DefaultVaultConfig(root)}, repo, nil)

	resp, err := service.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Written != 3 {
		t.Fatalf("written = %d, want 3 memory/moc files", resp.Written)
	}
	notePath := filepath.Join(root, "10-projects", "the-one", "decisions", "relation-expansion-depth-one--mem_decision_001.md")
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"readonly: true",
		"memory_id: mem_decision_001",
		"## Memory",
		"Relation expansion must stay depth=1.",
		"## Relations",
		"- supports [[no-vault-online-read--mem_constraint_001]]",
		"## Context",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("note missing %q:\n%s", want, text)
		}
	}
	manifestPath := filepath.Join(root, ".theone", "dream-manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifest), "mem_decision_001") || !strings.Contains(string(manifest), "relation-expansion-depth-one--mem_decision_001.md") {
		t.Fatalf("manifest missing exported memory/path: %s", manifest)
	}
}

func TestServiceWritesManagedMarkdownAsFilesystemReadonly(t *testing.T) {
	root := t.TempDir()
	repo := fakeRepository{memories: []MemoryRecord{{
		ID:         "mem_readonly_001",
		Title:      "Readonly managed note",
		Content:    "Managed Markdown should not be casually edited in Obsidian.",
		MemoryType: memory.TypeDecision,
		Scope:      memory.ScopeProjectLocal,
		ProjectID:  "the-one",
		State:      memory.StateStable,
		Tier:       memory.TierDurable,
	}}}
	service := NewService(Config{Enabled: true, Vault: DefaultVaultConfig(root)}, repo, nil)

	if _, err := service.Run(context.Background(), RunRequest{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	notePath := filepath.Join(root, "10-projects", "the-one", "decisions", "readonly-managed-note--mem_readonly_001.md")
	info, err := os.Stat(notePath)
	if err != nil {
		t.Fatalf("stat note: %v", err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("note mode = %v, want readonly managed markdown", info.Mode().Perm())
	}
}

func TestServiceConsolidatesTopicWhenCuratorGroupsMemories(t *testing.T) {
	root := t.TempDir()
	curator := CuratorFunc(func(ctx context.Context, input CurationInput) (CurationResult, error) {
		return CurationResult{Groups: []CurationGroup{{
			ProjectionID:     "topic_memory_system",
			TopicKey:         "memory-system",
			Title:            "Memory system export design",
			Summary:          "Dream export keeps SQLite as fact source and Obsidian as readonly projection.",
			SourceMemoryIDs:  []string{"mem_decision_001", "mem_fact_001"},
			SourceMap:        map[string][]string{"summary": {"mem_decision_001", "mem_fact_001"}},
			RouteCategory:    RouteKnowledge,
			RouteSubject:     "memory-systems",
			MemoryTypeBucket: "decisions",
		}}}, nil
	})
	service := NewService(Config{
		Enabled: true,
		Vault:   DefaultVaultConfig(root),
		Curation: CurationConfig{
			Enabled:       true,
			MinGroupSize:  2,
			FallbackRules: true,
		},
	}, fakeRepository{memories: []MemoryRecord{
		{ID: "mem_decision_001", Title: "Dream readonly", Content: "Dream is readonly.", MemoryType: memory.TypeDecision, Scope: memory.ScopeProjectLocal, ProjectID: "the-one", State: memory.StateStable, Tier: memory.TierDurable, Importance: 0.8},
		{ID: "mem_fact_001", Title: "Obsidian graph", Content: "Obsidian graph reads wikilinks.", MemoryType: memory.TypeProjectFact, Scope: memory.ScopeProjectLocal, ProjectID: "the-one", State: memory.StateStable, Tier: memory.TierShortTerm, Importance: 0.5},
	}}, curator)

	resp, err := service.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Written != 2 {
		t.Fatalf("written = %d, want topic note + moc", resp.Written)
	}
	topicPath := filepath.Join(root, "20-knowledge", "memory-systems", "decisions", "memory-system-export-design--topic_memory_system.md")
	data, err := os.ReadFile(topicPath)
	if err != nil {
		t.Fatalf("read topic note: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"note_mode: consolidated",
		"topic_key: memory-system",
		"source_memory_ids:",
		"  - mem_decision_001",
		"  - mem_fact_001",
		"source_map:",
		"## Summary",
		"Dream export keeps SQLite as fact source",
		"## Source Memories",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("topic note missing %q:\n%s", want, text)
		}
	}
}

func TestServiceSkipsUnchangedProjectionsOnSecondRun(t *testing.T) {
	root := t.TempDir()
	repo := fakeRepository{memories: []MemoryRecord{{
		ID:          "mem_decision_001",
		Title:       "Stable projection",
		Content:     "Dream export should not rewrite unchanged projection files.",
		MemoryType:  memory.TypeDecision,
		Scope:       memory.ScopeProjectLocal,
		WorkspaceID: "ws",
		ProjectID:   "the-one",
		State:       memory.StateStable,
		Tier:        memory.TierDurable,
		UpdatedAt:   time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC),
	}}}
	service := NewService(Config{Enabled: true, Vault: DefaultVaultConfig(root)}, repo, nil)

	first, err := service.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if first.Written != 2 || first.Skipped != 0 {
		t.Fatalf("first Run() = %+v, want note+moc written", first)
	}
	notePath := filepath.Join(root, "10-projects", "the-one", "decisions", "stable-projection--mem_decision_001.md")
	firstInfo, err := os.Stat(notePath)
	if err != nil {
		t.Fatalf("stat first note: %v", err)
	}

	second, err := service.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second.Written != 0 || second.Skipped != 2 {
		t.Fatalf("second Run() = %+v, want unchanged note+moc skipped", second)
	}
	for _, item := range second.Items {
		if item.Action != actionSkip {
			t.Fatalf("second item = %+v, want skip action", item)
		}
	}
	secondInfo, err := os.Stat(notePath)
	if err != nil {
		t.Fatalf("stat second note: %v", err)
	}
	if !secondInfo.ModTime().Equal(firstInfo.ModTime()) {
		t.Fatalf("note mtime changed from %s to %s, want unchanged file not rewritten", firstInfo.ModTime(), secondInfo.ModTime())
	}
}

func TestServiceRemovesStaleManagedFilesButKeepsUserNotes(t *testing.T) {
	root := t.TempDir()
	service := NewService(Config{Enabled: true, Vault: DefaultVaultConfig(root)}, fakeRepository{}, nil)
	staleRel := filepath.ToSlash(filepath.Join("10-projects", "the-one", "decisions", "old--mem_old.md"))
	stalePath := filepath.Join(root, filepath.FromSlash(staleRel))
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatalf("mkdir stale: %v", err)
	}
	if err := os.WriteFile(stalePath, []byte("old managed projection"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	userNotePath := filepath.Join(root, "99-user-notes", "manual.md")
	if err := os.MkdirAll(filepath.Dir(userNotePath), 0o755); err != nil {
		t.Fatalf("mkdir user note: %v", err)
	}
	if err := os.WriteFile(userNotePath, []byte("manual note"), 0o644); err != nil {
		t.Fatalf("write user note: %v", err)
	}
	manifestPath := filepath.Join(root, ".theone", "dream-manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("mkdir manifest: %v", err)
	}
	manifest := `{"generated_at":"2026-06-24T08:00:00Z","items":{"mem_old":{"path":"` + staleRel + `","mode":"memory","memory_id":"mem_old","route_key":"project:the-one:decisions","content_hash":"sha256:old"}}}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	resp, err := service.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.Planned != 0 {
		t.Fatalf("Run() = %+v, want empty export plan", resp)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale managed file stat err = %v, want removed", err)
	}
	if _, err := os.Stat(userNotePath); err != nil {
		t.Fatalf("user note stat err = %v, want preserved", err)
	}
}

func TestServiceReturnsCuratorErrorWhenFallbackDisabled(t *testing.T) {
	root := t.TempDir()
	curator := CuratorFunc(func(ctx context.Context, input CurationInput) (CurationResult, error) {
		return CurationResult{}, errors.New("model unavailable")
	})
	service := NewService(Config{
		Enabled: true,
		Vault:   DefaultVaultConfig(root),
		Curation: CurationConfig{
			Enabled:       true,
			MinGroupSize:  2,
			FallbackRules: false,
		},
	}, fakeRepository{memories: []MemoryRecord{
		{ID: "mem_1", Title: "One", Content: "First fragmented memory.", MemoryType: memory.TypeDecision, ProjectID: "the-one", State: memory.StateStable},
		{ID: "mem_2", Title: "Two", Content: "Second fragmented memory.", MemoryType: memory.TypeConstraint, ProjectID: "the-one", State: memory.StateStable},
	}}, curator)

	if _, err := service.Run(context.Background(), RunRequest{}); err == nil || !strings.Contains(err.Error(), "DREAM_CURATION_FAILED") {
		t.Fatalf("Run() error = %v, want curation failure when fallback disabled", err)
	}
}

func TestServiceRecordsDiagnosticWhenCuratorFallbacksToRuleExport(t *testing.T) {
	root := t.TempDir()
	curator := CuratorFunc(func(ctx context.Context, input CurationInput) (CurationResult, error) {
		return CurationResult{}, errors.New("model timeout")
	})
	service := NewService(Config{
		Enabled: true,
		Vault:   DefaultVaultConfig(root),
		Curation: CurationConfig{
			Enabled:       true,
			MinGroupSize:  2,
			FallbackRules: true,
		},
	}, fakeRepository{memories: []MemoryRecord{
		{ID: "mem_1", Title: "Fallback", Content: "Fallback should export raw memory.", MemoryType: memory.TypeDecision, ProjectID: "the-one", State: memory.StateStable},
	}}, curator)

	resp, err := service.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(resp.Diagnostics) == 0 || !strings.Contains(resp.Diagnostics[0], "dream curation failed") {
		t.Fatalf("diagnostics = %+v, want curation fallback diagnostic", resp.Diagnostics)
	}
	if resp.Written != 2 {
		t.Fatalf("Run() = %+v, want fallback memory note + moc", resp)
	}
}

func TestServiceSanitizesModelRouteFields(t *testing.T) {
	root := t.TempDir()
	curator := CuratorFunc(func(ctx context.Context, input CurationInput) (CurationResult, error) {
		return CurationResult{Groups: []CurationGroup{{
			ProjectionID:     "topic_bad_route",
			Title:            "Bad Route",
			Summary:          "Route fields from model must not define arbitrary directory structure.",
			SourceMemoryIDs:  []string{"mem_1", "mem_2"},
			RouteCategory:    RouteKnowledge,
			RouteSubject:     "../outside/custom",
			MemoryTypeBucket: "../bad",
		}}}, nil
	})
	service := NewService(Config{
		Enabled: true,
		Vault:   DefaultVaultConfig(root),
		Curation: CurationConfig{
			Enabled:       true,
			MinGroupSize:  2,
			FallbackRules: false,
		},
	}, fakeRepository{memories: []MemoryRecord{
		{ID: "mem_1", Title: "One", Content: "One.", MemoryType: memory.TypeDecision, ProjectID: "the-one", State: memory.StateStable},
		{ID: "mem_2", Title: "Two", Content: "Two.", MemoryType: memory.TypeDecision, ProjectID: "the-one", State: memory.StateStable},
	}}, curator)

	resp, err := service.Run(context.Background(), RunRequest{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var topicPath string
	for _, item := range resp.Items {
		if item.Mode == NoteModeConsolidated {
			topicPath = item.Path
		}
	}
	if strings.Contains(topicPath, "..") || strings.Contains(topicPath, "/custom/") || strings.Contains(topicPath, "/bad/") || !strings.Contains(topicPath, "/decisions/") {
		t.Fatalf("topic path = %q, want sanitized local route fields", topicPath)
	}
}
