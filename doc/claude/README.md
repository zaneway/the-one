# Claude Code × The One 安装模板

> 与 `doc/cursor/` 对称：可复制到项目或对照 `.claude/` 配置。  
> 运行时实现：`drivers/claude_code/hooks/` + `drivers/shared/`。  
> 映射表：`doc/adapters/claude_code/mapping.yaml`。

---

## 1. 目录说明

| 路径 | 作用 |
|------|------|
| `settings.hooks.example.json` | Hook 注册模板（合并进 `.claude/settings.json`） |
| `hooks/` | 薄转发脚本（`exec` → `drivers/claude_code/hooks/`） |
| `mcp.json` | MCP `theone serve` 配置模板（需替换 `__THEONE_REPO__`） |
| `CLAUDE.md` | 建议并入项目 `CLAUDE.md` 的 MCP 记忆捕获说明 |
| `context/theone-context.md.template` | prefetch 空状态 Surface 参考（运行时为 `.claude/theone-context.md`） |

**Claude 与 Cursor 的配置差异**

| 项 | Cursor | Claude Code |
|----|--------|-------------|
| Hook 配置 | `.cursor/hooks.json` | `.claude/settings.json` 的 `hooks` 段 |
| 回合结束 | `afterAgentResponse` → `turn.completed` | `Stop` → `turn.completed` |
| 会话结束 | `sessionEnd` only | `SessionEnd` only（**勿**把 `Stop` 当 session.end） |
| 提交前召回 | `beforeSubmitPrompt` | `UserPromptSubmit` + `additionalContext` |
| 注入面 | `.cursor/rules/theone-injected-context.mdc` | `.claude/theone-context.md` |
| binding 文件 | `binding.cursor.json` | `binding.claude_code.json` |

---

## 2. 安装步骤

### 2.1 编译

```bash
go build -tags sqlite_fts5 -o bin/theone ./cmd/theone
```

确认 `theone.yaml` 中 `adapter.expand_mode: v2`。

### 2.2 安装 Hook

```bash
chmod +x scripts/install-claude-hooks.sh
./scripts/install-claude-hooks.sh
```

脚本会读取 `doc/claude/settings.hooks.example.json`，生成 `.claude/settings.theone-hooks.json`，并提示合并到 `.claude/settings.json`。

手动合并示例：

```bash
jq -s '.[0] * .[1] | .hooks = (.[0].hooks * .[1].hooks)' \
  .claude/settings.json doc/claude/settings.hooks.example.json \
  > .claude/settings.merged.json
# 检查无误后替换
mv .claude/settings.merged.json .claude/settings.json
```

将模板中的 `__THEONE_REPO__` 替换为本机仓库**绝对路径**（安装脚本会自动替换）。

### 2.3 MCP（推荐）

将 `doc/claude/mcp.json` 复制到 Claude Code 项目 MCP 配置（或合并进 `.mcp.json` / 工具配置），替换 `__THEONE_REPO__` 后重启 Claude Code。

### 2.4 项目说明（可选）

将 `doc/claude/CLAUDE.md` 中的段落合并进仓库根 `CLAUDE.md`，以便 Agent 主动调用 `memory_observe` / `memory_remember`（Hook 不能替代 MCP）。

---

## 3. Hook 事件与行为

| Claude 事件 | 脚本 | theone 行为 |
|-------------|------|-------------|
| `SessionStart` | `theone-session-start.sh` | `session.start` → `ingest` |
| `UserPromptSubmit` | `theone-user-prompt-submit.sh` | `prefetch-context` + `additionalContext` |
| `PostToolUse` | `theone-post-tool-use.sh` | `capture.atomic`（文件/工具） |
| `PostToolUseFailure` | `theone-post-tool-use-failure.sh` | atomic，`exit_code=1` |
| `Stop` | `theone-stop.sh` | `turn.completed` → `ingest` |
| `SessionEnd` | `theone-session-end.sh` | `session.end` + 清理 runtime |

**语义注意**

- **`Stop`** = 每轮模型回复结束 → 对应 Cursor 的 `afterAgentResponse`，**不是**关闭会话。
- **`SessionEnd`** = 会话结束 → 清理 `binding.claude_code.json` 与缓存。

---

## 4. 运行时状态

目录：`.theone-data/runtime-state/`

| 文件 | 说明 |
|------|------|
| `binding.claude_code.json` | 会话/任务绑定 |
| `prompt-cache.claude_code.json` | 本轮 prompt 快照 |
| `inject-cache.claude_code.json` | prefetch 注入元数据 |
| `prefetch.json` / `context-cache.claude_code.json` | 可选缓存 |

Surface：`.claude/theone-context.md`（由 prefetch 自动刷新）。

---

## 5. 验收

```bash
scripts/acceptance/p4_claude_hooks.sh
```

---

## 6. 与 Cursor 并行

同一仓库可同时配置 Cursor（`.cursor/`）与 Claude（`.claude/`）。binding 与 prompt-cache **按 agent 分文件**，互不覆盖。

---

## 7. 相关文档

- `doc/Cursor 适配与安装后配置说明.md` — MCP、数据目录、通用说明（Cursor 为主，原理相同）
- `doc/Agent 接入层与 Hook 设计.md` — 架构与 P4 设计
- `doc/adapters/claude_code/mapping.yaml` — 事件映射
