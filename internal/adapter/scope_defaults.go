package adapter

import (
	"os"
	"path/filepath"
	"strings"
)

func defaultProjectID() string {
	if value := strings.TrimSpace(os.Getenv("THEONE_PROJECT_ID")); value != "" {
		return value
	}
	return projectDirName(nil)
}

func defaultRepoID() string {
	if value := strings.TrimSpace(os.Getenv("THEONE_REPO_ID")); value != "" {
		return value
	}
	return defaultProjectID()
}

func projectIDFromPayload(payload map[string]any) string {
	if value := stringFromPayload(payload, "project_id"); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("THEONE_PROJECT_ID")); value != "" {
		return value
	}
	return projectDirName(payload)
}

func repoIDFromPayload(payload map[string]any) string {
	if value := stringFromPayload(payload, "repo_id"); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("THEONE_REPO_ID")); value != "" {
		return value
	}
	return projectIDFromPayload(payload)
}

func projectDirName(payload map[string]any) string {
	for _, key := range []string{"cwd", "current_working_directory", "currentWorkingDirectory", "working_directory", "workingDirectory", "workspace_dir", "workspaceDir"} {
		if name := baseName(stringFromPayload(payload, key)); name != "" {
			return name
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if name := baseName(wd); name != "" {
			return name
		}
	}
	for _, key := range []string{"THEONE_PROJECT_DIR", "ROOT_DIR", "PWD"} {
		if name := baseName(os.Getenv(key)); name != "" {
			return name
		}
	}
	return ""
}

func baseName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	name := strings.TrimSpace(filepath.Base(filepath.Clean(path)))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}
