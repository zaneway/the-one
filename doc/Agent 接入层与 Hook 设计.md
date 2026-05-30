# Agent 接入层与 Hook 重设计

> **文档状态**：设计中（讨论定稿后进入实现）  
> **版本**：v2.0-draft（架构审查修订）  
> **更新日期**：2026-05-29  
>

> **关联代码**（当前实现）：
> - `internal/adapter/`（`TurnPayload`、`IngestEnvelope`、`TurnRuntime`、`FileStateStore`）
> - `cmd/theone`（`ingest` / `observe` / `observe-turn` / `observe-envelope`（= ingest）/ `context`）
> - `.cursor/hooks/`、`doc/cursor/`（Cursor Driver 模板）
> - `drivers/cursor/`、`drivers/claude_code/hooks/`、`drivers/shared/`（P4/P5 薄 Driver）
> - `doc/claude/`（Claude Code 安装模板，对称 `doc/cursor/`）
> - `doc/adapters/claude_code/`、`doc/cursor/hooks/`（映射与 Hook 转发，P4/P5）

---

## 1. 背景与动机

### 1.1 v1 已验证的价值

Cursor Hook 方案验证了以下方向正确：

- **系统 Hook（被动）+ MCP（主动）** 双层分工；
- **`failClosed: false` + Hook 恒成功退出**，不阻断 Agent 主流程；
- **摘要入库**，符合 Capture 内容边界；
- **`observe-turn` + `TurnRuntime`** 统一回合语义，并与 Codex `observe-envelope` 共用展开逻辑。

### 1.2 暴露的问题

| 问题 | 根因 | 影响 |
|------|------|------|
| `session_id` 漂移 | 各 Hook 独立解析 payload，无单一 Session Binding | `raw_event` 链断裂、检索 scope 错位 |
| `session.json` 语义混用 | Hook 元数据与 `TurnRuntime` 去重状态共写同一文件 | 去重字段被覆盖或丢失 |
| 事件噪声大 | 文件/工具 Hook 走「完整回合」，产生占位 `conversation.message` | 库膨胀、抽取信号稀释 |
| 多 Agent 扩展成本高 | 业务逻辑散落在 `.cursor/hooks/*.sh` 与 Python | Claude/Codex 需复制一套脚本 |
| 可观测性弱 | 失败多静默重定向到 `/dev/null` | 验收与排障困难 |

### 1.3 要解决的问题

将 **Hook 从「业务实现」降级为「薄驱动（Driver）」**，在 Go 接入层统一：

1. Session / Task 绑定；
2. 事件种类（回合 vs 原子）与展开；
3. 去重与 ingest 追踪；
4. 多 Agent 映射与能力声明。

---

## 2. 设计目标与非目标

### 2.1 目标

| # | 目标 | 说明 |
|---|------|------|
| G1 | 多 Agent 一套接入语义 | Cursor、Claude Code、Codex wrapper 等共用 wire format |
| G2 | 事件粒度正确 | 回合级（user+agent）与增量级（tool/file）分离；**默认 `ExpandMode=legacy` 时不达成**，见 §2.3 |
| G3 | Session 单一真相 | 全线程 canonical `session_id` / `task_id` 由 Go 维护 |
| G4 | 读写路径分离 | 捕获（write）与召回注入（read）抽象为可选 Surface Adapter |
| G5 | 失败不阻断 Agent | 保持 v1 的降级策略 |
| G6 | 可验收、可追踪 | 每次 ingest 有 `ingest_id`，可 dead-letter、可对账 |

### 2.3 目标达成阶段（避免误读默认配置）

| 目标 | 默认配置下 | 显式切换后 |
|------|------------|------------|
| G2 事件粒度正确 | legacy 下未达成 | **P2 已落地（the-one dogfood）**：`adapter.expand_mode=v2` + Hook 仅 atomic/turn 分流 |
| G3 Session 单一真相 | v1 由 `session.json` 各 Hook 各自解析，存在漂移 | **P1 已落地**：`SessionBinder` + `binding.{agent_type}.json` |
| G6 ingest 条级幂等/对账 | v1 仅有 DB `content_hash` 兜底 | **P0 已落地**：`ingest-ledger.json` + dead_letter 含 `event_index` |

### 2.2 非目标（本阶段不做）

- **Driver / Hook 内同步调用 LLM**（含摘要增强、记忆价值判断）；详见 §3.4。
- **在 Hook 或 ingest 同步路径实现 F-001**；F-001 归属 `internal/processor` + `async_job`，不阻塞 `observe`。
- 用 Hook 替代 MCP `memory_observe` 的 L2/L3 字段质量（仍由 Agent + Rule 负责）。
- 保证 100% 捕获各 Agent 的内置非 MCP 工具（通过 `capture_capabilities` 诚实声明 + 降级）。
- 一次性删除 v1 CLI（`observe-turn` 等保留为兼容别名）。

---

## 3. 总体架构

### 3.1 分层

```text
┌─────────────────────────────────────────────────────────────────────────┐
│ Agent 产品层（能力各异）                                                  │
│  Cursor hooks.json │ Claude Code hooks │ Codex wrapper │ 手动 CLI        │
└────────────┬────────────────┬──────────────────┬───────────────┬────────┘
             │ 原生 JSON       │ 原生 JSON         │ 日志/子进程      │
             v                 v                   v                v
┌─────────────────────────────────────────────────────────────────────────┐
│ Driver 层（薄：map + 调 CLI，建议单脚本入口 per agent）                      │
│  driver=cursor │ driver=claude_code │ driver=codex_wrapper               │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │ IngestEnvelope（单条或批量）
                                v
┌─────────────────────────────────────────────────────────────────────────┐
│ Ingest 平面（Go，稳定核心）                                               │
│  theone ingest │ SessionBinder │ EventExpander │ Dedup │ FailureQueue    │
│  theone prefetch-context（可选）                                          │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │ []capture.ObserveRequest
                                v
┌─────────────────────────────────────────────────────────────────────────┐
│ Capture Service → raw_event（事实层，快速落库）                            │
│       → async_job：extract_evidence / generate_memory_candidate / …       │
│       → processor（rule_based 默认；F-001：llm / hybrid，仅异步）          │
│       → Admission → memory_item                                           │
└─────────────────────────────────────────────────────────────────────────┘

旁路 — Surface Adapter（各 Agent 不同）：
  将 memory.context 结果注入用户可见上下文
  Cursor: beforeSubmit stdout + theone-injected-context.mdc
  Claude: 仓库内 markdown 片段 / hook 附加文本（按产品 API 选型）
  Codex:  wrapper 拼接下一轮 prompt
```

### 3.2 与总体架构的映射

| 总体架构概念 | v2 实现 |
|--------------|---------|
| Capture Adapter | Driver + Ingest 平面 |
| Level4 捕获 | 摘要级；能力由 `capture_capabilities` 声明 |
| MCP Tools | `theone serve`（不变） |
| 接入协议 | `IngestEnvelope` v1 演进（可选 `kind`、批量 `events`） |

### 3.3 职责分工（固化）

| 层级 | 机制 | 职责 | 字段质量 | 时延 |
|------|------|------|----------|------|
| **L0** Driver + Ingest | Hook / wrapper → `theone ingest` | 覆盖率、归属、原子事实、检索 trace 关联；确定性截断与启发式 keywords | 中 | 同步，毫秒～秒级 |
| **L1** Agent + MCP + Rule | `memory_observe` / `memory_remember`；`theone-memory-observe.mdc` | 高信号事件、用户纠正、长期结论；补全 Capture L2/L3 | 高 | 随 Agent 回合 |
| **L2** Go Processor（异步） | `async_job` + `processor.Provider` | 记忆价值判断、证据/候选生成；可选摘要增强（F-001）；**不**在 Hook 调 LLM | 中→高（可配置） | 异步，不阻塞 observe |

**原则**：Hook 负责「先记下发生了什么」；是否晋升为长期记忆由 L2（规则或 LLM）在事实入库之后判断；高意图表述优先由 L1 写入。

#### 3.3.1 L0 与 L1 写入分工（避免双份 `raw_event`）

