package docindex

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const defaultMarkdownMaxDocSizeKB = 512

// MarkdownBuildOptions 描述 Markdown 文档快照构建参数。
// RootDir 为空时从当前工作目录向上寻找 go.mod；RepoID 如果是存在的绝对目录，会作为 repo root。
type MarkdownBuildOptions struct {
	WorkspaceID         string
	ProjectID           string
	RepoID              string
	Path                string
	Role                string
	RootDir             string
	MaxDocSizeKB        int
	MaxSections         int
	StoreSectionSummary bool
}

// BuildMarkdownSnapshot 构建 Markdown 文档快照。
// 安全边界：只接受 repo/workspace 相对 .md/.markdown 路径，拒绝绝对路径、.. 路径和符号链接逃逸。
// 数据边界：返回结果只包含路径、hash、标题路径、行号和短摘要，不保存完整文档正文。
func BuildMarkdownSnapshot(opts MarkdownBuildOptions) (DocumentSnapshot, error) {
	if strings.TrimSpace(opts.WorkspaceID) == "" {
		return DocumentSnapshot{}, fmt.Errorf("VALIDATION_FAILED: workspace_id is required")
	}
	if opts.MaxDocSizeKB <= 0 {
		opts.MaxDocSizeKB = defaultMarkdownMaxDocSizeKB
	}
	cleanPath, fullPath, err := safeMarkdownPath(opts)
	if err != nil {
		return DocumentSnapshot{}, err
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return DocumentSnapshot{}, fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: stat %s: %w", cleanPath, err)
	}
	if info.IsDir() {
		return DocumentSnapshot{}, fmt.Errorf("VALIDATION_FAILED: doc_path is directory")
	}
	snapshot := DocumentSnapshot{
		WorkspaceID: strings.TrimSpace(opts.WorkspaceID),
		ProjectID:   strings.TrimSpace(opts.ProjectID),
		RepoID:      strings.TrimSpace(opts.RepoID),
		Path:        filepath.ToSlash(cleanPath),
		Role:        strings.TrimSpace(opts.Role),
		ModifiedAt:  info.ModTime().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if info.Size() > int64(opts.MaxDocSizeKB)*1024 {
		hash, err := hashFile(fullPath)
		if err != nil {
			return DocumentSnapshot{}, err
		}
		snapshot.ContentHash = hash
		return snapshot, nil
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return DocumentSnapshot{}, fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: read %s: %w", cleanPath, err)
	}
	normalized := normalizeMarkdownForHash(string(data))
	snapshot.ContentHash = hashString(normalized)
	snapshot.Sections = buildMarkdownSections(normalized, opts.MaxSections, opts.StoreSectionSummary)
	snapshot.SectionCount = len(snapshot.Sections)
	return snapshot, nil
}

func safeMarkdownPath(opts MarkdownBuildOptions) (string, string, error) {
	docPath := strings.TrimSpace(opts.Path)
	if docPath == "" {
		return "", "", fmt.Errorf("VALIDATION_FAILED: doc_path is required")
	}
	cleaned := filepath.Clean(filepath.FromSlash(docPath))
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("VALIDATION_FAILED: unsafe doc_path")
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext != ".md" && ext != ".markdown" {
		return "", "", fmt.Errorf("VALIDATION_FAILED: doc_path must be markdown")
	}
	root := markdownRootDir(opts.RootDir, opts.RepoID)
	fullPath := filepath.Join(root, cleaned)
	if _, err := os.Lstat(fullPath); err != nil {
		return "", "", fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: lstat %s: %w", cleaned, err)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: resolve root: %w", err)
	}
	fullReal, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", "", fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: resolve doc_path: %w", err)
	}
	rootAbs, err := filepath.Abs(rootReal)
	if err != nil {
		return "", "", fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: abs root: %w", err)
	}
	fullAbs, err := filepath.Abs(fullReal)
	if err != nil {
		return "", "", fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: abs doc_path: %w", err)
	}
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(filepath.Separator)) {
		return "", "", fmt.Errorf("VALIDATION_FAILED: doc_path escapes root")
	}
	return cleaned, fullAbs, nil
}

