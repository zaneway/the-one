package dream

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zaneway/theone/internal/memory"
)

const (
	actionWrite = "write"
	actionSkip  = "skip"
)

type Service struct {
	cfg     Config
	repo    Repository
	curator Curator
	mu      sync.Mutex
}

func NewService(cfg Config, repo Repository, curator Curator) *Service {
	cfg = normalizeConfig(cfg)
	return &Service{cfg: cfg, repo: repo, curator: curator}
}

func (s *Service) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	started := time.Now()
	if s.repo == nil {
		return RunResponse{}, fmt.Errorf("VALIDATION_FAILED: dream repository is required")
	}
	if strings.TrimSpace(s.cfg.Vault.Root) == "" {
		return RunResponse{}, fmt.Errorf("VALIDATION_FAILED: dream vault root is required")
	}
	memories, err := s.repo.ListMemoriesForDream(ctx, ListRequest{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		Limit:       req.Limit,
	})
	if err != nil {
		return RunResponse{}, err
	}
	relations, err := s.repo.ListRelationsForDream(ctx, ListRequest{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		RepoID:      req.RepoID,
		Limit:       req.Limit,
	})
	if err != nil {
		return RunResponse{}, err
	}
	oldManifest, err := s.readManifest()
	if err != nil {
		return RunResponse{}, err
	}
	projections, diagnostics, err := s.planProjections(ctx, memories, relations)
	if err != nil {
		return RunResponse{}, err
	}
	plans := make([]projectionPlan, 0, len(projections)+1)
	for _, projection := range projections {
		plans = append(plans, projection)
	}
	if len(plans) > 0 {
		plans = append(plans, s.planMOC(plans))
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Path < plans[j].Path })

	newManifest := buildManifest(plans)
	resp := RunResponse{DryRun: req.DryRun, Planned: len(plans), Diagnostics: diagnostics, Items: make([]PlanItem, 0, len(plans)), StartedAt: started}
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return resp, err
		}
		action := s.planAction(oldManifest, plan)
		resp.Items = append(resp.Items, PlanItem{
			ProjectionID: plan.ProjectionID,
			MemoryID:     plan.MemoryID,
			SourceIDs:    append([]string(nil), plan.SourceMemoryIDs...),
			Mode:         plan.Mode,
			Path:         plan.Path,
			Action:       action,
			RouteKey:     plan.RouteKey,
		})
		if action == actionSkip {
			resp.Skipped++
		}
		if req.DryRun {
			continue
		}
		if action == actionSkip {
			continue
		}
		if err := s.writeProjection(ctx, oldManifest, plan); err != nil {
			return resp, err
		}
		resp.Written++
	}
	if !req.DryRun {
		if err := ctx.Err(); err != nil {
			return resp, err
		}
		if err := s.removeStaleManagedFiles(ctx, oldManifest, newManifest); err != nil {
			return resp, err
		}
		if !manifestItemsEqual(oldManifest, newManifest) {
			if err := s.writeManifest(ctx, newManifest); err != nil {
				return resp, err
			}
		}
	}
	resp.EndedAt = time.Now()
	return resp, nil
}

func (s *Service) planProjections(ctx context.Context, memories []MemoryRecord, relations []RelationRecord) ([]projectionPlan, []string, error) {
	remaining := map[string]MemoryRecord{}
	for _, item := range memories {
		if item.ID == "" {
			continue
		}
		remaining[item.ID] = item
	}
	plans := make([]projectionPlan, 0, len(memories))
	diagnostics := []string{}
	if s.cfg.Curation.Enabled && s.curator != nil {
		result, err := s.curator.Curate(ctx, CurationInput{Memories: memories, Relations: relations, Config: s.cfg.Curation})
		if err != nil {
			if !s.cfg.Curation.FallbackRules {
				return nil, nil, fmt.Errorf("DREAM_CURATION_FAILED: %w", err)
			}
			diagnostics = append(diagnostics, "dream curation failed; fallback to rule export: "+err.Error())
		}
		if err == nil {
			minGroup := curationMinGroupSize(s.cfg.Curation.MinGroupSize)
			for _, group := range result.Groups {
				if len(group.SourceMemoryIDs) < minGroup {
					continue
				}
				sourceMemories := make([]MemoryRecord, 0, len(group.SourceMemoryIDs))
				for _, id := range group.SourceMemoryIDs {
					item, ok := remaining[id]
					if !ok {
						continue
					}
					if isStandaloneDreamMemory(item) {
						continue
					}
					sourceMemories = append(sourceMemories, item)
					delete(remaining, id)
				}
				if len(sourceMemories) < minGroup {
					for _, item := range sourceMemories {
						remaining[item.ID] = item
					}
					continue
				}
				plans = append(plans, s.planConsolidated(group, sourceMemories, relations))
			}
		}
	} else if s.cfg.Curation.Enabled && s.curator == nil && !s.cfg.Curation.FallbackRules {
		return nil, nil, fmt.Errorf("DREAM_CURATION_FAILED: dream curator is required")
	}
	for _, item := range remaining {
		plans = append(plans, s.planMemory(item, relations, remaining))
	}
	return plans, diagnostics, nil
}

