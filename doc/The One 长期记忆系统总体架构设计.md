# The One 总体架构设计

> 状态：v0.1 架构基线已冻结。后续研发按独立实现设计和分期规划推进，除非发现影响一期目标成立的重大逻辑问题，否则不再修改本文档的核心架构。
> 本次补充：新增设计复查 checkpoint 需求，用于降低重复设计复查任务的历史上下文加载成本，不改变一期核心架构边界。

## 1. 背景和目标

本文档描述一期 AI Coding Agent 长期记忆系统的总体架构和核心数据模型。

一期目标不是构建完整学习画像系统，也不是企业级知识治理平台，而是先实现一个本地个人工具：

```text
面向 Codex、Claude Code、Cursor 的本地 AI Coding Agent 持久记忆层。
```

核心目标：

1. 降低重复上下文传递带来的 Token 消耗。
2. 让多个 AI Coding Agent 共享项目事实、架构约束、历史决策和失败经验。
3. 提升跨会话任务连续性，减少重复探索、重复解释和重复踩坑。
4. 建立可扩展到小团队服务和企业平台的 Memory Engine 基础。
5. 对反复发生的设计复查、架构评审和文档校验任务，沉淀可复用 checkpoint，减少重复加载完整历史对话。

一期明确不做：

1. 完整用户学习画像系统。
2. 企业级高可用、备份恢复、合规审计。
3. 完整自研 Codegraph。
4. 在线 LLM rerank。
5. 保存完整源码和完整工具输出。

## 2. 设计原则

### 2.1 Memory 不是聊天历史

长期记忆不应等同于聊天记录、工具日志或向量数据库。系统应围绕记忆生命周期设计：

```text
捕获 -> 过滤 -> 编码 -> 准入 -> 巩固 -> 检索 -> 使用反馈 -> 强化/衰减 -> 遗忘
```

### 2.2 MCP 统一接入，Capture Adapter 负责自动捕获

Codex、Claude Code、Cursor 一期统一通过 MCP 暴露记忆能力，但自动捕获不能只依赖 MCP。

系统采用：

```text
MCP Tools + Agent-specific Capture Adapter
```

MCP 负责主动工具调用，Capture Adapter 负责尽可能捕获每个 Agent 的对话、工具调用、文件编辑和 session 生命周期事件。

### 2.3 不保存完整代码和 output 全文

系统保存：

1. 关键词。
2. 关键片段。
3. 简化后、理解后的语句。
4. hash。
5. source ref / code ref / tool ref。

系统不保存：

1. 完整源码。
2. 完整工具 output。
3. 原始大段日志。

### 2.4 Codegraph 是代码结构索引层，不是长期记忆层

Codegraph 类能力只负责代码结构事实：

```text
文件、符号、调用关系、import/export、路由、影响面
```

Memory Engine 负责：

```text
历史决策、设计原因、用户偏好、项目约束、失败经验、过程规则
```

Memory 不复制代码结构事实，只保存 `code_ref`。

### 2.5 在线检索轻量化，重处理异步化

一期在线检索 P95 目标为 100ms。

在线允许：

1. FTS/BM25。
2. 向量检索。
3. metadata filter。
4. 轻量关系扩展。
5. 规则 rerank。

异步执行：

1. LLM 抽取。
2. 深度摘要。
3. 关系构建。
4. retention 计算。
5. 冲突检测。
6. 巩固和归档。

### 2.6 设计复查需要 checkpoint，而不是重复加载历史

架构设计、研发规划和技术方案文档会反复被复查。对于“再次从头检查是否有内容、逻辑缺失或者错误”的任务，系统不应每次加载完整历史对话，而应沉淀结构化 checkpoint：

```text
设计文档事实源 -> 章节 hash / diff -> 复查结论 -> 用户确认/忽略项 -> 下次复查策略
```

checkpoint 负责保存：

1. 上一次复查的目标文档和章节。
2. 文档版本、章节 hash、最近修改时间。
3. 复查意图，例如完整性、逻辑一致性、业务闭环、分期可验收性。
4. 结论，例如无重大缺失、已补充、延期处理。
5. 用户确认忽略或后续再处理的问题。
6. 下一次复查时应优先关注的边界。

checkpoint 不替代文档事实源。下一次复查仍应读取当前文档、目录、关键章节或 diff，但历史对话只通过 checkpoint 压缩加载。

## 3. 一期约束

| 维度 | 约束 |
|---|---|
| 产品形态 | 本地个人工具 |
| 接入对象 | Codex、Claude Code、Cursor |
| 接入方式 | MCP + Capture Adapter |
| 捕获目标 | Level4：对话、工具调用、工具结果摘要、文件编辑摘要、session 生命周期 |
| 实现语言 | Go |
| 部署形态 | 单二进制 |
| 默认存储 | SQLite + FTS5 |
| 本地向量增强 | sqlite-vec，可选启用 |
| 可选存储 | PostgreSQL + pgvector |
| 写入方式 | 异步 |
| 检索目标 | P95 <= 100ms |
| 原始数据 | 不保存完整代码和 output 全文 |
| Codegraph | 一期抽象 Code Index Adapter，优先接入现有实现 |
| 高可用 | 一期不设计 |
| 备份恢复 | 一期不设计 |
| 企业治理 | 一期不设计 |

## 4. 总体架构

```text
Codex / Claude Code / Cursor
        |
        | MCP Tools + Capture Adapter
        v
Local Memory Daemon
        |
        +-- MCP Server
        |     - memory.search
        |     - memory.context
        |     - memory.remember
        |     - memory.observe
        |     - memory.review
        |
        +-- Capture Layer
        |     - conversation capture
        |     - tool call capture
        |     - tool input/output summarization
        |     - file edit summary
        |     - session lifecycle
        |
        +-- Ingestion Pipeline
        |     - event normalization
        |     - content minimization
        |     - dedup
        |     - evidence extraction
        |     - memory candidate generation
        |
        +-- Async Memory Engine
        |     - admission controller
        |     - memory processor
        |     - consolidation worker
        |     - relation builder
        |     - retention scorer
        |
        +-- Retrieval Orchestrator
        |     - intent detection
        |     - hybrid retrieval
        |     - relation expansion
        |     - rule-based rerank
        |     - context budget builder
        |
        +-- Code Index Adapter
        |     - symbol search
        |     - callers/callees
        |     - impact analysis
        |     - code ref resolution
        |
        +-- Storage Layer
              - SQLite metadata/event log
              - FTS5 keyword index
              - optional sqlite-vec vector index
              - relation edge table
              - config/state
```

## 5. 模块职责

### 5.1 MCP Server

MCP Server 是所有 Agent 共享记忆能力的统一入口。

一期 MCP 工具：

| 工具 | 职责 |
|---|---|
| `memory.search` | 按任务、查询、scope 检索相关记忆 |
| `memory.context` | 构造适合注入 Agent 上下文的压缩记忆包 |
| `memory.remember` | 显式写入用户声明、决策、失败经验等记忆 |
| `memory.observe` | 接收 Agent 或 Adapter 上报的观察事件 |
| `memory.review` | 查看、确认、拒绝、编辑 pending memory |

通用接口约束：

1. 所有 MCP 工具响应都应包含 `request_id` 或可由服务端日志关联的 trace id。
2. 写入类工具必须支持幂等：`memory.observe` 优先使用 `content_hash + session_id + event_type` 去重，`session_id` 为空时使用 `content_hash + source_channel + workspace_id + project_id + repo_id + event_type`；`memory.remember` 使用 `content_hash + scope + memory_type` 辅助去重。
3. 读路径错误不应中断 Agent 主流程，允许返回空结果和 diagnostics；写路径错误应明确 `accepted=false` 和原因。
4. 错误响应统一包含 `error_code`、`message`、`retryable`、`fallback_hint`。
5. 所有接口入参在服务端再次执行 scope validator 和内容最小化检查，不能完全信任 Adapter。
6. `task_id` 是可选入参。调用方未提供时，服务端按 `session_id + normalized_task` 查找或创建 `agent_task`；仍无法识别任务边界时使用该 session 的 `default_task`。
7. `content_hash` 可由 Adapter 上报，也可由服务端根据最小化后的摘要、关键片段、source refs 和 scope 重新计算；不得要求客户端发送完整原文来计算 hash。

通用错误响应示例：

```json
{
  "request_id": "req_001",
  "accepted": false,
  "error": {
    "error_code": "CONTENT_TOO_LARGE",
    "message": "payload exceeds configured summary boundary",
    "retryable": false,
    "fallback_hint": "send summarized content with salient_spans and content_hash"
  }
}
```

#### 5.1.1 memory.search

用途：面向 Agent 的轻量检索接口，返回候选记忆，不直接构造最终上下文。

请求：

```json
{
  "query": "为什么 auth 模块没有使用异步消息",
  "task": "分析认证模块架构演进",
  "task_id": "task_001",
  "workspace_id": "ws_001",
  "project_id": "proj_001",
  "repo_id": "repo_001",
  "scope": ["project_local", "repo_local", "user_global"],
  "memory_types": ["decision", "constraint", "failure", "project_fact"],
  "limit": 10,
  "include_archived": false,
  "include_evidence": true
}
```

响应：

```json
{
  "request_id": "req_search_001",
  "results": [
    {
      "memory_id": "mem_001",
      "memory_type": "decision",
      "scope": "project_local",
      "title": "auth 模块暂不引入异步消息",
      "content": "认证链路要求请求内完成身份校验，历史决策是不引入异步消息以避免一致性和排障复杂度。",
      "score": 0.86,
      "confidence": 0.91,
      "state": "stable",
      "tier": "long_term",
      "evidence_refs": ["ev_001"],
      "code_refs": ["cr_001"]
    }
  ],
  "diagnostics": {
    "retrieval_trace_id": "rt_001",
    "fts_hits": 4,
    "vector_hits": 6,
    "relation_expanded": 2,
    "latency_ms": 42
  }
}
```

约束：

1. 默认只返回 `stable` 和高置信 `provisional`。
2. `archived` 仅在 `include_archived=true` 时返回。
3. 返回结果必须包含 score 组成的基本诊断，便于后续评估召回质量。
4. 响应必须返回 `retrieval_trace_id`，后续 `memory_access_log` 使用该 ID 关联候选排序、注入和反馈。

#### 5.1.2 memory.context

用途：根据当前任务构造可注入 Agent prompt 的压缩上下文包。

请求：

```json
{
  "task": "继续实现 auth token 过期边界修复",
  "task_id": "task_002",
  "workspace_id": "ws_001",
  "project_id": "proj_001",
  "repo_id": "repo_001",
  "agent_type": "codex",
  "token_budget": 1800,
  "include_code_refs": true,
  "include_evidence_summary": true
}
```

响应：

