# The One Codex 安装手册

本文面向 **使用发布包的 Codex 用户**，不要求用户克隆源码、安装 Go 或执行 `make build`。

发布包已经包含：

| 路径 | 作用 |
|------|------|
| `bin/theone` | The One MCP server / ingest CLI |
| `install-codex.sh` | 安装到目标项目的脚本 |
| `theone.yaml` | 默认运行配置 |
| `drivers/codex/` | Codex Hook 脚本 |
| `drivers/shared/` | Hook 共享运行时 |
| `doc/codex/` | Codex 模板与说明 |

源码仓库维护者生成发布包使用：

```bash
make package-codex VERSION=1.0.0
```

生成结果在 `dist/theone-codex-<version>-<os>-<arch>.tar.gz`。

---

## 1. 用户安装流程

假设用户已经拿到并解压发布包：

```bash
tar -xzf theone-codex-1.0.0-darwin-arm64.tar.gz
cd theone-codex-1.0.0-darwin-arm64
./install-codex.sh --project /path/to/your/project
```

如果已经在目标项目目录下，也可以：

```bash
/path/to/theone-codex-1.0.0-darwin-arm64/install-codex.sh
```

安装脚本会写入或更新目标项目：

| 目标路径 | 内容 |
|----------|------|
| `bin/theone` | 发布包中的二进制 |
| `theone.yaml` | 默认配置；已存在时默认保留 |
| `drivers/codex/` | Codex Hook 脚本 |
| `drivers/shared/` | Hook 共享脚本 |
| `.codex/hooks.json` | Codex Hooks 配置 |
| `.codex/theone-mcp.toml` | 渲染后的 MCP 片段，需合并到 `~/.codex/config.toml` |
| `.codex/theone-context.md` | prefetch 注入面初始文件 |
| `.theone-data/` | SQLite 数据库、日志、runtime state |

已有 `.codex/hooks.json` 时，脚本会在安装了 `jq` 的环境中自动合并；没有 `jq` 时会提示用户手工合并。

覆盖运行时文件：

```bash
./install-codex.sh --project /path/to/your/project --force
```

### 1.1 合并 MCP 配置

Codex 的 MCP 配置位于用户主目录 `~/.codex/config.toml`。安装脚本会在项目内生成 `.codex/theone-mcp.toml`，将其中 `[mcp_servers.theone]` 段合并进 `~/.codex/config.toml` 后重启 Codex。

示例（路径已替换为项目绝对路径）：

```toml
[mcp_servers.theone]
command = "/path/to/project/bin/theone"
args = [
  "serve",
  "-config",
  "/path/to/project/theone.yaml",
  "-data-dir",
  "/path/to/project/.theone-data",
]
cwd = "/path/to/project"
startup_timeout_sec = 20
tool_timeout_sec = 120
enabled = true
required = false
```

---

## 2. Codex 内启用

安装并完成 MCP 合并后：

1. 在 Codex 中打开目标项目根目录。
2. 重启 Codex 或重新加载配置。
3. 确认 MCP server `theone` 已启用。
4. 调用 `memory_health`，应返回 `ok: true`。
5. 将 `doc/codex/AGENTS.md` 中的段落合并进项目 `AGENTS.md`（可选但推荐）。
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
| `file.edit.summary` | 视工具而定 | `apply_patch` 等文件类工具可能写入 |
| `turn.completed` | 是 | 一轮用户请求 + Agent 回复（`Stop` hook） |
| Agent 主动 `memory_observe` | 是 | 高价值事实/结论由 AGENTS.md 引导写入 |

检查 binding：

```bash
test -f .theone-data/runtime-state/binding.codex.json && echo "binding ok"
```

检查日志：

```bash
tail -n 50 .theone-data/logs/theone.log
```

Hook 形状验收（无需完整 MCP 会话）：

```bash
scripts/acceptance/p6_codex_hooks.sh
```

---

## 4. 能力边界

完整 Codex 体验由三部分组成：

| 组件 | 作用 |
|------|------|
| MCP | `memory_health`、`memory_search`、`memory_context`、`memory_observe`、`memory_remember` |
| Hooks | 自动 prefetch、turn/tool 事件捕获、session binding |
| AGENTS.md | 引导 Agent 在高价值时主动调用 `memory_observe` / `memory_remember` |

数据流：

```text
用户提交 prompt
  -> UserPromptSubmit: prefetch -> additionalContext + .codex/theone-context.md
  -> Agent 推理
  -> PostToolUse: capture.atomic（工具/文件，控制面诊断）
  -> Stop: turn.completed -> raw_event
  -> SessionStart: 只做控制面处理，不写 raw_event
  -> 异步 Worker: raw_event -> evidence -> memory_candidate -> memory_item
```

Wrapper 兼容入口见 `scripts/examples/codex_wrapper_observe_envelope.sh`，用于无 hook 的 `codex exec` 或 CI 场景。

---

## 5. 模板说明

`doc/codex` 中的模板由安装脚本消费：

| 文件 | 作用 |
|------|------|
| `hooks.json` | Codex Hooks 配置模板 |
| `config.toml.template` | MCP 配置模板，占位符 `__THEONE_REPO__` 会替换为目标项目绝对路径 |
| `context/theone-context.md.template` | 初始注入面 |
| `AGENTS.md` | 建议合并进项目 `AGENTS.md` 的记忆捕获说明 |

`doc/adapters/codex/mapping.yaml` 是维护者参考的事件映射说明，不要求普通用户阅读。

---

## 6. 排障

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| MCP 找不到 `theone` | `~/.codex/config.toml` 未合并 | 合并 `.codex/theone-mcp.toml` 并重启 Codex |
| `memory_health` 失败 | `bin/theone` 不存在、不可执行或配置错误 | 检查 `bin/theone`、`theone.yaml`、`.theone-data/logs/theone.log` |
| `readonly database` | 数据目录不可写 | 确认 MCP args 中 `-data-dir` 指向项目内 `.theone-data` |
| prefetch 无注入 | 尚无可召回记忆或 binding 未建立 | 先完成一轮高价值对话并让 Agent 调用 `memory_observe` / `memory_remember` |
| raw_event 为空 | Hooks 未启用或 Agent 未产生有效事件 | 完成一轮有内容的 Agent 回复，再查 `turn.completed` |
| 看不到 `session.start` / `tool.result.summary` | 当前策略就是不写入 raw_event | 查 binding 和日志，不把它作为 raw_event 验收项 |
| 换目录后失效 | MCP 片段中仍是旧绝对路径 | 重跑安装脚本并重新合并 MCP |

---

## 7. 维护者发布清单

发布前至少验证：

```bash
scripts/acceptance/p6_codex_hooks.sh
python3 -m json.tool doc/codex/hooks.json >/dev/null
python3 -m unittest \
  drivers/shared/theone-build-ingest_test.py \
  drivers/shared/theone-hook-prefetch_test.py \
  drivers/shared/theone-hook-session_test.py
make package-codex VERSION=<version>
```
