

> 基线来源：
> - `The One 长期记忆系统总体架构设计.md` v0.1 冻结版
> - `The One 长期记忆系统分期迭代研发规划.md`

## 1. 设计目标

本文档面向多人协作研发，覆盖 P0 工程基座和 P1 手动记忆可用两个阶段的详细设计。

P0 目标：

```text
本地 memoryd 可启动、可初始化 SQLite、可执行 migration、可通过 MCP health/status 验证系统可用。
```

P1 目标：

```text
用户或 Agent 可显式写入记忆和设计复查 checkpoint，检索记忆，构造上下文，处理 review，并可纠错、归档和删除误写入记忆。
```

P0/P1 完成后的业务闭环：

```text
memoryd start
  -> sqlite migration
  -> MCP health/status
  -> memory.remember
  -> memory.search
  -> memory.context
  -> memory.review/edit/archive/delete
  -> manual review_checkpoint
  -> FTS/索引一致性验证
```

## 2. 阶段边界

### 2.1 P0 交付边界

P0 只解决“系统能可靠跑起来”。

必须交付：

1. Go 单二进制 `memoryd`。
2. 配置加载和默认配置。
3. SQLite 初始化。
4. `schema_migration`。
5. SQLite WAL、foreign key、busy timeout。
6. MCP server 框架。
7. `memory.health`。
8. `memory.status`。
9. 基础日志。
10. P0 验收脚本。

不交付：

1. 不做记忆写入。
2. 不做检索。
3. 不做 Agent Adapter。
4. 不做 embedding。
5. 不做 UI。

### 2.2 P1 交付边界

P1 解决“手动记忆可用”。

必须交付：

1. `memory.remember`。
2. `memory.search`。
3. `memory.context`。
4. `memory.review`。
5. `memory_item`、`evidence`、`memory_evidence_link`、`memory_review`。
6. `review_checkpoint` 手动写入和检索。
7. scope validator。
8. content minimization。
9. FTS5 `search_text`。
10. 手动 edit/archive/delete。
11. P1 验收脚本。

不交付：

1. 不做自动捕获。
2. 不做 raw_event 自动转 memory。
3. 不做完整 Retention Job。
4. 不做复杂 relation expansion。
5. 不做 sqlite-vec。

## 3. 多人协作分工

建议 5 个并行工作流。

| 工作流                 | 负责人角色       | P0 任务                    | P1 任务                             |
| ------------------- | ----------- | ------------------------ | --------------------------------- |
| A. Runtime/CLI      | 后端工程师       | `cmd/memoryd`、启动参数、日志    | 本地验收命令、调试命令                       |
| B. Storage          | 数据工程师/后端工程师 | SQLite 连接、migration、WAL  | P1 schema、DAO、事务、FTS              |
| C. MCP/API          | 后端工程师       | MCP server、health/status | remember/search/context/review    |
| D. Memory/Retrieval | 架构/后端工程师    | 无业务逻辑，只定义接口              | scope、状态流转、FTS 检索、context builder |
| E. QA/Acceptance    | 测试/研发       | P0 验收脚本                  | P1 验收数据集、回归脚本                     |

协作原则：

1. Storage 先合入 P0 schema 和 migration 框架，API 和 Memory 基于接口并行开发。
2. MCP/API 不直接拼 SQL，通过 storage repository 接口访问。
3. Memory/Retrieval 不直接处理 MCP 协议对象，通过 service DTO 交互。
4. QA 每期维护可重复执行的本地验收脚本。
5. P1 期间禁止引入 P2 自动捕获逻辑，避免阶段边界污染。

## 4. 代码模块设计

建议目录：

```text
cmd/memoryd
internal/app
internal/config
internal/logging
internal/mcp
internal/mcp/tools
internal/storage
internal/storage/sqlite
internal/storage/sqlite/migrations
internal/memory
internal/retrieval
internal/ingest
internal/diagnostics
internal/idgen
internal/timeutil
internal/testutil
```

模块职责：

