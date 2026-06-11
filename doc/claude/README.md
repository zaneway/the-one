# The One Claude Code 安装手册

本文面向 **使用发布包的 Claude Code 用户**，不要求用户克隆源码、安装 Go 或执行 `make build`。

发布包已经包含：

| 路径 | 作用 |
|------|------|
| `bin/theone` | The One MCP server / ingest CLI |
| `install-claude.sh` | 安装到目标项目的脚本 |
| `theone.yaml` | 默认运行配置 |
| `drivers/claude_code/` | Claude Code Hook 脚本 |
| `drivers/shared/` | Hook 共享运行时 |
| `doc/claude/` | Claude 模板与说明 |

源码仓库维护者生成发布包使用：

```bash
make package-claude VERSION=1.0.0
```

生成结果在 `dist/theone-claude-<version>-<os>-<arch>.tar.gz`。

---

## 1. 用户安装流程

假设用户已经拿到并解压发布包：

```bash
tar -xzf theone-claude-1.0.0-darwin-arm64.tar.gz
cd theone-claude-1.0.0-darwin-arm64
./install-claude.sh --project /path/to/your/project
```

如果已经在目标项目目录下，也可以：

```bash
/path/to/theone-claude-1.0.0-darwin-arm64/install-claude.sh
```

安装脚本会写入或更新目标项目：

| 目标路径 | 内容 |
|----------|------|
| `bin/theone` | 发布包中的二进制 |
| `theone.yaml` | 默认配置；已存在时默认保留 |
| `drivers/claude_code/` | Claude Hook 脚本 |
| `drivers/shared/` | Hook 共享脚本 |
| `.claude/settings.json` | 合并 Hook 配置（`hooks` 段） |
| `.mcp.json` | MCP 配置，指向目标项目内的 `bin/theone` |
| `.claude/theone-context.md` | prefetch 注入面初始文件 |
| `.theone-data/` | SQLite 数据库、日志、runtime state |

已有 `.claude/settings.json` / `.mcp.json` 时，脚本会在安装了 `jq` 的环境中自动合并；没有 `jq` 时会提示用户手工合并。

覆盖运行时文件：

```bash
./install-claude.sh --project /path/to/your/project --force
```

源码仓库内仍可使用兼容入口：

```bash
make install-claude
# 等价于 scripts/install-claude-hooks.sh
```

---

## 2. Claude Code 内启用

安装完成后：

1. 用 Claude Code 打开目标项目根目录。
2. 重启 Claude Code 或重新加载 MCP。
3. 确认 MCP server `theone` 已启用。
4. 调用 `memory_health`，应返回 `ok: true`。
5. 将 `doc/claude/CLAUDE.md` 中的段落合并进项目 `CLAUDE.md`（可选但推荐）。
6. 新开对话，完成一轮有实质内容的问答。

---

## 3. 验收命令

在目标项目根目录执行：

```bash
bin/theone health -config ./theone.yaml -data-dir ./.theone-data
```

查看最近 raw_event：

```bash
sqlite3 .theone-data/memory.db \
  "SELECT event_type, occurred_at FROM raw_event ORDER BY occurred_at DESC LIMIT 10;"
```

当前策略下：

| 事件 | 是否进入 `raw_event` | 说明 |
|------|----------------------|------|
| `session.start` | 否 | Hook 保留，用于 session/task 绑定与诊断；不写 raw_event |
| `tool.result.summary` | 否 | Hook 保留，用于工具链路诊断；不写 raw_event |
| `file.edit.summary` | 视工具而定 | 文件类工具可能写入 |
| `turn.completed` | 是 | 一轮用户请求 + Agent 回复（`Stop` hook） |
| `session.end` | 是 | 会话结束与清理（`SessionEnd` hook） |
| Agent 主动 `memory_observe` | 是 | 高价值事实/结论由 CLAUDE.md 引导写入 |

检查 binding：

```bash
test -f .theone-data/runtime-state/binding.claude_code.json && echo "binding ok"
```

完整 Hook 验收：

```bash
scripts/acceptance/p4_claude_hooks.sh
```

---

## 4. 能力边界

