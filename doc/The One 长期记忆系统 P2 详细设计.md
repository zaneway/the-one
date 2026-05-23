# The One 长期记忆系统 P2 详细设计

> 基线来源：
> - `The One 长期记忆系统总体架构设计.md` v0.1 冻结版
> - `The One 长期记忆系统分期迭代研发规划.md`
> - `The One 长期记忆系统 P0-P1 详细设计.md`
> - `当前工程阶段实现状态.md`

## 1. 设计目标

P2 目标：

```text
让 Codex、Claude Code、Cursor 至少能通过统一 MCP 和轻量 Adapter 上报 session、task、tool、file edit 等事件，为 P3 自动记忆生成准备可信的 RawEvent 层。
```

P2 完成后的业务闭环：

```text
Agent / Adapter
  -> memory.observe
  -> validate / normalize / minimize
  -> upsert agent_session
  -> upsert agent_task or default_task
  -> append raw_event
  -> dedup by content_hash
  -> capture diagnostics query
```

P2 的核心交付不是“自动记忆变聪明”，而是“事件捕获可信、边界清晰、质量可观测”。P3 才会从 `raw_event` 自动抽取 `evidence`、生成候选记忆并进入 Admission/Review/Retention。

P2 通过 MCP 上报的数据只需要足够支撑可信 RawEvent 层，不直接承担高质量长期记忆生成。也就是说，P2 必须保证事件可归属、可去重、可诊断、可追溯、可验证内容边界；但不要求在事件层完成语义准入、用户确认、强化衰减或自动 Review。

## 2. 阶段边界

### 2.1 必须交付

1. `memory.observe` MCP 工具。
2. `agent_session`、`agent_task`、`raw_event` 三张表和 migration。
3. Capture Adapter 抽象、capability 模型和 capture quality 模型。
4. RawEvent DTO、事件类型规范、source/channel 规范。
5. P2 事件归一化、内容最小化、幂等去重。
6. session/task 生命周期写入。
7. 工具调用、工具结果摘要、文件编辑摘要写入。
8. 捕获诊断查询：
   - session 列表。
   - task 列表。
   - raw_event 列表和过滤。
   - capture quality 查看。
9. Codex、Claude Code、Cursor 的 P2 接入样例或降级策略。
10. P2 单元测试、repository 测试、MCP 工具测试和验收脚本。

### 2.2 明确不交付

1. 不从 `raw_event` 自动生成长期记忆。
2. 不生成 `evidence`。
3. 不实现 Admission Control。
4. 不实现 Retention Job。
5. 不实现 `async_job` 执行器和异步任务表写入。
6. 不要求三个 Agent 都达到 Level4。
7. 不保存完整会话、完整工具输出、完整 diff 或完整源码。
8. 不做在线 LLM 摘要、rerank 或自动反思。
9. 不重写 P1 `memory.remember/search/context/review`。

说明：总体架构中 `memory.observe` 最终会进入 `raw_event -> async_job -> evidence extraction` 链路。P2 是分期裁剪版本，只保存 `raw_event` 并返回 `pipeline=raw_event_only`；P3 再补 `async_job`、evidence extraction、candidate generation 和 Admission，不应在 P2 提前实现半成品异步队列。

### 2.3 与 P1 的衔接

P2 复用 P1 已完成能力：

1. MCP Registry 和工具注册机制。
2. `internal/ingest` 的内容边界检查思路。
3. `internal/storage/sqlite` 的 migration、短事务、错误码映射。
4. `internal/diagnostics` 的 health/status 组织方式。
5. `workspace_id/project_id/repo_id/session_id/task_id` scope 字段语义。
6. P1 中已预留的 `memory_item.session_id`、`memory_item.task_id`、`evidence.raw_event_id`。

P2 不直接调用 P1 `Remember` 自动写入 memory。用户显式声明如果需要立即形成稳定记忆，仍由 Agent 调用 `memory.remember`；`memory.observe` 只保存事件层事实。

## 3. 总体架构

```text
Codex / Claude Code / Cursor
        |
        | MCP Tools + Capture Adapter / Hook / Rule / Wrapper
        v
memory.observe
        |
        +-- request validation
        +-- event normalization
        +-- content minimization
        +-- session/task resolution
        +-- event dedup
        v
Capture Service
        |
        +-- CaptureRepository
        |     - upsert agent_session
        |     - upsert agent_task
        |     - insert raw_event append-only
        |
        +-- Capture Diagnostics
              - list sessions
              - list tasks
              - list raw events
              - capture quality summary
```

推荐新增代码目录：

```text
internal/capture
internal/mcp/tools/capture.go
internal/storage/sqlite/capture_repository.go
internal/storage/sqlite/migrations/0004_init_capture.sql
examples/agents/claude-code
examples/agents/codex
examples/agents/cursor
```

模块职责：

