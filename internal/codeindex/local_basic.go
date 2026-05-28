package codeindex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zaneway/theone/internal/config"
	"github.com/zaneway/theone/internal/memory"
)

const defaultMaxFileSizeKB = 512

// LocalBasicAdapter 是默认本地轻量 Code Index 实现。
// 它只读取单个 repo-relative 文件，计算文件 hash，并用字符串扫描做 best-effort 符号定位。
type LocalBasicAdapter struct {
	cfg     config.CodeIndexConfig
	rootDir string
}

// NewLocalBasicAdapter 创建 local_basic Code Index Adapter。
// rootDir 为空时使用当前工作目录；repo_id 如果是存在的绝对路径，会优先作为 repo root。
func NewLocalBasicAdapter(cfg config.CodeIndexConfig, rootDir string) *LocalBasicAdapter {
	if cfg.MaxFileSizeKB <= 0 {
		cfg.MaxFileSizeKB = defaultMaxFileSizeKB
	}
	if cfg.MaxResolveRefs <= 0 {
		cfg.MaxResolveRefs = 30
	}
	if cfg.Provider == "" {
		cfg.Provider = "local_basic"
	}
	if rootDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			rootDir = findModuleRoot(cwd)
		}
	}
	return &LocalBasicAdapter{cfg: cfg, rootDir: rootDir}
}

func findModuleRoot(start string) string {
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

// Name 返回 Adapter 名称。
func (a *LocalBasicAdapter) Name() string {
	return "local_basic"
}

// Capabilities 返回 local_basic 的保守能力声明。
func (a *LocalBasicAdapter) Capabilities(ctx context.Context) (Capabilities, error) {
	return Capabilities{
		Provider:        a.Name(),
		FilePathResolve: true,
		SymbolResolve:   true,
		CallGraph:       false,
		Impact:          false,
	}, nil
}

// ResolveCodeRefs 对 code_ref 做 best-effort 文件/符号解析。
// 解析结果只更新定位、hash、摘要和状态；摘要不包含源码正文。
func (a *LocalBasicAdapter) ResolveCodeRefs(ctx context.Context, refs []memory.CodeRef) ([]memory.CodeRef, error) {
	limit := a.cfg.MaxResolveRefs
	if limit <= 0 {
		limit = 30
	}
	if len(refs) > limit {
		refs = refs[:limit]
	}
	out := make([]memory.CodeRef, 0, len(refs))
	for _, ref := range refs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		out = append(out, a.resolveOne(ref))
	}
	return out, nil
}

// resolveOne 对单个 code_ref 做 best-effort 文件/符号解析。
// 解析流程：
//  1. 路径安全校验（拒绝绝对路径、.. 路径逃逸）
//  2. 文件存在性和大小检查（超过 MaxFileSizeKB 跳过）
//  3. 计算文件 hash，与已有 hash 对比判断是否 stale
//  4. 字符串扫描定位符号行号（唯一匹配 → resolved，多匹配 → ambiguous，无匹配 → missing）
//
// 设计约束：不读取完整源码到 memory，只更新定位、hash 和摘要。
func (a *LocalBasicAdapter) resolveOne(ref memory.CodeRef) memory.CodeRef {
	resolved := ref
	path := strings.TrimSpace(ref.FilePath)
	if path == "" {
		resolved.ResolveStatus = memory.CodeRefStatusUnresolved
		resolved.RefSummary = "local_basic: file_path missing"
		return resolved
	}
	fullPath, ok := a.safeFullPath(ref.RepoID, path)
	if !ok {
		resolved.ResolveStatus = memory.CodeRefStatusUnresolved
		resolved.RefSummary = "local_basic: unsafe file_path"
		return resolved
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		resolved.ResolveStatus = memory.CodeRefStatusMissing
		resolved.RefSummary = "local_basic: file missing"
		return resolved
	}
	if info.IsDir() {
		resolved.ResolveStatus = memory.CodeRefStatusMissing
		resolved.RefSummary = "local_basic: file_path is directory"
		return resolved
	}
	if info.Size() > int64(a.cfg.MaxFileSizeKB)*1024 {
		resolved.ResolveStatus = memory.CodeRefStatusUnresolved
		resolved.RefSummary = "local_basic: file too large"
		return resolved
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		resolved.ResolveStatus = memory.CodeRefStatusMissing
		resolved.RefSummary = "local_basic: file unreadable"
		return resolved
	}
	fileHash := "sha256:" + fmt.Sprintf("%x", sha256.Sum256(data))
	// hash 不一致：文件已被修改，标记为 stale
	if ref.ContentHash != "" && ref.ContentHash != fileHash {
		resolved.ResolveStatus = memory.CodeRefStatusStale
		resolved.ContentHash = fileHash
		resolved.RefSummary = "local_basic: file hash changed"
		return resolved
	}
	lineStart, lineEnd, status := resolveSymbolLines(data, ref.Symbol)
	resolved.ResolveStatus = status
	resolved.ContentHash = fileHash
	if status == memory.CodeRefStatusResolved {
		if lineStart > 0 {
			resolved.LineStart = lineStart
			resolved.LineEnd = lineEnd
		}
		resolved.RefSummary = formatRefSummary(resolved)
		return resolved
	}
	resolved.RefSummary = "local_basic: symbol not uniquely resolved"
	return resolved
}

func (a *LocalBasicAdapter) safeFullPath(repoID, refPath string) (string, bool) {
	cleaned := filepath.Clean(refPath)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	root := a.rootDir
	if repoID != "" && filepath.IsAbs(repoID) {
		if info, err := os.Stat(repoID); err == nil && info.IsDir() {
			root = repoID
		}
	}
	fullPath := filepath.Join(root, cleaned)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	fullAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", false
	}
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(filepath.Separator)) {
		return "", false
	}
	return fullAbs, true
}

// resolveSymbolLines 通过字符串扫描定位符号的行号。
// 解析策略：逐行扫描，使用 symbolMatchesLine 匹配多种语言的声明模式。
// 结果：唯一匹配 → resolved，多匹配 → ambiguous，无匹配 → missing。
func resolveSymbolLines(data []byte, symbol string) (int, int, string) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return 0, 0, memory.CodeRefStatusResolved
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNo := 0
	matches := make([]int, 0, 2)
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if symbolMatchesLine(line, symbol) {
			matches = append(matches, lineNo)
			if len(matches) > 1 {
				return 0, 0, memory.CodeRefStatusAmbiguous
			}
		}
	}
	if len(matches) == 0 {
		return 0, 0, memory.CodeRefStatusMissing
	}
	return matches[0], matches[0], memory.CodeRefStatusResolved
}

func symbolMatchesLine(line, symbol string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	parts := strings.Split(symbol, ".")
	name := parts[len(parts)-1]
	if name == "" {
		name = symbol
	}
	patterns := []string{
		"func " + name + "(",
		"func (" + name,
		") " + name + "(",
		"type " + name + " ",
		"var " + name + " ",
		"const " + name + " ",
		"class " + name,
		"def " + name + "(",
		"function " + name + "(",
		name + " :=",
	}
	for _, pattern := range patterns {
		if strings.Contains(trimmed, pattern) {
			return true
		}
	}
	return strings.Contains(trimmed, symbol)
}

func formatRefSummary(ref memory.CodeRef) string {
	parts := []string{"local_basic: resolved", ref.FilePath}
	if ref.Symbol != "" {
		parts = append(parts, ref.Symbol)
	}
	if ref.LineStart > 0 {
		parts = append(parts, fmt.Sprintf("line=%d", ref.LineStart))
	}
	return strings.Join(parts, " ")
}