L0（ingest）与 L1（`memory_observe`）均可落库，但**同一事实不应两条通道各写一遍**。约定如下：

| 事件类型 / 场景 | 主通道 | 辅通道 | 说明 |
|-----------------|--------|--------|------|
| `session.start` / `session.end` | L0 `session.lifecycle` | L1 可选补 `capture_capabilities` | 以 L0 建立 DB session；L1 不得使用与 L0 冲突的 `session_id` |
| `tool.result.summary` / `file.edit.summary` | L0 `capture.atomic`（v2） | L1 **不写**同工具/同文件增量 | 工具/文件由 Hook 负责；Agent 仅在 MCP 工具**未**被 Hook 覆盖时补打 |
| `conversation.message` / `agent.response.summary` | L0 `turn.completed`（摘要级） | L1 可补 L2 字段（keywords、salient_spans） | 若 L1 与 L0 摘要一致，依赖 `content_hash` 去重；**禁止** L0 占位 user + L1 再写完整对话 |
| `user.correction` / `agent.decision` / `memory_remember` | L1 MCP + Rule | L0 **不写** | 高意图、长期结论仅 L1 |
| Cursor 内置非 MCP 工具 | L0 atomic（若 Hook 能捕获） | L1 不重复 | 能力由 `capture_capabilities` 声明 |

**ingest 侧建议**：对 `producer` 含 `mcp:` 的包络**拒绝或仅审计**（MCP 应直连 `theone serve`，不经 ingest），避免与 L1 双路径混淆。

> **与 Cursor `afterMCPExecution` 的边界**：Hook 捕获的 MCP 工具结果走 `producer=cursor_hook:afterMCPExecution`（`*_hook:` 通道），**不受** `mcp:` 拒绝规则约束。`mcp:` 仅指 Agent 经 MCP 协议直连 `theone serve` 的包络。Driver 应过滤 `theone` 自身 MCP 工具（如 `memory_observe`），避免自捕获循环（与 `theone-memory-observe.mdc` 一致）。

### 3.4 智能增强放置原则（LLM 与摘要）

#### 3.4.1 两类 LLM 需求（不可混为一谈）

| 类型 | 目的 | 典型输入 | 合适位置 |
|------|------|----------|----------|
| **捕获期摘要增强** | 将过长 tool/diff 等压成可检索的 `content_summary` / keywords | 截断后的工具输出、文件元数据 | **Go 异步**（可选 job）；Hook 仅做有界截断 |
| **记忆价值分析** | 判断是否生成 evidence / `memory_item` | 已最小化的 `raw_event` + 近邻上下文 | **Go `processor`（F-001）**，在 `extract_evidence` 等 `async_job` 内 |

#### 3.4.2 为何不在 Hook 内调 LLM

| 因素 | 说明 |
|------|------|
| 超时与阻断 | Hook 预算约 8s、`failClosed: false`；LLM 延迟波动大，易被杀进程或拖慢 `beforeSubmitPrompt` |
| 重复推理 | `afterAgentResponse` 等路径已有 Agent 生成的 `agent_summary`；Hook 再摘要属同回合二次推理 |
| 多 Agent 成本 | Cursor / Claude / Codex 各维护一套密钥、重试、审计，违背「ingest 单点」 |
| 事实源混乱 | 若在 Hook 用 LLM「包装」摘要以通过词表，等于把「是否值得记」前移到捕获层，破坏事实层与记忆层分离 |
| 已有管道 | `observe` 已触发 `extract_evidence`；F-001 扩展 `processor` 即可，无需第二条 LLM 链 |

#### 3.4.3 推荐路径（与 F-001 / F-002 对齐）

```text
捕获（同步）
  Hook：截断 + hash + 元数据（file_path、exit_code、tool_name）→ ingest → raw_event
  turn.completed：优先使用 Agent 已有 user/agent 摘要，不另调 LLM

增强（异步，可关）
  短期：TurnRuntime / ingest 启发式 keywords；F-002 可配置信号词（rule_based）
  中期：F-001 hybrid（词表命中跳过模型，否则 LLM 判断 worth_remembering）
  可选：独立 async_job enrich_summary（仅当 raw_event 标记 summary_pending）

准入
  AdmissionController 保持本地规则，不让 LLM 绕过 review（与 F-001 一致）
```

#### 3.4.4 与读路径（prefetch）的边界

- `prefetch-context` / `memory.context` 属于**召回注入**，不是捕获层摘要；**禁止**为写 `raw_event` 在 Hook 内调 LLM。
- 若需在下一轮 prompt 前压缩工具结果，应走检索与 `context_pack`，而非 Hook 同步 LLM。

#### 3.4.5 字段质量演进（接入层不写 LLM）

| 阶段 | 接入层（L0） | 异步层（L2） |
|------|--------------|--------------|
| v2 默认 | 确定性截断 + 启发式 keywords（路径、工具名、错误码） | `rule_based`（现状） |
| 配置 F-002 | 同上；`is_substantive` 与词表联动 | 对话类命中可调词表 |
| 配置 F-001 | 仍不调用 LLM | `llm` / `hybrid` 记忆价值分析；输入输出受最小化边界约束 |

---

## 4. 统一协议：IngestEnvelope v1 演进

### 4.1 设计原则

- **向后兼容**：现有 `observe-envelope`（Codex）与 `observe-turn`（Cursor）输入在缺省 `kind` 时行为与 v1 一致。
- **增量扩展**：新字段均为可选；旧客户端无需修改即可继续工作。
- **单一入口**：对外推荐 `theone ingest`；内部分发到现有实现直至 Phase 2 完成。
- **显式优先于推断**：`capture.atomic` 必须带 `kind`（或满足 §4.4 的 atomic 推断条件）；禁止仅靠 `file_edits` / `tool_results` 数组触发 `turn.completed` 推断（见 §4.4）。
- **实现态说明（P0 已落地）**：`theone ingest` 与 `observe-envelope`（别名）经 `IngestProcessor` + `InferKind` 路由；`ExpandMode` 默认 `legacy`（`THEONE_EXPAND_MODE`）。验收：`scripts/acceptance/p0_ingest.sh`。

### 4.2 单条包络（与现结构兼容）

现有 Go 类型（`internal/adapter/model.go`）：

```go
type IngestEnvelope struct {
    IngestID        string         `json:"ingest_id"`
    ProtocolVersion string         `json:"protocol_version"`
    Producer        string         `json:"producer"`
    SessionID       string         `json:"session_id"`
    TurnID          string         `json:"turn_id,omitempty"`
    EventType       string         `json:"event_type"`
    OccurredAt      string         `json:"occurred_at,omitempty"`
    Payload         map[string]any `json:"payload"`
}
```

**v2 新增可选字段**（JSON，非破坏性）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `kind` | string | `session.lifecycle` \| `turn.completed` \| `capture.atomic`；缺省见 §4.4 |
| `agent_type` | string | 顶层冗余，缺省从 `payload.agent_type` 读取 |

**`producer` 命名规范**（仅 ingest / Hook / wrapper 使用）：

| 模式 | 示例 | 走 ingest？ |
|------|------|-------------|
| `{agent_type}_hook:{hook_name}` | `cursor_hook:afterFileEdit` | 是 |
| `{agent_type}_wrapper` | `codex_wrapper` | 是 |
| `mcp:memory_observe` | 审计/排障用标签 | **否**（MCP 直连 Capture，见 §3.3.1） |

示例 — 会话开始（可直接映射为现有 `theone observe` 参数，或经 ingest 转发）：

```json
{
  "ingest_id": "ing_sess_start_01",
  "protocol_version": "v1",
  "producer": "cursor_hook:sessionStart",
  "agent_type": "cursor",
  "session_id": "02c3c964-5c75-45c5-85d9-8f01e6ececc3",
  "kind": "session.lifecycle",
  "event_type": "session.start",
  "occurred_at": "2026-05-29T10:00:00+08:00",
  "payload": {
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "task_id": "task_cursor_auto",
    "content_summary": "Cursor session start",
    "capture_capabilities": {
      "conversation_capture": true,
      "tool_call_capture": true,
      "tool_output_capture": true,
      "file_edit_capture": true,
      "session_lifecycle": true,
      "mcp_observe": true,
      "requires_rules_injection": true
    },
    "session": { "goal_summary": "…", "status": "active" },
    "task": { "task_summary": "…", "status": "active", "outcome_summary": "" }
  }
}
```

