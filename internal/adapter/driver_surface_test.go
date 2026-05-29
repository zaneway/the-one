package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeCacheName(t *testing.T) {
	if got := RuntimeCacheName("prompt-cache.json", "cursor"); got != "prompt-cache.json" {
		t.Fatalf("cursor cache = %q", got)
	}
	if got := RuntimeCacheName("inject-cache.json", "claude_code"); got != "inject-cache.claude_code.json" {
		t.Fatalf("claude cache = %q", got)
	}
}

func TestDriverSurfacePaths(t *testing.T) {
	dir := t.TempDir()
	repo := t.TempDir()
	s := DriverSurface{AgentType: "claude_code", StateDir: dir, RepoRoot: repo}
	if !strings.HasSuffix(s.BindingPath(), "binding.claude_code.json") {
		t.Fatalf("binding path: %s", s.BindingPath())
	}
	if !strings.HasSuffix(s.SurfacePath(), filepath.Join(".claude", "theone-context.md")) {
		t.Fatalf("surface: %s", s.SurfacePath())
	}
	if err := s.WriteSurface("测试记忆", true); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(s.SurfacePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "测试记忆") {
		t.Fatalf("body=%q", body)
	}
	if strings.Contains(string(body), "alwaysApply") {
		t.Fatal("claude surface should not have mdc frontmatter")
	}
}