| 模块 | 职责 |
|---|---|
| `cmd/memoryd` | main 函数、启动参数、退出处理 |
| `internal/app` | 组装 config、logger、storage、MCP server |
| `internal/config` | 配置结构、默认值、环境变量覆盖 |
| `internal/logging` | 结构化日志、日志级别、关键字段 |
| `internal/mcp` | MCP server 生命周期、工具注册 |
| `internal/mcp/tools` | health/status/remember/search/context/review 工具 |
| `internal/storage` | repository 接口定义 |
| `internal/storage/sqlite` | SQLite 实现、事务、migration、FTS |
| `internal/memory` | Memory CRUD、scope validator、状态流转 |
| `internal/retrieval` | FTS 检索、metadata filter、context builder |
| `internal/ingest` | content minimization、search_text 构建 |
| `internal/diagnostics` | health/status 诊断聚合 |
| `internal/idgen` | ID 生成 |
| `internal/timeutil` | 时间注入，便于测试 |
| `internal/testutil` | 测试 DB、fixture、验收 helper |

## 5. P0 详细设计

### 5.1 启动流程

```text
memoryd start
  -> load config
  -> init logger
  -> open sqlite
  -> apply pragmas
  -> run schema migrations
  -> detect capabilities
  -> build services
  -> register MCP tools
  -> start MCP server
  -> wait for shutdown signal
```

启动失败策略：

| 阶段 | 失败处理 |
|---|---|
| config 解析失败 | 直接退出，打印错误 |
| SQLite 打开失败 | 直接退出，打印 db path 和错误 |
| migration 失败 | 直接退出，不启动 MCP |
| FTS5 不可用 | 允许启动，但 status 标记 `fts5=false`，P1 不允许通过 |
| sqlite-vec 不可用 | 允许启动，status 标记 `sqlite_vec=false` |
| MCP server 启动失败 | 直接退出 |

### 5.2 启动参数

P0 只需要最小参数：

```text
memoryd serve
  --config <path>
  --data-dir <path>
  --db-path <path>
  --mcp-addr <addr>
  --log-level <debug|info|warn|error>
```

默认值：

| 参数 | 默认值 |
|---|---|
| `data-dir` | `$HOME/.memoryd` |
| `db-path` | `$HOME/.memoryd/memory.db` |
| `mcp-addr` | `stdio` 或本地 MCP 默认传输 |
| `log-level` | `info` |

P0 不要求 HTTP API。若实现 MCP stdio，`mcp-addr` 可以只保留为内部配置项。

### 5.3 配置模型

配置结构：

```yaml
storage:
  backend: sqlite
  path: ~/.memoryd/memory.db
  sqlite_vec_enabled: auto
  busy_timeout_ms: 1000

server:
  mcp_addr: stdio

logging:
  level: info
  format: text

memory:
  default_user_id: local_default_user
  default_workspace: local_default_workspace

retrieval:
  default_limit: 10
  default_token_budget: 1800
  online_timeout_ms: 100

embedding:
  provider: none
  model: ""

retention:
  job_enabled: false
  temporary_ttl_days: 5
  short_term_ttl_days: 90
```

环境变量覆盖建议：

| 环境变量 | 配置项 |
|---|---|
| `MEMORYD_DATA_DIR` | `data-dir` |
| `MEMORYD_DB_PATH` | `storage.path` |
| `MEMORYD_LOG_LEVEL` | `logging.level` |
| `MEMORYD_MCP_ADDR` | `server.mcp_addr` |

### 5.4 SQLite 连接和 PRAGMA

打开 DB 后必须执行：

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 1000;
```

连接策略：

1. 一个主 `*sql.DB`。
2. 写事务保持短事务。
3. P0 不需要单写者队列，但 storage 层接口应为 P1 保留事务封装。
4. migration 期间不接受 MCP 请求。

### 5.5 Migration 设计

Migration 表：

```sql
schema_migration (
  version             integer primary key,
  name                text not null,
  applied_at          datetime not null,
  checksum            text
)
```

Migration 文件命名：

```text
0001_init_core.sql
0002_init_memory.sql
0003_init_fts.sql
```

P0 migration：

```text
0001_init_core.sql
  - schema_migration
  - local_identity
  - workspace
  - project
  - repo