### 4.3 批量包络（v2 推荐 Hook 使用）

减少 CLI 冷启动次数；一次 Hook 触发可提交 0~N 条事件。

**与单条包络区分**：存在顶层 `events[]` 时按 `BatchEnvelope` 解析，**不要求**顶层 `event_type`（与 `ValidateIngestEnvelope` 单条校验分离）。

建议 Go 类型（实现期）：

```go
// BatchEnvelope 批量入站；与 IngestEnvelope 二选一，由 ingest 根对象判别。
type BatchEnvelope struct {
    IngestID        string           `json:"ingest_id"`
    ProtocolVersion string           `json:"protocol_version"`
    Producer        string           `json:"producer"`
    AgentType       string           `json:"agent_type,omitempty"`
    SessionID       string           `json:"session_id"` // 批默认值，可被 events[i] 覆盖
    Events          []IngestEventItem `json:"events"`
}

// IngestEventItem 批内单条；每条独立 kind / event_type。
type IngestEventItem struct {
    Kind       string         `json:"kind,omitempty"`
    EventType  string         `json:"event_type"`
    TurnID     string         `json:"turn_id,omitempty"`
    OccurredAt string         `json:"occurred_at,omitempty"`
    Payload    map[string]any `json:"payload"`
}
```

**校验**：

- 批级：`ingest_id`、`protocol_version`、`producer`、`session_id` 必填；`events` 可为空数组（no-op）。
- 条级：每条 `event_type` + 非空 `payload`；`kind` 缺省时按 §4.4 **条级**推断（不用批级 `event_type`）。

**处理语义**（§9.2）：**best-effort 逐条**提交；不因单条失败回滚已成功条目；批级 `ingest_id` 用于审计，条级失败写入 `dead_letter.jsonl` 并列入 `failures[]`。

```json
{
  "ingest_id": "ing_batch_01",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterMCPExecution",
  "agent_type": "cursor",
  "session_id": "02c3c964-5c75-45c5-85d9-8f01e6ececc3",
  "events": [
    {
      "kind": "capture.atomic",
      "event_type": "tool.result.summary",
      "turn_id": "turn_abc",
      "occurred_at": "2026-05-29T10:01:00+08:00",
      "payload": {
        "workspace_id": "local_default_workspace",
        "project_id": "the-one",
        "repo_id": "the-one",
        "task_id": "task_xxx",
        "tool_name": "Shell",
        "input_summary": "go test ./...",
        "output_summary": "ok",
        "exit_code": 0
      }
    }
  ]
}
```

### 4.4 `kind` 与展开规则

| kind | 含义 | 展开为 `ObserveRequest` |
|------|------|-------------------------|
| `session.lifecycle` | `session.start` / `session.end` | 各 1 条，不经 `TurnRuntime` 回合逻辑 |
| `turn.completed` | 一轮对话结束 | `conversation.message` + `agent.response.summary`（+ 可选 task/decision/task.result） |
| `capture.atomic` | 增量事实 | **仅**对应 `event_type`（如 `tool.result.summary`、`file.edit.summary`），**禁止**附带占位 user 消息 |

**载荷形状约束**：

| kind | 允许的形状 | 禁止 |
|------|------------|------|
| `capture.atomic` | `payload` 为**扁平** observe 字段（`tool_name`、`file_path` 等） | `user_summary` / `agent_summary`；`file_edits[]` / `tool_results[]` 数组（TurnPayload 形状） |
| `turn.completed` | `adapter.TurnPayload`（§4.5） | 在无 user/agent 摘要时仅含 `file_edits` 却期望 atomic 行为 |
| `session.lifecycle` | observe 级 session/task 字段 | 走 `BuildObserveRequests` 展开 |

**缺省 `kind` 推断（兼容 v1，按优先级）**：

> **前置**：根对象是否含 `events[]` 已在 §9.2 判别为 `BatchEnvelope`；进入本节的单条 `IngestEnvelope` **不会**再带批级 `events[]`。

1. 若显式 `kind` 非空 → 使用该值（校验与形状表一致）。
2. 若 `event_type` 为 `session.start` / `session.end` → `session.lifecycle`。
3. 若 `event_type` 为 `tool.result.summary` / `file.edit.summary` 等**原子事件枚举** → `capture.atomic`。
4. 若 `payload` 含非空 `user_summary` **或** 非空 `agent_summary` → `turn.completed`（与现 `TurnPayloadFromEnvelope` 一致）。
5. 否则 → `capture.atomic`（必须已有合法 `event_type`；否则校验失败）。

**步骤 3 形状冲突（默认行为）**：`event_type` 已属原子枚举，但 payload 误带 `file_edits[]` / `tool_results[]` / `user_summary` 等 Turn 字段时，**默认拒绝**该条并写入 `dead_letter.jsonl`（`error_code=INVALID_ATOMIC_SHAPE`），**不**静默剥离后入库。开发模式可配置 `adapter.atomic_strip_turn_fields=true` 改为剥离数组字段后继续（仅 dogfood，不计入验收默认路径）。

> **v1 兼容说明**：旧客户端常把 `file_edits` 塞进 Turn 且无 `kind`；在 `ExpandMode=legacy` 下步骤 5 仍会判为 `turn.completed`（保留占位 user）。`ExpandMode=v2` 时 Driver **必须**对文件/工具 Hook 显式 `kind=capture.atomic`，不得依赖步骤 5。

**路由（ingest 内，禁止混用）**：

```text
kind=session.lifecycle  → 直接 ObserveRequest（不经 TurnPayloadFromEnvelope）
kind=turn.completed     → TurnPayloadFromEnvelope → EventExpander(ExpandMode)
kind=capture.atomic     → AtomicPayload → ObserveRequest（单条，不经 BuildObserveRequests 全量展开）
```

### 4.5 `turn.completed` 载荷

与现有 `adapter.TurnPayload` 对齐（`internal/adapter/turn_runtime.go`），作为 `kind=turn.completed` 时 `payload` 的形状。Codex wrapper 与 Cursor `afterAgentResponse` 继续使用该结构。

### 4.6 `capture.atomic` 载荷

直接承载 `memory.observe` 所需字段子集（经 Capture 校验与最小化），**一条 envelope 对应一条 `event_type`**，例如：

- `tool.result.summary`：`tool_name`、`input_summary`、`output_summary`、`exit_code`
- `file.edit.summary`：`file_path`、`change_type`、`content_summary`（+ `source_refs`）

`source_refs` 统一建议包含：

```json
{
  "source_type": "agent_session",
  "capture_method": "adapter_hook",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterFileEdit"
}
```

---

## 5. 事件模型与去重

### 5.1 展开模式（`ExpandMode`）

在 `TurnRuntime`（或重命名为 `EventExpander`）上增加模式，**默认 `legacy` 直至显式切换**：

| 模式 | 行为 |
|------|------|
| `legacy` | 与 v1 相同（文件/工具回合仍可能产生占位 `conversation.message`） |
| `v2` | `capture.atomic` 仅展开目标事件；`turn.completed` 才发 user/agent 对 |

**与 `SkipBaseTurn` 组合**（`TurnPayload`，现已在 `turn_runtime.go`）：

| ExpandMode | kind / 场景 | `SkipBaseTurn` | 结果 |
|------------|-------------|----------------|------|
| `legacy` | 文件/工具经 `observe-turn` 且未改 Driver | `false`（默认） | 可能占位 user + file/tool 事件 |
| `v2` | `capture.atomic` | N/A（不走 Turn 展开） | 仅 atomic 一条 |
| `v2` | `turn.completed` | `true` 可选 | 仅 agent/user/decision，**不**在回合末重复展开已 atomic 的 tool/file |
| `legacy` | 迁移期文件 Hook | `true` + 单独 atomic CLI | 过渡：先 atomic 再 turn 不带 file_edits |

