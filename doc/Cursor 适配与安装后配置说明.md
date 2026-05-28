# Cursor 适配与安装后配置说明

> 适用版本：theone v1.0.0-dev（官方 MCP Go SDK + stdio）  
> 更新：2026-05-27

本文档说明：**在本地完成 theone 编译（安装）之后**，为在 Cursor 中稳定、连续使用还需要做哪些配置。  
theone **没有**独立安装包或 `install.sh`；「安装」= 克隆仓库 + `make build`。

---

## 1. 安装本身会做什么 / 不会做什么

### 1.1 `make build` 之后自动具备的能力

| 能力 | 说明 |
|------|------|
| 单二进制 | `bin/theone`，支持 `serve` / `health` / `status` |
| 首次 `serve` | 自动建库、执行 migration、注册 MCP 工具 |
| 服务端自动处理 | `observe` 写入 `raw_event` 后**必定**入队 `extract_evidence`（`processor.provider` 仅允许 `rule_based`） |
| 后台 Worker | `serve` 模式**始终**启动 Worker 消费 `async_job` |
| 显式 remember | `memory.remember` 同步经准入控制器，未通过返回 `ADMISSION_REJECTED` |

### 1.2 安装后不会自动完成的事项

| 事项 | 说明 |
|------|------|
| 写入 Cursor 全局 MCP | 需项目级或用户级 `mcp.json` |
| Cursor Rules | 需 `.cursor/rules/` 或用户 Rules |
| 对话自动进记忆 | MCP 连通 ≠ 自动 `memory_observe` |
| 跨会话记忆 | 需 Agent 调用 `memory_observe` / `memory_remember` 后再检索 |
| 修改 `theone.yaml` 默认路径 | 默认仍指向 `~/.theone/`，Cursor 场景建议改用项目内数据目录 |

---

## 2. 安装后必做配置（清单）

按顺序完成：

- [ ] **1. 编译二进制**（见 §3）
- [ ] **2. 配置项目级 MCP**（见 §4，修改为本机绝对路径）
- [ ] **3. 确认 Cursor Rules 已加载**（见 §5）
- [ ] **4. 重启 Cursor / Reload Window**
- [ ] **5. 验收 MCP 与记忆链路**（见 §7）

可选：

- [ ] 使用 `sqlite_fts5` 构建以获得完整 FTS 检索（`make build` 默认已带）
- [ ] 将同一 MCP 配置复制到 `~/.cursor/mcp.json`（多仓库复用）

---

## 3. 编译与本地自检

在项目根目录执行：

```bash
make build
```

等价于：

```bash
go build -tags sqlite_fts5 -o bin/theone ./cmd/theone
```

**安装后自检**（数据目录与 MCP 配置保持一致，下文以 `.theone-data` 为例）：

```bash
bin/theone health \
  -config ./theone.yaml \
  -data-dir ./.theone-data
```

期望：stdout 返回 JSON，且 `ok: true`。

---

## 4. Cursor MCP 配置（必做）

### 4.1 配置文件位置

| 范围 | 路径 | 优先级 |
|------|------|--------|
| **项目级（推荐）** | `<仓库根>/.cursor/mcp.json` | 打开本仓库时生效 |
| 用户级 | `~/.cursor/mcp.json` | 所有项目 |

本仓库已提供模板：`.cursor/mcp.json`。**换机器或换目录后必须改 `command` 与路径为当前环境的绝对路径。**

### 4.2 推荐配置说明

Cursor 以子进程方式拉起 `theone serve`。在受限环境下需注意：

1. **数据目录**：使用仓库内可写目录（如 `.theone-data`），避免仅写 `~/.theone` 导致 `readonly database`。
2. **PID 文件**：设置 `THEONE_DISABLE_PID=1`，避免写 `~/.theone/theone.pid` 失败。
3. **日志**：通过 `THEONE_LOG_PATH` 指向项目内日志，便于在 MCP Output 旁对照排障。

**模板**（将 `<REPO_ROOT>` 替换为本机仓库绝对路径）：

```json
{
  "mcpServers": {
    "theone": {
      "command": "<REPO_ROOT>/bin/theone",
      "args": [
        "serve",
        "-config",
        "<REPO_ROOT>/theone.yaml",
        "-data-dir",
        "<REPO_ROOT>/.theone-data"
      ],
      "env": {
        "THEONE_DISABLE_PID": "1",
        "THEONE_LOG_PATH": "<REPO_ROOT>/.theone-data/logs/theone.log"
      }
    }
  }
}
```

### 4.3 与 `theone.yaml` 的关系

| 配置项 | `theone.yaml` 默认 | Cursor 推荐 |
|--------|-------------------|-------------|
| `storage.path` | `~/.theone/memory.db` | 由 `-data-dir` 覆盖为 `<data-dir>/memory.db` |
| `logging.path` | `~/.theone/logs/theone.log` | 由 `THEONE_LOG_PATH` 覆盖 |
| PID 文件 | `~/.theone/theone.pid` | `THEONE_DISABLE_PID=1` 跳过写入 |

**优先级**：CLI / 环境变量 > YAML > 内置默认。

### 4.4 MCP 工具命名（Cursor 专用）

对外暴露的工具名使用 **下划线**（如 `memory_health`），内部实现仍为 `memory.health`。

| 对外名（Cursor 调用） | 内部 canonical |
|----------------------|----------------|
| `memory_health` | `memory.health` |
| `memory_observe` | `memory.observe` |
| `memory_remember` | `memory.remember` |
| `memory_search` | `memory.search` |