func (s *Service) planMemory(item MemoryRecord, relations []RelationRecord, memoryByID map[string]MemoryRecord) projectionPlan {
	route := s.routeMemory(item)
	path := filepath.ToSlash(filepath.Join(route.Dir, slugTitle(item.Title, item.ID)+"--"+item.ID+".md"))
	body := renderMemoryNote(item, relations, memoryByID)
	return projectionPlan{
		ProjectionID:    item.ID,
		MemoryID:        item.ID,
		SourceMemoryIDs: []string{item.ID},
		Mode:            NoteModeMemory,
		Path:            path,
		RouteKey:        route.Key,
		Body:            body,
		ContentHash:     hashString(body),
	}
}

func (s *Service) planConsolidated(group CurationGroup, memories []MemoryRecord, relations []RelationRecord) projectionPlan {
	projectionID := strings.TrimSpace(group.ProjectionID)
	if projectionID == "" {
		projectionID = "topic_" + hashShort(strings.Join(group.SourceMemoryIDs, "\x00"))
	}
	route := s.routeGroup(group, memories)
	path := filepath.ToSlash(filepath.Join(route.Dir, slugTitle(group.Title, projectionID)+"--"+projectionID+".md"))
	body := renderConsolidatedNote(group, memories, relations)
	return projectionPlan{
		ProjectionID:    projectionID,
		SourceMemoryIDs: append([]string(nil), group.SourceMemoryIDs...),
		Mode:            NoteModeConsolidated,
		Path:            path,
		RouteKey:        route.Key,
		Body:            body,
		ContentHash:     hashString(body),
	}
}

func (s *Service) planMOC(plans []projectionPlan) projectionPlan {
	body := renderMOC(plans)
	path := filepath.ToSlash(filepath.Join(s.cfg.Vault.Directories.MOC, "all-dream-projections.md"))
	return projectionPlan{
		ProjectionID: "moc_all_dream_projections",
		Mode:         NoteModeMOC,
		Path:         path,
		RouteKey:     "moc:all",
		Body:         body,
		ContentHash:  hashString(body),
	}
}

func (s *Service) writeProjection(ctx context.Context, oldManifest manifestFile, plan projectionPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := safeJoin(s.cfg.Vault.Root, plan.Path)
	if err != nil {
		return err
	}
	if err := s.ensureProjectionWritable(fullPath, plan.Path, oldManifest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("DREAM_EXPORT_FAILED: mkdir %s: %w", plan.Path, err)
	}
	if err := os.WriteFile(fullPath, []byte(plan.Body), 0o644); err != nil {
		return fmt.Errorf("DREAM_EXPORT_FAILED: write %s: %w", plan.Path, err)
	}
	if err := os.Chmod(fullPath, 0o444); err != nil {
		return fmt.Errorf("DREAM_EXPORT_FAILED: chmod readonly %s: %w", plan.Path, err)
	}
	return nil
}

func (s *Service) manifestPath() string {
	return filepath.ToSlash(filepath.Join(s.cfg.Vault.SystemDir, "dream-manifest.json"))
}