P2 验收目标：`ExpandMode=v2` 且文件/工具 Hook 仅 atomic；`afterAgentResponse` 的 `turn.completed` **默认不带** `tool_results` / `file_edits`（见 §5.4）。

**`ExpandMode=v2` 切换决议（Q3，已决）**：

| 项 | 决议 |
|----|------|
| **时机** | **P2**（**P1** 验收通过后）；P0/P1 保持 `legacy` |
| **方式** | 配置单开关：`THEONE_EXPAND_MODE=v2` 或 `theone.yaml` → `adapter.expand_mode: v2` |
| **顺序** | **先**改 Cursor Driver（`mapping.yaml`：文件/工具仅 `capture.atomic`，`turn.completed` 不带 file/tool）→ **再**开 v2 开关 |
| **过渡** | **不双写**（禁止 atomic + turn 内 `file_edits`/`tool_results` 同记一遍） |
| **范围** | 先 **the-one 仓库** dogfood；Codex/别名 CLI 在改 mapping 前保持 `legacy` |
| **默认配置** | P2 验收通过前：默认 `legacy`；通过后 **the-one 默认改为 `v2`** |
| **回滚** | 设回 `legacy`；可临时恢复文件/工具 `observe-turn` 路径；DB schema 不变 |

### 5.2 去重策略（双层 + 跨 kind）

| 层级 | 机制 | 键 | 适用范围 |
|------|------|-----|----------|
| 接入层（文件） | `turn-dedup.json` | `(session_id, turn_id, turn_signature)` | **仅** `turn.completed` 的 user/agent 对 |
| 接入层（文件，v2 可选） | `atomic-dedup.json` 或内存 LRU | `(session_id, event_type, atomic_fingerprint)` | `capture.atomic`；fingerprint 见下 |
| 入库层（DB） | Capture `FindDuplicateEvent` | `content_hash + session_id + event_type + source_channel + workspace/project/repo` | 全部事件 |

**`atomic_fingerprint`（建议）**：

- `tool.result.summary`：`sha256(tool_name + "|" + input_summary + "|" + output_summary + "|" + exit_code)`（截断后字段）
- `file.edit.summary`：`sha256(file_path + "|" + change_type + "|" + after_hash)`；无 hash 时用 `content_summary` 前 200 字

接入层去重失败时，入库层仍可兜底；**不应**仅依赖 Hook 侧去重。

### 5.3 `is_substantive` 与 `hasHighSignal`（legacy）

- **语义范围**：`is_substantive` 仅用于 `turn.completed` 的**对话摘要**是否值得写 user/agent。
- **现实现注意**：`BuildObserveRequests` 在 `legacy` 下 `emitBaseTurn := IsSubstantive || hasHighSignal`，其中 `hasHighSignal` 含 `tool_results` / `file_edits`，会导致「有工具/文件即发 base turn」。v2 通过 **atomic 分流 + `ExpandMode=v2`** 取消该耦合；`turn.completed` 不应再依赖 `hasHighSignal` 发占位 user。
- **v2 默认规则**（Go 内配置）：
  - user+agent 合计字数低于阈值 → 跳过或仅写 agent 侧；
  - 纯寒暄（可配置词表，与 F-002 联动）→ 跳过；
  - 工具失败 / 大段编辑 → **仅** `capture.atomic`，不参与 `is_substantive`。

### 5.4 跨 kind 去重（atomic vs turn.completed）

同一物理操作可能被 `afterFileEdit`（atomic）与 `afterAgentResponse`（turn 内 `file_edits`）各记一次。

**v2 默认策略（已定）**：

1. **写入分工**：文件/工具增量**只**走 `capture.atomic`；`turn.completed` 的 TurnPayload **不包含** `tool_results` / `file_edits`（Driver 与 `mapping.yaml` 强制）。
2. **回合内关联**：atomic payload 可带 `turn_id`（与 Cursor 回合对齐），仅用于检索关联，不触发 turn 展开。
3. **兜底（有限承诺）**：若 legacy 双写，且 `turn.completed` 内 `file_edits[]` / `tool_results[]` 与 atomic 侧 **fingerprint 输入字段完全一致**（见下表），ingest 可跳过重复展开；否则**不保证**接入层去重，仅依赖 DB `content_hash`。

| `event_type` | atomic fingerprint 输入 | turn 内对应字段 | 对齐要求 |
|--------------|-------------------------|-----------------|----------|
| `tool.result.summary` | `tool_name` + `input_summary` + `output_summary` + `exit_code` | `tool_results[]` 同名字段 | 两侧截断规则一致（§5.2）时可兜底 |
| `file.edit.summary` | `file_path` + `change_type` + `after_hash`（无则 `content_summary` 前 200 字） | `file_edits[]` 常缺 `after_hash` | **默认不对齐**；file.edit 跨 kind 兜底**不承诺**，仅靠 DB |

Codex wrapper 若单包同时含 `user_summary` 与 `file_edits`：应拆为 **1× turn.completed + N× atomic** 批量提交，勿走单包 Turn 全量展开。

---

## 6. Session Binding（单一真相）

### 6.0 Cursor Hook 会话/回合 ID 字段对照（事实核查，G3 前置）

