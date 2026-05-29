package adapter

import (
	"os"
	"path/filepath"
	"strings"
)

// DriverSurface 各 Agent Driver 的 binding / 缓存 / Surface 路径（P4）。
type DriverSurface struct {
	AgentType string
	RepoRoot  string
	StateDir  string
}

// NormalizeAgentType 默认 cursor。
func NormalizeAgentType(agentType string) string {
	at := strings.TrimSpace(agentType)
	if at == "" {
		return "cursor"
	}
	return at
}

// RuntimeCacheName cursor 保持无前缀文件名；其它 agent 加后缀避免并行 IDE 覆盖。
func RuntimeCacheName(base string, agentType string) string {
	at := NormalizeAgentType(agentType)
	if at == "cursor" {
		return base
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		return stem + "." + at
	}
	return stem + "." + at + ext
}

func (d DriverSurface) normalized() DriverSurface {
	out := d
	out.AgentType = NormalizeAgentType(d.AgentType)
	if out.RepoRoot == "" {
		out.RepoRoot = "."
	}
	return out
}

// BindingPath binding.{agent_type}.json
func (d DriverSurface) BindingPath() string {
	d = d.normalized()
	return filepath.Join(d.StateDir, "binding."+d.AgentType+".json")
}

// PromptCachePath 回合前 prompt 快照。
func (d DriverSurface) PromptCachePath() string {
	d = d.normalized()
	return filepath.Join(d.StateDir, RuntimeCacheName("prompt-cache.json", d.AgentType))
}

// InjectCachePath prefetch 注入元数据。
func (d DriverSurface) InjectCachePath() string {
	d = d.normalized()
	return filepath.Join(d.StateDir, RuntimeCacheName("inject-cache.json", d.AgentType))
}

// PrefetchCachePath prefetch-context 结果缓存。
func (d DriverSurface) PrefetchCachePath() string {
	d = d.normalized()
	return filepath.Join(d.StateDir, RuntimeCacheName("prefetch.json", d.AgentType))
}

// ContextCachePath memory.context 快照。
func (d DriverSurface) ContextCachePath() string {
	d = d.normalized()
	return filepath.Join(d.StateDir, RuntimeCacheName("context-cache.json", d.AgentType))
}

// SurfacePath Cursor → .mdc；Claude Code → .claude/theone-context.md
func (d DriverSurface) SurfacePath() string {
	d = d.normalized()
	switch d.AgentType {
	case "claude_code":
		return filepath.Join(d.RepoRoot, ".claude", "theone-context.md")
	default:
		return filepath.Join(d.RepoRoot, ".cursor", "rules", "theone-injected-context.mdc")
	}
}

// WriteSurface 写入 Agent 可见的注入面。
func (d DriverSurface) WriteSurface(body string, alwaysApply bool) error {
	d = d.normalized()
	path := d.SurfacePath()
	text := strings.TrimSpace(body)
	if text == "" {
		text = "（本轮未召回相关记忆。）"
	}
	var content string
	if d.AgentType == "claude_code" {
		content = ClaudeSurfaceContent(text)
	} else {
		content = RuleFileContent(text, alwaysApply)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// DefaultRuleFile 供 prefetch 未指定 rule_file 时使用。
func DefaultRuleFile(repoRoot, agentType string) string {
	return DriverSurface{AgentType: agentType, RepoRoot: repoRoot}.SurfacePath()
}