```json
{
  "request_id": "req_context_001",
  "context_pack": {
    "summary": "当前项目认证模块历史上多次出现 token 过期边界问题，用户偏好先确认边界条件和测试覆盖再修改实现。",
    "memories": [
      {
        "memory_id": "mem_101",
        "type": "failure",
        "compressed": "上次认证问题根因是过期时间比较使用了错误边界，修复时应覆盖等于过期时间的测试。",
        "why_included": ["task_match", "failure_memory", "high_retention_score"],
        "score_breakdown": {
          "semantic": 0.76,
          "bm25": 0.62,
          "retention": 0.81
        }
      }
    ],
    "constraints": [
      "不保存完整工具输出，只保留错误签名和摘要。"
    ],
    "code_refs": [
      {
        "repo_id": "repo_001",
        "file_path": "internal/auth/middleware.go",
        "symbol": "ValidateToken",
        "ref_summary": "token 过期判断入口"
      }
    ]
  },
  "used_memory_ids": ["mem_101"],
  "retrieval_trace_id": "rt_002",
  "latency_ms": 57
}
```

约束：

1. `memory.context` 必须记录 `injected` access log。
2. 上下文包必须按 token budget 裁剪。
3. 不直接输出完整源码和完整工具输出。
4. 响应必须返回 `retrieval_trace_id`，便于把注入记忆、任务结果和后续强化关联起来。

#### 5.1.3 memory.remember

用途：用户或 Agent 显式保存高价值记忆。

请求：

```json
{
  "content": "用户偏好先进行架构边界和风险分析，再给工程实现方案。",
  "memory_type": "preference",
  "scope": "user_global",
  "workspace_id": "ws_001",
  "project_id": null,
  "source_type": "user_declared",
  "importance": 0.9,
  "confidence": 1.0,
  "pinned": false,
  "evidence": {
    "interpreted_statement": "用户明确要求回答面向架构师，强调技术深度、系统性和工程可落地性。",
    "keywords": ["架构师", "系统性", "工程落地"]
  }
}
```

响应：

```json
{
  "request_id": "req_remember_001",
  "memory_id": "mem_201",
  "state": "stable",
  "tier": "durable",
  "effective_reinforcement": 2.5
}
```

约束：

1. `source_type=user_declared` 默认强强化。
2. 架构决策、安全约束、用户能力短板即使显式写入，也可按策略进入 `pending_review`。
3. 显式写入必须保留 evidence。

#### 5.1.4 memory.observe

用途：Capture Adapter 或 Agent 主动上报观察事件，是自动捕获链路的入口。

请求：

```json
{
  "session_id": "sess_001",
  "task_id": "task_003",
  "agent_type": "claude_code",
  "workspace_id": "ws_001",
  "project_id": "proj_001",
  "repo_id": "repo_001",
  "event_type": "tool.result.summary",
  "actor": "tool",
  "tool_name": "go test",
  "input_summary": "运行 auth package 测试",
  "output_summary": "TestTokenExpiry 在边界时间失败",
  "keywords": ["go test", "auth", "token expiry", "boundary"],
  "salient_spans": [
    "TestTokenExpiry failed at exact expiry timestamp"
  ],
  "source_refs": [
    {
      "source_type": "tool_output",
      "command_hash": "sha256:...",
      "exit_code": 1
    }
  ],
  "content_hash": "sha256:..."
}
```

响应：

```json
{
  "request_id": "req_observe_001",
  "raw_event_id": "evt_001",
  "accepted": true,
  "pipeline": "async",
  "deduped": false
}
```

约束：

1. `memory.observe` 必须快速返回，不等待 LLM 抽取和巩固。
2. 上报内容必须已经过 adapter 侧截断和摘要。
3. 服务端需要再次执行持久化边界检查，拒绝完整代码、完整 output 和完整会话全文进入长期存储。

#### 5.1.5 memory.review

用途：查看和处理待确认记忆。

请求：

```json
{
  "action": "list",
  "workspace_id": "ws_001",
  "project_id": "proj_001",
  "state": "pending_review",
  "limit": 20
}
```

处理请求：

```json
{
  "action": "approve",
  "memory_id": "mem_301",
  "edit_content": "确认后的架构决策内容",
  "feedback": "保留该决策，但补充适用边界。"
}
```

响应：

```json
{
  "request_id": "req_review_001",
  "memory_id": "mem_301",
  "state": "stable",
  "user_confirmed": true,
  "effective_reinforcement": 2.0
}
```

约束：

1. `approve` 记录 `user_confirmed`。
2. `reject` 记录 `user_rejected` 并降权或归档。
3. `edit` 必须生成新版本，保留旧版本来源。

### 5.2 Capture Layer

Capture Layer 负责把不同 Agent 的事件归一化为统一 `RawEvent`。

一期目标捕获：

1. 用户消息。
2. Agent 回复摘要。
3. 工具调用。
4. 工具输入摘要。
5. 工具输出摘要。
6. 文件编辑摘要。
7. session start / end。
8. 用户纠正。
9. 用户显式声明。
10. 任务成功或失败信号。

每个 Adapter 需要上报 capability：

```text
supports_conversation_capture
supports_tool_call_capture
supports_tool_output_capture
supports_file_edit_capture
supports_session_lifecycle_capture
supports_mcp_observe
```

#### 5.2.1 Capture Adapter 统一接口

Capture Adapter 负责把 Agent 原生事件转换为 `memory.observe` 请求。

Go 接口建议：

```go
type CaptureAdapter interface {
    Name() string
    Capabilities(ctx context.Context) (CaptureCapabilities, error)
    Start(ctx context.Context, session AgentSession) error
    Stop(ctx context.Context, sessionID string) error
    Observe(ctx context.Context, event AgentNativeEvent) (*RawEventEnvelope, error)
}

type CaptureCapabilities struct {
    ConversationCapture   bool
    ToolCallCapture       bool
    ToolOutputCapture     bool
    FileEditCapture       bool
    SessionLifecycle      bool
    MCPObserve            bool
    RequiresWrapper       bool
    RequiresRulesInjection bool
}
```

统一事件 envelope：

```json
{
  "adapter": "claude_code",
  "capability_level": 4,
  "session_id": "sess_001",
  "event_type": "tool.result.summary",
  "occurred_at": "2026-05-22T21:00:00Z",
  "payload": {},
  "capture_quality": {
    "has_conversation_events": true,
    "has_conversation_summary": true,
    "has_tool_input": true,
    "has_tool_output_summary": true,
    "has_file_diff_summary": true
  }
}
```

#### 5.2.2 Level4 捕获定义

一期 Level4 标准：

| 能力 | 要求 |
|---|---|
| conversation capture | 捕获用户消息和 Agent 回复摘要 |
| tool call capture | 捕获工具名、调用时间、参数摘要 |
| tool output capture | 捕获 output 摘要、错误签名、exit code、hash |
| file edit capture | 捕获文件路径、符号、diff 摘要、content hash |
| session lifecycle | 捕获 session start/end、任务目标、任务结果 |
| memory observe | 能主动上报标准 RawEvent |

注意：Level4 不要求保存完整会话、完整工具输出或完整 diff。

Adapter 可以在进程内短暂读取完整原始事件用于摘要和 hash 计算，但持久层只保存摘要、关键片段、关键词、hash 和 source ref。一期不设计敏感内容改写流水线，只做内容最小化和敏感等级标记。

#### 5.2.3 Claude Code Adapter

推荐策略：

1. 优先使用 Claude Code hooks 捕获 session、prompt、tool use、stop 事件。
2. 通过 MCP 提供 `memory.observe` 和 `memory.context`。
3. 在 session end 执行一次摘要和候选记忆生成。
4. 对工具输出做本地截断、摘要、错误签名提取。

目标：

```text
conversation_capture: true
tool_call_capture: true
tool_output_capture: true
file_edit_capture: true
session_lifecycle: true
```

#### 5.2.4 Codex Adapter

推荐策略：

1. MCP 作为主接入路径。
2. 通过本地 wrapper 或日志 collector 捕获会话和工具事件。
3. 通过项目规则注入要求 Agent 在关键节点调用 `memory.observe`。
4. 对缺失的工具事件，使用 session checkpoint 摘要补偿。

目标：

```text
conversation_capture: true
tool_call_capture: true
tool_output_capture: true
file_edit_capture: true
session_lifecycle: true
```

风险：

1. Codex 运行环境差异可能导致被动捕获能力不一致。
2. 需要 adapter capability 探测，避免假定所有事件都能捕获。

#### 5.2.5 Cursor Adapter

推荐策略：

1. MCP 提供统一工具能力。
2. 使用 Cursor rules 引导 Agent 调用 `memory.context` 和 `memory.observe`。
3. 通过可用的本地日志、插件或 wrapper 捕获对话和工具摘要。
4. 文件编辑事件优先基于 Git diff 或 filesystem watcher 生成摘要。

目标：

```text
conversation_capture: true
tool_call_capture: true
tool_output_capture: true
file_edit_capture: true
session_lifecycle: true
```

风险：

1. Cursor 对内部工具调用的暴露能力可能受版本影响。
2. 一期需要把文件编辑捕获和会话事件捕获解耦，避免因为工具捕获不足影响核心记忆链路。

#### 5.2.6 捕获失败降级

虽然一期目标是三个 Agent 都做到 Level4，但实现必须支持降级：

| 降级级别 | 能力 |
|---|---|
| Level1 | Agent 主动调用 memory tools |
| Level2 | 捕获 session start/end 和显式记忆 |
| Level3 | 捕获工具调用摘要 |
| Level4 | 捕获对话、工具、文件编辑和 session 生命周期 |

降级只影响数据完整性，不应影响 Memory Engine 的稳定性。每个 session 应记录实际 capture level，用于后续评估。

### 5.3 Ingestion Pipeline

Ingestion Pipeline 将原始事件加工为可入库候选。

流程：

```text
RawEvent
  -> normalize
  -> content minimization
  -> dedup
  -> extract evidence
  -> generate memory candidate
  -> classify memory type
  -> compute admission score
  -> provisional write
```

关键规则：

1. 所有自动捕获事件先进入事件层，不直接进入稳定长期记忆。
2. 一期先通过内容最小化避免保存完整代码、完整 output 和完整会话，不实现复杂敏感内容改写流程。
3. 工具输出只保存摘要、关键片段、错误签名、hash 和 tool ref。
4. 代码相关内容只保存 code ref 和理解后的摘要。
5. 高影响记忆进入 `pending_review`。

### 5.4 Async Memory Engine

Async Memory Engine 负责长期记忆质量。

核心任务：

1. Admission Control：判断候选是否值得进入长期记忆。
2. Memory Processor：生成摘要、实体、关系、检索线索。
3. Consolidation Worker：去重、合并、冲突检测、状态提升。
4. Relation Builder：构建支持、矛盾、因果、来源、泛化关系。
5. Retention Scorer：计算保留分数、衰减率和 tier。
6. Review Queue：管理需要用户确认的高影响记忆。

状态流转：

```text
raw_event
  -> provisional
  -> pending_review
  -> stable
  -> archived
  -> deleted
```

其中 `pending_review` 不是必经状态，主要用于：

1. 架构决策。
2. 安全约束。
3. 用户能力短板。
4. 高影响失败经验。

### 5.5 Retrieval Orchestrator

Retrieval Orchestrator 负责 100ms 内完成轻量检索和上下文构建。

流程：