func markdownRootDir(rootDir, repoID string) string {
	if repoID != "" && filepath.IsAbs(repoID) {
		if info, err := os.Stat(repoID); err == nil && info.IsDir() {
			return repoID
		}
	}
	if rootDir != "" {
		return rootDir
	}
	if cwd, err := os.Getwd(); err == nil {
		return findMarkdownModuleRoot(cwd)
	}
	return "."
}

func findMarkdownModuleRoot(start string) string {
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: open file: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("DOC_SNAPSHOT_BUILD_FAILED: hash file: %w", err)
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}

func normalizeMarkdownForHash(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// buildMarkdownSections 解析 Markdown 文档为 section 列表。
// 解析策略：
//  1. 收集所有标题（跳过代码块内的 # 行）
//  2. 按标题层级构建 heading path（如 "检索 > rerank"）
//  3. 每个 section 计算独立的 content_hash，用于后续变更检测
//  4. section_id 由 heading path 生成 slug，重复时追加序号
func buildMarkdownSections(markdown string, maxSections int, storeSummary bool) []DocumentSection {
	if maxSections <= 0 {
		return nil
	}
	lines := strings.Split(markdown, "\n")
	headers := collectMarkdownHeadings(lines)
	if len(headers) == 0 {
		return nil
	}
	sections := make([]DocumentSection, 0, minInt(len(headers), maxSections))
	seenIDs := make(map[string]int, len(headers))
	for i, header := range headers {
		if len(sections) >= maxSections {
			break
		}
		endLine := len(lines)
		if i+1 < len(headers) {
			endLine = headers[i+1].line - 1
		}
		sectionLines := lines[header.line-1 : endLine]
		sectionID := uniqueSectionID(slugHeadingPath(header.path), seenIDs)
		section := DocumentSection{
			SectionID:   sectionID,
			HeadingPath: append([]string(nil), header.path...),
			Level:       header.level,
			StartLine:   header.line,
			EndLine:     endLine,
			ContentHash: hashString(strings.Join(sectionLines, "\n")),
			CreatedAt:   time.Now().UTC(),
		}
		if storeSummary {
			section.Summary = compactSummary(strings.Join(header.path, " > "))
		}
		sections = append(sections, section)
	}
	return sections
}

type markdownHeading struct {
	level int
	line  int
	title string
	path  []string
}

func collectMarkdownHeadings(lines []string) []markdownHeading {
	inFence := false
	stack := make([]string, 0, 6)
	headings := make([]markdownHeading, 0)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		level, title, ok := parseMarkdownHeading(trimmed)
		if !ok {
			continue
		}
		if len(stack) >= level {
			stack = stack[:level-1]
		}
		for len(stack) < level-1 {
			stack = append(stack, "")
		}
		stack = append(stack, title)
		path := make([]string, 0, len(stack))
		for _, part := range stack {
			if part != "" {
				path = append(path, part)
			}
		}
		headings = append(headings, markdownHeading{level: level, line: i + 1, title: title, path: path})
	}
	return headings
}

func parseMarkdownHeading(line string) (int, string, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) {
		return 0, "", false
	}
	if line[level] != ' ' && line[level] != '\t' {
		return 0, "", false
	}
	title := strings.TrimSpace(line[level:])
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

func slugHeadingPath(path []string) string {
	parts := make([]string, 0, len(path))
	for _, item := range path {
		item = strings.ToLower(strings.TrimSpace(item))
		var b strings.Builder
		lastDash := false
		for _, r := range item {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
				lastDash = false
				continue
			}
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		slug := strings.Trim(b.String(), "-")
		if slug != "" {
			parts = append(parts, slug)
		}
	}
	if len(parts) == 0 {
		return "section"
	}
	return strings.Join(parts, "/")
}

func uniqueSectionID(base string, seen map[string]int) string {
	if base == "" {
		base = "section"
	}
	seen[base]++
	if seen[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, seen[base])
}

func compactSummary(summary string) string {
	const maxSummaryRunes = 160
	summary = strings.TrimSpace(summary)
	runes := []rune(summary)
	if len(runes) <= maxSummaryRunes {
		return summary
	}
	return string(runes[:maxSummaryRunes-3]) + "..."
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