func (s *Service) readManifest() (manifestFile, error) {
	path := s.manifestPath()
	fullPath, err := safeJoin(s.cfg.Vault.Root, path)
	if err != nil {
		return manifestFile{Items: map[string]manifestItem{}}, err
	}
	data, err := os.ReadFile(fullPath)
	if os.IsNotExist(err) {
		return manifestFile{Items: map[string]manifestItem{}}, nil
	}
	if err != nil {
		return manifestFile{}, fmt.Errorf("DREAM_EXPORT_FAILED: read manifest: %w", err)
	}
	var manifest manifestFile
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifestFile{}, fmt.Errorf("DREAM_EXPORT_FAILED: decode manifest: %w", err)
	}
	if manifest.Items == nil {
		manifest.Items = map[string]manifestItem{}
	}
	return manifest, nil
}

func buildManifest(plans []projectionPlan) manifestFile {
	manifest := manifestFile{GeneratedAt: time.Now(), Items: map[string]manifestItem{}}
	for _, plan := range plans {
		manifest.Items[plan.ProjectionID] = manifestItem{
			Path:            plan.Path,
			Mode:            plan.Mode,
			MemoryID:        plan.MemoryID,
			SourceMemoryIDs: append([]string(nil), plan.SourceMemoryIDs...),
			RouteKey:        plan.RouteKey,
			ContentHash:     plan.ContentHash,
		}
	}
	return manifest
}

func (s *Service) planAction(old manifestFile, plan projectionPlan) string {
	item, ok := old.Items[plan.ProjectionID]
	if ok && item.Path == plan.Path && item.ContentHash == plan.ContentHash {
		return actionSkip
	}
	return actionWrite
}

func (s *Service) writeManifest(ctx context.Context, manifest manifestFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.ToSlash(filepath.Join(s.cfg.Vault.SystemDir, "dream-manifest.json"))
	fullPath, err := safeJoin(s.cfg.Vault.Root, path)
	if err != nil {
		return err
	}
	if err := ensureWritableVaultPath(s.cfg.Vault.Root, fullPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("DREAM_EXPORT_FAILED: mkdir manifest: %w", err)
	}
	manifest.GeneratedAt = time.Now()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return fmt.Errorf("DREAM_EXPORT_FAILED: write manifest: %w", err)
	}
	return nil
}

func (s *Service) removeStaleManagedFiles(ctx context.Context, old, current manifestFile) error {
	for projectionID, oldItem := range old.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		newItem, ok := current.Items[projectionID]
		if ok && newItem.Path == oldItem.Path {
			continue
		}
		if oldItem.Path == "" || s.isUserNotePath(oldItem.Path) {
			continue
		}
		fullPath, err := safeJoin(s.cfg.Vault.Root, oldItem.Path)
		if err != nil {
			return err
		}
		if err := ensureWritableVaultPath(s.cfg.Vault.Root, fullPath); err != nil {
			return err
		}
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("DREAM_EXPORT_FAILED: remove stale projection %s: %w", oldItem.Path, err)
		}
	}
	return nil
}

func isStandaloneDreamMemory(item MemoryRecord) bool {
	switch item.MemoryType {
	case memory.TypeDecision, memory.TypeConstraint, memory.TypeFailure, memory.TypePreference, memory.TypeReviewCheckpoint:
		return true
	default:
		return false
	}
}

func (s *Service) ensureProjectionWritable(fullPath, relativePath string, oldManifest manifestFile) error {
	if err := ensureWritableVaultPath(s.cfg.Vault.Root, fullPath); err != nil {
		return err
	}
	if _, err := os.Lstat(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("DREAM_EXPORT_FAILED: stat %s: %w", relativePath, err)
	}
	for _, item := range oldManifest.Items {
		if item.Path == relativePath {
			return nil
		}
	}
	return fmt.Errorf("DREAM_EXPORT_FAILED: refuse to overwrite non-system file %s", relativePath)
}

func (s *Service) isUserNotePath(path string) bool {
	userDir := strings.Trim(filepath.ToSlash(filepath.Clean(s.cfg.Vault.UserNotesDir)), "/")
	cleaned := strings.Trim(filepath.ToSlash(filepath.Clean(path)), "/")
	return userDir != "" && (cleaned == userDir || strings.HasPrefix(cleaned, userDir+"/"))
}