```text
query
  -> infer intent
  -> scope filter
  -> FTS retrieve
  -> vector retrieve
  -> metadata retrieve
  -> relation expansion
  -> rule-based rerank
  -> conflict/staleness filter
  -> context budget pack
```

排序信号：

1. 语义相关性。
2. BM25/关键词相关性。
3. task fit。
4. scope fit。
5. confidence。
6. retention score。
7. base activation。
8. source support。
9. relation support。
10. conflict penalty。
11. staleness penalty。
12. context cost。

一期 rerank 公式建议：

```text
retrieval_score =
  0.28 * semantic_score
  + 0.22 * bm25_score
  + 0.16 * task_fit
  + 0.12 * scope_fit
  + 0.10 * retention_score
  + 0.06 * relation_support
  + 0.04 * source_quality
  + 0.02 * recency_fit
  - 0.20 * conflict_penalty
  - 0.16 * staleness_penalty
  - 0.10 * context_cost_penalty
```

如果 sqlite-vec 不可用，`semantic_score` 置为 0，并按剩余正向项重新归一化。`retrieval_score` 只用于排序，不直接改变 retention。

工程实现应先计算 `raw_retrieval_score`，再执行：

```text
retrieval_score = clamp(raw_retrieval_score, 0, 1)
```

避免强冲突、过期或高上下文成本导致负分在不同实现中处理不一致。

### 5.6 Code Index Adapter

Code Index Adapter 抽象代码结构索引能力。

一期接口：

```text
searchSymbols(query, repo, filters)
getSymbol(symbolId)
getCallers(symbolId)
getCallees(symbolId)
getImpact(symbolId | filePath)
getFileStructure(repo, path)
buildTaskContext(task, budget)
resolveCodeRefs(memory.code_refs)
```

一期接入优先级：

| 优先级 | 实现 | 适用场景 | 取舍 |
|---:|---|---|---|
| 1 | 本地 Git + tree-sitter/ctags 轻量索引 | 单机本地、常见语言符号定位 | 实现简单，但跨语言语义和调用关系有限 |
| 2 | LSP Adapter | Go、TypeScript、Python、Rust 等语言工作区 | 复用语言服务器能力，但启动和协议适配复杂 |
| 3 | SCIP/LSIF 索引导入 | 需要稳定跨语言符号图 | 索引质量高，但生成链路更重 |
| 4 | 外部 Codegraph 服务适配 | 后续小团队或企业版 | 能力强，但一期不作为默认依赖 |

一期默认建议先实现轻量本地索引 Adapter，接口保持可替换；Memory 层只依赖 `code_ref` 和 `CodeIndexAdapter` 抽象，不依赖具体索引实现。

设计边界：

1. Code Index 负责代码结构事实。
2. Memory 负责决策、经验、偏好、失败模式。
3. Memory 只保存 `code_ref`，不复制源码。
4. 代码结构变化通过 Code Index 重新计算，不通过 Memory 更新。

## 6. 记忆类型和 Scope

### 6.1 Memory Type

| 类型 | 含义 | 默认策略 |
|---|---|---|
| `project_fact` | 项目事实、模块职责、部署方式 | 中长期 |
| `decision` | 架构决策和原因 | pending review，长期 |
| `constraint` | 项目约束、安全约束、技术边界 | pending review，长期 |
| `preference` | 用户工程偏好、沟通偏好 | 长期，可用户编辑 |
| `failure` | 失败经验、事故结论、踩坑记录 | 高价值长期 |
| `procedure` | 调试流程、评审流程、实施步骤 | 长期 |
| `session_summary` | 会话摘要 | 短中期 |
| `review_checkpoint` | 设计复查、架构评审、文档校验后的结构化检查点 | 中长期；冻结基线可 durable |
| `common_knowledge` | 常识、定理、行业基础知识 | 长期，但默认不自动泛化到全局 |
| `skill` | 技能、经验、工程方法 | 长期 |
| `theorem` | 定理、基础规则 | durable |
| `temporary_state` | 临时任务状态 | 5 天 |

### 6.2 Scope

一期按大粒度隔离：

| Scope | 含义 |
|---|---|
| `global_common` | 通用知识，不按用户或项目隔离 |
| `user_global` | 用户长期偏好和跨项目经验 |
| `project_local` | 项目级记忆 |
| `repo_local` | 仓库级记忆和 code ref |
| `session` | 会话级临时状态 |

默认规则：

1. 自动写入默认不提升到 `global_common`。
2. 项目记忆默认不跨项目共享。
3. 用户偏好可跨项目共享。
4. 通用知识只有在用户明确整理、项目上下文绑定、失败经验引用或多次影响决策时才长期保存。

Scope 字段一致性约束：

| scope | 必填字段 | 禁止或默认空字段 |
|---|---|---|
| `global_common` | 无强制项目字段 | `user_id`、`project_id`、`repo_id` 默认空 |
| `user_global` | `user_id` | `project_id`、`repo_id` 默认空 |
| `project_local` | `workspace_id`、`project_id` | `repo_id` 可空 |
| `repo_local` | `workspace_id`、`repo_id` | `project_id` 可通过 `repo.project_id` 推导 |
| `session` | `workspace_id`、`session_id` | `task_id` 可选；不进入长期稳定记忆 |

实现上应在写入 `memory_item` 时执行 scope validator，避免 `user_global` 记忆误带 `repo_id`，或 `project_local` 记忆缺少 `project_id` 导致跨项目污染。

## 7. 核心数据模型

### 7.1 agent_session

```sql
agent_session (
  id                  text primary key,
  agent_type          text not null,
  workspace_id        text not null,
  project_id          text,
  repo_id             text,
  capture_level       integer default 1,
  capture_capabilities_json text,
  capture_quality_json text,
  started_at          datetime not null,
  ended_at            datetime,
  goal_summary        text,
  status              text not null,
  created_at          datetime not null
)
```

字段说明：

| 字段 | 说明 |
|---|---|
| `agent_type` | `codex`、`claude_code`、`cursor` |
| `capture_level` | 该 session 实际捕获等级，范围 1-4 |
| `capture_capabilities_json` | Adapter 上报的捕获能力 |
| `capture_quality_json` | 实际捕获质量，用于 MVP 统计 |
| `goal_summary` | 当前 session 的目标摘要 |
| `status` | `active`、`completed`、`failed`、`interrupted` |

### 7.1.1 agent_task

`agent_task` 用于在一个 session 内划分任务边界，支撑任务级检索归因、成功率统计和强化计算。

```sql
agent_task (
  id                  text primary key,
  session_id          text,
  workspace_id        text,
  project_id          text,
  repo_id             text,
  task_summary        text not null,
  status              text not null,
  started_at          datetime not null,
  ended_at            datetime,
  outcome_summary     text,
  created_at          datetime not null,
  updated_at          datetime not null
)
```

`task_summary` 和 `outcome_summary` 是摘要字段，不保存完整用户 prompt、完整 Agent 回复或完整工具输出。

推荐 `status`：

```text
active
succeeded
failed
interrupted
unknown
```

约束：

1. 一个 `agent_session` 可以包含多个 `agent_task`。
2. 如果 Adapter 无法识别明确任务边界，默认为 session 创建一个 `default_task`。
3. `task_success`、`task_failure` 和检索归因优先绑定 `task_id`，不能只按 `session_id` 粗粒度归因。

### 7.2 raw_event

```sql
raw_event (
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
)
```

`raw_event` 是事件层，不代表稳定长期记忆。

`session_id` 允许为空，用于支持手动 CLI 写入、历史导入、批处理巩固和 global/common 知识写入。Agent 自动捕获事件应尽量绑定 `session_id`。

`workspace_id` 和 `agent_type` 也允许为空，原因是 `raw_event` 还承载手动写入、导入和批处理巩固事件。约束是：当 `source_channel=agent_session` 时，`session_id`、`workspace_id`、`agent_type` 必须全部存在；非 Agent 来源可以使用 `source_channel` 表达来源。

推荐 `source_channel`：

```text
agent_session
manual_cli
import
batch_consolidation
mcp_tool
```

系统通用 `source_type` 规范值：

```text
user_declared
user_confirmed
manual_review
design_review_checkpoint
agent_summary
tool_output
file_edit_summary
session_summary
task_result
multi_session_consolidation
auto_log
import
```

其中 `tool_output` 指工具输出摘要或错误签名，不表示保存完整工具输出。Adapter 如果上报 `tool_summary`、`tool_result` 等别名，Ingestion 层应归一化为 `tool_output`。

推荐 `event_type`：

```text
conversation.message
agent.response.summary
tool.call
tool.result.summary
file.edit.summary
session.start
session.end
user.correction
user.declaration
agent.decision
task.result
```

### 7.3 evidence

```sql
evidence (
  id                       text primary key,
  raw_event_id              text,
  source_type               text not null,
  interpreted_statement     text not null,
  keywords_json             text,
  salient_spans_json        text,
  source_ref_json           text,
  confidence                real default 0.7,
  created_at                datetime not null
)
```

`evidence` 保存可解释证据，不保存完整原文。

示例：

```json
{
  "source_type": "tool_output",
  "keywords": ["token expiry", "auth middleware", "boundary condition"],
  "salient_spans": [
    "测试失败集中在 token 过期边界判断"
  ],
  "interpreted_statement": "认证问题根因是过期时间边界判断错误，后续类似问题应优先检查时间比较逻辑。",
  "source_ref": {
    "repo": "service-a",
    "commit": "abc123",
    "file_path": "internal/auth/middleware.go",
    "symbol": "ValidateToken",
    "content_hash": "sha256:..."
  }
}
```

### 7.4 memory_item

```sql
memory_item (
  id                       text primary key,

  scope                    text not null,
  workspace_id             text,
  user_id                  text,
  project_id               text,
  repo_id                  text,
  session_id               text,
  task_id                  text,

  memory_type              text not null,
  source_type              text,
  created_by               text,
  source_quality           real default 0.7,
  title                    text,
  content                  text not null,
  normalized_content       text,
  search_text              text,
  keywords_json            text,
  entities_json            text,
  retrieval_cues_json      text,
  tags_json                text,

  state                    text not null,

  confidence               real default 0.7,
  importance               real default 0.5,
  encoding_depth           integer default 2,

  decay_rate               real not null,
  reinforcement_count      real default 0,
  effective_reinforcement  real default 0,

  retention_score          real default 0,
  tier                     text not null,

  valid_from               datetime,
  valid_until              datetime,
  created_at               datetime not null,
  updated_at               datetime not null,
  last_accessed_at         datetime,
  last_reinforced_at       datetime,
  last_validated_at        datetime,

  pinned                   boolean default false,
  user_confirmed           boolean default false,
  version                  integer default 1,
  supersedes_id            text
)
```

推荐 `state`：

```text
provisional
pending_review
stable
archived
deleted
```

推荐 `tier`：

```text
temporary
short_term
reinforced_short
long_term
durable
archived
```

### 7.5 memory_evidence_link

```sql
memory_evidence_link (
  memory_id        text not null,
  evidence_id      text not null,
  relation_type    text not null,
  weight           real default 1.0,
  primary key (memory_id, evidence_id)
)
```

