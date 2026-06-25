package dream

import (
	"path/filepath"
	"strings"

	"github.com/zaneway/theone/internal/memory"
)

func (s *Service) routeMemory(item MemoryRecord) routeResult {
	typeDir := s.memoryTypeDir(item.MemoryType)
	if item.State == memory.StateArchived {
		subject := firstNonEmpty(item.ProjectID, item.RepoID, "general")
		return routeResult{
			Dir: filepath.Join(s.cfg.Vault.Directories.Archive, slugTitle(subject, "archive"), typeDir),
			Key: RouteArchive + ":" + subject + ":" + typeDir,
		}
	}
	if item.ProjectID != "" || item.RepoID != "" {
		project := firstNonEmpty(item.ProjectID, item.RepoID)
		return routeResult{
			Dir: filepath.Join(s.cfg.Vault.Directories.Projects, slugTitle(project, "project"), typeDir),
			Key: RouteProject + ":" + project + ":" + typeDir,
		}
	}
	return routeResult{
		Dir: filepath.Join(s.cfg.Vault.Directories.Inbox, "ungrouped", typeDir),
		Key: RouteInbox + ":ungrouped:" + typeDir,
	}
}

func (s *Service) routeGroup(group CurationGroup, memories []MemoryRecord) routeResult {
	typeDir := s.curatedTypeDir(group.MemoryTypeBucket, dominantMemoryType(memories))
	subject := firstNonEmpty(group.RouteSubject, s.topicDir(group.TopicKey), "general")
	switch group.RouteCategory {
	case RouteKnowledge:
		return routeResult{
			Dir: filepath.Join(s.cfg.Vault.Directories.Knowledge, slugTitle(subject, "knowledge"), typeDir),
			Key: RouteKnowledge + ":" + subject + ":" + typeDir,
		}
	case RouteThinking:
		return routeResult{
			Dir: filepath.Join(s.cfg.Vault.Directories.Thinking, slugTitle(subject, "thinking"), typeDir),
			Key: RouteThinking + ":" + subject + ":" + typeDir,
		}
	case RouteSkills:
		return routeResult{
			Dir: filepath.Join(s.cfg.Vault.Directories.Skills, slugTitle(subject, "skills"), typeDir),
			Key: RouteSkills + ":" + subject + ":" + typeDir,
		}
	}
	if len(memories) > 0 {
		return s.routeMemory(memories[0])
	}
	return routeResult{
		Dir: filepath.Join(s.cfg.Vault.Directories.Inbox, "ungrouped", typeDir),
		Key: RouteInbox + ":ungrouped:" + typeDir,
	}
}

func (s *Service) memoryTypeDir(memoryType string) string {
	if dir := s.cfg.Vault.MemoryTypeDirs[memoryType]; dir != "" {
		return dir
	}
	return "notes"
}

func (s *Service) curatedTypeDir(bucket, dominantType string) string {
	bucket = strings.TrimSpace(bucket)
	if bucket != "" {
		if bucket == "notes" {
			return bucket
		}
		for _, configured := range s.cfg.Vault.MemoryTypeDirs {
			if bucket == configured {
				return bucket
			}
		}
	}
	return s.memoryTypeDir(dominantType)
}

func (s *Service) topicDir(topic string) string {
	if topic == "" {
		return ""
	}
	if dir := s.cfg.Vault.TopicDirs[topic]; dir != "" {
		return dir
	}
	return slugTitle(topic, "topic")
}

func dominantMemoryType(memories []MemoryRecord) string {
	if len(memories) == 0 {
		return ""
	}
	counts := map[string]int{}
	bestType := memories[0].MemoryType
	bestCount := 0
	for _, item := range memories {
		counts[item.MemoryType]++
		if counts[item.MemoryType] > bestCount {
			bestType = item.MemoryType
			bestCount = counts[item.MemoryType]
		}
	}
	return bestType
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