| 模块 | 职责 |
|---|---|
| `internal/capture` | DTO、事件归一化、capability/quality、Observe Service |
| `internal/mcp/tools/capture.go` | `memory.observe` 和捕获诊断工具 MCP 适配 |
| `internal/storage` | Capture repository 接口定义 |
| `internal/storage/sqlite` | session/task/raw_event 持久化、去重、查询 |
| `internal/diagnostics` | 汇总捕获质量和 storage capability |
| `examples/agents/*` | 三类 Agent 的 P2 接入样例和降级说明 |

## 4. 核心数据模型

### 4.1 agent_session

`agent_session` 表记录一次 Agent 工作会话。P2 允许 Adapter 显式传入 `session_id`，也允许服务端在 `session.start` 且 `session_id` 为空时生成。

```sql
create table if not exists agent_session (
  id                         text primary key,
  agent_type                 text not null,
  workspace_id               text not null,
  project_id                 text,
  repo_id                    text,
  capture_level              integer not null default 1,
  capture_capabilities_json  text,
  capture_quality_json       text,
  started_at                 datetime not null,
  ended_at                   datetime,
  goal_summary               text,
  status                     text not null,
  created_at                 datetime not null,
  updated_at                 datetime not null
);
```

推荐索引：

```sql
create index if not exists idx_agent_session_scope
  on agent_session(workspace_id, project_id, repo_id, agent_type, started_at);

create index if not exists idx_agent_session_status
  on agent_session(status, updated_at);
```

字段规则：

| 字段 | 规则 |
|---|---|
| `agent_type` | `codex`、`claude_code`、`cursor`、`unknown` |
| `capture_level` | 实际捕获等级，范围 1-4 |
| `capture_capabilities_json` | Adapter 声明能力，不代表实际捕获完整度 |
| `capture_quality_json` | 本 session 已捕获事件质量统计 |
| `goal_summary` | 任务目标摘要，不保存完整 prompt |
| `status` | `active`、`completed`、`failed`、`interrupted`、`unknown` |

### 4.2 agent_task

`agent_task` 在 session 内表达任务边界。P2 不做复杂任务识别；如果 Adapter 未提供明确 task，服务端为每个 session 创建 `default_task`。

```sql
create table if not exists agent_task (
  id                 text primary key,
  session_id         text,
  workspace_id       text not null,
  project_id         text,
  repo_id            text,
  task_summary       text not null,
  status             text not null,
  started_at         datetime not null,
  ended_at           datetime,
  outcome_summary    text,
  created_at         datetime not null,
  updated_at         datetime not null
);
```

推荐索引：

```sql
create index if not exists idx_agent_task_session
  on agent_task(session_id, status, started_at);

create index if not exists idx_agent_task_scope
  on agent_task(workspace_id, project_id, repo_id, status, started_at);
```

约束：

1. `task_summary` 是摘要，不保存完整用户 prompt。
2. `outcome_summary` 是结果摘要，不保存完整 Agent 回复。
3. `session_id` 可空，用于未来导入或批处理；P2 Agent 自动捕获应尽量绑定 session。
4. `status` 推荐值：`active`、`succeeded`、`failed`、`interrupted`、`unknown`。

### 4.3 raw_event

`raw_event` 是 P2 的核心表。它保存最小化后的事件事实，不代表长期记忆。

```sql
create table if not exists raw_event (
  id                  text primary key,
  session_id          text,
  task_id             text,
  workspace_id        text,
  project_id          text,
  repo_id             text,
  agent_type          text,
  event_type          text not null,
  source_channel      text,
  occurred_at         datetime not null,

  actor               text,
  tool_name           text,
  input_summary       text,
  output_summary      text,
  content_summary     text,

  keywords_json       text,
  salient_spans_json  text,
  source_refs_json    text,
  content_hash        text,

  sensitivity         text default 'normal',
  retention_hint      text,
  created_at          datetime not null
);
```

推荐索引：

```sql
create index if not exists idx_raw_event_session
  on raw_event(session_id, task_id, event_type, occurred_at);

create index if not exists idx_raw_event_scope
  on raw_event(workspace_id, project_id, repo_id, event_type, occurred_at);

create index if not exists idx_raw_event_hash
  on raw_event(content_hash, session_id, event_type);

create index if not exists idx_raw_event_agent
  on raw_event(agent_type, source_channel, occurred_at);
```

可选唯一索引：

```sql
create unique index if not exists idx_raw_event_dedup_session
  on raw_event(content_hash, session_id, event_type)
  where content_hash is not null and content_hash != '' and session_id is not null;
```

如果 SQLite partial unique index 在目标环境中出现兼容问题，则用 repository 层查询实现幂等，不强依赖唯一索引。

## 5. 事件规范

### 5.1 event_type

P2 支持以下事件类型：

| event_type | 用途 |
|---|---|
| `session.start` | 会话开始 |
| `session.end` | 会话结束 |
| `task.start` | 明确任务开始 |
| `task.result` | 任务结果或阶段结果 |
| `conversation.message` | 用户消息摘要 |
| `agent.response.summary` | Agent 回复摘要 |
| `tool.call` | 工具调用摘要 |
| `tool.result.summary` | 工具结果摘要 |
| `file.edit.summary` | 文件编辑摘要 |
| `user.correction` | 用户纠正 |
| `user.declaration` | 用户显式声明事件 |
| `agent.decision` | Agent 在任务中形成的中间决策摘要 |

