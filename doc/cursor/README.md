# The One × Cursor 手动配置说明

本文面向 **使用发布包** 的 Cursor 用户：不要求克隆源码、安装 Go，也 **不依赖任何安装脚本**。按下列步骤在目标项目中手工配置 MCP、Hooks 与 Rules 即可。

---

## 0. 准备

### 0.1 获取发布包

从发布渠道下载并解压，例如：

```text
~/Apps/theone-cursor-1.0.0-darwin-arm64/
├── bin/theone
├── theone.yaml
├── drivers/cursor/
├── drivers/shared/
└── doc/cursor/          ← 模板与 Rule 原文
```

记下两个绝对路径（下文用占位符表示，请替换为你的实际路径）：

| 占位符 | 含义 | 示例 |
|--------|------|------|
| `PACKAGE_DIR` | 发布包解压目录 | `/Users/you/Apps/theone-cursor-1.0.0-darwin-arm64` |
| `PROJECT_DIR` | 要用 Cursor 打开的项目根目录 | `/Users/you/projects/my-app` |

确认 `PACKAGE_DIR/bin/theone` 可执行。若不可执行，在终端对该文件执行一次 `chmod +x`。

### 0.2 在 Cursor 中打开项目

必须用 **`PROJECT_DIR` 作为工作区根目录** 打开 Cursor，否则 Hooks 相对路径与 Rules 不会生效。

---

## 1. 部署运行时文件（`.theone-data`）

在 `PROJECT_DIR` 下手工创建目录并复制 Hook 脚本：

```text
PROJECT_DIR/
└── .theone-data/
    ├── drivers/
    │   ├── cursor/      ← 从 PACKAGE_DIR/drivers/cursor/ 整目录复制
    │   └── shared/      ← 从 PACKAGE_DIR/drivers/shared/ 整目录复制
    ├── logs/            ← 空目录即可，运行时写日志
    └── runtime-state/   ← 空目录即可，运行时写 binding
```

操作要点：

1. 将 `PACKAGE_DIR/drivers/cursor/` **整目录内容** 复制到 `PROJECT_DIR/.theone-data/drivers/cursor/`。
2. 将 `PACKAGE_DIR/drivers/shared/` **整目录内容** 复制到 `PROJECT_DIR/.theone-data/drivers/shared/`。
3. 为以下文件添加可执行权限（在 Finder 或终端均可）：
   - `.theone-data/drivers/cursor/entry.sh`
   - `.theone-data/drivers/cursor/hooks/*.sh`
   - `.theone-data/drivers/shared/*.sh`
   - `.theone-data/drivers/shared/*.py`

### 1.1 写入安装环境文件

在 `PROJECT_DIR/.theone-data/theone-install.env` 新建文件，内容如下（路径改为你自己的绝对路径）：

```bash
THEONE_PACKAGE_DIR="/绝对路径/到/PACKAGE_DIR"
THEONE_PROJECT_DIR="/绝对路径/到/PROJECT_DIR"
# 可选：默认从 Hook payload 中的当前工作目录名推导；THEONE_PROJECT_DIR 仅作兜底
# THEONE_PROJECT_ID="my-project"
# THEONE_REPO_ID="my-project"
```

Hook 运行时通过此文件定位 `bin/theone` 与 `theone.yaml`。**发布包移动位置后须更新此文件**，并同步更新下文 MCP 配置中的路径。

---

## 2. 配置 MCP

### 2.1 新建或编辑项目 MCP 配置

在 `PROJECT_DIR/.cursor/mcp.json` 写入（或合并）以下内容。若文件已存在其他 MCP server，只增加 `mcpServers.theone` 段，勿覆盖已有 server。