func manifestItemsEqual(a, b manifestFile) bool {
	if len(a.Items) != len(b.Items) {
		return false
	}
	for id, left := range a.Items {
		right, ok := b.Items[id]
		if !ok {
			return false
		}
		if left.Path != right.Path || left.Mode != right.Mode || left.MemoryID != right.MemoryID ||
			left.RouteKey != right.RouteKey || left.ContentHash != right.ContentHash ||
			!stringSlicesEqual(left.SourceMemoryIDs, right.SourceMemoryIDs) {
			return false
		}
	}
	return true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeConfig(cfg Config) Config {
	if cfg.Vault.Root == "" {
		cfg.Vault = DefaultVaultConfig("")
		return cfg
	}
	defaultVault := DefaultVaultConfig(cfg.Vault.Root)
	if cfg.Vault.SystemDir == "" {
		cfg.Vault.SystemDir = defaultVault.SystemDir
	}
	if cfg.Vault.UserNotesDir == "" {
		cfg.Vault.UserNotesDir = defaultVault.UserNotesDir
	}
	cfg.Vault.Directories = mergeDirectories(defaultVault.Directories, cfg.Vault.Directories)
	if len(cfg.Vault.MemoryTypeDirs) == 0 {
		cfg.Vault.MemoryTypeDirs = defaultVault.MemoryTypeDirs
	}
	if len(cfg.Vault.TopicDirs) == 0 {
		cfg.Vault.TopicDirs = defaultVault.TopicDirs
	}
	if cfg.Curation.MinGroupSize <= 0 {
		cfg.Curation.MinGroupSize = 2
	}
	return cfg
}

func mergeDirectories(defaults, override DirectoryConfig) DirectoryConfig {
	if override.Inbox == "" {
		override.Inbox = defaults.Inbox
	}
	if override.Projects == "" {
		override.Projects = defaults.Projects
	}
	if override.Knowledge == "" {
		override.Knowledge = defaults.Knowledge
	}
	if override.Thinking == "" {
		override.Thinking = defaults.Thinking
	}
	if override.Skills == "" {
		override.Skills = defaults.Skills
	}
	if override.MOC == "" {
		override.MOC = defaults.MOC
	}
	if override.Archive == "" {
		override.Archive = defaults.Archive
	}
	return override
}

func safeJoin(root, relative string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("VALIDATION_FAILED: dream vault root is required")
	}
	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("VALIDATION_FAILED: unsafe dream export path")
	}
	return filepath.Join(root, cleaned), nil
}

func ensureWritableVaultPath(root, fullPath string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("DREAM_EXPORT_FAILED: resolve vault root: %w", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("DREAM_EXPORT_FAILED: resolve export path: %w", err)
	}
	rootPrefix := absRoot + string(filepath.Separator)
	if absPath != absRoot && !strings.HasPrefix(absPath, rootPrefix) {
		return fmt.Errorf("VALIDATION_FAILED: dream export path escapes vault root")
	}
	if fi, err := os.Lstat(absPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("VALIDATION_FAILED: refuse to write through symlink %s", fullPath)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("DREAM_EXPORT_FAILED: stat %s: %w", fullPath, err)
	}
	return nil
}

func slugTitle(title, fallback string) string {
	text := strings.ToLower(strings.TrimSpace(title))
	if text == "" {
		text = strings.ToLower(strings.TrimSpace(fallback))
	}
	re := regexp.MustCompile(`[^a-z0-9]+`)
	text = strings.Trim(re.ReplaceAllString(text, "-"), "-")
	if text == "" {
		return "memory"
	}
	if len(text) > 80 {
		text = strings.Trim(text[:80], "-")
	}
	return text
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}

func hashShort(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])[:12]
}

func curationMinGroupSize(value int) int {
	if value <= 0 {
		return 2
	}
	return value
}

type projectionPlan struct {
	ProjectionID    string
	MemoryID        string
	SourceMemoryIDs []string
	Mode            string
	Path            string
	RouteKey        string
	Body            string
	ContentHash     string
}

type routeResult struct {
	Dir string
	Key string
}

type manifestFile struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Items       map[string]manifestItem `json:"items"`
}

type manifestItem struct {
	Path            string   `json:"path"`
	Mode            string   `json:"mode"`
	MemoryID        string   `json:"memory_id,omitempty"`
	SourceMemoryIDs []string `json:"source_memory_ids,omitempty"`
	RouteKey        string   `json:"route_key"`
	ContentHash     string   `json:"content_hash"`
}