P2 只要求 `session.start`、`session.end`、`task.result`、`tool.call`、`tool.result.summary`、`file.edit.summary` 可用。conversation 类事件取决于具体 Agent 能力。

### 5.2 source_channel

推荐值：

| source_channel | 含义 |
|---|---|
| `agent_session` | 来自真实 Agent session 的自动或半自动捕获 |
| `mcp_tool` | Agent 主动调用 MCP 工具上报 |
| `manual_cli` | 本地命令或验收脚本手动写入 |

`source_channel` 表达事件来源语义，应与总体架构保持稳定。Hook、wrapper、日志采集和 Cursor rule 这类采集技术细节不作为新的 `source_channel`，统一放入 `source_refs.capture_method`：

```text
adapter_hook
wrapper_log
cursor_rule
manual_mcp_call
filesystem_watcher
git_diff
```

### 5.3 actor

推荐值：

```text
user
agent
tool
adapter
system
```

### 5.4 MCP 可上报数据范围

P2 的 MCP 上报入口是 `memory.observe`。该工具可以上报以下数据：

| 数据类别 | 代表字段或事件 | P2 用途 | 是否足够 |
|---|---|---|---|
| 范围和来源 | `workspace_id`、`project_id`、`repo_id`、`agent_type`、`source_channel`、`actor`、`occurred_at` | 事件归属、查询过滤、审计定位 | 足够支撑 P2 |
| Session 生命周期 | `session_id`、`session.start`、`session.end`、`session.goal_summary`、`session.status` | 建立 Agent 工作会话边界 | 足够支撑 P2 |
| Task 边界 | `task_id`、`task.start`、`task.result`、`task.task_summary`、`task.outcome_summary` | 建立任务级归因边界 | 基本足够，依赖 `normalized_task` 兜底 |
| 工具调用 | `tool.call`、`tool.result.summary`、`tool_name`、`input_summary`、`output_summary`、`source_refs.exit_code`、`source_refs.command_hash` | 保存工具调用摘要、结果摘要和错误签名 | 足够支撑 P2 |
| 文件编辑 | `file.edit.summary`、`content_summary`、`source_refs.file_path`、`source_refs.symbol`、`source_refs.before_hash`、`source_refs.after_hash` | 保存文件变更摘要，不保存完整 diff | 足够支撑 P2 |
| 用户/Agent 语义事件 | `user.declaration`、`user.correction`、`agent.decision`、`conversation.message`、`agent.response.summary` | 保存显式声明、纠正和中间决策摘要 | 只够事件层，P3 仍需 evidence 抽取 |
| 边界和质量 | `keywords`、`salient_spans`、`content_hash`、`sensitivity`、`retention_hint`、`capture_capabilities` | 内容最小化、幂等去重、敏感等级、捕获质量诊断 | 足够支撑 P2 |

P2 数据足够回答“发生了什么、来自哪里、属于哪个 session/task、是否被可靠捕获、是否触碰内容边界”。P2 数据不承诺直接回答“是否值得长期记忆、应该是什么 memory_type、是否需要用户 review、应该保留多久”，这些判断进入 P3。

## 6. Capture Capability 和 Quality

### 6.1 capability 声明

Adapter 在 `session.start` 或首次 `memory.observe` 时上报能力：

```json
{
  "conversation_capture": true,
  "tool_call_capture": true,
  "tool_output_capture": true,
  "file_edit_capture": false,
  "session_lifecycle": true,
  "mcp_observe": true,
  "requires_wrapper": false,
  "requires_rules_injection": true
}
```

能力到等级的映射：

| 等级 | 条件 |
|---:|---|
| Level1 | 仅支持 Agent 主动调用 memory tools |
| Level2 | 支持 session lifecycle 和显式声明/任务结果 |
| Level3 | 支持工具调用和工具结果摘要 |
| Level4 | 支持 conversation、tool、file edit、session lifecycle 和 memory observe |

`capture_level` 取实际可用能力等级，不取理论目标。

### 6.2 quality 统计

`capture_quality_json` 推荐结构：

```json
{
  "has_session_start": true,
  "has_session_end": true,
  "has_task_result": true,
  "captured_event_count": 18,
  "deduped_event_count": 3,
  "tool_call_count": 5,
  "tool_result_count": 5,
  "file_edit_count": 2,
  "conversation_message_count": 4,
  "missing_capabilities": ["file_edit_capture"],
  "content_boundary_rejections": 0,
  "last_event_at": "2026-05-23T20:00:00Z"
}
```

P2 不需要精准判断理论上“应该捕获多少事件”，但必须能回答：

1. 这个 session 实际捕获到了哪些类型事件。
2. Adapter 声称支持哪些能力。
3. 哪些事件被去重。
4. 哪些事件因内容边界被拒绝。
5. 当前 session 大致处于 Level2、Level3 还是 Level4。