推荐 `relation_type`：

```text
derived_from
supports
contradicts
updates
```

### 7.6 memory_relation

```sql
memory_relation (
  id               text primary key,
  source_id        text not null,
  target_id        text not null,
  relation_type    text not null,
  weight           real default 1.0,
  created_at       datetime not null,
  updated_at       datetime not null
)
```

推荐 `relation_type`：

```text
supports
contradicts
caused_by
precedes
refines
generalizes
related_to
linked_to_long_term
supersedes
superseded_by
```

一期使用 SQLite edge table 表达关系图，不引入 Neo4j。

### 7.7 memory_access_log

```sql
memory_access_log (
  id                  text primary key,
  memory_id           text not null,
  session_id          text,
  task_id             text,
  retrieval_trace_id  text,
  event_type          text not null,
  event_weight        real not null,
  source_type         text,
  source_quality      real default 0.7,
  query               text,
  rank                integer,
  score               real,
  score_breakdown_json text,
  inclusion_reason_json text,
  used_in_context     boolean default false,
  feedback            text,
  created_at          datetime not null
)
```

推荐事件权重：

| event_type | event_weight |
|---|---:|
| `retrieved` | 0.2 |
| `injected` | 0.5 |
| `cited_by_agent` | 1.0 |
| `user_confirmed` | 2.0 |
| `user_declared` | 2.5 |
| `task_success` | 1.5 |
| `repeated_signal` | 1.0 |
| `linked_to_long` | 1.0 |
| `ignored` | -0.5 |
| `task_failure` | -1.5 |
| `user_rejected` | -3.0 |

注意：`retrieved` 只能弱强化。只有被注入、引用、确认或证明对任务成功有贡献，才应显著强化。

检索评估字段说明：

| 字段 | 说明 |
|---|---|
| `rank` | 该记忆在本次检索候选中的排序位置 |
| `score` | 最终 rerank 分数 |
| `score_breakdown_json` | BM25、vector、retention、scope、relation、penalty 等分数拆解 |
| `inclusion_reason_json` | 被召回、注入或过滤的原因，用于解释错误注入和漏召回 |

### 7.8 retrieval_trace

```sql
retrieval_trace (
  id                  text primary key,
  session_id          text,
  task_id             text,
  workspace_id        text,
  project_id          text,
  repo_id             text,
  query               text,
  task                text,
  retrieval_mode      text,
  used_fts            boolean default false,
  used_vector         boolean default false,
  used_relation       boolean default false,
  used_code_index     boolean default false,
  fallback_reason     text,
  candidate_count     integer default 0,
  injected_count      integer default 0,
  latency_ms          integer,
  created_at          datetime not null
)
```

`query` 和 `task` 字段保存归一化后的短文本，不保存完整 prompt、完整代码片段或完整工具输出。需要排障时依赖 `retrieval_trace.id`、`memory_access_log.score_breakdown_json`、`source_ref` 和 hash 串联证据。

用途：

1. 记录一次完整检索链路。
2. 支撑检索延迟、错误注入率、fallback 比例和召回质量评估。
3. 解释 FTS、vector、relation、code index 各自是否参与。
4. 与 `memory_access_log.retrieval_trace_id` 关联，分析每条被召回记忆的使用结果。

### 7.9 memory_embedding

```sql
memory_embedding (
  memory_id           text primary key,
  embedding_model     text not null,
  embedding_dim       integer not null,
  embedding           blob not null,
  created_at          datetime not null,
  updated_at          datetime not null
)
```

默认实现可替换为 sqlite-vec virtual table，但逻辑模型保持一致。

### 7.10 code_ref

```sql
code_ref (
  id                  text primary key,
  memory_id           text not null,
  repo_id             text not null,
  commit_hash         text,
  file_path           text,
  symbol              text,
  line_start          integer,
  line_end            integer,
  content_hash        text,
  ref_summary         text,
  created_at          datetime not null,
  updated_at          datetime not null
)
```

`code_ref` 只保存定位、摘要和 hash，不保存源码全文。

### 7.11 retention_policy

```sql
retention_policy (
  id                  text primary key,
  memory_type          text not null,
  default_tier         text not null,
  default_ttl_days     integer,
  base_decay_rate      real not null,
  min_confirm_required boolean default false,
  auto_delete          boolean default true,
  policy_version       integer default 1,
  created_at           datetime not null,
  updated_at           datetime not null
)
```

初始策略：

| 记忆类型 | 默认 tier | 默认保留 |
|---|---|---:|
| 临时外部输入 | `temporary` | 5 天 |
| 临时任务状态 | `temporary` | 5 天 |
| session summary | `short_term` | 30-90 天 |
| 设计复查 checkpoint | `long_term` | 180-365 天 |
| 强化次数 >= 3 | `reinforced_short` | 90 天 |
| 强化次数 >= 5 | `long_term` | 365 天 |
| 架构决策 | `durable` | 默认不自动删除 |
| 安全约束 | `durable` | 默认不自动删除 |
| 失败经验 | `long_term` / `durable` | 365 天或更长 |
| 用户显式声明 | `durable` | 默认不自动删除 |
| pinned | `durable` | 不自动删除 |

### 7.12 memory_review

```sql
memory_review (
  id                  text primary key,
  memory_id           text not null,
  review_type         text not null,
  status              text not null,
  reviewer            text,
  feedback            text,
  original_content    text,
  edited_content      text,
  created_at          datetime not null,
  reviewed_at         datetime
)
```

用途：

1. 记录 `pending_review` 记忆的审核过程。
2. 保存用户确认、拒绝、编辑和补充边界的原因。
3. 为架构决策、安全约束、用户能力短板和高影响失败经验保留审核链路。
4. 只保存 memory 内容修改前后，不保存完整代码、完整 output 或完整会话。

推荐 `review_type`：

```text
architecture_decision
security_constraint
capability_gap
high_impact_failure
manual_review
```

推荐 `status`：

```text
pending
approved
rejected
edited
archived
```

### 7.13 review_checkpoint

`review_checkpoint` 用于把反复设计复查任务的历史上下文压缩成结构化状态。它与 `memory_item(memory_type=review_checkpoint)` 一对一或多对一关联：`memory_item` 负责检索和上下文注入，`review_checkpoint` 负责保存复查目标、文档版本、结论和下次复查策略。

```sql
review_checkpoint (
  id                            text primary key,
  memory_id                     text not null,

  workspace_id                  text,
  project_id                    text,
  repo_id                       text,
  session_id                    text,
  task_id                       text,

  checkpoint_type               text not null,
  review_intent_json            text not null,
  target_docs_json              text not null,
  target_sections_json          text,
  target_hashes_json            text,

  conclusion                    text not null,
  confirmed_baseline_json       text,
  ignored_items_json            text,
  deferred_items_json           text,
  open_items_json               text,
  next_review_policy_json       text,

  created_at                    datetime not null,
  updated_at                    datetime not null
)
```

推荐 `checkpoint_type`：

```text
architecture_design_review
iteration_plan_review
implementation_design_review
requirements_review
```

推荐 `conclusion`：

```text
no_major_gap
has_major_gap
supplemented
deferred
baseline_frozen
```

`target_docs_json` 示例：

```json
[
  {
    "path": "The One 长期记忆系统总体架构设计.md",
    "doc_role": "architecture_baseline",
    "content_hash": "sha256:...",
    "last_modified": "2026-05-23T10:00:00+08:00"
  }
]
```

设计约束：

1. 不保存完整文档正文，只保存路径、章节、hash、摘要和复查结论。
2. `ignored_items_json` 只保存用户明确忽略或延期的问题，避免下次复查重复提出。
3. `next_review_policy_json` 应描述下次复查优先级，例如“只关注重大逻辑缺失，细节调整后续处理”。
4. 文档事实以当前文件或文档索引为准；checkpoint 只作为历史复查状态和上下文压缩依据。

### 7.14 async_job

```sql
async_job (
  id                  text primary key,
  job_type            text not null,
  target_type         text not null,
  target_id           text not null,
  status              text not null,
  priority            integer default 5,
  retry_count         integer default 0,
  max_retries         integer default 3,
  next_run_at         datetime not null,
  last_error          text,
  dedup_key           text,
  payload_json        text,
  created_at          datetime not null,
  updated_at          datetime not null
)
```

用途：

1. 支撑异步写入、摘要、抽取、巩固和 retention 计算。
2. 避免 Agent 主流程被 LLM、embedding、关系构建阻塞。
3. 支持轻量失败记录和有限重试，但一期不强制保证所有异步任务最终成功。

推荐 `job_type`：

```text
extract_evidence
generate_memory_candidate
compute_embedding
build_relation
consolidate_memory
compute_retention
cleanup_temporary
delete_consistency
```

推荐 `status`：

```text
pending
running
succeeded
failed
cancelled
```

异步任务约束：

1. 失败任务记录 `last_error`，超过 `max_retries` 后标记 `failed`。
2. `failed` 不阻塞 Agent 主流程。
3. `dedup_key` 用于减少重复事件排队，但一期不要求复杂分布式锁。
4. 任务恢复以本地 SQLite 状态为准，不设计强一致任务调度。
5. `priority` 数值越小优先级越高；同优先级按 `next_run_at` 和 `created_at` 调度。

### 7.15 workspace / project / repo

一期可以用最小元数据表管理大粒度隔离。

```sql
workspace (
  id                  text primary key,
  name                text not null,
  root_path           text,
  created_at          datetime not null,
  updated_at          datetime not null
)

project (
  id                  text primary key,
  workspace_id        text not null,
  name                text not null,
  created_at          datetime not null,
  updated_at          datetime not null
)

repo (
  id                  text primary key,
  project_id          text,
  workspace_id        text not null,
  root_path           text not null,
  current_commit      text,
  code_index_provider text,
  created_at          datetime not null,
  updated_at          datetime not null
)
```

这些表不承担复杂权限治理，只用于本地项目、仓库和记忆 scope 对齐。

### 7.16 local_identity

一期是本地个人工具，不设计复杂用户系统，但需要为 `user_id` 提供稳定默认值。

```sql
local_identity (
  id                  text primary key,
  display_name        text,
  created_at          datetime not null,
  updated_at          datetime not null
)
```

默认规则：

```text
user_id = local_default_user
```

未来演进到小团队版本时，再把 `local_identity` 扩展为用户和权限模型。

### 7.17 memory_tombstone

```sql
memory_tombstone (
  memory_id           text primary key,
  deleted_reason      text,
  deleted_by          text,
  content_hash        text,
  deleted_at          datetime not null
)
```

用途：

1. 记录最小删除标记。
2. 避免已删除记忆被误恢复。
3. 支持本地删除一致性检查。

### 7.18 schema_migration

```sql
schema_migration (
  version             integer primary key,
  name                text not null,
  applied_at          datetime not null,
  checksum            text
)
```

用途：

1. 记录本地 SQLite schema 迁移状态。
2. 支持单二进制启动时自动执行幂等迁移。
3. 避免后续数据模型演进依赖外部迁移工具。

### 7.19 状态机

允许的状态迁移：

