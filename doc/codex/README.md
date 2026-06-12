# The One × Codex 手动配置说明

本文面向 **使用发布包** 的 Codex 用户：不要求克隆源码、安装 Go，也 **不依赖任何安装脚本**。按下列步骤手工配置 MCP、Hooks 与 AGENTS.md 片段即可。

---

## 0. 准备

### 0.1 获取发布包

从发布渠道下载并解压，例如：

```text
~/Apps/theone-codex-1.0.0-darwin-arm64/
├── bin/theone
├── theone.yaml
├── drivers/codex/
├── drivers/shared/
└── doc/codex/           ← 模板与 AGENTS.md 片段
```

记下两个绝对路径：

| 占位符 | 含义 | 示例 |
|--------|------|------|
| `PACKAGE_DIR` | 发布包解压目录 | `/Users/you/Apps/theone-codex-1.0.0-darwin-arm64` |
| `PROJECT_DIR` | Codex 工作区项目根目录 | `/Users/you/projects/my-app` |

确认 `PACKAGE_DIR/bin/theone` 可执行；必要时 `chmod +x`。

### 0.2 在 Codex 中打开项目

以 **`PROJECT_DIR` 为根目录** 启动或打开 Codex 会话。

---

## 1. 部署运行时文件（`.theone-data`）

在 `PROJECT_DIR` 下手工创建目录并复制 Hook 脚本：

```text
PROJECT_DIR/
└── .theone-data/
    ├── drivers/
    │   ├── codex/       ← 从 PACKAGE_DIR/drivers/codex/ 整目录复制
    │   └── shared/      ← 从 PACKAGE_DIR/drivers/shared/ 整目录复制
    ├── logs/
    └── runtime-state/
```

操作要点：

1. 复制 `PACKAGE_DIR/drivers/codex/` → `PROJECT_DIR/.theone-data/drivers/codex/`。
2. 复制 `PACKAGE_DIR/drivers/shared/` → `PROJECT_DIR/.theone-data/drivers/shared/`。
3. 为 `.theone-data/drivers/codex/hooks/*.sh` 与 `drivers/shared/` 下 `.sh`、`.py` 添加可执行权限。

### 1.1 写入安装环境文件

创建 `PROJECT_DIR/.theone-data/theone-install.env`：

```bash
THEONE_PACKAGE_DIR="/绝对路径/到/PACKAGE_DIR"
THEONE_PROJECT_DIR="/绝对路径/到/PROJECT_DIR"
```

发布包路径变更后须同步更新此文件与下文 MCP 配置。

---

## 2. 配置 MCP

Codex 的 MCP 配置位于用户级 **`~/.codex/config.toml`**（或你实际使用的 Codex 配置文件）。The One 以 server 名 `theone` 注册。

### 2.1 合并 MCP 段

在 `~/.codex/config.toml` 中 **追加** 以下段落（若已有 `[mcp_servers.theone]`，用新内容覆盖该段即可）。将占位符换成你的绝对路径：

```toml
[mcp_servers.theone]
command = "PACKAGE_DIR/bin/theone"
args = [
  "serve",
  "-config",
  "PACKAGE_DIR/theone.yaml",
  "-data-dir",
  "PROJECT_DIR/.theone-data",
]
cwd = "PACKAGE_DIR"
startup_timeout_sec = 20
tool_timeout_sec = 120
enabled = true
required = false
```

说明：

- `command` / `-config` 指向 **发布包目录**（`bin/theone` 与 `theone.yaml` 留在包内，不复制进项目）。
- `-data-dir` 指向 **项目内** `.theone-data`。
- `cwd` 设为 `PACKAGE_DIR`，与发布包默认布局一致。

若你使用 Codex 图形界面的 MCP 设置，按上表字段逐项填写即可。

### 2.2 启用 MCP

1. 保存 `config.toml` 后 **重启 Codex** 或重新加载配置。
2. 确认 MCP server `theone` 已启用。
3. 调用 `memory_health`，应返回 `ok: true`。

> **多项目注意**：`~/.codex/config.toml` 是用户级配置。若多个项目共用同一 `theone` 段，`-data-dir` 只能指向一个项目的 `.theone-data`。多项目并存时，应为每个项目使用不同的 MCP server 名称，或在使用前手工切换 `-data-dir` 绝对路径。

---

## 3. 配置 Hooks

### 3.1 项目级 Hooks 文件