完整 Claude Code 体验由三部分组成：

| 组件 | 作用 |
|------|------|
| MCP | `memory_health`、`memory_search`、`memory_context`、`memory_observe`、`memory_remember` |
| Hooks | 自动 prefetch、turn/tool/session 事件捕获、session binding |
| CLAUDE.md | 引导 Agent 在高价值时主动调用 `memory_observe` / `memory_remember` |

**Claude 与 Cursor 的配置差异**

| 项 | Cursor | Claude Code |
|----|--------|-------------|
| Hook 配置 | `.cursor/hooks.json` | `.claude/settings.json` 的 `hooks` 段 |
| 回合结束 | `afterAgentResponse` → `turn.completed` | `Stop` → `turn.completed` |
| 会话结束 | `sessionEnd` only | `SessionEnd` only（**勿**把 `Stop` 当 session.end） |
| 提交前召回 | `beforeSubmitPrompt` | `UserPromptSubmit` + `additionalContext` |
| 注入面 | `.cursor/rules/theone-injected-context.mdc` | `.claude/theone-context.md` |
| binding 文件 | `binding.cursor.json` | `binding.claude_code.json` |

数据流：

```text
用户提交 prompt
  -> UserPromptSubmit: prefetch -> additionalContext + .claude/theone-context.md
  -> Agent 推理
  -> PostToolUse: capture.atomic（工具/文件）
  -> Stop: turn.completed -> raw_event
  -> SessionEnd: session.end + cleanup
  -> SessionStart: 控制面处理，不写 raw_event
  -> 异步 Worker: raw_event -> evidence -> memory_candidate -> memory_item
```

---

## 5. 模板说明

`doc/claude` 中的模板由安装脚本消费：

| 文件 | 作用 |
|------|------|
| `settings.hooks.example.json` | Hook 配置片段，占位符 `__THEONE_REPO__` 会替换为目标项目绝对路径 |
| `mcp.json` | MCP 配置模板 |
| `context/theone-context.md.template` | 初始注入面 |
| `CLAUDE.md` | 建议合并进项目 `CLAUDE.md` 的记忆捕获说明 |

`doc/adapters/claude_code/mapping.yaml` 是维护者参考的事件映射说明，不要求普通用户阅读。

---

## 6. 排障

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| MCP 找不到 `theone` | `.mcp.json` 未写入或路径错误 | 重跑 `install-claude.sh --project <项目>` |
| `memory_health` 失败 | `bin/theone` 不存在、不可执行或配置错误 | 检查 `bin/theone`、`theone.yaml`、`.theone-data/logs/theone.log` |
| Hooks 不触发 | `.claude/settings.json` 未合并 | 检查 `.claude/settings.theone-hooks.json` 并合并 |
| prefetch 无注入 | 尚无可召回记忆或 binding 未建立 | 先完成一轮高价值对话并让 Agent 调用 `memory_observe` / `memory_remember` |
| raw_event 为空 | Hooks 未启用或 Agent 未产生有效事件 | 完成一轮有内容的 Agent 回复，再查 `turn.completed` |
| 看不到 `session.start` / `tool.result.summary` | 当前策略就是不写入 raw_event | 查 binding 和日志，不把它作为 raw_event 验收项 |
| 换目录后失效 | MCP / hooks 中仍是旧绝对路径 | 重跑安装脚本 |

---

## 7. 与 Cursor / Codex 并行

同一仓库可同时配置 Cursor（`.cursor/`）、Claude（`.claude/`）与 Codex（`.codex/`）。binding 与 prompt-cache **按 agent 分文件**，互不覆盖。

---

## 8. 维护者发布清单

发布前至少验证：

```bash
scripts/acceptance/p4_claude_hooks.sh
python3 -m json.tool doc/claude/settings.hooks.example.json >/dev/null
python3 -m json.tool doc/claude/mcp.json >/dev/null
python3 -m unittest \
  drivers/shared/theone-build-ingest_test.py \
  drivers/shared/theone-hook-prefetch_test.py \
  drivers/shared/theone-hook-session_test.py
make package-claude VERSION=<version>
```