```text
provisional -> pending_review
provisional -> stable
provisional -> archived

pending_review -> stable
pending_review -> archived
pending_review -> deleted

stable -> archived
stable -> deleted

archived -> stable
archived -> deleted

deleted -> terminal
```

约束：

1. `deleted` 是终态，不允许恢复为 `stable`。
2. 从 `archived` 恢复为 `stable` 必须重新计算 retention score。
3. `pending_review -> stable` 必须写入 `memory_review` 记录。

## 8. Retention 模型

### 8.1 基础激活

采用幂律激活，不使用简单指数 TTL 作为唯一依据。

```text
base_activation =
  log(1 + Σ max(0, event_weight_i) * (age_i + 1)^(-decay_rate_i))
```

设计原因：

1. 早期遗忘快、后期遗忘慢。
2. 长期低频但稳定复用的信息不应被快速删除。
3. 短期密集 burst 不等价于长期价值。
4. 检索、引用、确认、任务成功的强化价值不同。
5. 负反馈不进入基础激活项，统一通过 `negative_penalty`、`conflict_penalty` 和状态流转处理，避免 log 输入异常。

工程实现建议：

```text
age_i = max(0, days_between(now, event_time_i))
decay_rate_i = memory.decay_rate * event_decay_modifier(event_type)
base_activation_norm = 1 - exp(-base_activation)
```

事件衰减修正：

| event_type | event_decay_modifier | 说明 |
|---|---:|---|
| `user_declared` | 0.5 | 用户声明更稳定 |
| `user_confirmed` | 0.6 | 用户确认更稳定 |
| `task_success` | 0.8 | 任务成功有较强长期价值 |
| `cited_by_agent` | 0.9 | 被引用强于被检索 |
| `injected` | 1.0 | 正常衰减 |
| `retrieved` | 1.2 | 单纯检索衰减更快 |
| `ignored` | 1.4 | 被忽略说明价值偏低 |
| `task_failure` | 1.5 | 失败关联要谨慎 |
| `user_rejected` | 2.0 | 用户拒绝应快速降权 |

有效强化次数：

```text
effective_reinforcement =
  Σ max(0, event_weight_i) * spacing_factor_i * source_quality_factor_i
```

其中 `spacing_factor` 用于区分长期稳定重复和短期密集 burst：

```text
if repeated events span <= 2 days:
    spacing_factor = 0.4
elif repeated events span <= 14 days:
    spacing_factor = 0.7
elif repeated events span <= 90 days:
    spacing_factor = 1.0
else:
    spacing_factor = 1.2
```

这保证“短时间高频出现”不会被误判为长期价值。

### 8.2 Retention Score

```text
retention_score =
  salience
  * encoding_depth_factor
  * consolidation_factor
  * confidence_factor
  * relation_factor
  * lifecycle_factor
  + base_activation_norm
  + explicit_boost
  - negative_penalty
  - staleness_penalty
  - conflict_penalty
```

字段含义：

| 因子 | 含义 |
|---|---|
| `salience` | 记忆天然重要性 |
| `encoding_depth_factor` | 加工深度，摘要、实体、关系、策略抽象越深越高 |
| `consolidation_factor` | 巩固状态，stable 高于 provisional |
| `confidence_factor` | 可信度 |
| `relation_factor` | 是否被长期记忆、决策链、失败经验连接 |
| `lifecycle_factor` | 当前 tier 和状态 |
| `base_activation_norm` | 历史使用和时间间隔共同决定的归一化激活 |
| `explicit_boost` | 用户声明、pin、确认 |
| `negative_penalty` | 用户拒绝、任务失败、被忽略 |
| `staleness_penalty` | 过期或长期未验证 |
| `conflict_penalty` | 与稳定记忆冲突 |

`explicit_boost` 建议：

| 条件 | boost |
|---|---:|
| `pinned=true` | 0.30 |
| `source_type=user_declared` | 0.25 |
| `user_confirmed=true` | 0.20 |
| `tier=durable` 且人工确认 | 0.30 |

多个条件命中时：

```text
explicit_boost = min(0.4, sum(boost_i))
```

#### 8.2.1 Salience

```text
salience =
  0.35 * type_weight
  + 0.25 * importance
  + 0.20 * source_weight
  + 0.20 * scope_weight
```

`type_weight` 建议：

| memory_type | type_weight |
|---|---:|
| `decision` | 0.95 |
| `constraint` | 0.95 |
| `failure` | 0.90 |
| `procedure` | 0.85 |
| `preference` | 0.85 |
| `project_fact` | 0.75 |
| `skill` | 0.70 |
| `common_knowledge` | 0.65 |
| `session_summary` | 0.50 |
| `temporary_state` | 0.30 |

`source_weight` 建议：

| source | source_weight |
|---|---:|
| 用户显式声明 | 1.00 |
| 用户确认 | 1.00 |
| 多次任务复现 | 0.85 |
| Agent 高置信总结 | 0.70 |
| 工具输出摘要 | 0.55 |
| 单次对话推断 | 0.45 |
| 自动日志 | 0.35 |

`source_quality_factor` 建议：

| source_type | factor |
|---|---:|
| `user_declared` | 1.0 |
| `user_confirmed` | 1.0 |
| `task_result` | 0.9 |
| `multi_session_consolidation` | 0.85 |
| `agent_summary` | 0.7 |
| `manual_review` | 0.8 |
| `session_summary` | 0.65 |
| `tool_output` | 0.6 |
| `file_edit_summary` | 0.6 |
| `auto_log` | 0.4 |
| `import` | 0.5 |

该值可写入 `memory_item.source_quality` 和 `memory_access_log.source_quality`，用于 retention 和 reinforcement 计算。

`scope_weight` 建议：

| scope | scope_weight |
|---|---:|
| `user_global` | 0.85 |
| `project_local` | 0.90 |
| `repo_local` | 0.80 |
| `global_common` | 0.70 |
| `session` | 0.35 |

#### 8.2.2 Factor 映射

```text
encoding_depth_factor = 0.6 + 0.1 * encoding_depth
```

`encoding_depth` 范围为 0-4：

| depth | 含义 |
|---:|---|
| 0 | 原始事件指针 |
| 1 | 表层摘要 |
| 2 | 语义摘要 |
| 3 | 实体、关系、来源绑定 |
| 4 | 策略、过程、失败模式抽象 |

`consolidation_factor`：

| state | factor |
|---|---:|
| `provisional` | 0.70 |
| `pending_review` | 0.80 |
| `stable` | 1.00 |
| `archived` | 0.45 |
| `deleted` | 0 |

`confidence_factor`：

```text
confidence_factor = clamp(confidence, 0.2, 1.0)
```

`relation_factor`：

```text
relation_factor =
  1.0
  + min(0.3, 0.05 * supporting_relation_count)
  + min(0.2, 0.08 * linked_long_term_count)
  - min(0.4, 0.15 * contradicting_relation_count)
```

`lifecycle_factor`：

| tier | factor |
|---|---:|
| `temporary` | 0.40 |
| `short_term` | 0.65 |
| `reinforced_short` | 0.80 |
| `long_term` | 1.00 |
| `durable` | 1.20 |
| `archived` | 0.50 |

#### 8.2.3 Penalty

```text
negative_penalty =
  abs(sum(negative_event_weight_i)) * 0.15
```

```text
staleness_penalty =
  if valid_until < now: 0.4
  else if last_validated_at is null and age_days > ttl_days * 0.8: 0.2
  else 0
```

```text
conflict_penalty =
  min(0.6, 0.2 * unresolved_conflict_count)
```

归一化：

```text
retention_score = clamp(retention_score, 0, 1)
```

说明：`base_activation` 原始值是对历史事件的幂律累积，可能随事件数量增长超过 1。进入最终 `retention_score` 前必须转换为 `base_activation_norm`，避免大量高频记忆被简单 clamp 到 1，导致排序失去区分度。

### 8.3 Promotion 规则

```text
if effective_reinforcement >= 3:
    tier = reinforced_short
    default_retention = 90d

if effective_reinforcement >= 5:
    tier = long_term
    default_retention = 365d

if linked_to_long_term_memory:
    promote one tier, max long_term

if user_declared or pinned:
    tier = durable

if periodic_repetition_detected:
    decrease decay_rate

if dense_burst_only and no later reuse:
    do not decrease decay_rate
```

### 8.4 Decay Rate

默认 `decay_rate`：

| 类型 | decay_rate |
|---|---:|
| 临时任务状态 | 1.2-1.6 |
| 工具输出摘要 | 1.0-1.4 |
| session summary | 0.8-1.1 |
| 项目事实 | 0.5-0.8 |
| 架构决策 | 0.2-0.5 |
| 用户偏好 | 0.2-0.5 |
| 失败经验 | 0.2-0.6 |
| 通用知识、定理、行业技能 | 0.1-0.3 |
| pinned / 用户声明 | 0 |

周期重复降低衰减率：

```text
if repetition_span_days > 30 and occurrence_count >= 3:
    decay_rate *= 0.7

if repetition_span_days > 90 and occurrence_count >= 5:
    decay_rate *= 0.5
```

短期密集出现不降低衰减率：

```text
if occurrence_count >= 5 and repetition_span_days <= 2:
    treat_as_short_term_hotspot
```

### 8.5 自动归档和删除

一期采用保守策略：

| 条件 | 动作 |
|---|---|
| `temporary` 到期 | soft delete 或汇总后删除 |
| `short_term` 到期且 score < 0.35 | archive |
| `reinforced_short` 到期且 score < 0.45 | archive |
| `long_term` 到期且 score < 0.50 | archive，不直接 hard delete |
| `durable` | 不自动删除 |
| `user_rejected` 且无其他支持证据 | archive 或 deleted |
| 与稳定记忆冲突 | 降权并进入 conflict review |

硬删除只用于：

1. 用户明确删除。
2. 敏感信息误入库。
3. 数据损坏或重复垃圾数据。

### 8.6 Retention Job

调度建议：

| 任务 | 频率 |
|---|---|
| access log 聚合 | 每 10 分钟 |
| retention score 重算 | 每日或 session end |
| temporary 清理 | 每日 |
| archive 扫描 | 每周 |
| relation consistency check | 每周 |

伪代码：

```text
for memory in active_memories:
    events = load_access_events(memory.id)
    base_activation = compute_base_activation(memory, events)
    score = compute_retention_score(memory, events, relations)
    tier = compute_tier(memory, score, effective_reinforcement)
    action = decide_lifecycle_action(memory, score, tier)
    persist_score(memory.id, score, tier, action)
```

## 9. Admission Control

写入长期记忆前需要进行准入控制。

一期准入评分：

```text
raw_admission_score =
  0.22 * future_need
  + 0.18 * encoding_depth_score
  + 0.16 * stability
  + 0.14 * task_control_signal
  + 0.12 * episodic_semantic_value
  + 0.08 * retrieval_trainability
  - 0.16 * interference_risk
  - 0.12 * decay_risk
  - 0.10 * conflict_risk

admission_score = clamp(raw_admission_score, 0, 1)
```

说明：负向风险项可能让原始分数小于 0，工程实现必须先 clamp 再进入决策矩阵，避免不同实现对负分处理不一致。