## 7. memory.observe 设计

### 7.1 请求结构

```json
{
  "session_id": "sess_001",
  "task_id": "task_001",
  "agent_type": "claude_code",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "event_type": "tool.result.summary",
  "source_channel": "agent_session",
  "occurred_at": "2026-05-23T20:00:00+08:00",
  "actor": "tool",
  "tool_name": "go test",
  "input_summary": "运行全部 Go 测试",
  "output_summary": "测试通过",
  "content_summary": "",
  "keywords": ["go test", "pass"],
  "salient_spans": ["make test: pass"],
  "source_refs": [
    {
      "source_type": "tool_output",
      "capture_method": "adapter_hook",
      "command_hash": "sha256:...",
      "exit_code": 0
    }
  ],
  "content_hash": "sha256:...",
  "sensitivity": "normal",
  "retention_hint": "short_term",
  "capture_capabilities": {
    "conversation_capture": true,
    "tool_call_capture": true,
    "tool_output_capture": true,
    "file_edit_capture": true,
    "session_lifecycle": true,
    "mcp_observe": true
  },
  "session": {
    "goal_summary": "实现 P2 事件捕获",
    "status": "active"
  },
  "task": {
    "task_summary": "运行测试验证 P2",
    "status": "active",
    "outcome_summary": ""
  }
}
```

字段说明：

| 字段 | 要求 |
|---|---|
| `event_type` | 必填 |
| `workspace_id` | Agent session 来源必填 |
| `agent_type` | Agent session 来源必填 |
| `occurred_at` | 可空；为空时服务端使用当前时间 |
| `content_hash` | 推荐必填；为空时服务端用最小化字段计算 |
| `source_refs` | 保存 hash、路径、符号、exit_code 等引用，不保存全文 |
| `session` | session lifecycle 事件或首次观察时使用 |
| `task` | task lifecycle 或 default_task 创建时使用 |

`source_refs` 推荐结构：

工具调用或工具结果：

```json
{
  "source_type": "tool_output",
  "capture_method": "adapter_hook",
  "tool_name": "go test",
  "command_hash": "sha256:...",
  "exit_code": 1,
  "error_signature": "TestTokenExpiry failed at exact expiry timestamp"
}
```

文件编辑：

```json
{
  "source_type": "file_edit_summary",
  "capture_method": "git_diff",
  "file_path": "internal/auth/middleware.go",
  "symbol": "ValidateToken",
  "change_type": "modify",
  "before_hash": "sha256:...",
  "after_hash": "sha256:..."
}
```

用户纠正：

```json
{
  "source_type": "user_correction",
  "target_event_id": "evt_001",
  "correction_type": "wrong_assumption"
}
```

Agent 中间决策：

```json
{
  "source_type": "agent_decision",
  "decision_summary": "P2 只保存 raw_event，不提前生成 evidence。",
  "reason_summary": "保持阶段边界，避免污染 P1 稳定记忆。"
}
```

这些结构只保存引用、摘要、hash 和错误签名，不保存完整命令输出、完整 diff、完整 prompt 或完整 Agent 回复。

### 7.2 响应结构

```json
{
  "request_id": "req_observe_001",
  "raw_event_id": "evt_001",
  "session_id": "sess_001",
  "task_id": "task_001",
  "accepted": true,
  "pipeline": "raw_event_only",
  "deduped": false,
  "capture_level": 3
}
```

被去重时：

```json
{
  "request_id": "req_observe_001",
  "raw_event_id": "evt_existing",
  "session_id": "sess_001",
  "task_id": "task_001",
  "accepted": true,
  "pipeline": "raw_event_only",
  "deduped": true,
  "capture_level": 3
}
```

### 7.3 处理流程

```text
memory.observe
  -> decode request
  -> assign request_id
  -> validate event_type / scope / agent_type
  -> normalize source_channel / capture_method / occurred_at / content_hash
  -> content minimization check
  -> resolve or create session
  -> resolve or create task
  -> find duplicate raw_event
  -> if duplicate: update session quality and return deduped
  -> insert raw_event
  -> update session/task status if lifecycle event
  -> update capture_quality_json
  -> return accepted
```

### 7.4 幂等去重规则

优先级：

```text
if session_id != "" and content_hash != "":
    dedup_key = content_hash + session_id + event_type
else:
    dedup_key = content_hash + source_channel + workspace_id + project_id + repo_id + event_type
```

如果 `content_hash` 为空，服务端使用以下字段计算：

```text
event_type
agent_type
workspace_id
project_id
repo_id
actor
tool_name
input_summary
output_summary
content_summary
keywords
salient_spans
source_refs
```

服务端计算 hash 时只能使用最小化后的字段，不要求客户端发送完整原文。如果最小化字段不足以稳定计算 hash，返回 `VALIDATION_FAILED`，不得要求 Adapter 回传完整原文。

### 7.5 内容边界

P2 复用并扩展 P1 内容最小化规则：