```

P1 migration：

```text
0002_init_memory.sql
  - memory_item
  - evidence
  - memory_evidence_link
  - memory_review
  - memory_tombstone

0003_init_fts.sql
  - memory_item_fts
  - FTS trigger or application-managed sync
```

Migration 执行规则：

1. 按 version 升序执行。
2. 每个 migration 独立事务。
3. 成功后写 `schema_migration`。
4. checksum 不匹配时启动失败。
5. migration 文件只允许追加，不允许修改已发布 migration。

### 5.6 P0 MCP 工具

#### 5.6.1 memory.health

用途：判断 daemon 基础可用。

请求：

```json
{}
```

响应：

```json
{
  "request_id": "req_health_001",
  "ok": true,
  "version": "v0.1.0",
  "uptime_ms": 12345,
  "storage": {
    "ok": true,
    "backend": "sqlite"
  }
}
```

失败响应：

```json
{
  "request_id": "req_health_002",
  "ok": false,
  "error": {
    "error_code": "STORAGE_UNAVAILABLE",
    "message": "sqlite ping failed",
    "retryable": true,
    "fallback_hint": "restart memoryd or check db path"
  }
}
```

#### 5.6.2 memory.status

用途：返回能力和配置摘要。

请求：

```json
{
  "include_config": true
}
```

响应：

```json
{
  "request_id": "req_status_001",
  "version": "v0.1.0",
  "storage": {
    "backend": "sqlite",
    "db_path": "/Users/user/.memoryd/memory.db",
    "sqlite": true,
    "fts5": true,
    "sqlite_vec": false,
    "fallback_retrieval": ["fts", "metadata"]
  },
  "migrations": {
    "current_version": 1,
    "dirty": false
  },
  "config": {
    "embedding_provider": "none",
    "retention_job_enabled": false
  }
}
```

`include_config=false` 时不返回配置摘要。

### 5.7 P0 日志设计

关键日志：

| 场景 | 字段 |
|---|---|
| 启动 | `version`、`db_path`、`mcp_addr` |
| migration | `version`、`name`、`duration_ms` |
| SQLite capability | `fts5`、`sqlite_vec` |
| MCP 请求 | `request_id`、`tool`、`duration_ms`、`ok` |
| 关闭 | `uptime_ms`、`reason` |

日志不记录：

1. API key。
2. 完整用户输入。
3. 完整工具输出。
4. 完整代码片段。

### 5.8 P0 错误码

| error_code | 场景 | retryable |
|---|---|---|
| `CONFIG_INVALID` | 配置解析失败 | false |
| `STORAGE_OPEN_FAILED` | DB 无法打开 | false |
| `STORAGE_UNAVAILABLE` | DB ping 失败 | true |
| `MIGRATION_FAILED` | migration 执行失败 | false |
| `MCP_SERVER_FAILED` | MCP 启动失败 | false |
| `INTERNAL_ERROR` | 未分类错误 | true |

### 5.9 P0 测试

单元测试：

1. config 默认值。
2. 环境变量覆盖。
3. migration 排序和幂等。
4. checksum 不匹配失败。
5. health/status 响应。

集成测试：

1. 使用临时目录启动 memoryd。
2. 创建 SQLite DB。
3. 执行 health/status。
4. 重启 memoryd，migration 不重复执行。

验收命令示例：

```text
memoryd serve --data-dir /tmp/memoryd-test
memoryctl health
memoryctl status
```

如果 P0 暂不实现 `memoryctl`，验收脚本可以直接调用 MCP tool。

## 6. P1 详细设计

### 6.1 P1 业务流程

#### 6.1.1 显式写入流程

```text
memory.remember
  -> validate request
  -> scope validator
  -> content minimization check
  -> normalize source_type / memory_type / tier
  -> build evidence
  -> build search_text
  -> transaction:
       insert memory_item
       insert evidence
       insert memory_evidence_link
       insert FTS
  -> return memory_id/state/tier