将 `PACKAGE_DIR`、`PROJECT_DIR` 替换为你的绝对路径：

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
        "THEONE_LOG_PATH": "PROJECT_DIR/.theone-data/logs/theone.log"
      }
    }
  }
}
```

也可在 Cursor 图形界面配置：**Settings → MCP → Add new MCP server**，字段与上表对应：

| 界面字段 | 值 |
|----------|-----|
| Name | `theone` |
| Command | `PACKAGE_DIR/bin/theone` |
| Args | `serve`、`-config`、`PACKAGE_DIR/theone.yaml`、`-data-dir`、`PROJECT_DIR/.theone-data` |
| Env | `THEONE_LOG_PATH=PROJECT_DIR/.theone-data/logs/theone.log`（可选；省略时日志默认写入数据目录） |

### 2.2 启用 MCP

1. **Reload Window**（命令面板：`Developer: Reload Window`）或重启 Cursor。
2. 打开 **Settings → MCP**，确认 `theone` 已启用且无报错。
3. 在 Agent 对话中调用工具 `memory_health`，应返回 `ok: true`。

---

## 3. 配置 Hooks

### 3.1 新建或编辑 Hooks 配置

在 `PROJECT_DIR/.cursor/hooks.json` 写入（或合并）以下内容。Hook 命令使用 **相对项目根的路径**，无需写绝对路径。

若已有 `hooks.json`，将 `hooks` 对象中的 The One 相关事件 **追加** 到现有配置，不要删除你已有的其他 hook。

```json
{
  "version": 1,
  "hooks": {
    "sessionStart": [
      {
        "command": ".theone-data/drivers/cursor/entry.sh sessionStart",
        "timeout": 8,
        "failClosed": false
      }
    ],
    "beforeSubmitPrompt": [
      {
        "command": ".theone-data/drivers/cursor/entry.sh beforeSubmitPrompt",
        "timeout": 8,
        "failClosed": false
      }
    ],
    "afterAgentResponse": [
      {
        "command": ".theone-data/drivers/cursor/entry.sh afterAgentResponse",
        "timeout": 8,
        "failClosed": false
      }
    ],
    "afterFileEdit": [
      {
        "command": ".theone-data/drivers/cursor/entry.sh afterFileEdit",
        "timeout": 8,
        "failClosed": false
      }
    ],
    "afterMCPExecution": [
      {
        "command": ".theone-data/drivers/cursor/entry.sh afterMCPExecution",
        "timeout": 8,
        "failClosed": false
      }
    ],
    "sessionEnd": [
      {
        "command": ".theone-data/drivers/cursor/entry.sh sessionEnd",
        "timeout": 8,
        "failClosed": false
      }
    ]
  }
}
```

### 3.2 各 Hook 作用

| 事件 | 作用 |
|------|------|
| `sessionStart` | 建立 session / task binding（控制面，不写 `raw_event`） |
| `beforeSubmitPrompt` | prefetch 记忆，刷新注入面 |
| `afterAgentResponse` | 写入 `turn.completed` |
| `afterFileEdit` | 写入 `file.edit.summary` |
| `afterMCPExecution` | MCP 调用链路诊断（控制面） |
| `sessionEnd` | 写入 `session.end` 并清理 |

保存后再次 **Reload Window**。

---

## 4. 配置 Rules

Cursor 通过 **项目 Rules** 引导 Agent 在高价值场景主动调用 `memory_observe` / `memory_remember`。

### 4.1 记忆捕获 Rule（必装）

1. 在 `PROJECT_DIR/.cursor/rules/` 下新建目录（若不存在）。
2. 将发布包内 `doc/cursor/rule/theone-memory-observe.mdc` **复制**为：
   ```text
   PROJECT_DIR/.cursor/rules/theone-memory-observe.mdc
   ```
3. `project_id` / `repo_id` 默认从 Hook payload 中的当前对话工作目录名推导；如需覆盖，可在 `.theone-data/theone-install.env` 或 Hook 环境中设置 `THEONE_PROJECT_ID` / `THEONE_REPO_ID`。

在 **Cursor Settings → Rules** 中应能看到该 Rule，且为 **Always Apply**。

### 4.2 记忆注入面（初始占位）

1. 将 `doc/cursor/context/theone-injected-context.mdc.template` 复制为：
   ```text
   PROJECT_DIR/.cursor/rules/theone-injected-context.mdc
   ```
2. **不要手工编辑此文件正文**；`beforeSubmitPrompt` Hook 会在有命中记忆时自动覆盖内容。

---

## 5. 验收

完成上述配置后，按顺序自检：

| 步骤 | 检查方式 | 期望结果 |
|------|----------|----------|
| 1 | Settings → MCP → `theone` | 已连接，无红色错误 |
| 2 | Agent 调用 `memory_health` | `ok: true` |
| 3 | Settings → Rules | 可见 The One 两条 Rule |
| 4 | 新开 Agent 对话，完成一轮有实质内容的问答 | `.theone-data/runtime-state/binding.cursor.json` 出现 |
| 5 | 查看日志 | `.theone-data/logs/theone.log` 有 `theone initialized` 等记录 |

可选：在终端执行（将路径换成你的）：

```bash
PACKAGE_DIR/bin/theone health \
  -config PACKAGE_DIR/theone.yaml \
  -data-dir PROJECT_DIR/.theone-data