若 MCP 面板出现 *some tools have naming issues*，请确认已使用当前 `bin/theone`（含别名适配的版本），并 Reload Cursor。

---

## 5. Cursor Rules（记忆连续可用，强烈建议）

### 5.1 文件位置

```
<仓库根>/.cursor/rules/theone-memory-observe.mdc
```

本仓库已提交，`alwaysApply: true`。用 **仓库根目录** 作为 Cursor 工作区打开，Rules 才会加载。

### 5.2 作用

- 引导 Agent 在**有实质内容**的对话中调用 **`memory_observe`**（至少 `session.start` + 用户消息 + Agent 结论）
- **入库关键字段完备**（L1 归属 + L2 检索字段 + L3 事件专属），不强制机械传满 schema 所有键；详见 Rule 中 L1/L2/L3 分级
- 说明与 `memory_remember` / `memory_search` / `memory_context` 的分工
- 强调只写**摘要**，禁止全文、完整 diff、密钥；**禁止**在 `conversation.message` 中填写 `tool_name`

**重要**：仅配置 MCP **不会**自动写入记忆。完整规范与 JSON 示例见 `.cursor/rules/theone-memory-observe.mdc`。

### 5.3 用户级 Rules（可选）

若希望所有项目共用 observe 规则，可复制到：

```text
~/.cursor/rules/theone-memory-observe.mdc
```

一般更建议只在本仓库保留项目级 Rule。

---

## 6. 安装后无需改动的服务端配置（了解即可）

`theone.yaml` 中与「连续可用」相关的默认项（一般保持即可）：

```yaml
processor:
  provider: "rule_based"        # 唯一合法值；observe 后必入队，经准入决定是否写入 memory_item

automation:
  poll_interval_ms: 1000          # serve 时始终启动 Worker

capture:
  require_session_for_agent_events: true   # observe 需 session_id（除 session.start）

memory:
  default_workspace: "local_default_workspace"
```

---

## 7. 验收步骤

### 7.1 MCP 连接

1. Cursor：**Settings → MCP**，确认 `theone` 已启用（非持久 error）。
2. 工具列表中应能看到 `memory_health`、`memory_observe` 等（下划线名）。
3. 调用 `memory_health`，应返回 `ok: true`。

### 7.2 日志

查看：

```text
<REPO_ROOT>/.theone-data/logs/theone.log
```

正常启动应出现 `theone initialized`，且 `db_path` 指向 `.theone-data/memory.db`。  
周期性出现 `mcp stdio client disconnected` / `stdio server stopped` 多为 Cursor 探测连接，**不一定表示失败**。

### 7.3 记忆是否写入

新开 Agent 对话，按 Rule 应至少调用一次 `memory_observe`（`session.start`）。然后：

- 调用 `memory_capture_events`（limit=5），或
- 本地查询：`sqlite3 .theone-data/memory.db "SELECT COUNT(*) FROM raw_event;"`

若仍为 0，检查 Rules 是否加载、Agent 是否实际调用了工具。

### 7.4 Rules 是否加载

**Settings → Rules**（或 Project Rules）中应能看到 **theone-memory-observe** 相关描述。若无，Reload Window。

---

## 8. 常见问题

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| MCP 显示 error，日志 `readonly database` | 数据写在不可写目录 | 使用 `-data-dir` 指向 `.theone-data` |
| `write pid file: operation not permitted` | Cursor 禁止写 home | 设置 `THEONE_DISABLE_PID=1` |
| 工具名 naming issues 警告 | 工具名含 `.` | 使用含下划线别名的最新 `bin/theone` |
| MCP 绿但记忆为空 | 未调用 observe/remember | 启用 Rule，或手动让 Agent 调工具 |
| `fts5 unavailable` 警告 | 构建未带 `sqlite_fts5` | `make build`（默认带 tag） |
| 路径失效 | 克隆到新目录 | 更新 `mcp.json` 中所有绝对路径 |

---

## 9. 一期「连续可用」的含义（对照）

| 目标 | 安装后是否自动达成 |
|------|-------------------|
| MCP 可连接、可调 health | 需 §3 编译 + §4 MCP + Reload |
| SQLite / migration 就绪 | `serve` 时自动 |
| FTS 检索 | 需 `sqlite_fts5` 构建 |
| 对话轨迹进入 theone | 需 §5 Rules + Agent 调 `memory_observe` |
| 稳定偏好进长期记忆 | 需 `memory_remember` 或 observe 后自动候选链路 |
| 新任务拉取历史上下文 | 需先有写入，再 `memory_search` / `memory_context` |

---

## 10. 相关文档

| 文档 | 内容 |
|------|------|
| `README.md` | 构建、Codex/Claude 接入（Cursor 细节以本文为准） |
| `doc/P0 工程初始化与本地启动说明.md` | 配置项与环境变量 |
| `doc/P1 手动记忆接口与验收说明.md` | remember/search/context |
| `doc/MCP 官方 SDK 接入与多 Agent 兼容改造方案.md` | SDK 改造背景与 Phase 1 实现状态 |
| `.cursor/rules/theone-memory-observe.mdc` | observe 调用规则全文 |

---

## 11. 新机器快速复制命令

```bash
cd <REPO_ROOT>
make build
# 编辑 .cursor/mcp.json，将路径改为本机 <REPO_ROOT>
bin/theone health -config ./theone.yaml -data-dir ./.theone-data
# 重启 Cursor，验收 memory_health
```

完成以上步骤后，theone 在 Cursor 中即可作为一期本地 Memory Runtime 持续使用；记忆积累依赖 Agent 按 Rule 调用 MCP 工具，而非安装器自动完成。