```

#### 6.1.2 搜索流程

```text
memory.search
  -> validate query and scope
  -> select candidate by FTS5
  -> metadata filter
  -> state filter
  -> simple rerank
  -> optional evidence summary
  -> return results + diagnostics
```

#### 6.1.3 上下文构建流程

```text
memory.context
  -> search memories
  -> group by type
  -> apply priority
  -> compress item content
  -> enforce token budget
  -> record injected access log if table is available
  -> return context_pack
```

P1 可不实现完整 `memory_access_log` 表，但接口设计应保留 `retrieval_trace_id` 或 diagnostics 占位。

#### 6.1.4 Review/纠错流程

```text
memory.review list
  -> query pending_review memories

memory.review approve
  -> transaction:
       update memory_item state=stable
       set user_confirmed=true
       insert memory_review
       update FTS

memory.review reject
  -> transaction:
       update memory_item state=archived
       insert memory_review
       remove or update FTS visibility

memory.review edit
  -> transaction:
       archive old memory or increment version
       update content/search_text
       insert memory_review
       update FTS

memory.review archive/delete
  -> transaction:
       state transition
       tombstone if deleted
       remove FTS entry if deleted
```

### 6.2 P1 数据表

P1 最小表：

```text
local_identity
workspace
project
repo
memory_item
evidence
memory_evidence_link
memory_review
review_checkpoint
memory_tombstone
schema_migration
memory_item_fts
```

### 6.3 P1 SQL 设计

#### 6.3.1 memory_item

```sql
create table if not exists memory_item (
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
  decay_rate               real not null default 0.8,
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
);
```

P1 必要索引：

```sql
create index if not exists idx_memory_scope
  on memory_item(scope, workspace_id, project_id, repo_id);

create index if not exists idx_memory_state
  on memory_item(state, tier, updated_at);

create index if not exists idx_memory_type
  on memory_item(memory_type, state);
```

#### 6.3.2 evidence

```sql
create table if not exists evidence (
  id                       text primary key,
  raw_event_id              text,
  source_type               text not null,
  interpreted_statement     text not null,
  keywords_json             text,
  salient_spans_json        text,
  source_ref_json           text,
  confidence                real default 0.7,
  created_at                datetime not null
);
```

#### 6.3.3 memory_evidence_link

```sql
create table if not exists memory_evidence_link (
  memory_id        text not null,
  evidence_id      text not null,
  relation_type    text not null,
  weight           real default 1.0,
  primary key (memory_id, evidence_id)
);
```

#### 6.3.4 memory_review

```sql
create table if not exists memory_review (
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
);
```

#### 6.3.5 review_checkpoint

P1 只支持手动写入和检索 `review_checkpoint`，不做自动生成。自动识别复查会话和文档 diff 留到 P3/P4。

```sql
create table if not exists review_checkpoint (
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
);

create index if not exists idx_review_checkpoint_scope
  on review_checkpoint(workspace_id, project_id, repo_id, checkpoint_type, updated_at);