在 `PROJECT_DIR/.codex/hooks.json` 新建文件（或合并 `hooks` 段）。Hook 命令使用 **相对项目根** 的路径：

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.theone-data/drivers/codex/hooks/theone-session-start.sh",
            "timeout": 30
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.theone-data/drivers/codex/hooks/theone-user-prompt-submit.sh",
            "timeout": 45
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash|apply_patch|Edit|Write|mcp__.*",
        "hooks": [
          {
            "type": "command",
            "command": "~/.theone-data/drivers/codex/hooks/theone-post-tool-use.sh",
            "timeout": 30
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.theone-data/drivers/codex/hooks/theone-stop.sh",
            "timeout": 60
          }
        ]
      }
    ]
  }
}
```

### 3.2 各 Hook 作用

| 事件 | 作用 |
|------|------|
| `SessionStart` | session / task binding（控制面） |
| `UserPromptSubmit` | prefetch → `additionalContext` + `.codex/theone-context.md` |
| `PostToolUse` | 工具/文件链路诊断（控制面） |
| `Stop` | 写入 `turn.completed` |

保存后重启 Codex 或重新打开项目。

---

## 4. 配置 AGENTS.md（记忆捕获说明）

Codex 没有 Cursor 式 Rules 文件，通过项目根 **`AGENTS.md`** 引导 Agent 行为。

### 4.1 合并记忆捕获段落

1. 打开发布包内 `doc/codex/AGENTS.md`。
2. 将其中的 **「The One 记忆」整节** 复制到你项目根目录的 `AGENTS.md`（文件不存在则新建）。
3. 将文中的 `project_id` / `repo_id` 改为你的实际项目名。
4. 若项目已有 `AGENTS.md`，把 The One 段落 **追加在文末**，勿删除原有内容。

### 4.2 记忆注入面（初始占位）

将 `doc/codex/context/theone-context.md.template` 复制为：

```text
PROJECT_DIR/.codex/theone-context.md
```

**不要手工编辑正文**；`UserPromptSubmit` Hook 会在 prefetch 后自动更新。

---

## 5. 验收

| 步骤 | 检查方式 | 期望结果 |
|------|----------|----------|
| 1 | Codex MCP 列表 | `theone` 已启用 |
| 2 | 调用 `memory_health` | `ok: true` |
| 3 | 项目根 `AGENTS.md` | 含 The One 记忆段落 |
| 4 | 新开对话并完成一轮有实质内容的问答 | `.theone-data/runtime-state/binding.codex.json` 出现 |
| 5 | 日志 | `.theone-data/logs/theone.log` 有正常记录 |

可选健康检查：

```bash
PACKAGE_DIR/bin/theone health \
  -config PACKAGE_DIR/theone.yaml \
  -data-dir PROJECT_DIR/.theone-data
```

查看最近 `raw_event`：

```bash
sqlite3 PROJECT_DIR/.theone-data/memory.db \
  "SELECT event_type, occurred_at FROM raw_event ORDER BY occurred_at DESC LIMIT 10;"
```

### 5.1 当前 raw_event 策略

| 事件 | 是否进入 `raw_event` |
|------|----------------------|
| `session.start` | 否 |
| `tool.result.summary` | 否 |
| `file.edit.summary` | 视工具而定 |
| `turn.completed` | 是（`Stop` hook） |
| Agent 主动 `memory_observe` | 是 |

---

## 6. 能力概览

| 组件 | 作用 |
|------|------|
| **MCP** | `memory_health`、`memory_search`、`memory_context`、`memory_observe`、`memory_remember` |
| **Hooks** | prefetch、turn/tool 事件、session binding |
| **AGENTS.md** | 引导 Agent 结构化 `memory_observe` / `memory_remember` |

数据流：

```text
用户提交 prompt
  → UserPromptSubmit: prefetch → additionalContext + .codex/theone-context.md
  → Agent 推理
  → PostToolUse: 控制面诊断
  → Stop: turn.completed → raw_event
  → SessionStart: 控制面，不写 raw_event
  → 异步处理: raw_event → evidence → memory_candidate → memory_item
```

结构化 `content_summary` 规范见 `doc/shared/content-summary-structured.md`。

---

## 7. 排障

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| MCP 无 `theone` | `~/.codex/config.toml` 未合并 | 核对第 2 节并重启 Codex |
| `memory_health` 失败 | 路径或权限问题 | 查 `theone-install.env`、日志 |
| `readonly database` | `-data-dir` 不可写 | 确认指向 `PROJECT_DIR/.theone-data` |
| prefetch 无注入 | 无记忆或 binding 未建立 | 先完成高价值对话 + `memory_observe` |
| `raw_event` 为空 | Hooks 未启用或无有效对话 | 完成一轮 Agent 回复后重查 |
| 多项目数据串线 | 用户级 MCP 共用同一 `-data-dir` | 为各项目拆分 MCP 段或切换路径 |

---

## 8. 与 Cursor / Claude Code 并行

同一仓库可同时配置 `.cursor/`、`.claude/`、`.codex/`。binding 按 agent 分文件存储，互不覆盖。
