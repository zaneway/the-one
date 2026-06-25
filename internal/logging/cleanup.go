package logging

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CleanupLogFiles 删除配置目录下（含子目录）所有以 .log 结尾的普通文件。
// 设计约束：仅处理显式配置的目录；跳过不存在目录；不跟随删除目录本身。
// 返回删除的文件数量；单个文件删除失败时继续处理其余文件并汇总错误。
func CleanupLogFiles(dirs []string) (int, error) {
	if len(dirs) == 0 {
		return 0, nil
	}
	var removed int
	var errs []string
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		absDir, err := normalizeCleanupDir(dir)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if _, ok := seen[absDir]; ok {
			continue
		}
		seen[absDir] = struct{}{}
		count, walkErr := cleanupLogFilesInDir(absDir)
		removed += count
		if walkErr != nil {
			errs = append(errs, walkErr.Error())
		}
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("log cleanup: %s", strings.Join(errs, "; "))
	}
	return removed, nil
}

func normalizeCleanupDir(dir string) (string, error) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return "", fmt.Errorf("empty cleanup dir")
	}
	absDir, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve cleanup dir %q: %w", trimmed, err)
	}
	if absDir == string(filepath.Separator) {
		return "", fmt.Errorf("refusing to clean filesystem root %q", absDir)
	}
	return absDir, nil
}

func cleanupLogFilesInDir(absDir string) (int, error) {
	info, err := os.Stat(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", absDir, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", absDir)
	}

	var removed int
	walkErr := filepath.WalkDir(absDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".log") {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		removed++
		return nil
	})
	return removed, walkErr
}