```

P1 写入约束：

1. 必须同时创建 `memory_item(memory_type=review_checkpoint)`，用于 FTS 检索和 `memory.context` 注入。
2. `target_docs_json` 不保存完整文档正文，只保存路径、角色、hash、修改时间和章节标识。
3. `ignored_items_json` 只保存用户明确忽略或延期的问题。
4. `next_review_policy_json` 保存下次复查策略，例如“只关注重大逻辑缺失，细节后续处理”。

#### 6.3.6 memory_tombstone

```sql
create table if not exists memory_tombstone (
  memory_id           text primary key,
  deleted_reason      text,
  deleted_by          text,
  content_hash        text,
  deleted_at          datetime not null
);
```

#### 6.3.7 FTS5

P1 使用 application-managed sync，避免 trigger 逻辑过早复杂化。

```sql
create virtual table if not exists memory_item_fts
using fts5(
  memory_id unindexed,
  search_text,
  tokenize = 'unicode61'
);
```

写入规则：

| memory_item 状态 | FTS 操作 |
|---|---|
| `stable` | insert/update FTS |
| `provisional` | insert/update FTS |
| `pending_review` | insert/update FTS，但 search 默认弱化或标记 |
| `archived` | 默认从 FTS 删除，include_archived 时走普通表过滤 |
| `deleted` | 从 FTS 删除 |

### 6.4 Scope Validator

规则：

| scope | 必填 | 默认空 |
|---|---|---|
| `user_global` | `user_id` | `project_id`、`repo_id` |
| `project_local` | `workspace_id`、`project_id` | 无 |
| `repo_local` | `workspace_id`、`repo_id` | `project_id` 可推导 |
| `session` | `workspace_id`、`session_id` | 无 |

`global_common` P1 不开放给普通写入，避免通用知识污染。

校验失败错误：

```json
{
  "error_code": "SCOPE_INVALID",
  "message": "project_local memory requires workspace_id and project_id",
  "retryable": false
}
```

### 6.5 Content Minimization

P1 不做复杂脱敏，但必须做边界控制。

配置：

```text
capture.max_event_bytes = 32768
memory.max_content_chars = 4000
memory.max_evidence_chars = 1200
memory.max_keyword_count = 30
memory.max_salient_span_count = 10
```

规则：

1. `content` 超过 `memory.max_content_chars` 时拒绝写入。
2. `evidence.interpreted_statement` 超过限制时拒绝写入。
3. `salient_spans` 只保存关键片段，不保存完整日志。
4. `source_ref` 可以保存 hash、file path、symbol、command hash，不保存完整 output。

### 6.6 search_text 构建

输入字段：

```text
title
content
normalized_content
keywords
tags
retrieval_cues
entities
```

构建规则：

```text
search_text =
  title
  + "\n" + content
  + "\n" + normalized_content
  + "\nkeywords: " + join(keywords)
  + "\ntags: " + join(tags)
  + "\nretrieval: " + join(retrieval_cues)
  + "\nentities: " + join(entities)
```

P1 不做中文分词扩展，先使用 FTS5 unicode61。后续如召回不足，再评估 tokenizer 或额外关键词字段。

### 6.7 P1 MCP 工具详细设计

#### 6.7.1 memory.remember

请求：

```json
{
  "content": "用户偏好先进行架构边界和风险分析，再给工程实现方案。",
  "title": "用户偏好：先架构分析再实现",
  "memory_type": "preference",
  "scope": "user_global",
  "workspace_id": "ws_local",
  "project_id": null,
  "repo_id": null,
  "session_id": null,
  "task_id": null,
  "source_type": "user_declared",
  "importance": 0.9,
  "confidence": 1.0,
  "pinned": false,
  "tags": ["communication", "architecture"],
  "keywords": ["架构边界", "风险", "工程落地"],
  "review_checkpoint": null,
  "evidence": {
    "interpreted_statement": "用户明确要求技术方案先分析架构边界、风险和工程落地。",
    "keywords": ["架构边界", "风险", "工程落地"],
    "salient_spans": ["以后技术方案先分析架构边界、风险和工程落地"]
  }
}
```

当 `memory_type=review_checkpoint` 时，请求必须额外携带 `review_checkpoint`：

```json
{
  "memory_type": "review_checkpoint",
  "scope": "project_local",
  "content": "总体架构设计已冻结；后续复查只关注影响一期目标成立的重大逻辑缺失，细节优化后续处理。",
  "review_checkpoint": {
    "checkpoint_type": "architecture_design_review",
    "review_intent": ["logic_completeness", "business_loop", "phase_consistency"],
    "target_docs": [
      {
        "path": "The One 长期记忆系统总体架构设计.md",
        "doc_role": "architecture_baseline",
        "content_hash": "sha256:..."
      }
    ],
    "conclusion": "baseline_frozen",
    "confirmed_baseline": ["一期只做 AI Coding Agent 记忆层", "Code Index 与 Memory 分层"],
    "ignored_items": ["非重大细节调整后续完善"],
    "deferred_items": [],
    "open_items": [],
    "next_review_policy": {
      "focus": "major_logic_gap_only",
      "read_strategy": "checkpoint_first_then_current_doc_or_diff"
    }
  }
}
```

响应：

```json
{
  "request_id": "req_remember_001",
  "memory_id": "mem_001",
  "state": "stable",
  "tier": "durable",
  "deduped": false
}
```

默认规则：

| 条件 | state | tier |
|---|---|---|
| `source_type=user_declared` 且非高影响 | `stable` | `durable` |
| `memory_type=decision` | `pending_review` | `long_term` |
| `memory_type=constraint` | `pending_review` | `long_term` |
| `memory_type=failure` 且 importance >= 0.8 | `pending_review` | `long_term` |
| `memory_type=review_checkpoint` | `stable` | `long_term` |
| 普通显式写入 | `stable` | `long_term` |
| `scope=session` | `stable` | `temporary` |

#### 6.7.2 memory.search

请求：

```json
{
  "query": "为什么项目没有使用 Kafka",
  "workspace_id": "ws_local",
  "project_id": "proj_a",
  "repo_id": null,
  "scope": ["project_local", "user_global"],
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
      "memory_id": "mem_123",
      "memory_type": "decision",
      "scope": "project_local",
      "title": "暂不引入 Kafka",
      "content": "当前异步需求不足，历史决策是暂不引入 Kafka，避免过早复杂化。",
      "score": 0.82,
      "confidence": 0.9,
      "state": "stable",
      "tier": "long_term",
      "evidence_refs": ["ev_123"]
    }
  ],
  "diagnostics": {
    "fts_hits": 3,
    "filtered_count": 1,
    "latency_ms": 15,
    "fallback": "fts_metadata"
  }
}
```

P1 排序：

```text
score =
  0.55 * bm25_norm
  + 0.20 * scope_weight
  + 0.15 * confidence
  + 0.10 * importance
  - archived_penalty
