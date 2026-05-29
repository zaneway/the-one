# Claude Code Hook 模板（P4 / P5 薄 Driver）

**实现位于：**

| 角色 | 路径 |
|------|------|
| Hook 脚本 | `drivers/claude_code/hooks/*.sh` |
| 共享逻辑 | `drivers/shared/theone-hook-*.py`、`theone-build-ingest.py` |
| 配置模板 | `doc/claude/settings.hooks.example.json` → 合并进 `.claude/settings.json` |

本目录脚本仅为 **exec 转发**（路径深度适配 `doc/claude/hooks/`），禁止内嵌 Python（`` python3 - <<'PY' ``）。

## 事件对照（Claude vs Cursor）

| Claude | Cursor | 记忆行为 |
|--------|--------|----------|
| `SessionStart` | `sessionStart` | `session.start` |
| `UserPromptSubmit` | `beforeSubmitPrompt` | prefetch |
| `PostToolUse` | `afterFileEdit` / `afterMCPExecution` | atomic |
| `Stop` | `afterAgentResponse` | `turn.completed` |
| `SessionEnd` | `sessionEnd` | `session.end` + cleanup |

**勿**将 Claude `Stop` 配置为 `session.end`（那是 `SessionEnd` 的职责）。