准入结果：

| 分数 | 处理 |
|---:|---|
| `< 0.30` | 不写长期记忆，只保留短期事件或丢弃 |
| `0.30 - 0.50` | 会话级或任务级短期摘要 |
| `0.50 - 0.70` | 写入 provisional memory |
| `0.70 - 0.85` | 写入长期记忆，需要来源绑定 |
| `> 0.85` | 高价值长期记忆，进入巩固队列 |

高影响记忆即使分数高，也进入 `pending_review`：

1. 架构决策。
2. 安全约束。
3. 用户能力短板。
4. 高影响失败经验。

### 9.1 Admission 输入特征

| 特征 | 来源 | 说明 |
|---|---|---|
| `future_need` | 任务类型、重复出现、用户目标 | 未来复用概率 |
| `encoding_depth_score` | Memory Processor | 归一化加工深度 |
| `stability` | 来源数量、时间跨度、确认情况 | 是否稳定 |
| `task_control_signal` | 当前任务状态 | 是否影响当前或后续任务 |
| `episodic_semantic_value` | Evidence 和 Memory Type | 是否能形成事件证据或语义知识 |
| `retrieval_trainability` | 检索线索数量 | 是否值得后续检索练习 |
| `interference_risk` | 写入频率、冲突数量 | 是否污染未巩固记忆 |
| `decay_risk` | 类型、时效性 | 是否很快过期 |
| `conflict_risk` | 与稳定记忆冲突 | 是否存在事实冲突 |

### 9.2 特征评分规则

`future_need`：

```text
future_need =
  0.4 * repeated_topic_score
  + 0.3 * task_relevance
  + 0.2 * project_scope_relevance
  + 0.1 * user_preference_match
```

`encoding_depth`：

```text
encoding_depth_score = encoding_depth / 4
```

修正依据：

1. `论文分析/Craik & Lockhart - Levels of Processing 精读总结.md` 将 Agent 记忆加工深度分为 0-4 级：原始记录、表层结构、语义摘要、关系建模、策略抽象。
2. `论文分析/记忆系统工程实现指导.md` 中 Admission Control 把 `encoding_depth` 作为重要正向因子，但该启发式公式默认各维度应处于可比较尺度。
3. 因此工程公式不能直接使用 0-4 的 `encoding_depth`，必须归一化为 0-1 的 `encoding_depth_score`，否则该项会在总分中被异常放大。

`stability`：

```text
stability =
  0.35 * source_count_score
  + 0.25 * time_span_score
  + 0.25 * confirmation_score
  + 0.15 * confidence
```

`task_control_signal`：

```text
task_control_signal =
  1.0 if user explicitly says "以后/记住/不要/必须"
  0.8 if affects architecture/security/implementation constraints
  0.6 if affects current task continuation
  0.3 if background only
```

`episodic_semantic_value`：

```text
episodic_semantic_value =
  max(episode_value, semantic_value, procedure_value)
```

`retrieval_trainability`：

```text
retrieval_trainability =
  min(1.0, 0.2 * retrieval_cue_count + 0.2 * relation_count)
```

`interference_risk`：

```text
interference_risk =
  0.4 * recent_write_pressure
  + 0.3 * similarity_to_unconsolidated
  + 0.3 * ambiguity_score
```

`decay_risk`：

```text
decay_risk =
  1.0 for transient external input
  0.8 for command output summary
  0.6 for session state
  0.4 for project fact
  0.2 for preference/decision/failure
```

`conflict_risk`：

```text
conflict_risk =
  min(1.0, 0.4 * unresolved_conflict_count + 0.2 * low_confidence_source_count)
```

### 9.3 Admission 决策矩阵

| 类型 | 默认动作 | 特殊规则 |
|---|---|---|
| 用户显式声明 | stable 或 pending_review | 安全约束、能力短板进入 review |
| 多次重复主题 | provisional | 达到强化阈值后晋级 |
| 用户纠正 | stable | 同时降权旧错误记忆 |
| 架构决策 | pending_review | 用户确认后 stable |
| 失败经验 | provisional 或 pending_review | 高影响失败进入 review |
| 工具输出摘要 | temporary 或 short_term | 不保存全文 |
| 临时外部输入 | temporary | 默认 5 天 |
| 通用知识 | long_term candidate | 需绑定项目、失败经验或用户整理 |
| 代码结构事实 | 不进入 Memory | 交给 Code Index |

### 9.4 Admission 输出

Admission Controller 输出：

```json
{
  "decision": "write_provisional",
  "admission_score": 0.73,
  "memory_type": "failure",
  "scope": "project_local",
  "initial_tier": "short_term",
  "decay_rate": 0.45,
  "requires_review": false,
  "reason_codes": [
    "task_success_related",
    "has_error_signature",
    "linked_to_project"
  ]
}
```

推荐 `decision`：

```text
drop
write_raw_only
write_temporary
write_provisional
write_pending_review
write_stable
merge_existing
update_existing
```

## 10. 检索和上下文注入

### 10.1 检索流程

```text
task
  -> infer retrieval intent
  -> select scope
  -> FTS retrieve
  -> vector retrieve
  -> metadata filter
  -> relation expansion
  -> code index query if needed
  -> rule rerank
  -> build context pack
  -> record access log
```

### 10.2 上下文构建规则

Context Builder 应优先注入：

1. 用户显式偏好。
2. 当前项目约束。
3. 相关架构决策。
4. 最近任务状态。
5. 历史失败经验。
6. 与任务相关的 code ref。
7. 与当前复查目标匹配的 `review_checkpoint`。

默认不注入：

1. 低置信记忆。
2. 已归档记忆。
3. 与当前项目 scope 不匹配的记忆。
4. 与稳定记忆冲突且未解决的记忆。
5. 只被检索但从未使用成功的热门噪声。

当前有效性解析规则：

1. `state=deleted` 永不召回。
2. `state=archived` 默认不召回，除非用户查询历史原因或 `include_archived=true`。
3. 存在 `supersedes/superseded_by` 关系时，默认只注入最新有效记忆；旧记忆只能作为历史 evidence 摘要出现。
4. `valid_until < now` 的记忆默认不进入当前结论，只能作为历史上下文。
5. `pending_review` 可以召回但必须标记未确认，不能作为强约束注入。

Context budget 默认分配：

| 内容 | 默认预算 |
|---|---:|
| 任务相关稳定约束和决策 | 35% |
| 用户偏好和工作方式 | 15% |
| 失败经验和反模式 | 20% |
| 当前 session / recent state | 15% |
| code_ref 和 evidence 摘要 | 15% |

实际构建时按任务类型动态调整，但必须保证高优先级约束、未过期决策和强相关失败经验优先于一般偏好。

设计复查任务的上下文预算应动态调整：

| 内容 | 默认预算 |
|---|---:|
| 冻结设计基线和相关架构决策 | 25% |
| 最近 `review_checkpoint` | 30% |
| 用户确认忽略或延期的问题 | 15% |
| 当前文档章节摘要或 diff | 20% |
| evidence 和 code_ref 摘要 | 10% |

当任务意图为设计复查、架构评审或文档完整性检查时，Context Builder 应优先召回最近一次相关 `review_checkpoint`，再读取当前文档事实源。不得仅依赖 checkpoint 判断文档是否仍然正确。

### 10.3 检索后反馈

检索后必须记录：

1. 是否被召回。
2. 是否被注入上下文。
3. 是否被 Agent 引用。
4. 是否被用户确认。
5. 是否导致任务成功或失败。
6. 是否被用户拒绝或纠正。

这些信号进入 `memory_access_log`，用于后续 Retention 和强化。

任务结果归因规则：

1. `task_success` 只能归因给 `used_in_context=true` 或 `cited_by_agent` 的记忆。
2. 单纯 `retrieved` 但未注入的记忆，不因任务成功获得额外强化。
3. 如果任务失败且某条记忆被 Agent 明确引用为依据，则记录 `task_failure` 或进入 conflict review。
4. 如果用户明确纠正某条注入记忆，则记录 `user_rejected`，并创建新 evidence 或 supersedes 关系。
5. session end 时可以基于 `task_id` 和 `retrieval_trace_id` 聚合同一任务的注入记忆和结果事件，但一期不做复杂因果推断。

### 10.4 在线检索性能边界

P95 <= 100ms 只适用于在线轻量检索路径：

1. FTS5/BM25。
2. metadata filter。
3. 已存在向量的 sqlite-vec 检索。
4. 进程内 query embedding LRU cache 命中。
5. 轻量 relation expansion。
6. rule-based rerank。

不纳入 100ms 在线路径：

1. 外部 embedding API 调用。
2. LLM rerank。
3. 深度冲突分析。
4. 大规模图遍历。
5. 大段上下文摘要生成。

一期不设计持久化 `query_embedding_cache` 表，原因是 query 本身通常短生命周期、复用率低，持久化会引入额外清理和隐私边界复杂度。默认只使用进程内 LRU cache，key 使用 `hash(scope + normalized_query + embedding_model)`。

如果进程内 query embedding LRU 未命中：

```text
if local_embedding_available and estimated_embedding_latency <= online_budget:
    compute query embedding locally
    put query embedding into process LRU cache
else:
    fallback to FTS + metadata + relation retrieval
    skip online vector retrieval
```

设计约束：

1. 外部模型如 OpenAI、DeepSeek 不进入在线关键路径。
2. 外部 embedding 只用于异步写入、离线重建或用户显式低延迟要求较低的检索。
3. `memory.context` 必须返回检索诊断，包含是否使用 vector、是否降级、latency。

## 11. 本地存储设计

默认使用 SQLite：

1. `agent_session`
2. `agent_task`
3. `raw_event`
4. `evidence`
5. `memory_item`
6. `memory_evidence_link`
7. `memory_relation`
8. `memory_access_log`
9. `retrieval_trace`
10. `memory_embedding`
11. `code_ref`
12. `retention_policy`
13. `memory_review`
14. `review_checkpoint`
15. `async_job`
16. `workspace`
17. `project`
18. `repo`
19. `local_identity`
20. `memory_tombstone`
21. `schema_migration`

索引：

1. FTS5：`memory_item.search_text`，由 `title`、`content`、`normalized_content`、`keywords_json`、`entities_json`、`retrieval_cues_json`、`tags_json` 生成，不直接把多个 JSON 字段暴露为 FTS 文档列。
2. sqlite-vec：`memory_embedding`，可选增强。
3. Memory B-tree：`scope`、`workspace_id`、`project_id`、`repo_id`、`session_id`、`task_id`、`memory_type`、`state`、`tier`、`updated_at`。
4. Raw event B-tree：`session_id`、`task_id`、`workspace_id`、`project_id`、`repo_id`、`event_type`、`occurred_at`。
5. Relation index：`source_id`、`target_id`、`relation_type`。
6. Retrieval log index：`retrieval_trace_id`、`task_id`、`memory_id`、`created_at`。
7. Task index：`session_id`、`workspace_id`、`project_id`、`repo_id`、`status`、`started_at`。
8. Review checkpoint index：`memory_id`、`workspace_id`、`project_id`、`repo_id`、`checkpoint_type`、`conclusion`、`updated_at`。