```

P1 不实现 vector、relation expansion。

#### 6.7.3 memory.context

请求：

```json
{
  "task": "设计任务调度模块",
  "workspace_id": "ws_local",
  "project_id": "proj_b",
  "repo_id": null,
  "agent_type": "codex",
  "token_budget": 1200,
  "include_code_refs": false,
  "include_evidence_summary": true
}
```

响应：

```json
{
  "request_id": "req_context_001",
  "context_pack": {
    "summary": "用户偏好技术方案先分析架构边界、风险和工程落地，再给实现步骤。",
    "memories": [
      {
        "memory_id": "mem_001",
        "type": "preference",
        "compressed": "输出技术方案时先分析架构边界、风险和工程落地，再给实现步骤。",
        "why_included": ["user_global", "task_fit", "stable"]
      }
    ],
    "constraints": [],
    "code_refs": []
  },
  "used_memory_ids": ["mem_001"],
  "latency_ms": 20
}
```

P1 context 预算：

| 类型 | 优先级 |
|---|---:|
| `constraint` | 1 |
| `decision` | 2 |
| `failure` | 3 |
| `review_checkpoint` | 4 |
| `preference` | 5 |
| `project_fact` | 6 |
| `procedure` | 7 |
| `temporary_state` | 8 |

超过 token budget 时按优先级和 score 裁剪。

当 `task` 命中设计复查、架构评审、文档完整性检查等意图时，P1 的 `memory.context` 应优先检索 `review_checkpoint`。P1 不做文档 hash 自动计算，但应把 checkpoint 中的目标文档路径、结论、忽略项和下次复查策略压缩进入上下文。

#### 6.7.4 memory.review

统一请求：

```json
{
  "action": "list",
  "workspace_id": "ws_local",
  "project_id": "proj_a",
  "state": "pending_review",
  "limit": 20
}
```

支持 action：

```text
list
approve
reject
edit
archive
delete
```

`approve` 请求：

```json
{
  "action": "approve",
  "memory_id": "mem_123",
  "feedback": "确认该架构决策"
}
```

`edit` 请求：

```json
{
  "action": "edit",
  "memory_id": "mem_123",
  "edit_content": "暂不引入 Kafka，适用边界是当前异步需求不足且团队不希望增加运维复杂度。",
  "feedback": "补充适用边界"
}
```

`archive` 请求：

```json
{
  "action": "archive",
  "memory_id": "mem_123",
  "feedback": "该决策已经过期"
}
```

`delete` 请求：

```json
{
  "action": "delete",
  "memory_id": "mem_123",
  "feedback": "误写入"
}
```

响应：

```json
{
  "request_id": "req_review_001",
  "memory_id": "mem_123",
  "state": "archived",
  "user_confirmed": false
}
```

### 6.8 状态流转

P1 允许：

```text
pending_review -> stable
pending_review -> archived
pending_review -> deleted