| 字段 | 默认限制 |
|---|---:|
| `input_summary` | 1200 字符 |
| `output_summary` | 2000 字符 |
| `content_summary` | 2000 字符 |
| `keywords` | 30 个 |
| `salient_spans` | 10 个 |
| 单个 `salient_span` | 500 字符 |
| `source_refs_json` | 4000 字符 |

拒绝规则：

1. 字段名或 JSON 中出现 `full_text`、`full_output`、`full_diff` 时拒绝。
2. `output_summary` 或 `content_summary` 超过限制时拒绝。
3. `file.edit.summary` 不允许保存完整 diff。
4. `tool.result.summary` 不允许保存完整工具输出。
5. `conversation.message` 不允许保存完整长 prompt，只保存摘要和关键片段。

错误响应：

```json
{
  "request_id": "req_observe_001",
  "accepted": false,
  "error": {
    "error_code": "CONTENT_TOO_LARGE",
    "message": "tool output summary exceeds max_output_summary_chars",
    "retryable": false,
    "fallback_hint": "send summarized content with salient_spans and content_hash"
  }
}
```

## 8. Session 和 Task 解析

### 8.1 session.start

当 `event_type=session.start`：

1. 如果 `session_id` 为空，服务端生成 `sess_*`。
2. 写入或更新 `agent_session`。
3. `status=active`。
4. 计算并保存 `capture_level`。
5. 如果请求带 `task`，同时创建 task；否则创建 `default_task`。
6. 写入一条 `raw_event`，用于保留 session start 证据。

### 8.2 普通事件

普通事件必须尽量绑定 `session_id`。如果未绑定：

1. `source_channel=manual_cli` 可以接受。
2. `source_channel=agent_session` 默认拒绝，并提示先发送 `session.start`。
3. 如果配置允许宽松模式，可创建 `unknown_session`，但验收环境不启用宽松模式。

### 8.3 default_task

当请求没有 `task_id`：

1. 如果请求带 `task.task_summary`，先对 `task_summary` 做 trim、空白归一化、长度截断，得到 `normalized_task`。
2. 按 `session_id + normalized_task` 查找已有 `agent_task`。
3. 如果未找到，则创建明确 task：

```text
task_summary = minimized task.task_summary
status = task.status or active
```

4. 如果请求没有 `task.task_summary`，查询该 session 是否已有 `default_task`。
5. 如果没有，创建：

```text
task_summary = session.goal_summary or "default task"
status = active
```

6. 将 raw_event 绑定到解析后的明确 task 或 `default_task`。

P2 不新增独立的 `normalized_task` 列，repository 可在写入前使用内存归一化值完成查找；如果后续 P3/P4 需要跨 session 任务聚合，再评估是否持久化标准化任务键。

### 8.4 task.result

当 `event_type=task.result`：

1. 更新 `agent_task.status`。
2. 写入 `outcome_summary`。
3. 不直接写入 `memory_item`。
4. P3 可基于该事件和已注入记忆做结果归因。

### 8.5 session.end

当 `event_type=session.end`：

1. 更新 `agent_session.status` 和 `ended_at`。
2. 如果 default_task 仍为 active，按请求 status 或 `unknown` 结束。
3. 写入 session end raw_event。
4. 更新 capture quality summary。

## 9. 捕获诊断工具

P2 新增以下 MCP 诊断工具。工具命名保守使用 `memory.capture.*`，避免和 P1 `memory.review` 混淆。

### 9.1 memory.capture.sessions

请求：

```json
{
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "agent_type": "cursor",
  "status": "active",
  "limit": 20
}
```

响应：

```json
{
  "sessions": [
    {
      "session_id": "sess_001",
      "agent_type": "cursor",
      "capture_level": 2,
      "status": "completed",
      "goal_summary": "形成 P2 详细设计文档",
      "started_at": "2026-05-23T19:22:00+08:00",
      "ended_at": "2026-05-23T20:10:00+08:00",
      "capture_quality": {
        "captured_event_count": 8,
        "file_edit_count": 1
      }
    }
  ]
}
```

### 9.2 memory.capture.tasks

请求：

```json
{
  "session_id": "sess_001",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "status": "",
  "limit": 20
}
```

### 9.3 memory.capture.events

请求：

```json
{
  "session_id": "sess_001",
  "task_id": "task_001",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "event_types": ["tool.result.summary", "file.edit.summary"],
  "agent_type": "claude_code",
  "limit": 50
}
```

响应必须只返回摘要字段、hash 和 source refs，不返回任何完整 output 或 diff。

### 9.4 memory.capture.quality

请求：

```json
{
  "session_id": "sess_001"
}
```

响应：

```json
{
  "session_id": "sess_001",
  "agent_type": "claude_code",
  "capture_level": 3,
  "capabilities": {
    "conversation_capture": true,
    "tool_call_capture": true,
    "tool_output_capture": true,
    "file_edit_capture": false,
    "session_lifecycle": true,
    "mcp_observe": true
  },
  "quality": {
    "captured_event_count": 18,
    "tool_call_count": 5,
    "tool_result_count": 5,
    "file_edit_count": 0,
    "deduped_event_count": 3,
    "content_boundary_rejections": 0
  }
}
```

