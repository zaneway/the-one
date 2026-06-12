# The One × Claude Code 手动配置说明

本文面向 **使用发布包** 的 Claude Code 用户：不要求克隆源码、安装 Go，也 **不依赖任何安装脚本**。按下列步骤手工配置 MCP、Hooks 与 CLAUDE.md 片段即可。

---

## 0. 准备

### 0.1 获取发布包

从发布渠道下载并解压，例如：

```text
~/Apps/theone-claude-1.0.0-darwin-arm64/
├── bin/theone
├── theone.yaml
├── drivers/claude_code/
├── drivers/shared/
└── doc/claude/          ← 模板与 CLAUDE.md 片段
```

记下两个绝对路径：

| 占位符 | 含义 | 示例 |
|--------|------|------|
| `PACKAGE_DIR` | 发布包解压目录 | `/Users/you/Apps/theone-claude-1.0.0-darwin-arm64` |
| `PROJECT_DIR` | Claude Code 打开的项目根目录 | `/Users/you/projects/my-app` |

确认 `PACKAGE_DIR/bin/theone` 可执行；必要时 `chmod +x`。

### 0.2 在 Claude Code 中打开项目

以 **`PROJECT_DIR` 为根目录** 打开 Claude Code。

---

## 1. 部署运行时文件（`.theone-data`）

在 `PROJECT_DIR` 下手工创建目录并复制 Hook 脚本：

```text
PROJECT_DIR/
└── .theone-data/
    ├── drivers/
    │   ├── claude_code/   ← 从 PACKAGE_DIR/drivers/claude_code/ 整目录复制
    │   └── shared/        ← 从 PACKAGE_DIR/drivers/shared/ 整目录复制
    ├── logs/
    └── runtime-state/
```

操作要点：

1. 复制 `PACKAGE_DIR/drivers/claude_code/` → `PROJECT_DIR/.theone-data/drivers/claude_code/`。
2. 复制 `PACKAGE_DIR/drivers/shared/` → `PROJECT_DIR/.theone-data/drivers/shared/`。
3. 为 `claude_code/hooks/*.sh` 与 `shared/` 下 `.sh`、`.py` 添加可执行权限。

### 1.1 写入安装环境文件

创建 `PROJECT_DIR/.theone-data/theone-install.env`：

```bash
THEONE_PACKAGE_DIR="/绝对路径/到/PACKAGE_DIR"
THEONE_PROJECT_DIR="/绝对路径/到/PROJECT_DIR"
```

发布包路径变更后须同步更新此文件与 MCP 配置。

---

## 2. 配置 MCP

Claude Code 使用项目根 **`PROJECT_DIR/.mcp.json`** 注册 MCP server。

### 2.1 新建或合并 MCP 配置

在 `PROJECT_DIR/.mcp.json` 写入（或合并）以下内容。若已有其他 server，仅增加 `mcpServers.theone` 段：

```json
{
  "mcpServers": {
    "theone": {
      "command": "PACKAGE_DIR/bin/theone",
      "args": [
        "serve",
        "-config",
        "PACKAGE_DIR/theone.yaml",
        "-data-dir",
        "PROJECT_DIR/.theone-data"
      ],
      "env": {
        "THEONE_DISABLE_PID": "1",
        "THEONE_LOG_PATH": "PROJECT_DIR/.theone-data/logs/theone.log"
      }
    }
  }
}
```

将 `PACKAGE_DIR`、`PROJECT_DIR` 替换为 **绝对路径**。

也可在 Claude Code 的 MCP 设置界面中按字段填写：`command`、`args`、`env` 与上表一致。

### 2.2 启用 MCP

1. 重启 Claude Code 或重新加载 MCP。
2. 确认 server `theone` 已连接。
3. 调用 `memory_health`，应返回 `ok: true`。

---

## 3. 配置 Hooks

Claude Code 的 Hook 配置写在 **`PROJECT_DIR/.claude/settings.json`** 的 `hooks` 字段中。

### 3.1 合并 Hooks 段

若尚无 `.claude/settings.json`，新建文件：

```json
{
  "hooks": { }
}
```

将发布包 `doc/claude/settings.hooks.example.json` 中的 **`hooks` 对象整体** 合并进 `settings.json` 的 `hooks` 字段。