表可变性约束：

| 表 | 可变性 | 说明 |
|---|---|---|
| `raw_event` | append-only | 事件层只追加，不原地改写；错误事件通过新事件纠正 |
| `evidence` | append-mostly | 默认追加，敏感误入库或孤立 evidence 可删除 |
| `memory_item` | mutable with version | 内容编辑、状态流转和 retention 更新允许改写；重要编辑增加 `version` 和 `supersedes_id` |
| `memory_access_log` | append-only | 作为反馈事实记录，不参与业务状态改写 |
| `retrieval_trace` | append-only | 作为检索诊断记录，不原地改写 |
| `review_checkpoint` | append-mostly | 新复查优先生成新 checkpoint；仅允许修正 hash、结论摘要和下次复查策略 |
| `memory_relation` | mutable | consolidation、supersedes、冲突处理可增删边 |
| `memory_embedding` | replaceable | embedding 模型变化或内容更新时替换 |
| `async_job` | mutable | 任务状态、重试次数和错误信息随调度更新 |
| `agent_task` | mutable | 任务状态和 outcome 可在 session 结束时更新 |
| `memory_tombstone` | append-only | 删除标记只追加或保持最小更新 |
| `schema_migration` | append-only | 记录已执行迁移版本，不回写历史版本 |

可选高级后端：

```text
PostgreSQL + pgvector
```

一期暂不引入：

1. Redis。
2. Elasticsearch。
3. Neo4j。

### 11.1 sqlite-vec 落地约束和降级

Go 单二进制默认优先保证 SQLite + FTS5 可用，sqlite-vec 作为可选增强能力。

需要在实现阶段确认：

1. 是否使用 CGO。
2. sqlite-vec 是否静态链接。
3. 是否允许 SQLite extension loading。
4. 不同操作系统下扩展加载路径。

降级策略：

```text
if sqlite_vec_available:
    enable vector retrieval
else:
    use FTS5 + metadata + relation retrieval
    mark vector capability disabled
```

系统启动时应记录 storage capability：

```json
{
  "sqlite": true,
  "fts5": true,
  "sqlite_vec": false,
  "fallback_retrieval": ["fts", "metadata", "relation"]
}
```

### 11.2 SQLite 并发和事务边界

一期是本地单机工具，但 Codex、Claude Code、Cursor 可能同时访问同一个 Memory Daemon。SQLite 使用方式必须明确：

1. 默认启用 WAL 模式，保证读多写少场景下读写并发。
2. 所有写入通过 daemon 内部单写者队列或短事务执行，避免多个 goroutine 长时间持有写锁。
3. `memory.search`、`memory.context` 只走只读事务；`memory.observe` 只做轻量事件写入和任务入队。
4. `busy_timeout` 设置为有限值，超时后写入类请求返回 retryable error，读路径返回降级 diagnostics。
5. 批量 consolidation、embedding 回填和 retention job 必须分批提交，避免长事务阻塞在线检索。
6. FTS、sqlite-vec、relation edge 的更新和 `memory_item` 状态变更必须在同一短事务内完成；如果异步索引更新失败，记录 `async_job.failed`，不阻塞 Agent 主流程。

推荐 SQLite 参数：

```text
journal_mode = WAL
synchronous = NORMAL
busy_timeout = 1000ms
foreign_keys = ON
```

事务边界建议：

| 操作 | 事务边界 |
|---|---|
| `memory.observe` | 写 `raw_event` + 写 `async_job` 同一事务 |
| `memory.remember` | 写 `memory_item` + evidence + FTS + optional embedding job 同一事务 |
| `memory.review approve/reject` | 写 review + 状态流转 + access log 同一事务 |
| retention job | 分页批处理，每批独立事务 |
| delete request | 状态标记 + tombstone + 索引删除同一事务 |

### 11.3 异步队列和背压

所有重处理任务通过 `async_job` 执行。

写入路径：

```text
memory.observe
  -> write raw_event
  -> enqueue extract_evidence
  -> return accepted
```

Worker 流程：

```text
poll pending jobs
  -> mark running
  -> execute
  -> write result
  -> enqueue next job
  -> mark succeeded
```

失败处理：

```text
if job failed and retry_count < max_retries:
    retry_count += 1
    next_run_at = now + backoff(retry_count)
else:
    mark failed
    persist last_error
```

背压策略：

| 条件 | 动作 |
|---|---|
| pending job 数过高 | 降低低优先级 job 调度 |
| embedding 队列积压 | 优先 FTS-only 检索 |
| LLM extraction 积压 | 延迟深度摘要，只保留 raw_event/evidence |
| session 内事件过多 | 合并为 session checkpoint |
| 重复工具输出 | 基于 content_hash 去重 |

推荐优先级：

| job_type | priority |
|---|---:|
| `extract_evidence` | 3 |
| `generate_memory_candidate` | 4 |
| `compute_embedding` | 5 |
| `consolidate_memory` | 6 |
| `build_relation` | 7 |
| `compute_retention` | 8 |
| `cleanup_temporary` | 9 |

### 11.4 删除一致性流程

即使一期没有企业级删除合规，也必须避免主表删除后索引仍可召回。

删除流程：

```text
delete request
  -> mark memory_item.state = deleted
  -> write tombstone marker
  -> remove memory_embedding / vec entry
  -> remove FTS entry
  -> remove memory_relation edges
  -> remove code_ref
  -> archive or remove memory_evidence_link
  -> enqueue delete_consistency check
```

删除策略：

| 数据 | 默认动作 |
|---|---|
| `memory_item` | soft delete，保留最小 tombstone |
| `memory_embedding` | 删除 |
| FTS entry | 删除 |
| `memory_relation` | 删除相关边 |
| `code_ref` | 删除 |
| `memory_access_log` | 默认保留统计信息，敏感删除时清理 |
| `evidence` | 若只被该 memory 引用，可删除或归档 |

敏感信息误入库时：

```text
hard delete memory content
hard delete evidence content
hard delete embedding
hard delete FTS entry
retain minimal deletion marker
```

删除一致性检查：

```text
for deleted memory:
    assert no FTS hit
    assert no vector hit
    assert no relation edge
    assert no code_ref
```

## 12. 建议代码模块边界

```text
cmd/memoryd
internal/mcp
internal/capture
internal/ingest
internal/memory
internal/retrieval
internal/retention
internal/codeindex
internal/storage/sqlite
internal/embedding
internal/config
```

模块职责：

| 模块 | 职责 |
|---|---|
| `cmd/memoryd` | 本地 daemon 启动入口 |
| `internal/mcp` | MCP server 和工具注册 |
| `internal/capture` | Agent adapter 和事件捕获 |
| `internal/ingest` | 事件归一化、内容最小化、证据抽取 |
| `internal/memory` | Memory CRUD、状态流转、准入 |
| `internal/retrieval` | 检索编排和上下文构建 |
| `internal/retention` | 保留分数、衰减、晋级、归档 |
| `internal/codeindex` | Code Index Adapter |
| `internal/storage/sqlite` | SQLite 存储实现 |
| `internal/embedding` | embedding provider 抽象 |
| `internal/config` | 配置加载和默认值 |

### 12.1 最小配置项

一期配置应保持少量、稳定、可本地覆盖。默认配置能直接启动，避免用户先理解完整系统才能使用。

```text
storage.path
storage.backend = sqlite
storage.sqlite_vec_enabled = auto
server.mcp_addr
capture.enabled_agents
capture.max_event_bytes
capture.max_salient_spans
memory.default_user_id
memory.default_workspace
retrieval.default_limit
retrieval.default_token_budget
retrieval.online_timeout_ms
embedding.provider = none | local | openai | deepseek
embedding.model
retention.job_enabled
retention.temporary_ttl_days
retention.short_term_ttl_days
```

配置约束：

1. 没有 embedding provider 时系统仍可运行，检索降级为 FTS + metadata + relation。
2. `capture.max_event_bytes` 是硬边界，超过后必须由 Adapter 摘要或拒绝写入。
3. `retrieval.online_timeout_ms` 默认不超过 100ms，外部模型调用不能进入该时间预算。
4. `retention.*_ttl_days` 是策略默认值，具体记忆仍由 `retention_policy` 和 `retention_score` 决定。
5. 敏感配置如外部模型 API key 不写入数据库，由环境变量或本地配置文件提供。

## 13. MVP 验收指标

| 指标 | 目标 |
|---|---:|
| Token savings | >= 30% |
| 重复上下文说明次数 | 降低 >= 50% |
| 历史决策召回准确率 | >= 80% |
| 错误记忆注入率 | <= 5% |
| 检索延迟 | P95 <= 100ms |
| 写入阻塞 | 不阻塞 Agent 主流程 |
| 设计复查历史上下文 Token savings | >= 60% |

建议验收任务：

1. 跨 session 继续修改同一项目。
2. 记住用户架构偏好并应用到方案设计。
3. 召回历史架构决策。
4. 避免重复踩坑。
5. 识别过期项目事实。
6. 多 Agent 共享同一项目上下文。
7. 不把临时工具输出污染长期记忆。
8. 不把源码结构事实混入普通 memory。
9. 用户纠正后后续行为改变。
10. 重复设计复查通过 `review_checkpoint` 降低历史上下文加载。

### 13.1 验收方法

MVP 验收采用对照实验：

```text
Baseline A: No Memory
Baseline B: Full Chat History / 手工粘贴上下文
Baseline C: Summary Only
Candidate: Hybrid Memory + Retention + Code Index Adapter
```

每个任务至少跑两轮：

1. 第一轮制造上下文、决策、失败经验和用户偏好。
2. 第二轮跨 session 或跨 Agent 继续任务，观察系统是否正确召回和使用记忆。

统一记录：

| 指标 | 说明 |
|---|---|
| `task_id` | 本轮验收任务 ID，用于关联 retrieval、access log 和任务结果 |
| `input_token_count` | Agent 输入 Token 数 |
| `memory_context_token_count` | 注入记忆 Token 数 |
| `manual_context_token_count` | 用户手工补充 Token 数 |
| `baseline_context_tokens` | Baseline 中为完成任务提供的上下文 Token 数 |
| `candidate_context_tokens` | Candidate 中用户输入、系统提示和 memory context 的总上下文 Token 数 |
| `task_success` | 任务是否完成 |
| `user_correction_count` | 用户纠正次数 |
| `repeated_explanation_count` | 用户重复说明次数 |
| `retrieved_memory_count` | 召回记忆数量 |
| `used_memory_count` | 实际注入或引用数量 |
| `wrong_memory_injected_count` | 错误注入数量 |
| `latency_p95_ms` | 检索 P95 延迟 |

Token savings 计算：

```text
token_savings =
  (baseline_context_tokens - candidate_context_tokens)
  / baseline_context_tokens
```

任务成功率：

```text
task_success_rate =
  successful_tasks / total_tasks
```

错误记忆注入率：

```text
wrong_memory_injection_rate =
  wrong_memory_injected_count / injected_memory_count
```