## 10. Agent 接入设计

### 10.1 Claude Code

P2 推荐路径：

```text
Claude Code hooks
  -> hook script summarizes event
  -> memory.observe
```

目标能力：

| 能力 | P2 目标 |
|---|---|
| session lifecycle | 支持 |
| tool call | 支持 |
| tool result summary | 支持 |
| file edit summary | 尽量支持 |
| conversation summary | 尽量支持 |
| 目标等级 | Level3+ |

接入样例内容：

1. hook 配置样例。
2. `session.start/session.end` 调用示例。
3. `tool.call/tool.result.summary` 调用示例。
4. 工具输出摘要脚本：只提取命令、exit code、错误签名、关键片段、hash。

### 10.2 Codex

P2 推荐路径：

```text
Codex wrapper / log collector
  -> normalize session and tool summary
  -> memory.observe
```

目标能力：

| 能力 | P2 目标 |
|---|---|
| session lifecycle | 支持 |
| tool call | 尽量支持 |
| tool result summary | 尽量支持 |
| file edit summary | 可通过 git diff summary |
| conversation summary | 不强制 |
| 目标等级 | Level2+ |

风险控制：

1. Codex 运行环境差异较大，不能假定所有工具事件都可被动捕获。
2. P2 先提供 wrapper/log collector 样例，不把 Codex 深度集成作为阻塞项。
3. 所有缺失能力必须写入 `capture_capabilities_json`，不能伪装 Level4。

### 10.3 Cursor

P2 推荐路径：

```text
Cursor rules + MCP observe
  -> Agent 主动上报关键事件
  -> file edit 通过 git diff / filesystem watcher 摘要
```

目标能力：

| 能力 | P2 目标 |
|---|---|
| session lifecycle | 通过规则或手动 observe 支持 |
| tool call | 不强制 |
| tool result summary | 通过 Agent 主动上报支持 |
| file edit summary | 通过 git diff summary 支持 |
| conversation summary | 不强制 |
| 目标等级 | Level2+ |

Cursor P2 不依赖内部私有事件接口。若内部工具调用不可见，不影响 P2 验收；必须通过 capture quality 明确体现能力缺口。

## 11. Repository 和 Service 接口

### 11.1 capture.Repository

建议接口：

```go
type Repository interface {
    UpsertSession(ctx context.Context, session AgentSession) (AgentSession, error)
    EndSession(ctx context.Context, sessionID string, status string, endedAt time.Time, quality CaptureQuality) (AgentSession, error)

    UpsertTask(ctx context.Context, task AgentTask) (AgentTask, error)
    EndTask(ctx context.Context, taskID string, status string, outcome string, endedAt time.Time) (AgentTask, error)
    GetDefaultTask(ctx context.Context, sessionID string) (AgentTask, bool, error)

    FindDuplicateEvent(ctx context.Context, dedup DedupKey) (RawEvent, bool, error)
    InsertRawEvent(ctx context.Context, event RawEvent) error

    ListSessions(ctx context.Context, req ListSessionsRequest) ([]AgentSession, error)
    ListTasks(ctx context.Context, req ListTasksRequest) ([]AgentTask, error)
    ListEvents(ctx context.Context, req ListEventsRequest) ([]RawEvent, error)
    GetCaptureQuality(ctx context.Context, sessionID string) (CaptureQualityReport, error)
}
```

### 11.2 capture.Service

建议接口：

```go
type Service struct {
    cfg  config.Config
    repo Repository
}

func (s *Service) Observe(ctx context.Context, req ObserveRequest) (ObserveResponse, error)
func (s *Service) ListSessions(ctx context.Context, req ListSessionsRequest) (ListSessionsResponse, error)
func (s *Service) ListTasks(ctx context.Context, req ListTasksRequest) (ListTasksResponse, error)
func (s *Service) ListEvents(ctx context.Context, req ListEventsRequest) (ListEventsResponse, error)
func (s *Service) Quality(ctx context.Context, req QualityRequest) (QualityResponse, error)
```

Service 负责：

1. 入参校验。
2. capability 到 capture_level 的计算。
3. 内容边界检查。
4. content hash 归一化。
5. session/task 解析。
6. raw_event 去重。
7. lifecycle 状态更新。

Repository 负责：

1. 短事务写入。
2. SQL 查询和索引使用。
3. SQLite 错误码映射。

## 12. 事务边界

| 操作 | 事务内容 |
|---|---|
| `session.start` | upsert session + upsert default_task + insert raw_event |
| 普通 observe | ensure session/task + dedup check + insert raw_event + update quality |
| `task.result` | update task + insert raw_event + update quality |
| `session.end` | update session + maybe end active default_task + insert raw_event + update quality |
| content boundary reject | 不写 raw_event；可只写日志，不入库 |

所有写事务必须保持短事务，不能在事务内执行外部命令、LLM 调用或大文本摘要。