**重要**：示例模板中的 `__THEONE_PROJECT__` 必须全部替换为 `PROJECT_DIR` 的 **绝对路径**（Claude Code Hook 使用绝对路径指向脚本）。合并后应类似：

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "PROJECT_DIR/.theone-data/drivers/claude_code/hooks/theone-session-start.sh",
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
            "command": "PROJECT_DIR/.theone-data/drivers/claude_code/hooks/theone-user-prompt-submit.sh",
            "timeout": 45
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit|NotebookEdit|Bash|Read|Grep|Glob|Task|WebFetch|WebSearch|mcp__.*",
        "hooks": [
          {
            "type": "command",
            "command": "PROJECT_DIR/.theone-data/drivers/claude_code/hooks/theone-post-tool-use.sh",
            "timeout": 30
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "PROJECT_DIR/.theone-data/drivers/claude_code/hooks/theone-post-tool-use-failure.sh",
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
            "command": "PROJECT_DIR/.theone-data/drivers/claude_code/hooks/theone-stop.sh",
            "timeout": 60
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "PROJECT_DIR/.theone-data/drivers/claude_code/hooks/theone-session-end.sh",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
```

若 `settings.json` 已有其他 Claude 设置项（如 `permissions`），**只合并 `hooks` 字段**，保留其余键不变。

### 3.2 各 Hook 作用

| 事件 | 作用 |
|------|------|
| `SessionStart` | session / task binding（控制面） |
| `UserPromptSubmit` | prefetch → `additionalContext` + `.claude/theone-context.md` |
| `PostToolUse` | 工具/文件链路诊断 |
| `PostToolUseFailure` | 失败工具诊断 |
| `Stop` | 写入 `turn.completed` |
| `SessionEnd` | 写入 `session.end` 并清理（**勿**把 `Stop` 当作 session 结束） |

保存后重启 Claude Code。

---

## 4. 配置 CLAUDE.md（记忆捕获说明）

### 4.1 合并记忆捕获段落

1. 打开发布包内 `doc/claude/CLAUDE.md`。
2. 将 **「The One 记忆」整节** 复制到项目根 `CLAUDE.md`（不存在则新建）。
3. 修改 `project_id` / `repo_id` 为实际项目名。
4. 与已有 `CLAUDE.md` 内容并存时，追加在文末即可。

### 4.2 记忆注入面（初始占位）

将 `doc/claude/context/theone-context.md.template` 复制为：

```text
PROJECT_DIR/.claude/theone-context.md
```

**不要手工编辑正文**；`UserPromptSubmit` Hook 会自动更新。

---

## 5. 验收

| 步骤 | 检查方式 | 期望结果 |
|------|----------|----------|
| 1 | Claude Code MCP 列表 | `theone` 已连接 |
| 2 | 调用 `memory_health` | `ok: true` |
| 3 | 项目根 `CLAUDE.md` | 含 The One 记忆段落 |
| 4 | `.claude/settings.json` | `hooks` 段路径均为正确绝对路径 |
| 5 | 新开对话并完成一轮有实质内容的问答 | `binding.claude_code.json` 出现 |
| 6 | 结束会话 | 可出现 `session.end` 类 `raw_event` |

可选健康检查：

```bash
PACKAGE_DIR/bin/theone health \
  -config PACKAGE_DIR/theone.yaml \
  -data-dir PROJECT_DIR/.theone-data
```

### 5.1 当前 raw_event 策略

| 事件 | 是否进入 `raw_event` |
|------|----------------------|
| `session.start` | 否 |
| `tool.result.summary` | 否 |
| `file.edit.summary` | 视工具而定 |
| `turn.completed` | 是（`Stop`） |
| `session.end` | 是（`SessionEnd`） |
| Agent 主动 `memory_observe` | 是 |

---

## 6. 能力概览与三端差异

| 组件 | 作用 |
|------|------|
| **MCP** | `memory_health`、`memory_search`、`memory_context`、`memory_observe`、`memory_remember` |
| **Hooks** | prefetch、turn/tool/session 事件、session binding |
| **CLAUDE.md** | 引导 Agent 结构化 `memory_observe` / `memory_remember` |

**Claude Code 与 Cursor 的配置差异**

| 项 | Cursor | Claude Code |
|----|--------|-------------|
| Hook 配置位置 | `.cursor/hooks.json` | `.claude/settings.json` → `hooks` |
| MCP 配置位置 | `.cursor/mcp.json` | `.mcp.json` |
| Hook 脚本路径 | 相对项目根 | **绝对路径** |
| 回合结束 | `afterAgentResponse` | `Stop` |
| 会话结束 | `sessionEnd` | `SessionEnd`（独立事件） |
| 注入面 | `.cursor/rules/theone-injected-context.mdc` | `.claude/theone-context.md` |
| Agent 说明 | Rules（`.mdc`） | `CLAUDE.md` |
| binding 文件 | `binding.cursor.json` | `binding.claude_code.json` |

数据流：

```text
用户提交 prompt
  → UserPromptSubmit: prefetch → additionalContext + .claude/theone-context.md
  → Agent 推理
  → PostToolUse: 控制面诊断
  → Stop: turn.completed → raw_event
  → SessionEnd: session.end + cleanup
  → SessionStart: 控制面，不写 raw_event
  → 异步处理: raw_event → evidence → memory_candidate → memory_item
```

---

## 7. 排障

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| MCP 无 `theone` | `.mcp.json` 缺失或路径错误 | 核对第 2 节绝对路径 |
| `memory_health` 失败 | 二进制或配置错误 | 查 `theone-install.env`、`.theone-data/logs/theone.log` |
| Hooks 不触发 | `settings.json` 未合并或路径仍为占位符 | 确认 `__THEONE_PROJECT__` 已全部替换 |
| prefetch 无注入 | 无记忆或 binding 未建立 | 先完成高价值对话 + `memory_observe` |
| `raw_event` 为空 | Hooks 未启用或无有效对话 | 完成一轮回复后重查 `turn.completed` |
| 移动项目目录后失效 | Hook 仍为旧绝对路径 | 更新 `settings.json` 中全部 hook `command` 路径 |

---

## 8. 与 Cursor / Codex 并行

同一仓库可同时存在 `.cursor/`、`.claude/`、`.codex/`。binding 与 prompt-cache 按 agent 分文件，互不覆盖。