当 `injected_memory_count = 0` 时，该轮不计入错误注入率分母，但仍计入召回失败或任务失败分析。

Level4 capability coverage：

```text
level4_capability_coverage =
  supported_required_capability_count / total_required_capability_count
```

其中 `total_required_capability_count` 包含：

1. conversation capture。
2. tool call capture。
3. tool output capture。
4. file edit capture。
5. session lifecycle。
6. memory observe。

该指标衡量某个 Agent Adapter 是否真正具备 Level4 捕获能力。

Event capture completeness：

```text
event_capture_completeness =
  captured_required_event_count / expected_required_event_count
```

其中 `expected_required_event_count` 按 session 内可观测事件计算，不按所有理论事件计算。一期统计以下事件：

1. 用户消息。
2. Agent 回复摘要。
3. 工具调用。
4. 工具输出摘要。
5. 文件编辑摘要。
6. session start/end。

如果某 Agent 的原生能力无法暴露某类事件，必须在 `capture_capabilities_json` 中标记不可用，不计入该 session 的 `event_capture_completeness` 分母；但会降低 `level4_capability_coverage`。产品目标要求 Codex、Claude Code、Cursor 最终都达到 Level4 capability coverage。

检索延迟口径：

```text
retrieval_latency_ms =
  time(memory.search or memory.context returns)
  - time(request accepted)
```

该指标不包含：

1. 异步 evidence extraction。
2. 异步 embedding 计算。
3. 异步 consolidation。
4. 异步 retention 计算。

### 13.2 验收任务 1：跨 Session 继续同一项目任务

目标：验证长期记忆能恢复项目上下文和任务状态。

第一轮输入：

```text
用户要求实现 auth token 过期边界修复，并说明项目采用 Go、PostgreSQL，认证模块要求请求内同步完成校验。
Agent 运行测试后发现 TestTokenExpiry 在精确过期时间失败。
```

第二轮输入：

```text
用户只说：继续上次 auth 的问题。
```

期望：

1. 系统召回 auth token 过期边界问题。
2. 系统召回项目同步校验约束。
3. 系统召回相关 code_ref。
4. Agent 不要求用户重新解释背景。

验收指标：

| 指标 | 目标 |
|---|---:|
| 重复上下文说明次数 | 0 |
| 历史任务状态召回 | 命中 |
| Token savings | >= 30% |

### 13.3 验收任务 2：用户架构偏好应用

目标：验证 `user_global` 偏好跨项目生效。

第一轮输入：

```text
用户明确声明：以后技术方案先分析架构边界、风险和工程落地，再给实现步骤。
```

第二轮输入：

```text
用户在另一个项目中请求设计任务调度模块。
```

期望：

1. 系统召回用户偏好。
2. Agent 输出结构先包含问题分析、架构边界、风险，再进入实现。
3. 不需要用户重复声明偏好。

验收指标：

| 指标 | 目标 |
|---|---:|
| 用户偏好召回准确率 | 100% |
| 重复说明次数 | 0 |
| 错误 scope 注入 | 0 |

### 13.4 验收任务 3：历史架构决策召回

目标：验证 `decision` 类型记忆和 evidence 能正确召回。

第一轮输入：

```text
用户和 Agent 讨论后决定暂不引入 Kafka，原因是当前异步需求不足，避免过早复杂化。
```

第二轮输入：

```text
用户问：这个项目为什么没有用 Kafka？
```

期望：

1. 系统召回决策结论。
2. 系统召回原因和适用边界。
3. 系统能返回 evidence 摘要。
4. 如果该决策处于 pending_review，输出应标记未确认。

验收指标：

| 指标 | 目标 |
|---|---:|
| 决策召回准确率 | >= 80% |
| evidence faithfulness | >= 90% |
| 未确认状态标记 | 100% |

### 13.5 验收任务 4：避免重复踩坑

目标：验证 `failure` 和 `procedure` 记忆能改变后续行为。

第一轮输入：

```text
一次问题定位失败，原因是 Agent 只看应用日志，没有检查数据库连接池耗尽。
用户纠正：后续类似慢请求问题要先看 metrics、trace 和 DB pool。
```

第二轮输入：

```text
用户报告另一个接口偶发慢请求。
```

期望：

1. 系统召回失败经验。
2. Agent 优先建议检查 metrics、trace、DB pool。
3. 旧错误路径被降权。

验收指标：

| 指标 | 目标 |
|---|---:|
| 失败经验召回 | 命中 |
| 用户纠正次数 | 降低 >= 50% |
| 旧错误策略复现 | 0 |

### 13.6 验收任务 5：识别过期项目事实

目标：验证 temporal validity 和 staleness penalty。

第一轮输入：

```text
项目使用 MySQL。
```

后续输入：

```text
用户纠正：项目已经迁移到 PostgreSQL，之前的 MySQL 信息过期。
```

测试输入：

```text
用户问：当前项目数据库是什么？
```

期望：

1. 系统返回 PostgreSQL。
2. MySQL 旧记忆被标记 archived 或 superseded。
3. 若旧记忆被召回，只能作为历史信息，不进入当前结论。

验收指标：

| 指标 | 目标 |
|---|---:|
| Temporal correctness | 100% |
| 过期记忆误用率 | 0 |
| supersedes 链接 | 存在 |

### 13.7 验收任务 6：多 Agent 共享同一项目上下文

目标：验证 Codex、Claude Code、Cursor 共享同一 Memory Daemon。

流程：

```text
Claude Code 完成一次架构决策讨论并写入记忆。
Codex 在同一项目中继续实现相关代码。
Cursor 在同一项目中询问历史决策。
```

期望：

1. 三个 Agent 使用同一 project scope。
2. 后续 Agent 能召回前一个 Agent 写入的稳定记忆。
3. session_id 不同但 project_id/repo_id 对齐。

验收指标：

| 指标 | 目标 |
|---|---:|
| 跨 Agent 召回成功率 | >= 80% |
| scope 错误率 | 0 |
| Level4 capability coverage | 100% |
| Event capture completeness | >= 90% |

### 13.8 验收任务 7：临时工具输出不污染长期记忆

目标：验证 Admission 和 Retention 对工具输出的控制。

输入：

```text
Agent 多次运行测试、构建、lint，产生大量临时输出。
```

期望：

1. 不保存完整 output。
2. 普通成功输出不进入长期记忆。
3. 失败输出只保存摘要、错误签名、关键词和 tool ref。
4. 未被重复引用的临时输出 5 天后清理或归档。

验收指标：

| 指标 | 目标 |
|---|---:|
| 完整 output 存储 | 0 |
| 临时输出长期入库率 | <= 5% |
| 错误签名提取准确率 | >= 80% |

### 13.9 验收任务 8：源码结构事实不混入普通 Memory

目标：验证 Codegraph 边界。

输入：

```text
Agent 分析代码，发现 OrderService 调用了 PaymentService.Charge。
```

期望：

1. 调用关系进入 Code Index，不作为普通 memory_item 长期保存。
2. 如果有设计原因，例如“同步调用是为了强一致确认”，则设计原因进入 Memory。
3. Memory 中只保存 code_ref。

验收指标：

| 指标 | 目标 |
|---|---:|
| 代码结构事实进入 Memory | 0 |
| code_ref 完整率 | >= 90% |
| 设计原因保存准确率 | >= 80% |

### 13.10 验收任务 9：用户纠正后后续行为改变

目标：验证纠错、负强化和版本化。

第一轮输入：

```text
Agent 记住用户偏好使用 Redis 做缓存。
```

纠正输入：

```text
用户纠正：不是所有缓存都用 Redis，本地进程缓存能满足的场景优先本地缓存。
```

测试输入：

```text
用户让 Agent 设计一个低频配置缓存。
```

期望：

1. 旧记忆被降权或 superseded。
2. 新偏好被 stable。
3. Agent 优先分析本地缓存是否足够，而不是直接推荐 Redis。

验收指标：

| 指标 | 目标 |
|---|---:|
| 纠正后偏好命中 | 100% |
| 旧偏好误用 | 0 |
| supersedes 关系 | 存在 |

### 13.11 验收任务 10：重复设计复查上下文压缩

目标：验证 `review_checkpoint` 能降低反复架构设计复查任务的历史上下文加载成本。

第一轮输入：

```text
用户要求从逻辑完整性角度检查总体架构设计、分期规划和 P0/P1 详细设计，并确认哪些问题忽略、哪些问题补充。
```

系统行为：

1. 读取当前文档事实源。
2. 形成复查结论。
3. 写入 `memory_item(memory_type=review_checkpoint)`。
4. 写入 `review_checkpoint`，记录目标文档、章节、hash、结论、忽略项、延期项和下次复查策略。

第二轮输入：

```text
用户再次要求从头检查是否有内容、逻辑缺失或者错误。
```

期望：

1. 系统优先召回最近 `review_checkpoint`，不加载完整历史对话。
2. 系统读取当前文档或 diff 校验事实源。
3. 已确认忽略的问题不重复作为重大缺陷提出。
4. 如果文档 hash 未变化，复查重点转向未覆盖风险和新问题；如果 hash 变化，只重查变化章节和受影响章节。

验收指标：

| 指标 | 目标 |
|---|---:|
| 历史上下文 Token savings | >= 60% |
| 已忽略问题重复提出次数 | 0 |
| checkpoint 召回准确率 | >= 90% |
| 文档 hash 命中后全文重读率 | <= 30% |

## 14. 关键风险

### 14.1 Level4 捕获能力依赖 Agent 实现

MCP 不是被动监听协议，不同 Agent 的 hook、日志、插件能力不同。一期目标是 Codex、Cursor、Claude Code 都做到 Level4，但实现上需要 adapter capability 探测和降级策略。

### 14.2 自动写入可能污染长期记忆

自动写入必须经过准入、证据绑定、状态机和 retention。不能把所有捕获事件直接写入 stable memory。

### 14.3 通用知识可能污染个人记忆库

常识、定理、行业基础知识、技能可以长期存储，但不应无条件写入。只有当它被用户整理、被项目上下文绑定、被失败经验引用或多次影响决策时，才应保留。

### 14.4 错误强化会形成系统性偏差

检索命中不等于有效。强化必须区分 retrieved、injected、cited、confirmed、task_success 和 rejected。

### 14.5 Codegraph 与 Memory 边界容易混淆

如果 Memory 保存大量代码结构事实，会造成过期和重复。代码结构事实应由 Code Index 重新计算，Memory 只保存决策和来源引用。

## 15. 后续演进

### 15.1 小团队版本

新增：

1. 多用户共享。
2. team scope。
3. 项目权限。
4. 中心化 PostgreSQL。
5. Review Queue 协作。
6. 共享项目记忆。

### 15.2 企业平台版本

新增：

1. 多租户隔离。
2. 权限模型。
3. 审计日志。
4. 删除证明。
5. 备份恢复。
6. 合规策略。
7. 管理控制台。
8. 组织级知识治理。

### 15.3 学习画像版本

在 Coding Memory 稳定后，再基于历史交互和任务表现派生学习画像。

用户能力短板不应自动固化为普通长期记忆，必须经过技术体系分析、证据聚合和用户确认。