stable -> archived
stable -> deleted
stable -> stable(version+1)

archived -> stable
archived -> deleted

deleted -> terminal
```

约束：

1. `deleted` 不可恢复。
2. `archive` 后默认 search/context 不返回。
3. `edit` stable memory 时 P1 可以原地 version+1，不强制复制新 memory；但必须记录 `memory_review`。
4. `delete` 必须删除 FTS entry 并写 tombstone。

### 6.9 事务边界

| 操作 | 事务内容 |
|---|---|
| remember | memory_item + evidence + link + FTS |
| review approve | memory_item state + memory_review + FTS |
| review reject/archive | memory_item state + memory_review + FTS delete |
| review edit | memory_item content/version + memory_review + FTS update |
| review delete | memory_item deleted + tombstone + memory_review + FTS delete |

事务失败时返回错误，不允许部分写入。

### 6.10 P1 错误码

| error_code | 场景 | retryable |
|---|---|---|
| `VALIDATION_FAILED` | 入参缺失或类型错误 | false |
| `SCOPE_INVALID` | scope 与字段不匹配 | false |
| `CONTENT_TOO_LARGE` | 内容超过边界 | false |
| `MEMORY_NOT_FOUND` | memory_id 不存在 | false |
| `STATE_CONFLICT` | 状态不允许该操作 | false |
| `FTS_UNAVAILABLE` | FTS5 不可用 | false |
| `STORAGE_BUSY` | SQLite busy timeout | true |
| `INTERNAL_ERROR` | 未分类错误 | true |

### 6.11 P1 日志

关键日志：

| 场景 | 字段 |
|---|---|
| remember | `request_id`、`memory_id`、`scope`、`memory_type`、`state` |
| search | `request_id`、`query_hash`、`scope`、`result_count`、`duration_ms` |
| context | `request_id`、`used_memory_count`、`token_budget`、`duration_ms` |
| review | `request_id`、`memory_id`、`action`、`old_state`、`new_state` |
| delete | `request_id`、`memory_id`、`fts_deleted`、`tombstone_written` |

日志不记录完整 `content`、完整 evidence 或完整 query。

### 6.12 P1 测试

单元测试：

1. scope validator。
2. content minimization。
3. search_text 构建。
4. state transition。
5. P1 score 排序。
6. token budget 裁剪。

Repository 测试：

1. remember 写入 memory/evidence/link/FTS。
2. archived memory 默认不被 search 返回。
3. delete 后 FTS 不可召回。
4. edit 后 FTS 更新。
5. project scope 隔离。
6. `review_checkpoint` 与 `memory_item` 同事务写入。

MCP 工具测试：

1. remember 成功。
2. remember scope invalid。
3. search 成功。
4. context token budget 生效。
5. review approve/reject/edit/archive/delete。
6. 设计复查任务优先召回 `review_checkpoint`。

集成验收：

1. 启动 memoryd。
2. 写入 user_global preference。
3. 在另一个 project context 召回 preference。
4. 写入 project_local Kafka decision。
5. 在其他 project 不召回该 decision。
6. archive Kafka decision。
7. 默认 search 不返回 archived decision。
8. delete memory 后 FTS 不返回。
9. 写入一次 `review_checkpoint`。
10. 再次设计复查时 `memory.context` 召回该 checkpoint。

## 7. 协作任务拆分

### 7.1 P0 任务拆分

| 任务 ID | 任务 | 输入 | 输出 | 可并行 |
|---|---|---|---|---|
| P0-A1 | 初始化 Go module 和目录 | 无 | 基础目录、main | 否 |
| P0-A2 | config loader | 配置默认值 | config package | 是 |
| P0-B1 | SQLite open/pragmas | db path | DB handle | 是 |
| P0-B2 | migration runner | migrations | schema_migration | 依赖 B1 |
| P0-C1 | MCP server skeleton | app services | tool registry | 是 |
| P0-C2 | health/status tools | storage diagnostics | MCP tools | 依赖 C1/B1 |
| P0-E1 | P0 acceptance script | binary | 验收脚本 | 依赖 C2 |

### 7.2 P1 任务拆分

| 任务 ID | 任务 | 输入 | 输出 | 可并行 |
|---|---|---|---|---|
| P1-B1 | P1 schema migration | P0 migration | memory/evidence/review/FTS | 依赖 P0-B2 |
| P1-B1a | review_checkpoint schema | P1 migration | checkpoint table + index | 依赖 P1-B1 |
| P1-D1 | scope validator | scope rules | validator | 是 |
| P1-D2 | content minimization | config | checker | 是 |
| P1-D3 | search_text builder | memory DTO | builder | 是 |
| P1-B2 | memory repository | schema | CRUD + tx | 依赖 P1-B1 |
| P1-B3 | checkpoint repository | schema | review_checkpoint tx write/query | 依赖 P1-B1a/B2 |
| P1-C1 | memory.remember | repository/service | MCP tool | 依赖 P1-B2/B3/D1/D2 |
| P1-C2 | memory.search | retrieval service | MCP tool | 依赖 P1-B2/D3 |
| P1-C3 | memory.context | search service | MCP tool | 依赖 P1-C2 |
| P1-C4 | memory.review | repository/state | MCP tool | 依赖 P1-B2 |
| P1-E1 | P1 acceptance | tools | 验收脚本 | 依赖 C1-C4 |

## 8. 合并顺序建议

```text
P0-A1
  -> P0-A2 + P0-B1 + P0-C1
  -> P0-B2
  -> P0-C2
  -> P0-E1
  -> P0 release