P2 的 `memory.observe` 事务不写 `async_job`。这是对总体架构异步链路的阶段性裁剪：P2 只保证 `raw_event` append-only 和 capture diagnostics；P3 引入 evidence extraction 时，再把 `raw_event` 与 `async_job` 放入同一短事务。

## 13. 错误码

| error_code | 场景 | retryable |
|---|---|---|
| `VALIDATION_FAILED` | 入参缺失、类型错误、不支持 event_type | false |
| `SESSION_REQUIRED` | Agent 自动捕获事件缺少 session_id | false |
| `TASK_INVALID` | task_id 与 session_id 不匹配 | false |
| `CONTENT_TOO_LARGE` | 摘要、片段、source refs 超出边界 | false |
| `CAPTURE_UNSUPPORTED` | Adapter 声明能力与事件类型冲突 | false |
| `STORAGE_BUSY` | SQLite busy timeout | true |
| `INTERNAL_ERROR` | 未分类错误 | true |

## 14. 日志设计

关键日志：

| 场景 | 字段 |
|---|---|
| observe accepted | `request_id`、`raw_event_id`、`session_id`、`task_id`、`event_type`、`agent_type` |
| observe deduped | `request_id`、`existing_event_id`、`dedup_key_hash` |
| content rejected | `request_id`、`event_type`、`reason`、`agent_type` |
| session started | `session_id`、`agent_type`、`capture_level` |
| session ended | `session_id`、`status`、`captured_event_count` |
| diagnostics query | `tool`、`filter_hash`、`result_count`、`duration_ms` |

日志不记录：

1. 完整用户输入。
2. 完整 Agent 回复。
3. 完整工具输出。
4. 完整 diff。
5. API key 或敏感配置。

## 15. 配置项

P2 新增配置建议：

```yaml
capture:
  require_session_for_agent_events: true
  max_input_summary_chars: 1200
  max_output_summary_chars: 2000
  max_content_summary_chars: 2000
  max_source_refs_chars: 4000
  max_salient_span_chars: 500
  max_salient_span_count: 10
  max_keyword_count: 30
  default_agent_type: unknown
```

配置原则：

1. 默认严格要求 Agent 自动捕获事件绑定 session。
2. 摘要字段限制小于 P1 `memory.max_content_chars`，避免 raw_event 成为隐藏全文存储。
3. 配置可以通过 YAML 和环境变量覆盖。
4. P2 不新增 embedding、LLM 或外部服务配置。

## 16. 测试设计

### 16.1 单元测试

1. event_type validator。
2. source_channel normalize。
3. `source_refs.capture_method` normalize。
4. capability 到 capture_level 计算。
5. content hash 计算。
6. content minimization：
   - output 过长拒绝。
   - `full_output/full_diff/full_text` 拒绝。
   - salient span 数量和长度限制。
7. `session_id + normalized_task` 任务解析。
8. default_task 解析。
9. dedup key 生成。

### 16.2 Repository 测试

1. `session.start` 写入 session、default_task、raw_event。
2. 普通事件绑定已有 session/task。
3. 同一 `content_hash + session_id + event_type` 去重。
4. `task.result` 更新 task 状态。
5. `session.end` 更新 session 状态和 ended_at。
6. raw_event 按 session/task/project/event_type 过滤。
7. capture quality 汇总正确。
8. content boundary rejected 不写 raw_event。

### 16.3 MCP 工具测试

1. `memory.observe session.start` 成功。
2. `memory.observe tool.result.summary` 成功。
3. `memory.observe file.edit.summary` 成功。
4. 响应包含 `request_id`，错误响应包含 `error_code/message/retryable/fallback_hint`。
5. 缺少 session 的 agent event 被拒绝。
6. 重复事件返回 `deduped=true`。
7. `memory.capture.sessions` 可查询 session。
8. `memory.capture.tasks` 可查询明确 task 和 default_task。
9. `memory.capture.events` 可按 event_type 查询。
10. `memory.capture.quality` 返回 capture_level 和质量统计。

### 16.4 集成验收

1. 启动 `memoryd`。
2. 调用 `memory.observe` 创建 Claude Code session。
3. 上报一次 `tool.call` 和一次失败 `tool.result.summary`。
4. 上报一次 `file.edit.summary`。
5. 重复上报同一 tool result，确认去重。
6. 调用 `task.result` 标记任务成功或失败。
7. 调用 `session.end` 结束 session。
8. 查询 capture sessions/tasks/events/quality。
9. 验证 raw_event 中无完整 output 和完整 diff。
10. 验证 P1 `memory.search/context/review` 仍然通过原有测试。

## 17. 验收标准

| 验收项 | 标准 |
|---|---|
| `memory.observe` | 能接收 session/task/tool/file edit 事件 |
| session 捕获 | 每个 Agent 接入样例能创建 session start/end |
| task 捕获 | 能创建明确 task 或 default_task |
| tool 捕获 | 至少保存工具名、输入摘要、输出摘要、exit code/hash |
| 文件编辑捕获 | 至少保存文件路径、diff 摘要、content hash |
| 去重 | 相同 content_hash 事件不会重复写入 |
| 内容边界 | 不保存完整 output、完整 diff、完整会话 |
| capability | session 写入 capture_capabilities_json 和 capture_level |
| quality | 可查询 capture_quality_json 或汇总结果 |
| 诊断查询 | 可按 agent/session/task/project/event_type 查询 |
| P1 回归 | P1 手动记忆、检索、context、review 测试仍通过 |