以下对照 **Cursor 官方 Hooks 文档**（[cursor.com/docs/hooks](https://cursor.com/docs/hooks)）与 v1 Driver 实测行为；实现前应在目标 Cursor 版本上复验一次。

**约定**：

- 官方 **Common schema** 向多数 Agent Hook 附加 `conversation_id`、`generation_id` 等；`workspaceOpen` 等 App 生命周期 Hook **不含**会话字段。
- `sessionStart` / `sessionEnd` 使用字段名 **`session_id`**，文档注明其与 **`conversation_id` 同值**（canonical 应取其一，勿重复生成两套 id）。

| Hook 事件 | 官方 payload 中的会话/回合 id | 说明 | v1 Driver（`.cursor/hooks/`） |
|-----------|------------------------------|------|-------------------------------|
| `sessionStart` | `session_id`（= `conversation_id`） | 新建 Composer 时触发；**无** `conversation_id` 字段名 | `pick(session_id, sessionId, conversation_id, conversationId)`；缺失时 **错误回退** `sess_cursor_auto_{YYYYMMDD}`（与本文 §6.2 禁止项冲突，P1 需改） |
| `beforeSubmitPrompt` | Common：`conversation_id`、`generation_id`；特有：`prompt` | `generation_id` = 每条用户消息一代 | 写 `prompt-cache.json`；`generation_id` **未**持久化 |
| `afterAgentResponse` | Common：`conversation_id`、`generation_id`；特有：`text` | 无原始 `prompt` 文本 | `turn_id` 由 `build_turn_id(session, user_summary, response, 时间戳)` 生成，**含时间戳**，与 beforeSubmit 不稳定对齐 |
| `afterFileEdit` / `afterMCPExecution` | Common：`conversation_id`、`generation_id`；工具有 `tool_use_id` 等 | 可能早于首条 `beforeSubmitPrompt` | 经 `resolve_session_task` 读 `session.json` / `prompt-cache` |
| `sessionEnd` | `session_id`（= `conversation_id`） | 关闭 Composer 时 | pick；**仅此**触发 `session.end` + 清 binding |
| `stop` | Common：`conversation_id`、`generation_id`；`status` | **每轮** Agent 回复结束 | **禁止**绑 `session.end`（与 `afterAgentResponse` 分工）；可选 no-op 或 lint/`followup_message` |

**对 §6.2 绑定的结论**：

1. **canonical 可在 `sessionStart` 建立**：绑定应接受 `session_id`（来自 `sessionStart`）与 `conversation_id`（来自 Common schema）为**同一 external key**，写入 `binding.external_session_key`，禁止在官方已提供 `session_id` 时仍生成 `sess_*_{YYYYMMDD}`。
2. **禁止仅认字段名 `conversation_id`**：否则 `sessionStart` 会被误判为「无 canonical」→ 合成 id → 后续真实 `conversation_id` 触发 `binding_mismatch.log` 且不可改绑（审查项 S1）。
3. **`task_id` 与 `generation_id`**：`task_id` 表线程级任务（多轮共享）；`generation_id` 表单条用户消息（回合级）。`turn_id` / inject-cache 对齐应优先用 **`generation_id`**（见 §7.2），而非在 `afterAgentResponse` 重算 prompt 指纹。

### 6.1 运行时状态文件

目录：`.theone-data/runtime-state/`（与现路径一致）

| 文件 | 写入方 | 内容 |
|------|--------|------|
| `binding.{agent_type}.json` | **仅** Ingest / SessionBinder | 见 §6.3；**不**含 `last_turn_*` |
| `turn-dedup.json` | EventExpander | **仅** `last_turn_id`、`last_turn_sig`、`last_task_summary`（**不**再存 `session_id` / `task_id`） |
| `atomic-dedup.json`（可选） | EventExpander | 近期 `atomic_fingerprint` 集合或 LRU（§5.2） |
| `prefetch.json` | prefetch-context | 最近一次 context 摘要、`retrieval_trace_id`（可选） |
| `prompt-cache.json` | Cursor Driver（beforeSubmit） | 本轮 `user_summary`、`generation_id`、`conversation_id`（过渡；`turn_id` 可由 `generation_id` 派生） |
| `inject-cache.json` | Surface Adapter | **按回合**：`generation_id`（权威）、`turn_id`（可选）、`used_memory_ids`、`injected_to_prompt`、`retrieval_trace_id` |
| `ingest-ledger.json` | Ingest | 已处理的 `(ingest_id, event_index)` 集合（或带 TTL 的 LRU）；用于 §9.2 重试幂等 |
| `binding_mismatch.log` | SessionBinder | append-only；payload 与已绑定 `external_session_key` 不一致时记录，不改绑 |

**禁止** Hook 脚本直接覆盖 `turn-dedup.json` / `binding.*.json` / `ingest-ledger.json`。  
**禁止** `binding` 与 `turn-dedup` 混写（解决 v1 `session.json` 问题）。

**读取顺序（ingest 强制）**：`SessionBinder` 从 `binding.{agent_type}.json` 解析 `session_id` / `task_id` → 注入每条 `ObserveRequest` → `EventExpander` 只读 `turn-dedup.json` 做回合去重。不得从 `turn-dedup` 回填 session（P1 迁移时从 legacy `session.json` 一次性导入 binding）。

### 6.2 绑定规则

**`session_id` 优先级（Cursor 默认）**：

1. `conversation_id` / `conversationId`（Common schema，canonical）
2. `session_id` / `sessionId`（`sessionStart` / `sessionEnd` 等；与 1 **同值**，见 §6.0）
3. **禁止**静默长期使用 `sess_*_{YYYYMMDD}` 作为 canonical id（同日多会话会碰撞；v1 回退行为，P1 移除）。仅当 1、2 均缺失时：
   - 生成 `sess_{agent_type}_{workspace_hash}_{unix_nano}` 或
   - 拒绝 ingest 并 `degraded` + stderr 提示（**推荐默认：拒绝**；`THEONE_ALLOW_SYNTHETIC_SESSION=1` 开发模式可生成）

**绑定时机**（同一 `agent_type` 同时只跟踪**一个活跃 Composer 会话**，见 §6.4）：

| 时机 | 行为 |
|------|------|
| `session.lifecycle` + `session.start`，且 `external_session_key` 与当前 binding **不同** | **新会话**：重置 binding（§6.4），用 1/2 写入 `session_id` / `external_session_key`，`task_id` 见下表 |
| 同一会话的后续 ingest | **只读** binding；payload 的 1/2 与 binding 不一致 → `binding_mismatch.log`，不改 `session_id`（合成 id 例外见 §6.2.2） |
| 乱序：尚无 binding，先来了 atomic/turn（§6.2.1 bootstrap） | 用 payload 的 1/2 建 binding + bootstrap DB；`task_id=task_{agent}_auto`，`task_from_prompt_pending=true`（§6.2.2） |

**`session_id` 稳定性**：在同一 `external_session_key` 下不变；仅在新 `session.start`（新 Chat）或 §6.2.2 合成→官方 id **一次性升级**时改变。

**`task_id` 语义（满足 G3 / 检索 scope）**：

| 概念 | 含义 |
|------|------|
| **Composer 会话** | `conversation_id` / `session_id`，对应 binding.`external_session_key` |
| **`task_id`** | 该会话内**用户当前工作目标**的稳定 id；默认取**本会话第一条非空用户 prompt** 的指纹，全会话内不变 |
| **`generation_id`** | 单轮用户消息 id（回合级），**不是** `task_id`（见 §7.2） |

**`task_id` 取值优先级**：

1. payload / binding 中已有**非** `task_{agent}_auto` 的显式 `task_id` → 沿用，且 `task_from_prompt_pending=false`
2. binding.`task_from_prompt_pending=true` 且收到**首条非空用户 prompt** → `task_{sha1(normalized_prompt)[:16]}`，并置 `task_from_prompt_pending=false`（§6.2.2、§7.1）
3. 否则 → `task_{agent_type}_auto`（占位，允许后续走规则 2 升级）

`prompt-cache.json` 只缓存 prompt 文本；**指纹在 `prefetch-context` / `SessionBinder` 内计算**，不以 cache 为权威来源。

#### 6.2.1 Session 就绪与乱序事件（`require_session`）

Capture 默认 `require_session_for_agent_events=true`：非 `session.start` 要求 DB 已有 session。

**ingest 策略（已定，策略 A + 队列）**：

| 步骤 | 行为 |
|------|------|
| 1 | 每条 ingest 先 `SessionBinder.Resolve`（内存 + `binding.*.json`） |
| 2 | 若 DB 无 session 且事件非 `session.start` → **自动合成**一条 `session.start`（`producer` 追加 `:auto_bootstrap`），`session_id` 与 Resolve 一致；binding 设 `task_{agent}_auto` + **`task_from_prompt_pending=true`**（§6.2.2） |
| 3 | 若自动 bootstrap 失败 → 该条入 `dead_letter.jsonl`，`failures[]` 记录 `SESSION_NOT_READY`；Hook 仍 exit 0 |
| 4 | Driver 推荐顺序仍为 `sessionStart` → 其他；验收用例覆盖「仅 afterFileEdit、无 sessionStart」仍能落库 |

自动 bootstrap **不**覆盖已有 binding 的 `session_id`；仅补齐 DB session 行（`task_id` 见 §6.2.2）。

#### 6.2.2 乱序 bootstrap 与 `task_id` 升级（无歧义规则）

**场景**：用户尚未发 prompt，先触发了 `afterFileEdit` 等 → ingest 必须先 bootstrap DB，此时还没有 prompt 指纹。

**做法**：用 binding 上的布尔标志 **`task_from_prompt_pending`** 区分「占位 task」与「已用 prompt 定 task」，**不要**用「库里有没有 `raw_event`」判断能否升级（bootstrap 也会产生 `raw_event`，用事件条数会误判）。

| 步骤 | 谁触发 | binding / DB |
|------|--------|----------------|
| 1 | bootstrap 合成 `session.start`（`producer` 含 `:auto_bootstrap`） | `task_id=task_{agent}_auto`，**`task_from_prompt_pending=true`**；DB 有 session 行 |
| 2 | 其间可有任意条 `capture.atomic`（文件/工具） | **不改变** `task_from_prompt_pending`（仍为 true） |
| 3 | 用户**第一次**非空 prompt（`beforeSubmitPrompt`） | `prefetch-context` 内调 `SessionBinder.BindTaskFromPrompt` → `task_id=task_{sha1...}`，**`task_from_prompt_pending=false`**；同步更新 DB task |
| 4 | 同会话后续 prompt / ingest | `task_id` **不再改变** |

**`BindTaskFromPrompt` 前置条件（全部满足才执行）**：

1. `binding.task_from_prompt_pending == true`
2. `normalized_prompt` 非空
3. 可选：`binding.external_session_key` 与本次 hook 的 `conversation_id` 一致（防止误用上一轮 cache）

**明确禁止的实现方式**：

- 用 `captured_event_count == 0` 作为升级条件（bootstrap 后恒为 ≥1，会导致 **永远停在 `task_*_auto`**）
- 在 `beforeSubmit` 里改 binding 却不走 `SessionBinder`（读写分裂）

**与 bootstrap 的关系（一句话）**：bootstrap 只负责「让 DB 有 session + 占位 task」；**不占用**「用 prompt 定 task」的机会；定 task 只发生在规则 2 的第一次 `beforeSubmit`。

**`session_id` 一次性升级（合成 → 官方 id）**：

- 条件：`binding.session_id` 为合成 `sess_{agent}_*`，且随后 ingest 收到带官方 1/2 的 `session.start`。
- 默认：允许升级 binding + DB，并 `UPDATE raw_event.session_id`（或 alias 表）；审计 `binding_mismatch.log`（`action=session_upgraded`）。
- `adapter.binding.strict_no_upgrade=true`：仅记 mismatch，不升级（非 Cursor 默认）。

#### 6.2.3 `SessionBinder.Resolve`（ingest 与 prefetch 共用）

```text
输入：agent_type, envelope_session_id, payload 中的 conversation_id/session_id
输出：session_id, task_id（供注入 ObserveRequest / memory.context）

1. 读 binding.{agent_type}.json；若无文件 → 仅使用本次 payload 的 1/2（须非空，否则 INVALID_SESSION）
2. 若本次为 session.start 且 external_session_key ≠ binding.external_session_key
     → 新会话：ResetBinding（§6.4），写入新 session_id；task_id=显式值或 auto + task_from_prompt_pending=true
3. 若已有 binding 且 payload 1/2 ≠ binding.external_session_key
     → 记 mismatch；ingest 仍用 binding.session_id 注入（不改绑）
4. 若 binding 存在 → session_id = binding.session_id；task_id = binding.task_id
5. session.start 禁止 session_id 为空（禁止 Capture 自行 idgen，与 G3 一致）
```

`BindTaskFromPrompt` 仅由 **`theone prefetch-context`** 调用（§7.1），不在 ingest 批处理里隐式触发，避免与包络 `task_id` 冲突。

### 6.3 多 Agent 并存

**已定**：按 Agent 分文件 `binding.{agent_type}.json`（例如 `binding.cursor.json`、`binding.claude_code.json`），避免并行 IDE 互相覆盖。

单文件内结构：

```json
{
  "agent_type": "cursor",
  "session_id": "02c3c964-…",
  "task_id": "task_…",
  "external_session_key": "02c3c964-…",
  "task_from_prompt_pending": false,
  "workspace_id": "local_default_workspace",
  "project_id": "the-one",
  "repo_id": "the-one",
  "bound_at": "2026-05-29T10:00:00+08:00"
}
```

- `external_session_key`：canonical `conversation_id`（或 Agent 等价物）。
- `task_from_prompt_pending`：`true` 表示仍为 `task_{agent}_auto` 占位，等待首条用户 prompt 绑定（§6.2.2）。
- ingest / prefetch 必须带 `agent_type`，以选择 `binding.{agent_type}.json`。

### 6.4 Binding 生命周期（多 Chat、会话切换）

**范围**：每个 `agent_type` 一个 binding 文件 = 该 IDE 进程内**当前活跃**的一条 Composer 会话（满足「多数场景 G3 单一真相」；并行双 Chat 属边缘，见下）。

| 事件 | binding | `turn-dedup.json` | `prompt-cache` / `inject-cache` |
|------|---------|-------------------|----------------------------------|
| 新 `session.start`（`external_session_key` 变化） | **ResetBinding**：新 `session_id`；`task_id`→auto 或显式；`task_from_prompt_pending=true` | **清空** `last_turn_*` | 清空或下一轮 beforeSubmit 覆盖 |
| `sessionEnd` | 删除 binding 文件（推荐） | 清空 | 清空（`theone-session-end.sh`） |
| `stop`（Cursor） | **不**删 binding | **不**清空 | 回合结束事件；记忆由 `afterAgentResponse` 入库 |
| 同会话内各 Hook | 只读 binding（`BindTaskFromPrompt` 例外） | 仅 EventExpander 读写 | Surface 按回合覆盖 inject-cache |

**并行两个 Composer（边缘）**：后触发的 `session.start` 会 ResetBinding，先打开的 Chat 后续事件可能 mismatch 进 `binding_mismatch.log`。**不**在本阶段做多 binding 槽位；若需支持，后续改为 `binding.{agent_type}.{external_session_key}.json`。

**ingest 注入**：只要 binding 存在，**强制**用 `binding.session_id` / `binding.task_id` 覆盖 envelope payload 中的同名字段（避免 Driver 传错 id）。

---

## 7. 读路径：Context Prefetch 与 Surface Adapter

### 7.1 `theone prefetch-context`

从 v1 `theone-before-submit-prompt.sh` 抽出：

```bash
theone prefetch-context -config theone.yaml -data-dir .theone-data < request.json
```

**输入**（与现 `memory.context` 请求对齐）：`task`、`workspace_id`、`project_id`、`repo_id`、`session_id`、`agent_type`、`token_budget` 等。

**输出**：

```json
{
  "ok": true,
  "context_pack": { },
  "inject_markdown": "# The One 记忆上下文…",
  "retrieval_trace_id": "rt_xxx",
  "used_memory_ids": ["mem_a"],
  "degraded": false,
  "error_summary": ""
}
```

**超时**：建议默认 3s 子超时；超时返回 `ok=false, degraded=true`，不阻断 Agent 提交。

**与 SessionBinder 的衔接（写清「谁定 task_id」）**：

```text
beforeSubmitPrompt (Driver)
  → 写 prompt-cache（含 conversation_id、generation_id、user_summary）
  → theone prefetch-context
       1. SessionBinder.Resolve（读 binding，校验 conversation_id）
       2. 若 task_from_prompt_pending 且 task 非空
            → BindTaskFromPrompt → 更新 binding + DB task_id
       3. memory.context（使用 binding 的 session_id / task_id）
       4. 写 inject-cache（generation_id、retrieval_trace_id、…）
```

读路径与写路径共用同一 binding，避免「capture 用一套 id、召回用另一套 id」。

### 7.2 Surface Adapter

| Agent | 注入方式 |
|-------|----------|
| Cursor | `inject_markdown` → 更新 `.cursor/rules/theone-injected-context.mdc`；有命中时可选 stdout `additional_context` |
| Claude Code | 写入 `.claude/theone-context.md` 或 hook 返回字段（以实现时官方 API 为准） |
| Codex | stdout / 文件供 wrapper 读入下一轮 prompt |

**`inject-cache.json` 回合隔离（避免串轮，审查项 M2）**：

| 字段 | 写入方 | 用途 |
|------|--------|------|
| `generation_id` | `beforeSubmitPrompt`（来自 Hook Common schema） | **权威回合键**；与 Cursor 每条用户消息对齐 |
| `prompt_fingerprint` | 同上（`sha1(normalized_prompt)[:16]`） | 无 `generation_id` 时的回退键 |
| `turn_id` | 可选；默认 `turn_{generation_id}` 或与 `turn-dedup` 一致 | 展开 `turn.completed` 时写入 envelope |

- `beforeSubmitPrompt`：将 `generation_id`（及 `conversation_id`）写入 `prompt-cache.json` 与 `inject-cache.json`。
- `afterAgentResponse`：**不得**依赖 hook 特有字段重算 prompt；从 `prompt-cache.json` 读取 `generation_id`，与 `inject-cache.generation_id` 比较，一致才 `merge_inject_fields`。
- 若 `inject-cache.generation_id` 缺失，回退比较 `prompt_fingerprint`（与 v1 `inject-cache` 字段兼容）。
- 不匹配则忽略 cache（不写错 trace）；新一轮 beforeSubmit **覆盖** inject-cache。

### 7.3 与 MCP 的关系

- prefetch 内部调用与 `theone context` 相同的 `memory.context`。
- Agent 仍可在对话中主动 `memory_context` / `memory_search`；prefetch 是**默认自动召回**，不是唯一读路径。

---

## 8. 多 Agent Driver 矩阵

### 8.1 能力对照

| 能力 | Cursor | Claude Code（P4，**Hooks 全量**，Q4 已决） | Codex wrapper |
|------|--------|--------------------------------------------------|---------------|
| 会话生命周期 | `sessionStart` / `sessionEnd` | `session.lifecycle` | Cursor `stop`≠会话结束，见 §6.0 |
| 回合结束 | `afterAgentResponse`（Cursor）；`Stop`（Claude） | `turn.completed` | v2 不含 tool/file 数组 |
| 用户 prompt 前召回 | `beforeSubmitPrompt` | PrePrompt / UserPromptSubmit 类 → `prefetch-context` | wrapper 拼 prompt |
| 回合结束 | `afterAgentResponse` | Stop / SubagentStop 等 → `turn.completed` | 每轮结束 ingest |
| 工具结果 | `afterMCPExecution` | `PostToolUse` / `PostToolUseFailure` → `capture.atomic` | 日志解析 |
| 文件编辑 | `afterFileEdit` | PostToolUse（Write/Edit）或专用 hook → `capture.atomic` | git diff / 日志 |
| MCP theone | 有 | 有（L1 补高信号；**不**替代 L0 Hook） | 通常无 |

### 8.2 Driver 映射配置（建议）

路径：`doc/adapters/{agent_type}/mapping.yaml`（实现期落地）

```yaml
agent_type: cursor
hooks:
  sessionStart:
    emit:
      - kind: session.lifecycle
        event_type: session.start
  beforeSubmitPrompt:
    action: prefetch-context
  afterAgentResponse:
    emit:
      - kind: turn.completed
        # v2：payload 不含 tool_results / file_edits（§5.4），增量已由 afterFileEdit / afterMCPExecution 提交
  afterFileEdit:
    emit:
      - kind: capture.atomic
        event_type: file.edit.summary
  afterMCPExecution:
    emit:
      - kind: capture.atomic
        event_type: tool.result.summary
  sessionEnd:
    emit:
      - kind: session.lifecycle
        event_type: session.end
```

### 8.3 目录规划（实现期）

```text
doc/adapters/
  README.md
  protocol/v1.md              # 可从本文 §4 拆出
  cursor/mapping.yaml
  claude_code/mapping.yaml
  codex/mapping.yaml

doc/claude/                   # Claude Code 安装模板（对称 doc/cursor/）
  README.md
  settings.hooks.example.json
  hooks/、mcp.json、CLAUDE.md

drivers/
  cursor/entry.sh             # 统一入口：读 HOOK_EVENT，调 theone ingest
  claude_code/entry.sh
  codex/                      # 或以 wrapper 脚本为主

.cursor/hooks.json            # command 指向 drivers/cursor/entry.sh
```

---

## 9. CLI 设计

### 9.1 命令一览

| 命令 | v2 角色 | v1 关系 |
|------|---------|---------|
| `theone ingest` | **推荐**统一入站 | 新增；内部分发 |
| `theone observe` | 单条 observe | 保留 |
| `theone observe-turn` | TurnPayload 入站 | 保留，= ingest 一种形状 |
| `theone observe-envelope` | Envelope 入站 | 保留，= ingest 一种形状 |
| `theone prefetch-context` | 读路径 | 从 shell 抽出 |
| `theone context` | 诊断 / 脚本直接调 | 保留 |

### 9.2 `ingest` 处理流程

```text
stdin JSON
  → 判别根类型：
       含 events[]     → BatchEnvelope + ValidateBatchEnvelope
       否则            → IngestEnvelope + ValidateIngestEnvelope（单条，必填 event_type）
  → SessionBinder.Resolve(agent_type)（读/写 binding.{agent_type}.json）
  → 对每条待处理事件：
       0. 若 ingest-ledger 已有 (ingest_id, event_index) → 记 deduped，跳过
       1. 解析 kind（§4.4，显式优先）
       2. EnsureSessionReady（§6.2.1：必要时 auto session.start）
       3. 用 binding **覆盖** payload 的 session_id / task_id（§6.4）
       4. 分支：
            session.lifecycle → ObserveRequest → observe（单条）
            turn.completed    → TurnPayload → EventExpander(ExpandMode, §5.4) → observe batch
            capture.atomic    → AtomicPayload → atomic 去重(§5.2) → ObserveRequest → observe（单条）
       5. observe 成功 → 写入 ingest-ledger（§9.2.1）；失败 → failures[] + dead_letter（不中断同批其余条）
  → 写 ingest.jsonl 审计（可选）
  → stdout JSON：
       {
         "ok": true,
         "ingest_id": "…",
         "accepted": 3,
         "deduped": 1,
         "failed": 0,
         "failures": []
       }
```

**批量语义**：

| 项 | 约定 |
|----|------|
| 原子性 | **无**跨条事务；已成功的 observe 不回滚 |
| 幂等 | 同一 `ingest_id` 重试：已处理条目标为 `deduped`；ingest 在 **`ingest-ledger.json`** 持久化 `(ingest_id, event_index)`（批内 `event_index` 从 0 起，单条包络视为 `0`） |
| 空批 | `events: []` → `ok=true, accepted=0` |
| MCP 包络 | `producer` 以 `mcp:` 开头 → 拒绝，`error_code=WRONG_TRANSPORT` |

#### 9.2.1 幂等账本与 observe 顺序（G6）

| 步骤 | 顺序 | 说明 |
|------|------|------|
| 重试入口 | 先查 `ingest-ledger` | 命中 `(ingest_id, event_index)` → 本条 `deduped`，**不**调 observe |
| 首次处理 | observe **成功** 后写 ledger | 避免「ledger 有记录但 DB 无事件」导致重试永远跳过 |
| observe 成功、ledger 写失败 | 允许重试 | 依赖 DB `content_hash` 二次 dedup；ledger 补写 |
| `deduped` 统计 | 区分 `ledger_deduped` 与 `content_hash_deduped`（响应字段可选） | 验收时不混淆 |

`ingest_id` 由 **Driver 每 Hook 触发生成**（建议 `ing_{producer}_{unix_nano}`），重试**必须复用**同一 `ingest_id`。

**`observe-turn` / `observe-envelope`（P0）**：

- 作为 `ingest` 别名；缺省 `kind` 按 §4.4 推断。
- P0 起：`observe-envelope` = `ingest` 别名，统一 `IngestProcessor` 分支（`legacy` 默认）。

### 9.3 可观测性

| 机制 | 说明 |
|------|------|
| `THEONE_HOOK_DEBUG=1` | Driver / ingest 保留 stderr，不丢弃 |
| `runtime-state/ingest.jsonl` | 可选：每次 ingest 一行审计 |
| 现有 `context-cache.error.log` | prefetch 失败保留 |
| `dead_letter.jsonl` | `FailureQueue`；每条含 `ingest_id`、**`event_index`**（批内下标，单条为 `0`）、`error_code`、`session_id` 等，支持条级重放 |
| `ingest-ledger.json` | 已成功的 `(ingest_id, event_index)`；重试时命中则计入 `deduped` 且不再 observe |

---

## 10. 与当前代码实现的兼容性

### 10.1 可直接复用（无需改语义）

| 组件 | 说明 |
|------|------|
| `TurnPayload` + `BuildObserveRequests` | v2 核心展开逻辑 |
| `IngestEnvelope` + `ValidateIngestEnvelope` + `TurnPayloadFromEnvelope` | 扩展可选字段即可 |
| `observe` / `observe-turn` / `observe-envelope` | 作为 ingest 后端 |
| `callLocalObserveBatch` | 批量调用 `memory.observe` |
| Capture `content_hash` 去重 | 入库层兜底 |
| `FailureQueue` | dead letter |
| `agent_type`：`cursor` / `codex` / `claude_code` | 校验已存在 |
| MCP `memory.*` | 不变 |

### 10.2 增量扩展（向后兼容）

- 新增 `theone ingest`、`theone prefetch-context`；
- Envelope 增加可选 `kind`、`events[]`、顶层 `agent_type`；
- 拆分 `binding.{agent_type}.json` / `turn-dedup.json`，`FileStateStore` 仅读写 dedup 字段；
- 新增 `ValidateBatchEnvelope`、`EnsureSessionReady`、`AtomicPayload` 映射；
- 新增 `SessionBinder`、`IngestLedger`、`ExpandMode` 配置（环境变量或 `theone.yaml`）；
- `FailureRecord` 增加可选 `event_index`（JSON 字段，向后兼容）。

### 10.3 行为变更（DB schema 不变，事件分布变）

| 变更 | 影响 |
|------|------|
| `ExpandMode=v2` + 文件/工具走 `capture.atomic` | 减少占位 `conversation.message`；统计与规则命中需重估 |
| Session 强绑定 | 新 session 归属更稳定；可能与 v1 混跑时会话链不连续 |
| 拆分 runtime 状态文件 | 需一次性迁移；进行中会话可能短暂丢失接入层去重（DB 仍去重） |

### 10.4 兼容矩阵摘要

```text
                    DB / MCP API    现有 CLI / Codex 脚本    新事件语义
协议 (Envelope)         ✓                  △ 别名即可              ✓
TurnRuntime 核心        ✓                  ✓                      △ 需 ExpandMode
Cursor hooks.json       ✓                  ✓                      ✓
runtime-state           ✓                  △ 迁移                  △
atomic 增量捕获         ✓                  ✗ 需 Go + Driver        ✗ 分布变化
```

### 10.5 Feature Flag（推荐）

配置项 `adapter.expand_mode` 或环境变量 `THEONE_EXPAND_MODE`：

- `legacy`（**P0–P1 默认**）：与 v1 行为一致；
- `v2`（**P2 起在 the-one dogfood**，P3 起建议为仓库默认）：启用 `capture.atomic` 与 Session 强绑定（§5.1）。

便于 dogfood 渐进切换；回滚时设回 `legacy` 即可，无需迁移 DB。

---

## 11. 实施阶段

| 阶段 | 内容 | 验收 |
|------|------|------|
| **P0** | `theone ingest`；`BatchEnvelope`；`kind` 路由（`legacy` 默认）；**最小 SessionBinder**（Resolve、bootstrap、`task_from_prompt_pending`）；`ingest-ledger`；dead_letter 含 `event_index` | 乱序 file hook 落库；ingest 重试 `deduped`；bootstrap 后首条 beforeSubmit 能把 `task_*_auto` 升为 prompt 指纹 |
| **P1** | `binding` / `turn-dedup` 拆文件；迁移 `session.json`；§6.4 新会话 Reset；禁止 Capture 空 `session_id` 自生成 | **已落地**；验收 `scripts/acceptance/p1_binding.sh` |
| **P2** | **ExpandMode=v2**（§5.1 决议）：`doc/adapters/cursor/mapping.yaml`；Hook → `theone ingest`；`atomic-dedup.json`；**不双写** | **已落地**；验收 `scripts/acceptance/p2_expand_v2.sh`；`theone.yaml` 默认 `v2` |
| **P3** | `prefetch-context` 含 `BindTaskFromPrompt`；Surface 瘦身；持久化 `generation_id`；the-one **默认 `v2`** | **已落地**；验收 `scripts/acceptance/p3_prefetch.sh` |
| **P4** | **Claude Code Hooks 全量**（Q4）：`drivers/claude_code` + `doc/adapters/claude_code/mapping.yaml`（会话 / prefetch / atomic 工具·文件 / turn.completed）；`binding.claude_code.json`；Surface → `.claude/theone-context.md`；与 Cursor 共用 `drivers/shared/theone-build-ingest.py` | **已落地**；验收 `scripts/acceptance/p4_claude_hooks.sh`；安装 `scripts/install-claude-hooks.sh` |
| **P5** | 废弃 shell 内 Python 业务逻辑；`drivers/cursor/entry.sh` + `drivers/shared/theone-hook-*.py`；`doc/cursor/hooks` 仅 `exec` 转发 | **已落地**；验收 `scripts/acceptance/p5_thin_driver.sh`；v1 脚本见 `doc/archive/v1-hook-scripts/` |

> **说明**：LLM 能力不纳入 P0–P5 Hook 改造范围；摘要增强与记忆价值分析由 [后续完善规划.md](./后续完善规划.md) F-001 / F-002 在 `internal/processor` 独立排期。

---

## 12. 已决建议

| 议题 | 决议 |
|------|------|
| 统一 CLI | `theone ingest` 为主入口，`observe-*` 保留别名 |
| Cursor canonical session | `conversation_id` / `sessionStart.session_id`；新 Chat 时 ResetBinding（§6.4） |
| 文件/工具 Hook | 仅 `capture.atomic`（`ExpandMode=v2`，P2 启用，§5.1） |
| **LLM 放置** | **不在 Hook**；捕获期仅截断；F-001 / 可选 enrich 在 Go `async_job`（§3.4） |
| Claude 接入 | **Hooks 全量**（Q4）；**P4** 在 Cursor **P2** 完成后实施；MCP 仅作 L1 补充，不作 P4 主交付 |
| Claude 注入表面 | 优先仓库内 `.claude/theone-context.md`（或官方 hook 返回字段，以实现时 API 为准） |
| project/repo | `theone.yaml` 默认 + 环境变量覆盖，Driver 不硬编码 `the-one` |
| 切换策略 | P0–P1 默认 `legacy`；P2 dogfood `v2`；P3 起 the-one 默认 `v2`（§5.1） |
| L0/L1 分工 | 见 §3.3.1；工具/文件以 L0 atomic 为主 |
| Session 乱序 | `EnsureSessionReady` 自动 bootstrap（§6.2.1） |
| 多 Agent binding | `binding.{agent_type}.json`（§6.3） |
| 无 conversation_id | 优先拒绝 ingest；开发模式可生成 `sess_*_{nano}` |
| inject-cache | 按 `generation_id` 隔离（§7.2）；`task_id` 由 prefetch 内 `BindTaskFromPrompt` 定 |
| 批量 ingest | best-effort 逐条 + 条级 dead_letter（§9.2） |

### 12.1 议题决议一览（均已闭合）

- [x] **Q1**：是否采纳 `theone ingest` 为唯一对外推荐入口？→ **是**（`observe-*` 保留别名）
- [x] **Q2**：Cursor 无 `conversation_id` 时是否允许回退 `sessionId`？→ **允许**；`sessionStart.session_id` 与 `conversation_id` 同值（§6.0）；禁止 `sess_*_{YYYYMMDD}`
- [x] **Q3**：`ExpandMode=v2` 何时、怎么切？→ **P2** 单开关；**先改 Cursor Driver 再开 v2**；**不双写**；P3 起 the-one 默认 `v2`；详见 §5.1
- [x] **Q4**：Claude Code 首版目标？→ **Hooks 全量**（非 MCP-only）；**P4**、Cursor **P2** 后启动；详见 §8.1、§11
- [x] **Q5**：`binding` 单文件多 agent 还是分文件？→ **`binding.{agent_type}.json`**
- [x] **Q6**：bootstrap 与 `task_id` 升级？→ **`task_from_prompt_pending`**（§6.2.2）
- [x] **Q7**：ingest 幂等账本？→ **`ingest-ledger.json`** + dead_letter **`event_index`**（§9.2.1）

---

---

## 13. 附录 B：参考代码索引

| 主题 | 路径 |
|------|------|
| Turn 展开 | `internal/adapter/turn_runtime.go` |
| Envelope | `internal/adapter/model.go`、`internal/adapter/ingest.go` |
| 运行时状态 | `internal/adapter/state.go` |
| CLI | `cmd/theone/main.go` |
| 入库去重 | `internal/capture/service.go` |
| Codex 验收 | `scripts/acceptance/p2_envelope.sh` |
| v1 Cursor 模板 | `doc/cursor/hooks/` |

---