```

查看最近入库事件：

```bash
sqlite3 PROJECT_DIR/.theone-data/memory.db \
  "SELECT event_type, occurred_at FROM raw_event ORDER BY occurred_at DESC LIMIT 10;"
```

### 5.1 当前 raw_event 策略

| 事件 | 是否进入 `raw_event` |
|------|----------------------|
| `session.start` | 否（仅 binding） |
| `tool.result.summary` | 否（仅诊断） |
| `file.edit.summary` | 是 |
| `turn.completed` | 是 |
| `session.end` | 是 |
| Agent 主动 `memory_observe` | 是 |

---

## 6. 能力概览

| 组件 | 作用 |
|------|------|
| **MCP** | `memory_health`、`memory_search`、`memory_context`、`memory_observe`、`memory_remember` |
| **Hooks** | 自动 prefetch、回合/文件/会话事件、session binding |
| **Rules** | 引导 Agent 结构化写入高价值记忆 |

数据流：

```text
用户提交 prompt
  → beforeSubmitPrompt: prefetch → .cursor/rules/theone-injected-context.mdc
  → Agent 推理
  → afterAgentResponse: turn.completed → raw_event
  → afterFileEdit: file.edit.summary → raw_event
  → sessionStart / afterMCPExecution: 控制面，不写 raw_event
  → 异步处理: raw_event → evidence → memory_candidate → memory_item
```

结构化 `content_summary` 规范见 `doc/shared/content-summary-structured.md`（发布包内 `doc/shared/` 亦有副本）。

---

## 7. 排障

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| Settings → MCP 无 `theone` | `.cursor/mcp.json` 未创建或路径错误 | 核对第 2 节，路径必须为绝对路径 |
| `memory_health` 失败 | 二进制不可执行或 `theone.yaml` 路径错误 | 检查 `chmod +x`、`theone-install.env`、日志 |
| `readonly database` | `.theone-data` 不可写 | 确认 MCP `-data-dir` 指向项目内 `.theone-data` |
| Rules 不生效 | 未用项目根打开 Cursor | 关闭后从 `PROJECT_DIR` 重新打开并 Reload |
| Hooks 不触发 | `hooks.json` 路径错或脚本无执行权限 | 核对第 1、3 节 |
| `raw_event` 为空 | 仅触发被抑制事件或未产生有效对话 | 完成一轮有内容的 Agent 回复后重查 |
| prefetch 无注入 | 尚无记忆或 binding 未建立 | 先完成高价值对话并让 Agent 调用 `memory_observe` |
| 移动发布包后失效 | 旧绝对路径残留 | 更新 `theone-install.env` 与 `.cursor/mcp.json` |

---

## 8. 与 Claude Code / Codex 并行

同一仓库可同时存在 `.cursor/`、`.claude/`、`.codex/` 配置。binding 与 prompt-cache **按 agent 分文件**，互不覆盖。