P2 退出条件：

```text
系统具备自动捕获事件、诊断捕获质量、验证内容边界的基础能力，但仍不承诺自动生成高质量长期记忆。
```

## 18. 协作任务拆分

| 任务 ID | 任务 | 输入 | 输出 | 可并行 |
|---|---|---|---|---|
| P2-B1 | capture schema migration | P1 migration | `0004_init_capture.sql` | 否 |
| P2-D1 | capture DTO 和 validator | 事件规范 | `internal/capture` types | 是 |
| P2-D2 | content minimization 扩展 | P1 ingest 规则 | observe 边界检查 | 是 |
| P2-D3 | capability/quality 计算 | Level 定义 | capture_level/quality helper | 是 |
| P2-B2 | capture repository | P2-B1 | session/task/event CRUD | 依赖 B1 |
| P2-C1 | `memory.observe` service | D1/D2/D3/B2 | Observe 闭环 | 依赖 B2 |
| P2-C2 | capture diagnostics service | B2 | sessions/tasks/events/quality 查询 | 依赖 B2 |
| P2-C3 | MCP tools 注册 | C1/C2 | `memory.observe` + diagnostics tools | 依赖 C1/C2 |
| P2-A1 | Agent 接入样例 | C3 | examples/agents | 依赖 C3 |
| P2-E1 | P2 repository/MCP 测试 | B2/C3 | 自动化测试 | 依赖 C3 |
| P2-E2 | P2 验收脚本 | C3/A1 | 本地验收脚本 | 依赖 A1/C3 |

## 19. 合并顺序建议

```text
P2-B1
  -> P2-D1 + P2-D2 + P2-D3
  -> P2-B2
  -> P2-C1
  -> P2-C2
  -> P2-C3
  -> P2-E1
  -> P2-A1
  -> P2-E2
  -> P2 release
```

## 20. P2 Done 定义

1. `memory.observe` 支持 P2 事件类型。
2. `agent_session`、`agent_task`、`raw_event` migration 幂等。
3. `session.start/session.end/task.result` 能正确更新生命周期状态。
4. 普通事件能按 `session_id + normalized_task` 绑定明确 task，缺少 task 信息时绑定 default_task。
5. raw_event append-only，重复事件返回 `deduped=true`。
6. capture capability 和 quality 可查询。
7. 三个 Agent 都有接入样例或降级说明。
8. Claude Code 样例至少达到 Level3 目标。
9. Codex 和 Cursor 样例至少达到 Level2 目标。
10. 内容边界测试证明没有保存完整 output、完整 diff、完整会话。
11. `memory.observe` 成功、去重和错误响应均包含 `request_id` 或可关联 trace id。
12. `source_refs` 按工具、文件编辑、用户纠正和 Agent 决策结构化保存引用和摘要。
13. P1 全量测试仍通过。
14. P2 验收脚本通过。

## 21. P2 后移交给 P3 的接口

P2 必须为 P3 保留以下稳定接口：

1. `raw_event.id`，供 `evidence.raw_event_id` 引用。
2. `raw_event.event_type/source_channel/source_refs_json/content_hash` 规范。
3. `agent_session.capture_quality_json`，供后续分析事件可靠性。
4. `agent_task.status/outcome_summary`，供后续任务结果归因。
5. `memory.observe` 的 accepted/deduped 语义。
6. content minimization 错误码和拒绝策略。
7. raw_event 按 session/task/project/event_type 查询能力。
8. `source_refs.target_event_id`，供 P3 将用户纠正绑定到原始事件。
9. `source_refs.decision_summary/reason_summary`，供 P3 抽取候选 decision evidence。

P3 可以在此基础上新增 `async_job`、evidence extraction、candidate generation 和 Admission，但不应改变 P2 raw_event 的 append-only 事实层语义。

## 22. 主要风险和控制点

| 风险 | 影响 | 控制点 |
|---|---|---|
| Agent 能力不一致 | Level4 无法一次达成 | capability 探测、capture_level 如实记录 |
| observe 被滥用保存全文 | 破坏长期架构边界 | 严格摘要长度、拒绝 full_text/full_output/full_diff |
| 去重不稳 | raw_event 膨胀、后续自动记忆噪声变多 | content_hash 优先，服务端 hash 兜底 |
| session/task 边界混乱 | P3 归因困难 | default_task 兜底，诊断工具暴露未绑定事件 |
| 过早自动写 memory | 污染 P1 稳定记忆 | P2 禁止 observe 直接写 `memory_item` |
| Cursor/Codex 被动捕获不足 | 验收预期偏差 | 接入样例按 Level2+ 验收，缺失能力显式记录 |