P1-B1 + P1-D1 + P1-D2 + P1-D3
  -> P1-B1a
  -> P1-B2 + P1-B3
  -> P1-C1
  -> P1-C2
  -> P1-C3 + P1-C4
  -> P1-E1
  -> P1 release
```

## 9. P0/P1 完成定义

### 9.1 P0 Done

1. `memoryd` 可启动。
2. SQLite DB 自动创建。
3. migration 幂等。
4. `memory.health` 可调用。
5. `memory.status` 可调用。
6. 无 embedding provider 仍可运行。
7. P0 验收脚本通过。

### 9.2 P1 Done

1. `memory.remember` 可写入 preference/decision/failure/project_fact/review_checkpoint。
2. `memory.search` 可按 scope/type/state 检索。
3. `memory.context` 可返回 token budget 内上下文。
4. `memory.review` 支持 list/approve/reject/edit/archive/delete。
5. archive/delete 后默认检索不再注入旧记忆。
6. project scope 隔离通过。
7. 不保存完整 output/大段日志。
8. 设计复查任务可召回手动 checkpoint。
9. P1 验收脚本通过。

## 10. P1 后移交给 P2 的接口

P1 必须为 P2 保留以下稳定接口：

1. `memory_item.session_id`。
2. `memory_item.task_id`。
3. `evidence.raw_event_id` 可空。
4. `source_type` 规范值。
5. `scope validator`。
6. content minimization checker。
7. repository transaction helper。
8. MCP error response 格式。
9. `review_checkpoint` 表和手动写入语义。

P2 不应重写 P1 的 Memory CRUD，而是在 raw_event/evidence 自动生成链路上复用 P1 服务。
