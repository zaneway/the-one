package dream

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func renderMemoryNote(item MemoryRecord, relations []RelationRecord, memoryByID map[string]MemoryRecord) string {
	var b strings.Builder
	title := firstNonEmpty(item.Title, item.ID)
	writeFrontmatter(&b, map[string]any{
		"theone_kind":  "memory",
		"readonly":     true,
		"note_mode":    NoteModeMemory,
		"memory_id":    item.ID,
		"memory_type":  item.MemoryType,
		"scope":        item.Scope,
		"workspace_id": item.WorkspaceID,
		"project_id":   item.ProjectID,
		"repo_id":      item.RepoID,
		"state":        item.State,
		"tier":         item.Tier,
		"version":      item.Version,
		"content_hash": hashString(item.Content),
		"tags": []string{
			"theone/type/" + item.MemoryType,
			"theone/state/" + item.State,
			"theone/project/" + item.ProjectID,
		},
	})
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Memory\n")
	b.WriteString(strings.TrimSpace(item.Content) + "\n\n")
	b.WriteString("## Relations\n")
	writeRelations(&b, item.ID, relations, memoryByID)
	b.WriteString("\n## Evidence\n")
	b.WriteString("- Source evidence is retained in SQLite; this readonly projection only exports summarized memory content.\n\n")
	b.WriteString("## Context\n")
	b.WriteString(fmt.Sprintf("- scope: %s\n", item.Scope))
	b.WriteString(fmt.Sprintf("- confidence: %.2f\n", item.Confidence))
	b.WriteString(fmt.Sprintf("- importance: %.2f\n", item.Importance))
	b.WriteString(fmt.Sprintf("- tier: %s\n", item.Tier))
	if !item.UpdatedAt.IsZero() {
		b.WriteString(fmt.Sprintf("- updated_at: %s\n", item.UpdatedAt.Format(time.RFC3339)))
	}
	return b.String()
}

func renderConsolidatedNote(group CurationGroup, memories []MemoryRecord, relations []RelationRecord) string {
	var b strings.Builder
	writeFrontmatter(&b, map[string]any{
		"theone_kind":       "memory_projection",
		"readonly":          true,
		"note_mode":         NoteModeConsolidated,
		"projection_id":     group.ProjectionID,
		"topic_key":         group.TopicKey,
		"source_memory_ids": group.SourceMemoryIDs,
		"source_map":        group.SourceMap,
		"tags": []string{
			"theone/topic/" + group.TopicKey,
			"theone/projection/consolidated",
		},
	})
	title := firstNonEmpty(group.Title, group.ProjectionID)
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Summary\n")
	b.WriteString(strings.TrimSpace(group.Summary) + "\n\n")
	b.WriteString("## Consolidated Knowledge\n")
	for _, item := range memories {
		b.WriteString("### " + firstNonEmpty(item.Title, item.ID) + "\n")
		b.WriteString(strings.TrimSpace(item.Content) + "\n\n")
	}
	b.WriteString("## Source Memories\n")
	for _, item := range memories {
		b.WriteString(fmt.Sprintf("- %s: %s\n", item.ID, firstNonEmpty(item.Title, item.Content)))
	}
	b.WriteString("\n## Relations\n")
	sourceSet := map[string]bool{}
	for _, id := range group.SourceMemoryIDs {
		sourceSet[id] = true
	}
	written := 0
	for _, relation := range relations {
		if sourceSet[relation.SourceID] || sourceSet[relation.TargetID] {
			b.WriteString(fmt.Sprintf("- %s `%s` -> `%s`\n", relation.RelationType, relation.SourceID, relation.TargetID))
			written++
		}
	}
	if written == 0 {
		b.WriteString("- No strong relations exported for this topic.\n")
	}
	return b.String()
}

func renderMOC(plans []projectionPlan) string {
	var b strings.Builder
	writeFrontmatter(&b, map[string]any{
		"theone_kind": "moc",
		"readonly":    true,
		"note_mode":   NoteModeMOC,
	})
	b.WriteString("# Dream Projection Index\n\n")
	for _, plan := range plans {
		if plan.Mode == NoteModeMOC {
			continue
		}
		b.WriteString(fmt.Sprintf("- [[%s]]\n", strings.TrimSuffix(filepathBase(plan.Path), ".md")))
	}
	return b.String()
}

func writeRelations(b *strings.Builder, sourceID string, relations []RelationRecord, memoryByID map[string]MemoryRecord) {
	count := 0
	for _, relation := range relations {
		if relation.SourceID != sourceID {
			continue
		}
		target, ok := memoryByID[relation.TargetID]
		if !ok {
			continue
		}
		link := slugTitle(target.Title, target.ID) + "--" + target.ID
		b.WriteString(fmt.Sprintf("- %s [[%s]]\n", relation.RelationType, link))
		count++
	}
	if count == 0 {
		b.WriteString("- No strong relations exported.\n")
	}
}

func writeFrontmatter(b *strings.Builder, values map[string]any) {
	b.WriteString("---\n")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeYAMLValue(b, key, values[key], 0)
	}
	b.WriteString("---\n\n")
}

func writeYAMLValue(b *strings.Builder, key string, value any, indent int) {
	prefix := strings.Repeat(" ", indent)
	switch v := value.(type) {
	case string:
		if v == "" {
			return
		}
		b.WriteString(fmt.Sprintf("%s%s: %s\n", prefix, key, yamlScalar(v)))
	case bool:
		b.WriteString(fmt.Sprintf("%s%s: %t\n", prefix, key, v))
	case int:
		if v == 0 {
			return
		}
		b.WriteString(fmt.Sprintf("%s%s: %d\n", prefix, key, v))
	case []string:
		if len(v) == 0 {
			return
		}
		b.WriteString(fmt.Sprintf("%s%s:\n", prefix, key))
		for _, item := range v {
			if item != "" {
				b.WriteString(fmt.Sprintf("%s  - %s\n", prefix, yamlScalar(item)))
			}
		}
	case map[string][]string:
		if len(v) == 0 {
			return
		}
		b.WriteString(fmt.Sprintf("%s%s:\n", prefix, key))
		keys := make([]string, 0, len(v))
		for nestedKey := range v {
			keys = append(keys, nestedKey)
		}
		sort.Strings(keys)
		for _, nestedKey := range keys {
			writeYAMLValue(b, nestedKey, v[nestedKey], indent+2)
		}
	}
}

func yamlScalar(value string) string {
	if strings.ContainsAny(value, ":#[]{}\n\r\t") || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func filepathBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
