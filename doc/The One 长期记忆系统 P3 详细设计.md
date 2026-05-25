# The One 长期记忆系统 P3 详细设计

> 基线来源：
> - `The One 长期记忆系统总体架构设计.md` v0.1 冻结版
> - `The One 长期记忆系统分期迭代研发规划.md`
> - `The One 长期记忆系统 P0-P1 详细设计.md`
> - `The One 长期记忆系统 P2 详细设计.md`
>
> 前置假设：P2 已按详细设计完成并通过测试，`memory.observe`、`agent_session`、`agent_task`、`raw_event`、capture quality 和捕获诊断接口均可作为 P3 稳定输入。

## 1. 设计目标

P3 目标：

```text
从 P2 捕获的 raw_event 中自动抽取 evidence，生成候选记忆，经过 Admission、Review 和基础 Retention，进入可解释、可纠错、可观测的长期记忆闭环。
```

P3 完成后的业务闭环：

```text
Agent / Adapter
  -> memory.observe
  -> append raw_event
  -> enqueue async_job(extract_evidence)
  -> Provider.ExtractEvidence
  -> write evidence
  -> enqueue async_job(generate_memory_candidate)
  -> Provider.GenerateCandidates
  -> Admission Control
  -> write_temporary / write_provisional / write_pending_review / write_stable
  -> review / retention / diagnostics
```

P3 的核心交付不是提升检索质量，也不是引入大模型智能抽取，而是让“自动写入长期记忆”第一次具备可用闭环：来源可追溯、准入可解释、写入可回滚、失败可诊断、临时信息可清理。

P3 引入 `Memory Processing Provider` 插拔架构。当前阶段默认实现 `rule_based` Provider，将规则优先抽取设计为正式 Provider，而不是临时分支。二期在同一 Provider 接口下扩展 Ollama、Deepseek、Minimax、OpenAI 等模型 Provider。

## 2. 阶段边界

### 2.1 必须交付

1. `async_job` 表、repository、worker 执行器和诊断能力。
2. `Memory Processing Provider` 接口。
3. `rule_based` Provider。
4. 从 `raw_event` 自动抽取 `evidence`。
5. 从 evidence 自动生成 memory candidate。
6. Admission Control 评分、决策矩阵和 reason codes。
7. 自动写入：
   - `write_temporary`
   - `write_provisional`
   - `write_pending_review`
   - `write_stable`
8. 高影响记忆进入 Review Queue。
9. 用户显式声明可自动写入 stable/durable。
10. 用户纠正可直接覆盖旧 memory，并保留 correction evidence 与 review 轨迹。
11. 设计复查类会话可生成 `review_checkpoint` candidate。
12. 基础 Retention Job：
   - temporary TTL 清理。
   - retention score 重算。
   - tier 更新。
13. delete consistency 检查任务。
14. 异步任务和 candidate 诊断工具。
15. P3 单元测试、repository 测试、worker 测试、MCP 工具测试和验收脚本。

### 2.2 明确不交付

1. 不实现外部 LLM Provider 的真实调用。
2. 不实现 Ollama、Deepseek、Minimax、OpenAI 的鉴权、密钥管理、模型路由和成本控制。
3. 不实现 embedding、sqlite-vec 或向量检索。
4. 不实现 P4 的 relation expansion 检索增强。
5. 不实现完整 Code Index Adapter。
6. 不实现在线 LLM rerank。
7. 不实现复杂多轮反思、自动总结长对话或深度推理链抽取。
8. 不要求异步任务最终全部成功；失败必须可诊断且不阻塞 Agent 主流程。
9. 不改变 P2 `raw_event` append-only 事实层语义。
10. 不把普通工具成功输出写入长期稳定记忆。

### 2.3 与 P2 的衔接

P3 复用 P2 稳定接口：

1. `raw_event.id` 作为 `evidence.raw_event_id`。
2. `raw_event.event_type/source_channel/source_refs_json/content_hash` 作为抽取输入。
3. `agent_session.capture_quality_json` 作为 evidence confidence 和 source_quality 的输入。
4. `agent_task.status/outcome_summary` 作为任务结果归因输入。
5. `source_refs.target_event_id` 用于用户纠正绑定原始事件。
6. `source_refs.decision_summary/reason_summary` 用于架构决策 candidate。
7. raw_event 按 session/task/project/event_type 查询能力用于 worker 拉取上下文。

P3 从 `memory.observe` 开始改变写入链路：P2 只返回 `pipeline=raw_event_only`；P3 在写入 raw_event 后 best-effort enqueue `async_job(extract_evidence)`，响应仍不等待抽取完成。

P3 不强保障异步任务可靠投递。若 raw_event 已写入但 enqueue 失败，`memory.observe` 仍可返回 `accepted=true`，但必须在响应 diagnostics 或日志中标记 `automation_enqueue_failed`，并允许通过捕获诊断看到 raw_event 仍存在。P3 提供手动 reconcile 入口发现并补偿 orphan raw_event；自动周期扫描留到后续运维增强。

### 2.4 与 P1 的衔接

P3 复用 P1 已完成能力：

1. `memory_item`、`evidence`、`memory_evidence_link`、`review_checkpoint`、`memory_review`。
2. Scope validator。
3. Content minimization。
4. FTS5 `search_text` 构建。
5. `memory.review` 的 approve/reject/edit/archive/delete 流转。
6. archive/delete 后 FTS 一致性策略。

P3 不重写 P1 手动记忆接口。自动写入和手动写入共享底层 repository 能力，但 service 层必须保留边界：`memory.remember` 表示用户或 Agent 显式写入，P3 worker 表示系统根据 raw_event 自动候选写入。

### 2.5 P3 对 P1 数据模型的最小扩展

P3 允许在 P1 已有数据模型上做最小兼容扩展，但不得破坏 P1 手动记忆行为。

P3 需要补充的 memory type：

| 类型 | 用途 | 默认 scope | 默认 tier |
|---|---|---|---|
| `requirement` | 用户明确需求、阶段目标、验收要求 | `project_local` | `long_term` |
| `assumption` | 架构或阶段设计成立的前提假设 | `project_local` | `long_term` |
| `open_issue` | 待确认问题、开放风险、跨会话未决事项 | `project_local` 或 `repo_local` | `short_term` 或 `long_term` |
| `session_summary` | session/task 结果摘要，只服务短中期连续性 | `session` | `short_term` |

P3 需要补充的 tier：

| tier | 用途 |
|---|---|
| `short_term` | 重复失败、session summary、未确认但短期有用的候选 |
| `archived` | 归档后的层级标记，配合 `state=archived` 排除默认检索 |

实现要求：

1. 同步更新 `internal/memory` 的常量、validator、默认状态和 tier 规则。
2. P1 已有 `temporary/long_term/durable` 语义保持不变。
3. `requirement` 与 `constraint` 必须区分：requirement 描述“要实现什么/验收什么”，constraint 描述“实现时不能突破什么边界”。
4. `assumption` 与 `project_fact` 必须区分：assumption 是可被验证或推翻的前提，project_fact 是当前稳定事实。
5. `open_issue` 不替代任务管理系统，只保存会影响后续设计、实现或验收的跨会话未决事项。
6. `session_summary` 不得进入 `stable + long_term/durable`，除非后续用户显式确认并改写为更具体的 `procedure/failure/project_fact`。
7. P3 migration 只追加 schema 或常量兼容，不修改已发布 P1/P2 migration。

## 3. 总体架构

```text
memory.observe
    |
    +-- Capture Service
    |     - validate / normalize / minimize
    |     - upsert session/task
    |     - append raw_event
    |     - enqueue extract_evidence job
    |
    v
Async Worker
    |
    +-- Job Repository
    |     - poll pending jobs
    |     - mark running/succeeded/failed
    |     - retry with backoff
    |
    +-- Processing Provider
    |     - rule_based
    |     - none
    |     - llm providers reserved for phase 2
    |
    +-- Memory Automation Service
          - write evidence
          - generate candidate
          - compute admission
          - write memory/review/checkpoint
          - enqueue retention/delete checks
```

推荐新增代码目录：

```text
internal/processor
internal/automation
internal/retention
internal/storage/sqlite/async_repository.go
internal/storage/sqlite/automation_repository.go
internal/mcp/tools/automation.go
internal/storage/sqlite/migrations/0005_init_automation.sql
```

模块职责：

| 模块 | 职责 |
|---|---|
| `internal/processor` | Provider 接口、rule_based Provider、candidate DTO |
| `internal/automation` | async worker、job 编排、Admission、自动写入服务 |
| `internal/retention` | retention score、tier 计算、temporary cleanup |
| `internal/storage` | P3 repository 接口定义 |
| `internal/storage/sqlite` | async_job、candidate 诊断、memory 自动写入和 relation 持久化 |
| `internal/mcp/tools/automation.go` | `memory.jobs.*`、`memory.candidates.*` 诊断工具 |
| `internal/app` | worker 启动和 service 组装 |

## 4. Processing Provider 设计

### 4.1 Provider 定位

Provider 负责把事件事实加工为可解释证据和候选记忆：

```text
raw_event + nearby context
  -> evidence draft
  -> memory candidate
```

Provider 不负责：

1. 写数据库。
2. 做 Admission 最终决策。
3. 执行 review 状态流转。
4. 纠正命中旧 memory 时原地覆盖旧记忆。
5. 查询外部 Code Index。

这样可以保证二期引入 LLM Provider 时，不改变 P3 的写入事务、准入策略和诊断模型。

### 4.2 Provider 接口

```go
package processor

type Provider interface {
	Name() string
	ExtractEvidence(ctx context.Context, input EvidenceInput) ([]EvidenceDraft, error)
	GenerateCandidates(ctx context.Context, input CandidateInput) ([]MemoryCandidate, error)
}
```

`EvidenceInput`：

```go
type EvidenceInput struct {
	RawEvent        capture.RawEvent
	Session         capture.AgentSession
	Task            capture.AgentTask
	CaptureQuality  CaptureQualitySnapshot
	RelatedEvents   []capture.RawEvent
	Now             time.Time
}
```

`EvidenceDraft`：

```go
type EvidenceDraft struct {
	SourceType           string
	InterpretedStatement string
	Keywords             []string
	SalientSpans         []string
	SourceRef            map[string]any
	Confidence           float64
}
```

`CandidateInput`：

```go
type CandidateInput struct {
	Evidence      memory.Evidence
	RawEvent      capture.RawEvent
	Session       capture.AgentSession
	Task          capture.AgentTask
	RelatedMemory []memory.MemoryItem
	Now           time.Time
}
```

`MemoryCandidate`：

```go
type MemoryCandidate struct {
	CandidateID       string
	MemoryType        string
	Scope             string
	WorkspaceID       string
	UserID            string
	ProjectID         string
	RepoID            string
	SessionID         string
	TaskID            string
	SourceType        string
	Title             string
	Content           string
	Keywords          []string
	Entities          []string
	RetrievalCues     []string
	Tags              []string
	Confidence        float64
	Importance        float64
	EncodingDepth     int
	ReviewCheckpoint  *ReviewCheckpointDraft
	CandidateReason   []string
	SourceEvidenceIDs []string
}
```

### 4.3 Provider 配置

新增配置：

```yaml
processor:
  provider: rule_based
  enable_auto_processing: true
  max_related_events: 20
  max_candidates_per_event: 3
```

默认值：

| 配置 | 默认值 | 说明 |
|---|---|---|
| `processor.provider` | `rule_based` | P3 默认 Provider |
| `processor.enable_auto_processing` | `true` | 是否在 observe 后 enqueue 自动处理 |
| `processor.max_related_events` | `20` | Worker 为抽取加载的同 session/task 近邻事件上限 |
| `processor.max_candidates_per_event` | `3` | 单个 evidence 最多生成候选数量 |

`provider=none` 时仍允许写入 raw_event，但不生成 evidence/candidate，`async_job` 直接记录 `succeeded` 或跳过 enqueue，诊断中标记 `provider_disabled`。

### 4.4 rule_based Provider

`rule_based` 是 P3 一期唯一真实 Provider。

抽取规则：

| raw_event.event_type | Evidence source_type | 抽取策略 |
|---|---|---|
| `user.declaration` | `user_declared` | 使用 `content_summary` 或 salient span 形成用户声明 evidence |
| `user.correction` | `user_confirmed` | 使用 `source_refs.target_event_id` 绑定原事件，形成纠正 evidence |
| `agent.decision` | `agent_summary` | 使用 `source_refs.decision_summary/reason_summary` 形成决策 evidence |
| `tool.result.summary` | `tool_output` | 仅在失败、重复失败或含错误签名时生成 evidence |
| `task.result` | `task_result` | 结合 outcome 形成任务结果 evidence，不直接长期写入 |
| `session.end` | `session_summary` | 仅生成 session summary 或 checkpoint 相关 evidence |
| `file.edit.summary` | `file_edit_summary` | 只保存编辑摘要和 file ref，不保存代码结构事实 |
| `conversation.message` | `agent_summary` | 只在包含显式偏好、约束、设计复查意图时生成 evidence |
| `agent.response.summary` | `agent_summary` | 只在包含明确结论、决策或复查结论时生成 evidence |

候选生成规则：

| Evidence 特征 | Candidate memory_type | 默认 scope | 默认动作 |
|---|---|---|---|
| 用户显式“记住/以后/必须/不要” | `preference` 或 `constraint` | `user_global` 或 `project_local` | stable 或 pending_review |
| 用户纠正已有事实 | 原类型或 `project_fact` | 继承原记忆 scope | stable，直接覆盖旧记忆 |
| 用户明确阶段目标、需求、验收条件 | `requirement` | `project_local` | stable 或 pending_review |
| 设计前提、阶段假设、边界假设 | `assumption` | `project_local` | provisional 或 pending_review |
| 待确认问题、开放风险、未决设计点 | `open_issue` | `project_local` 或 `repo_local` | pending_review 或 short_term |
| 架构决策 | `decision` | `project_local` | pending_review |
| 安全/数据边界约束 | `constraint` | `project_local` | pending_review |
| 重复失败错误签名 | `failure` | `repo_local` 或 `project_local` | provisional |
| 普通工具失败摘要 | `temporary_state` | `session` | temporary |
| task/session 结果摘要 | `session_summary` | `session` | short_term |
| 设计复查结论 | `review_checkpoint` | `project_local` | pending_review candidate |

`rule_based` Provider 必须宁可少写，不可泛化过度。无法从结构化字段得到明确结论时，返回空 candidate，并记录 reason code：`insufficient_structured_signal`。

### 4.5 二期 LLM Provider 扩展

二期在同一 Provider 接口下实现：

1. `ollama`：本地模型 Provider。
2. `deepseek`：外部 Deepseek Provider。
3. `minimax`：外部 Minimax Provider。
4. `openai`：外部 OpenAI Provider。

二期新增内容：

1. 模型配置和密钥读取。
2. 网络超时、重试和限流。
3. Prompt 模板版本管理。
4. 成本和 token 统计。
5. LLM 输出 JSON schema 校验。
6. LLM 失败时回退 `rule_based`。
7. 敏感内容出站策略。

P3 一期只在文档和接口层预留，不实现上述 Provider。

## 5. 核心数据模型

### 5.1 async_job

```sql
create table if not exists async_job (
  id                  text primary key,
  job_type            text not null,
  target_type         text not null,
  target_id           text not null,
  status              text not null,
  priority            integer not null default 5,
  retry_count         integer not null default 0,
  max_retries         integer not null default 3,
  next_run_at         datetime not null,
  last_error          text,
  dedup_key           text,
  payload_json        text,
  created_at          datetime not null,
  updated_at          datetime not null
);

create index if not exists idx_async_job_poll
  on async_job(status, next_run_at, priority, created_at);

create index if not exists idx_async_job_target
  on async_job(target_type, target_id, job_type);

create unique index if not exists idx_async_job_dedup
  on async_job(dedup_key)
  where dedup_key is not null and dedup_key != '';
```

推荐 `job_type`：

```text
extract_evidence
generate_memory_candidate
compute_admission
compute_retention
cleanup_temporary
delete_consistency
```

P3 不实现 `compute_embedding`、`build_relation`、`consolidate_memory` 的完整逻辑，可保留枚举兼容总体架构，但不 enqueue。

推荐 `target_type`：

```text
raw_event
evidence
memory_candidate
memory_item
session
```

推荐 `status`：

```text
pending
running
succeeded
failed
cancelled
```

### 5.2 memory_candidate

P3 新增 `memory_candidate` 诊断表，用于解释候选生成和 Admission 丢弃原因。该表不是稳定长期记忆，不进入 `memory.context`。

```sql
create table if not exists memory_candidate (
  id                         text primary key,
  raw_event_id                text,
  evidence_id                 text,
  provider                    text not null,
  memory_type                 text not null,
  scope                       text not null,
  workspace_id                text,
  user_id                     text,
  project_id                  text,
  repo_id                     text,
  session_id                  text,
  task_id                     text,
  title                       text,
  content                     text not null,
  keywords_json               text,
  entities_json               text,
  retrieval_cues_json         text,
  tags_json                   text,
  source_evidence_ids_json    text,
  review_checkpoint_json      text,
  confidence                  real not null default 0.7,
  importance                  real not null default 0.5,
  encoding_depth              integer not null default 2,
  candidate_reason_json       text,
  admission_score             real,
  admission_decision          text,
  admission_reason_json       text,
  resulting_memory_id         text,
  status                      text not null,
  dedup_key                   text,
  created_at                  datetime not null,
  updated_at                  datetime not null
);

create index if not exists idx_memory_candidate_source
  on memory_candidate(raw_event_id, evidence_id, provider);

create index if not exists idx_memory_candidate_status
  on memory_candidate(status, created_at);

create index if not exists idx_memory_candidate_scope
  on memory_candidate(workspace_id, project_id, repo_id, memory_type, status);

create unique index if not exists idx_memory_candidate_dedup
  on memory_candidate(dedup_key)
  where dedup_key is not null and dedup_key != '';
```

推荐 `status`：

```text
generated
admitted
dropped
merged
failed
```

保留 `memory_candidate` 的原因：

1. 区分 Provider 未生成、Admission 丢弃、写入失败。
2. 支持验收“自动候选未生成时可诊断”。
3. 支持二期比较不同 Provider 的候选质量。

字段约束：

1. `source_evidence_ids_json` 保存 candidate 关联的全部 evidence id；`evidence_id` 只作为主 evidence 快速过滤字段。
2. `review_checkpoint_json` 保存 `ReviewCheckpointDraft` 的结构化草案，供 `compute_admission` 异步阶段写入 `review_checkpoint` 表。
3. `dedup_key` 推荐使用 `provider + memory_type + scope + normalized_content_hash + primary_evidence_id`，避免 worker 重试产生重复 candidate。
4. `content`、`review_checkpoint_json` 和 `source_evidence_ids_json` 仍必须经过 content minimization，不允许携带完整文档、完整工具输出或完整 diff。

`normalized_content_hash` 计算规则：

1. 输入字段：`memory_type + scope + normalized_content + sorted(keywords) + primary_evidence_id`。
2. `normalized_content` 需要统一空白、去除首尾空格、按 Unicode code point 原样保留中文，不做语义改写。
3. hash 算法固定为 `sha256`，持久化格式为 `sha256:<hex>`。
4. 同一 provider 重试时 dedup_key 必须稳定。
5. 不同 provider 生成相同内容时 P3 不自动合并，只通过 diagnostics 暴露；跨 provider 质量比较留到二期。

### 5.3 memory_relation

P3 只实现最小关系边：

```sql
create table if not exists memory_relation (
  id               text primary key,
  source_id        text not null,
  target_id        text not null,
  relation_type    text not null,
  weight           real not null default 1.0,
  created_at       datetime not null,
  updated_at       datetime not null
);

create index if not exists idx_memory_relation_source
  on memory_relation(source_id, relation_type);

create index if not exists idx_memory_relation_target
  on memory_relation(target_id, relation_type);
```

P3 使用的 `relation_type`：

```text
supersedes
superseded_by
contradicts
supports
```

P3 不做 P4 的 relation expansion 排序，只保证用户纠正、冲突和支持关系可持久化、可诊断。

### 5.4 evidence 扩展约束

P1 已有 `evidence.raw_event_id` 字段。P3 自动 evidence 写入必须满足：

1. `raw_event_id` 必须存在。
2. `interpreted_statement` 不超过 `memory.max_evidence_chars`。
3. `source_ref_json` 只保存引用、hash、摘要和 target id。
4. 不保存完整 prompt、完整 output、完整 diff 或完整源码。
5. 同一 `raw_event_id + source_type + interpreted_statement` 应幂等去重。

### 5.5 memory_item 自动写入约束

自动写入 `memory_item` 必须满足：

1. 使用 P1 scope validator。
2. 构建 `search_text` 并同步写入 FTS。
3. 写入 `memory_evidence_link(relation_type=derived_from)`。
4. pending_review 记忆可被检索，但上下文注入必须标记未确认。
5. temporary/session 记忆必须设置 `valid_until` 或可由 retention policy 推导 TTL。
6. deleted/archived 记忆不得默认进入 FTS 检索结果。

## 6. Async Worker 设计

### 6.1 启动方式

P3 在 `memoryd serve` 进程内启动本地 worker goroutine：

```text
app.New
  -> create Store
  -> create MemoryService
  -> create CaptureService
  -> create ProcessorProvider
  -> create AutomationService
  -> register MCP tools

app.Serve
  -> start worker if automation.worker_enabled
  -> start MCP stdio server
```

新增配置：

```yaml
automation:
  worker_enabled: true
  poll_interval_ms: 1000
  batch_size: 10
  max_attempts: 3
  retry_base_delay_ms: 1000
```

默认值：

| 配置 | 默认值 | 说明 |
|---|---:|---|
| `automation.worker_enabled` | `true` | P3 默认启用本地 worker |
| `automation.poll_interval_ms` | `1000` | 空轮询间隔 |
| `automation.batch_size` | `10` | 每轮最多执行 job 数 |
| `automation.max_attempts` | `3` | 默认最大尝试次数 |
| `automation.retry_base_delay_ms` | `1000` | 指数退避基准 |

### 6.2 Job 生命周期

```text
pending
  -> running
  -> succeeded

running
  -> pending  (retry_count < max_retries)
  -> failed   (retry_count >= max_retries)

pending/running
  -> cancelled
```

Worker 流程：

```text
poll runnable pending jobs
  -> claim job in short transaction
  -> execute outside transaction
  -> persist result in short transaction
  -> enqueue next job if needed
  -> mark succeeded
```

claim 规则：

1. 只拉取 `status=pending and next_run_at <= now`。
2. 按 `priority asc, created_at asc` 排序。
3. claim 时把状态改为 `running` 并更新 `updated_at`。
4. 本地单进程 P3 不要求分布式锁。
5. 如果进程崩溃遗留 running job，启动时可把超时 running job 恢复为 pending。

running job 恢复规则：

1. 新增配置 `automation.running_timeout_ms`，默认 `300000`。
2. 进程启动时扫描 `status=running and updated_at < now - running_timeout_ms` 的 job。
3. 若 `retry_count < max_retries`，恢复为 `pending`，并设置 `next_run_at=now`。
4. 若 `retry_count >= max_retries`，标记 `failed`，错误摘要为 `WORKER_INTERRUPTED: stale running job`。
5. 恢复动作必须写日志，字段包含 `job_id/job_type/target_id/retry_count`。

P3 不引入分布式 lease 字段；`updated_at` 即本地单进程 worker 的最小 lease 时间来源。后续如果支持多 daemon 并发，再扩展 `claimed_by/claimed_until`。

### 6.3 Job 链路

`memory.observe` 成功写入 raw_event 后：

```text
enqueue extract_evidence(raw_event_id)
```

orphan raw_event 补偿：

1. 如果 raw_event 已写入但 enqueue 失败，`memory.observe` 可返回 `accepted=true`，但必须记录 `automation_enqueue_failed`。
2. P3 增加手动诊断/修复入口，扫描最近未关联 `extract_evidence` job 且未生成 evidence 的 raw_event。
3. 修复入口可复用 `memory.automation.status` 或新增 `memory.jobs.reconcile`，默认只 dry-run。
4. 自动周期扫描不作为 P3 必须交付，但手动 reconcile 能避免 best-effort enqueue 失败后完全不可恢复。

`extract_evidence` 成功后：

```text
write evidence[]
enqueue generate_memory_candidate(evidence_id) for each evidence
```

`generate_memory_candidate` 成功后：

```text
write memory_candidate[]
enqueue compute_admission(candidate_id) for each candidate
```

`compute_admission` 成功后：

```text
decision drop/write_raw_only
  -> mark candidate dropped

decision write_temporary/write_provisional/write_pending_review/write_stable
  -> write memory_item + evidence link + FTS
  -> mark candidate admitted

decision merge_existing/update_existing
  -> update existing memory or create relation
  -> mark candidate merged
```

### 6.4 失败处理

失败记录：

1. `async_job.last_error` 保存错误码和摘要。
2. `memory_candidate.status=failed` 仅用于候选阶段失败。
3. Provider 返回空结果不是失败，记录 reason code。
4. Admission drop 不是失败，记录 `admission_decision=drop`。

重试策略：

```text
next_run_at = now + retry_base_delay_ms * 2^retry_count
```

不可重试错误：

| 错误码 | 处理 |
|---|---|
| `VALIDATION_FAILED` | failed |
| `CONTENT_TOO_LARGE` | failed |
| `SCOPE_INVALID` | failed |
| `PROVIDER_DISABLED` | succeeded with skipped |

可重试错误：

| 错误码 | 处理 |
|---|---|
| `STORAGE_BUSY` | retry |
| `WORKER_INTERRUPTED` | retry |
| `PROVIDER_TEMPORARY_FAILURE` | retry，P3 rule_based 通常不会产生 |

## 7. Evidence Extraction

### 7.1 输入上下文

Worker 读取：

1. 当前 `raw_event`。
2. 所属 `agent_session`。
3. 所属 `agent_task`。
4. 同 task 最近 N 条相关 raw_event。
5. capture quality 摘要。

P3 不读取完整历史对话，不读取完整工具输出，不读取完整 diff。

### 7.2 Evidence 生成规则

Evidence 必须是“可解释证据”，不是候选记忆本身。

示例：

```json
{
  "source_type": "tool_output",
  "interpreted_statement": "认证测试失败集中在 token 过期边界判断。",
  "keywords": ["auth", "token expiry", "boundary"],
  "salient_spans": ["token 过期边界判断失败"],
  "source_ref": {
    "raw_event_id": "re_123",
    "tool_name": "go test",
    "exit_code": 1,
    "command_hash": "sha256:9f2c6a1b"
  },
  "confidence": 0.72
}
```

confidence 计算：

```text
confidence =
  clamp(
    0.50
    + source_type_bonus
    + capture_quality_bonus
    + structured_ref_bonus
    - ambiguity_penalty,
    0,
    1
  )
```

建议值：

| 因子 | 值 |
|---|---:|
| 用户显式声明 | `+0.30` |
| 用户纠正 | `+0.35` |
| Agent 决策摘要 | `+0.20` |
| 工具失败摘要 | `+0.10` |
| capture_level >= 3 | `+0.10` |
| 有 content_hash / target_event_id / file_path | `+0.10` |
| 语义不明确 | `-0.20` |
| capture_level <= 1 | `-0.15` |

### 7.3 不生成 Evidence 的情况

1. 普通成功工具输出没有错误、决策或复用价值。
2. 事件缺少摘要字段，仅有 hash。
3. `conversation.message` 只是普通闲聊或短期指令。
4. `file.edit.summary` 只描述代码结构事实，未包含设计原因或失败经验。
5. 内容边界检查发现疑似 full output/full diff/full prompt。

这些情况 job 应 `succeeded`，并在 payload 或 diagnostics 中记录：

```text
no_evidence_generated
insufficient_structured_signal
ordinary_success_output
code_structure_only
content_boundary_rejected
```

## 8. Candidate Generation

### 8.1 Candidate 生成原则

Candidate 是“可能值得写入 memory_item 的结构化草案”。它必须：

1. 有明确 memory_type。
2. 有明确 scope。
3. 有可检索 content。
4. 绑定至少一条 evidence。
5. 带 candidate reason。
6. 不保存完整原始内容。

### 8.2 Memory Type 规则

| 场景 | memory_type | 说明 |
|---|---|---|
| 用户偏好 | `preference` | 跨项目工作方式、沟通风格、工具偏好 |
| 需求和验收 | `requirement` | 用户明确提出的系统需求、阶段目标、验收条件 |
| 架构决策 | `decision` | 项目内技术选择和原因 |
| 约束 | `constraint` | 安全、数据、边界、兼容性约束 |
| 设计假设 | `assumption` | 架构、分期或实现方案成立的前提假设 |
| 开放问题 | `open_issue` | 待确认问题、未决风险、需要后续处理的设计点 |
| 失败经验 | `failure` | 可复用的错误模式和排查经验 |
| 项目事实 | `project_fact` | 当前项目稳定事实 |
| 流程方法 | `procedure` | 调试、发布、评审步骤 |
| 临时状态 | `temporary_state` | 当前 session/task 短期状态 |
| 会话摘要 | `session_summary` | session/task 结果摘要 |
| 设计复查 checkpoint | `review_checkpoint` | 复查结论和下次策略 |

P3 不新增 `common_knowledge`、`skill`、`theorem` 的自动写入。总体架构中提到的通用知识、行业技能和基础知识进入记忆系统，但 P3 仅保留数据模型方向；自动写入 `global_common` 放到后续专门设计，避免 P3 把项目上下文误泛化为通用知识。

P3 不新增以下细粒度类型，统一通过现有 type + tags/subtype 表达：

| 不新增类型 | P3 归类方式 |
|---|---|
| `incident` | 已发生且可复用的事故/错误归为 `failure` |
| `risk` | 未决风险归为 `open_issue`；已接受风险归为 `decision` |
| `api_contract` | 稳定接口事实归为 `project_fact`；强约束归为 `constraint` |
| `dependency` / `version` | 当前稳定依赖归为 `project_fact`，必要时设置 `valid_until` |
| `glossary` / `domain_term` | P3 归为 `project_fact`，后续知识库化再拆分 |
| `policy` | 用户习惯归为 `preference`，项目规则归为 `constraint`，流程规则归为 `procedure` |
| `todo` / `milestone` | 不做任务管理；仅跨会话关键未决事项归为 `open_issue` |

### 8.3 Scope 规则

| Candidate 来源 | 默认 scope |
|---|---|
| 用户全局偏好 | `user_global` |
| 项目需求和验收条件 | `project_local` |
| 项目架构决策 | `project_local` |
| 项目约束 | `project_local` |
| 项目设计假设 | `project_local` |
| 开放设计问题 | `project_local` 或 `repo_local` |
| 仓库失败经验 | `repo_local` |
| 文件编辑导致的短期状态 | `session` |
| 工具输出摘要 | `session` |
| task/session summary | `session` |
| 设计复查 checkpoint | `project_local` |

Scope validator 失败时，不写入 memory，candidate 标记 `failed`，job 标记 `failed`，错误码 `SCOPE_INVALID`。

### 8.4 Review Checkpoint Candidate

设计复查类会话识别条件：

1. `conversation.message`、`agent.response.summary` 或 `task.result` 包含设计复查意图。
2. keywords 命中：
   - `架构设计`
   - `分期规划`
   - `详细设计`
   - `复查`
   - `逻辑缺失`
   - `验收`
3. `source_refs` 包含文档路径。
4. task summary 包含 review/check/design/architecture 等语义。

生成内容：

1. `memory_item(memory_type=review_checkpoint)`。
2. `review_checkpoint` 结构化记录：
   - `checkpoint_type`
   - `review_intent_json`
   - `target_docs_json`
   - `target_sections_json`
   - `target_hashes_json`
   - `conclusion`
   - `confirmed_baseline_json`
   - `ignored_items_json`
   - `deferred_items_json`
   - `open_items_json`
   - `next_review_policy_json`

P3 只从事件中的结构化摘要生成 checkpoint，不读取并 hash 当前文档章节。文档章节 hash/diff-aware 复查属于 P4 `docindex` 范围。

### 8.5 自动 Checkpoint 事件 Schema

P3 自动生成 `review_checkpoint` 依赖 P2/P3 事件中的结构化字段，不从自然语言全文中猜测完整 checkpoint。

推荐 `source_refs` 最小 schema：

```json
{
  "target_docs": [
    {
      "path": "doc/The One 长期记忆系统 P3 详细设计.md",
      "role": "implementation_design",
      "content_hash": "optional_sha256",
      "modified_at": "optional_timestamp"
    }
  ],
  "review_intent": ["logic_consistency", "phase_consistency", "acceptance_completeness"],
  "conclusion": "has_minor_gap",
  "confirmed_baseline": ["P3 只做 rule_based Provider，不接外部 LLM"],
  "ignored_items": [],
  "deferred_items": ["文档 hash/diff-aware 复查放到 P4"],
  "open_items": ["确认自动写入 durable 规则"],
  "next_review_policy": {
    "focus": "major_logic_gap_and_new_diff",
    "read_strategy": "checkpoint_first_then_current_doc"
  }
}
```

字段规则：

1. `target_docs`、`review_intent`、`conclusion` 是生成 checkpoint candidate 的最小必填字段。
2. `confirmed_baseline/ignored_items/deferred_items/open_items/next_review_policy` 可为空，但字段存在时必须是摘要或结构化条目。
3. 缺少最小必填字段时，不生成 checkpoint candidate，job succeeded，reason code 记录 `checkpoint_schema_incomplete`。
4. `target_hashes_json` 在 P3 只接受事件已提供的 hash；P3 worker 不主动读取文件并计算章节 hash。

## 9. Admission Control

### 9.1 输入特征

P3 实现总体架构中的公式：

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

P3 必须先 clamp 再进入决策矩阵。

### 9.2 P3 特征估算

`future_need`：

```text
future_need =
  0.4 * repeated_topic_score
  + 0.3 * task_relevance
  + 0.2 * project_scope_relevance
  + 0.1 * user_preference_match
```

P3 的估算方式：

| 子项 | 估算方式 |
|---|---|
| `repeated_topic_score` | 同 project/repo 近 N 条 raw_event/candidate keyword 重合 |
| `task_relevance` | candidate 与当前 task summary / outcome keyword 重合 |
| `project_scope_relevance` | project_local/repo_local 且有 project_id/repo_id |
| `user_preference_match` | 用户显式声明或偏好类 candidate |

`encoding_depth_score`：

```text
encoding_depth_score = encoding_depth / 4
```

`stability`：

```text
stability =
  0.35 * source_count_score
  + 0.25 * time_span_score
  + 0.25 * confirmation_score
  + 0.15 * confidence
```

P3 中 `time_span_score` 可先用 0 或 0.2 保守值；多 session 巩固留给后续。

`task_control_signal`：

| 信号 | 分数 |
|---|---:|
| 用户显式“以后/记住/不要/必须” | `1.0` |
| 用户明确需求或验收条件 | `0.9` |
| 架构、安全、数据边界约束 | `0.8` |
| 待确认开放问题影响后续实现 | `0.7` |
| 影响当前任务继续 | `0.6` |
| 背景事实 | `0.3` |

`episodic_semantic_value`：

| 类型 | 分数 |
|---|---:|
| decision/constraint/requirement/review_checkpoint | `0.9` |
| preference/failure/procedure | `0.8` |
| assumption/open_issue | `0.7` |
| project_fact | `0.6` |
| session_summary | `0.4` |
| temporary_state | `0.3` |

`retrieval_trainability`：

```text
retrieval_trainability =
  min(1.0, 0.2 * retrieval_cue_count + 0.1 * keyword_count)
```

`interference_risk`：

| 风险 | 分数 |
|---|---:|
| 同 scope 近期候选过多 | `0.4` |
| 与 pending/provisional 高相似 | `0.3` |
| 内容存在多义 | `0.3` |

`decay_risk`：

| 类型 | 分数 |
|---|---:|
| 临时外部输入 | `1.0` |
| 命令输出摘要 | `0.8` |
| session state | `0.6` |
| project_fact | `0.4` |
| preference/decision/failure | `0.2` |

`conflict_risk`：

```text
conflict_risk =
  min(1.0, 0.4 * unresolved_conflict_count + 0.2 * low_confidence_source_count)
```

P3 只做基于同 scope FTS/LIKE 的轻量冲突查找，不做 P4 relation expansion。

### 9.2.1 Admission Feature Defaults

Admission 所有子分数必须归一化到 `[0,1]`，缺失字段使用固定默认值：

| 特征 | 缺失默认值 | 说明 |
|---|---:|---|
| `repeated_topic_score` | `0` | 无相关历史事件时不加分 |
| `task_relevance` | `0.3` | 有 task 但关键词缺失时使用保守默认 |
| `project_scope_relevance` | `0` | scope 字段缺失时由 validator 拒绝或不加分 |
| `user_preference_match` | `0` | 只有显式偏好才加分 |
| `encoding_depth_score` | `0.5` | encoding_depth 缺失时按中等深度 |
| `source_count_score` | `min(1, evidence_count / 3)` | 无 evidence 时 candidate invalid |
| `time_span_score` | `0.0` | P3 不做多 session 巩固 |
| `confirmation_score` | `1.0` for user_confirmed/user_declared，其他 `0` | 用户确认强加分 |
| `confidence` | candidate confidence，缺失 `0.7` | Provider 必须给默认 confidence |
| `retrieval_cue_count` | `0` | 无 cues 不加分 |
| `keyword_count` | `0` | 无 keywords 不加分 |
| `low_confidence_source_count` | evidence confidence < `0.5` 的数量 | 用于 conflict risk |

实现约束：

1. candidate 无 evidence 时不得进入 Admission，标记 `failed`，错误码 `VALIDATION_FAILED`。
2. `encoding_depth_score = clamp(encoding_depth / 4, 0, 1)`。
3. 所有 risk 分数也必须 clamp 到 `[0,1]`。
4. Admission 输出必须保存各分量到 `admission_reason_json`，至少包含最终 score、decision 和 reason codes。

### 9.3 决策矩阵

| 条件 | Admission decision |
|---|---|
| score < 0.30 | `drop` |
| 0.30 <= score < 0.50 | `write_temporary` 或 `write_raw_only` |
| 0.50 <= score < 0.70 | `write_provisional` |
| 0.70 <= score <= 0.85 | `write_pending_review` 或 `write_stable` |
| score > 0.85 | `write_stable`，高影响仍 review |

特殊规则优先于分数：

| Candidate 类型 | 特殊规则 |
|---|---|
| 用户显式声明 | 默认 `write_stable`，高影响约束进入 `write_pending_review` |
| 用户纠正 | 默认 `write_stable`，命中旧 memory 时原地覆盖 |
| 明确需求/验收条件 | 普通需求 `write_stable`，影响阶段边界或验收口径则 `write_pending_review` |
| 设计假设 | 默认 `write_provisional` 或 `write_pending_review`，不得直接作为 stable fact |
| 开放问题 | 默认 `write_pending_review`；仅 session 内短期问题 `write_temporary` |
| 架构决策 | `write_pending_review` |
| 安全约束 | `write_pending_review` |
| 高影响失败经验 | `write_pending_review` |
| 普通工具成功输出 | `drop` |
| 工具失败但不可复用 | `write_temporary` |
| 设计复查 checkpoint | `write_pending_review` |

### 9.4 Admission 输出

```json
{
  "decision": "write_pending_review",
  "admission_score": 0.78,
  "memory_type": "decision",
  "scope": "project_local",
  "initial_state": "pending_review",
  "initial_tier": "long_term",
  "decay_rate": 0.08,
  "requires_review": true,
  "reason_codes": [
    "architecture_decision",
    "has_decision_summary",
    "project_scoped",
    "high_impact_requires_review"
  ]
}
```

推荐 reason codes：

```text
user_declared
user_correction
requirement_declared
assumption_recorded
open_issue_recorded
architecture_decision
security_constraint
high_impact_failure
repeated_failure_signature
ordinary_success_output
session_only_state
insufficient_structured_signal
project_scoped
repo_scoped
scope_invalid
conflicts_with_stable_memory
high_impact_requires_review
provider_disabled
candidate_dropped_by_score
checkpoint_schema_incomplete
```

## 10. 自动写入策略

### 10.0 自动写入总规则

自动写入先走特殊规则，再走 Admission 分数矩阵。特殊规则用于保护长期记忆质量，分数矩阵只处理没有明确高影响语义的普通候选。

决策优先级：

```text
content boundary / scope validator
  -> explicit drop rule
  -> user correction
  -> user declared durable/stable rule
  -> high-impact review rule
  -> temporary/session-only rule
  -> score-based admission
```

写入矩阵：

| 来源/类型 | 条件 | 默认动作 | 说明 |
|---|---|---|---|
| 普通成功工具输出 | 无错误、无决策、无复用价值 | `drop` | 不写 memory，只保留 raw_event |
| 普通成功工具输出 | 承载当前任务状态摘要 | `write_temporary` | `temporary_state/session`，短 TTL |
| 单次工具失败 | 无重复、无可复用排查结论 | `write_temporary` | 只服务当前 session/task |
| 重复失败签名 | 同 repo/project 多次出现 | `write_provisional` | `failure`，短中期保留 |
| 明确需求/验收条件 | 用户声明或任务结果明确提出 | `write_stable` 或 `write_pending_review` | 普通需求可 stable，高影响需求进 review |
| 设计假设 | 表达“前置假设/默认认为/基于某条件” | `write_provisional` 或 `write_pending_review` | 不直接作为稳定事实注入 |
| 开放问题 | 表达“待确认/后续确认/开放风险/未决问题” | `write_pending_review` | session 内短期问题可 temporary |
| 架构决策 | 有 decision/reason summary | `write_pending_review` | 高影响，不直接 stable |
| 安全/数据边界约束 | 影响实现边界或安全策略 | `write_pending_review` | 高影响，不直接 stable |
| 设计复查 checkpoint | schema 完整 | `write_pending_review` | 用户确认后作为后续复查基线 |
| 用户显式偏好 | `user_global/preference` 且非高风险 | `write_stable` + `durable` | `user_confirmed=true` |
| 用户显式项目事实 | `project_local/project_fact` | `write_stable` + `long_term` | 若影响架构边界则转 review |
| 用户显式约束/决策 | `constraint/decision` 或高影响 | `write_pending_review` | 避免误把一次表达固化为强约束 |
| 用户纠正 | 指向旧 event/memory 或明确纠错语义 | `write_stable` | 原地覆盖旧 memory，保留 evidence/review 轨迹 |

高影响判断：

1. 改变架构、存储、中间件、安全、数据一致性、部署边界。
2. 会影响跨模块或跨 Agent 的后续实现决策。
3. 与现有 stable memory 冲突。
4. checkpoint 结论会影响后续设计复查基线。

低风险 durable 判断：

1. `source_type=user_declared` 或 `user_confirmed`。
2. `scope=user_global`。
3. `memory_type=preference`，或不绑定具体项目事实的跨项目 `procedure`。
4. 不包含安全、合规、架构强约束和团队能力评价等高影响内容。

### 10.1 write_temporary

写入：

| 字段 | 值 |
|---|---|
| `state` | `provisional` |
| `tier` | `temporary` |
| `scope` | `session` |
| `valid_until` | `now + retention.temporary_ttl_days` |

适用：

1. 当前任务临时状态。
2. 单次工具失败摘要。
3. session summary。

### 10.2 write_provisional

写入：

| 字段 | 值 |
|---|---|
| `state` | `provisional` |
| `tier` | `short_term` |
| `valid_until` | 可为空或 `now + short_term_ttl_days` |

适用：

1. 重复失败模式。
2. 有复用价值但证据不足的 procedure/failure。
3. 未被用户确认的项目事实。

### 10.3 write_pending_review

写入：

| 字段 | 值 |
|---|---|
| `state` | `pending_review` |
| `tier` | `long_term` |
| `user_confirmed` | `false` |

适用：

1. 架构决策。
2. 安全约束。
3. 高影响失败经验。
4. 设计复查 checkpoint。
5. 与 stable 记忆存在冲突的候选。

pending_review 可以被 `memory.search/context` 召回，但必须标记未确认，不得作为强约束注入。

### 10.4 write_stable

写入：

| 字段 | 值 |
|---|---|
| `state` | `stable` |
| `tier` | `long_term` 或 `durable` |
| `user_confirmed` | 用户声明或纠正时为 `true` |

适用：

1. 用户显式声明的普通偏好。
2. 用户纠正后的稳定事实。
3. 用户明确确认的 candidate。

`durable` 规则：

| 场景 | tier |
|---|---|
| `user_global/preference` 且来源为用户显式声明 | `durable` |
| `user_global/procedure` 且为跨项目工作方法 | `durable` |
| 用户确认后的安全约束、架构决策 | `durable` 或 `long_term`，由 review 操作决定 |
| 用户纠正后的项目事实 | `long_term`，除非用户明确要求持久保留 |
| `review_checkpoint` | 默认 `long_term`；冻结基线类 checkpoint 经用户确认后可提升 `durable` |

实现上 `write_stable` 必须显式写入 `tier`，不能只依赖 P1 `defaultStateAndTier`。自动写入 service 应把 Admission 输出作为唯一来源，避免同一 candidate 因入口不同得到不同生命周期。

### 10.5 merge_existing / update_existing

P3 只实现保守版本：

1. 同 scope/type/content 完全重复：复用现有 memory，新增 evidence link。
2. 用户纠正指向旧 memory：原地覆盖旧 memory，保留旧 `memory_id`，新增 correction evidence link 和 automation review 记录。
3. 相似但不确定：新 candidate 进入 pending_review，不自动 merge。

## 11. 用户纠正和覆盖

### 11.1 输入来源

用户纠正来源：

1. `raw_event.event_type=user.correction`。
2. `source_refs.target_event_id` 指向原始事件。
3. `source_refs.target_memory_id` 指向旧记忆。
4. `content_summary` 表示纠正后的事实。

### 11.2 处理流程

```text
user.correction raw_event
  -> extract evidence(source_type=user_confirmed)
  -> generate corrected candidate
  -> admission write_stable
  -> find target memory
  -> overwrite target memory in place
  -> write memory_review record
```

旧记忆处理：

| 场景 | 动作 |
|---|---|
| 明确 target_memory_id | 原地覆盖该 memory 的 content/search_text/type 元数据，`state=stable`，`user_confirmed=true`，`version+1` |
| 只有 target_event_id | 通过 evidence link 找旧记忆，找到后同上 |
| 未找到旧记忆 | 写新 stable，candidate reason 加 `target_not_found` |

覆盖后旧内容不再作为默认检索结果存在；旧内容差异通过 `memory_review.original_content/edited_content` 和 correction evidence 追溯。

### 11.3 纠正覆盖语义

P3 采用“纠正即覆盖”的单记忆语义，避免同一事实同时存在新旧两条 stable/pending memory：

1. `memory_id` 不变，避免已注入上下文、review 记录和外部引用失效。
2. `memory_item.version += 1`，`state=stable`，`user_confirmed=true`。
3. 新 correction evidence 通过 `memory_evidence_link(relation_type=corrected_by)` 追加到旧 memory。
4. `memory_review` 必须记录 `original_content` 和 `edited_content`，用于解释纠正历史。
5. `search_text` 和 FTS 条目必须在同一事务内更新，防止旧事实继续被默认召回。

`memory_relation.supersedes/superseded_by` 保留为 repository 能力和后续兼容边，但 P3 自动用户纠正链路不再默认创建新 memory 或 supersedes relation。

## 12. Review Queue

P3 复用 `memory.review`：

1. `list` 查看 pending_review。
2. `approve` 将 pending_review 变 stable。
3. `reject` 拒绝候选记忆。
4. `edit` 编辑内容后 stable 或 pending_review。
5. `archive/delete` 保持 P1 语义。

P3 新增自动来源字段使用约定：

| 字段 | 规则 |
|---|---|
| `source_type` | Provider 对应 evidence source type |
| `created_by` | `automation:rule_based` |
| `source_quality` | 由 capture quality、provider 和 source_type 计算 |
| `confidence` | evidence/candidate confidence |
| `importance` | Admission 估算值 |

Review approve 必须写入 `memory_review` 记录，并更新最小访问/确认聚合字段；完整 `memory_access_log(user_confirmed)` 留到 P4。

## 13. Retention

### 13.1 P3 Retention 范围

P3 实现基础 Retention：

1. temporary 过期清理。
2. retention score 重算。
3. tier 更新。
4. archive 扫描的最小诊断。

P3 不实现：

1. P4 完整 `memory_access_log` score breakdown。
2. relation expansion 对 retention 的复杂强化。
3. embedding 或 semantic similarity 强化。

### 13.1.1 P3 最小访问聚合

分期规划要求 P3 retention 能使用基础访问和强化信息，但完整 `memory_access_log`、retrieval trace 和 score breakdown 属于 P4。P3 采用最小聚合字段，不新增完整访问日志表。

P3 可更新的已有字段：

| 字段 | 触发时机 |
|---|---|
| `last_accessed_at` | `memory.search/context` 返回并进入结果集时 |
| `reinforcement_count` | 用户 approve、edit 后 stable，或同一事实重复 evidence 支持时 |
| `effective_reinforcement` | retention job 基于 confirmation、重复 evidence、近期访问计算 |
| `last_reinforced_at` | `reinforcement_count` 增加时 |
| `last_validated_at` | 用户确认、纠正或 review approve 时 |

约束：

1. P3 不要求记录每次检索明细，不提供完整 access log 查询。
2. 如果某入口尚未接入访问聚合，`reinforcement_factor` 可退化为 0，不影响 P3 验收。
3. P4 引入 `memory_access_log` 后，可以用明细回填或替代这些聚合字段，但不得改变 P3 retention 的基本语义。

### 13.2 Retention Score

P3 使用简化公式：

```text
retention_score =
  clamp(
    0.35 * importance
    + 0.25 * confidence
    + 0.20 * confirmation_factor
    + 0.10 * reinforcement_factor
    + 0.10 * recency_factor
    - decay_penalty,
    0,
    1
  )
```

`confirmation_factor`：

| 状态 | 值 |
|---|---:|
| user_confirmed stable | `1.0` |
| stable | `0.8` |
| pending_review | `0.4` |
| provisional | `0.3` |
| temporary | `0.1` |

`reinforcement_factor`：

```text
min(1.0, effective_reinforcement / 5)
```

`recency_factor`：

```text
1.0 if updated within 7 days
0.6 if updated within 30 days
0.3 if updated within 90 days
0.1 otherwise
```

`decay_penalty`：

| tier | 值 |
|---|---:|
| temporary | `0.40` |
| short_term | `0.20` |
| long_term | `0.05` |
| durable | `0.00` |

### 13.3 Tier 更新

| 条件 | tier |
|---|---|
| temporary 且未过期 | `temporary` |
| score < 0.30 | `short_term` 或 archive candidate |
| 0.30 <= score < 0.60 | `short_term` |
| 0.60 <= score <= 0.85 | `long_term` |
| score > 0.85 且 user_confirmed | `durable` |

P3 不自动删除 stable/durable，只自动清理过期 temporary。

Tier 更新约束：

1. `pinned=true` 的 memory 不自动降 tier。
2. `state=pending_review` 的 memory 不自动提升到 durable。
3. `review_checkpoint` 只有用户 approve 后才允许提升 durable。
4. retention job 对 stable/durable 只更新 `retention_score/tier/updated_at`，不得改写 content。

### 13.4 Cleanup Temporary

流程：

```text
scan memory_item
  where tier='temporary'
  and valid_until < now
  and pinned=false
  and user_confirmed=false

for each memory:
  set state='archived', tier='archived'
  remove FTS entry
  write cleanup diagnostic
```

不做 hard delete，除非用户显式 delete。

## 14. Delete Consistency

P3 在 P1 delete 基础上新增 `delete_consistency` job：

检查项：

1. `memory_item.state=deleted`。
2. FTS entry 不存在。
3. `memory_relation` 中无 source/target 指向该 memory。
4. `memory_candidate.resulting_memory_id` 可保留诊断引用，但不得被 context 注入。
5. `memory_evidence_link` 不再使 deleted memory 被召回。
6. 如果存在 `memory_tombstone`，content hash 与删除时记录一致。

失败时：

1. 可自动修复 FTS 和 relation 残留。
2. 不能修复时 job failed，诊断返回 `DELETE_CONSISTENCY_FAILED`。
3. 对 evidence 内容不做自动 hard delete；敏感误入库必须走用户显式 delete 或后续敏感删除流程。

## 15. MCP 诊断工具

### 15.1 memory.jobs.list

请求：

```json
{
  "status": "failed",
  "job_type": "extract_evidence",
  "target_type": "raw_event",
  "limit": 50
}
```

响应：

```json
{
  "jobs": [
    {
      "job_id": "job_001",
      "job_type": "extract_evidence",
      "target_type": "raw_event",
      "target_id": "re_001",
      "status": "failed",
      "retry_count": 3,
      "last_error": "CONTENT_TOO_LARGE: evidence draft exceeded limit",
      "next_run_at": "2026-05-24T00:00:00Z",
      "created_at": "2026-05-24T00:00:00Z",
      "updated_at": "2026-05-24T00:00:00Z"
    }
  ]
}
```

### 15.2 memory.jobs.get

请求：

```json
{
  "job_id": "job_001"
}
```

返回 job 详情、payload 摘要、错误摘要和 target 引用。

### 15.3 memory.candidates.list

请求：

```json
{
  "status": "dropped",
  "memory_type": "failure",
  "workspace_id": "ws",
  "project_id": "project_a",
  "limit": 50
}
```

响应包含：

1. candidate id。
2. provider。
3. memory_type/scope。
4. content 摘要。
5. admission_score。
6. admission_decision。
7. reason codes。
8. resulting_memory_id。

### 15.4 memory.candidates.get

请求：

```json
{
  "candidate_id": "cand_001"
}
```

返回 candidate 详情、evidence 引用、Admission 结果和最终写入记忆。

### 15.5 memory.retention.run

P3 提供手动触发入口，方便验收：

```json
{
  "mode": "cleanup_temporary",
  "dry_run": true,
  "limit": 100
}
```

`dry_run=true` 只返回将要处理的 memory id 和原因，不改写数据库。

### 15.6 memory.automation.status

返回：

1. worker 是否启用。
2. provider 名称。
3. pending/running/failed job 数。
4. 最近一次 job 执行时间。
5. 最近错误摘要。
6. retention 配置摘要。

### 15.7 诊断工具通用约束

P3 诊断工具必须遵守与业务接口一致的 scope 边界：

1. `memory.jobs.list` 至少需要 `workspace_id` 或明确 `target_id`。
2. `memory.candidates.list` 按 project/repo 查询时必须传 `workspace_id`。
3. `memory.candidates.get` 返回 content 摘要、evidence id、raw_event id 和 reason code，不返回完整 raw_event 或完整 source_refs。
4. `memory.jobs.get` 返回 payload 摘要和 hash，不返回完整 payload。
5. 默认 `limit <= 100`，超过上限自动截断并在 diagnostics 中标记。
6. 诊断工具不得跨 workspace 全表扫描；管理员级全局诊断留到后续版本。

### 15.8 memory.jobs.reconcile

P3 可提供手动补偿入口，用于修复 raw_event 写入成功但 enqueue 失败的情况：

```json
{
  "workspace_id": "ws",
  "project_id": "project_a",
  "mode": "orphan_raw_event",
  "dry_run": true,
  "limit": 100
}
```

行为：

1. 查找没有对应 `extract_evidence` job 且没有 evidence 的 raw_event。
2. `dry_run=true` 只返回候选 raw_event id 和原因。
3. `dry_run=false` 为候选 raw_event 补充 enqueue `extract_evidence`。
4. 对 `provider=none` 或 `processor.enable_auto_processing=false` 的环境返回 `provider_disabled`，不视为失败。

## 16. 配置项

新增配置结构：

```go
type Config struct {
	Storage    StorageConfig    `yaml:"storage" json:"storage"`
	Server     ServerConfig     `yaml:"server" json:"server"`
	Logging    LoggingConfig    `yaml:"logging" json:"logging"`
	Memory     MemoryConfig     `yaml:"memory" json:"memory"`
	Capture    CaptureConfig    `yaml:"capture" json:"capture"`
	Retrieval  RetrievalConfig  `yaml:"retrieval" json:"retrieval"`
	Embedding  EmbeddingConfig  `yaml:"embedding" json:"embedding"`
	Retention  RetentionConfig  `yaml:"retention" json:"retention"`
	Processor  ProcessorConfig  `yaml:"processor" json:"processor"`
	Automation AutomationConfig `yaml:"automation" json:"automation"`
}

type ProcessorConfig struct {
	Provider              string `yaml:"provider" json:"provider"`
	EnableAutoProcessing  bool   `yaml:"enable_auto_processing" json:"enable_auto_processing"`
	MaxRelatedEvents       int    `yaml:"max_related_events" json:"max_related_events"`
	MaxCandidatesPerEvent  int    `yaml:"max_candidates_per_event" json:"max_candidates_per_event"`
}

type AutomationConfig struct {
	WorkerEnabled     bool `yaml:"worker_enabled" json:"worker_enabled"`
	PollIntervalMS    int  `yaml:"poll_interval_ms" json:"poll_interval_ms"`
	BatchSize         int  `yaml:"batch_size" json:"batch_size"`
	MaxAttempts       int  `yaml:"max_attempts" json:"max_attempts"`
	RetryBaseDelayMS  int  `yaml:"retry_base_delay_ms" json:"retry_base_delay_ms"`
	RunningTimeoutMS  int  `yaml:"running_timeout_ms" json:"running_timeout_ms"`
}
```

默认值：

```go
Processor: ProcessorConfig{
	Provider:             "rule_based",
	EnableAutoProcessing: true,
	MaxRelatedEvents:      20,
	MaxCandidatesPerEvent: 3,
},
Automation: AutomationConfig{
	WorkerEnabled:    true,
	PollIntervalMS:   1000,
	BatchSize:        10,
	MaxAttempts:      3,
	RetryBaseDelayMS: 1000,
	RunningTimeoutMS: 300000,
},
```

状态输出 `memory.status --include-config` 应包含非敏感摘要：

```text
processor.provider=rule_based
processor.enable_auto_processing=true
automation.worker_enabled=true
automation.pending_jobs=0
automation.failed_jobs=0
```

## 17. Repository 和 Service 接口

### 17.1 automation.Repository

```go
type Repository interface {
	EnqueueJob(ctx context.Context, job AsyncJob) (AsyncJob, bool, error)
	ClaimJobs(ctx context.Context, now time.Time, limit int) ([]AsyncJob, error)
	RecoverStaleRunningJobs(ctx context.Context, now time.Time, timeout time.Duration) (int, error)
	MarkJobSucceeded(ctx context.Context, jobID string, payload string, now time.Time) error
	MarkJobRetry(ctx context.Context, jobID string, retryCount int, nextRunAt time.Time, lastError string, now time.Time) error
	MarkJobFailed(ctx context.Context, jobID string, lastError string, now time.Time) error

	GetRawEvent(ctx context.Context, rawEventID string) (capture.RawEvent, error)
	GetSession(ctx context.Context, sessionID string) (capture.AgentSession, error)
	GetTask(ctx context.Context, taskID string) (capture.AgentTask, error)
	ListRelatedEvents(ctx context.Context, req RelatedEventsRequest) ([]capture.RawEvent, error)

	FindDuplicateEvidence(ctx context.Context, draft EvidenceDraftKey) (memory.Evidence, bool, error)
	WriteEvidence(ctx context.Context, evidence memory.Evidence) error
	WriteCandidate(ctx context.Context, candidate MemoryCandidateRecord) error
	UpdateCandidateAdmission(ctx context.Context, candidateID string, admission AdmissionResult, status string, memoryID string) error
	FindRelatedMemory(ctx context.Context, req RelatedMemoryRequest) ([]memory.MemoryItem, error)
	WriteAutomatedMemory(ctx context.Context, input AutomatedMemoryWrite) (memory.MemoryItem, error)
	WriteMemoryRelation(ctx context.Context, relation memory.MemoryRelation) error
	ListJobs(ctx context.Context, req ListJobsRequest) ([]AsyncJob, error)
	ListCandidates(ctx context.Context, req ListCandidatesRequest) ([]MemoryCandidateRecord, error)
	ListOrphanRawEvents(ctx context.Context, req OrphanRawEventRequest) ([]capture.RawEvent, error)
}
```

### 17.2 automation.Service

```go
type Service struct {
	cfg      config.Config
	repo     Repository
	provider processor.Provider
}

func (s *Service) EnqueueRawEvent(ctx context.Context, rawEvent capture.RawEvent) error
func (s *Service) RunJob(ctx context.Context, job AsyncJob) error
func (s *Service) RunRetention(ctx context.Context, req RetentionRunRequest) (RetentionRunResponse, error)
func (s *Service) ListJobs(ctx context.Context, req ListJobsRequest) (ListJobsResponse, error)
func (s *Service) ListCandidates(ctx context.Context, req ListCandidatesRequest) (ListCandidatesResponse, error)
func (s *Service) Status(ctx context.Context) (AutomationStatusResponse, error)
func (s *Service) Reconcile(ctx context.Context, req ReconcileRequest) (ReconcileResponse, error)
```

### 17.3 capture.Service 变更

P3 对 `capture.Service` 增加可选 automation dependency：

```go
type JobEnqueuer interface {
	EnqueueRawEvent(ctx context.Context, rawEvent capture.RawEvent) error
}
```

`Observe` 写入 raw_event 后，可以在同一短事务内 enqueue job，也可以在 raw_event 写入成功后 best-effort enqueue。P3 不要求强投递一致性；如果 enqueue 失败，不回滚 raw_event，但必须返回或记录 `automation_enqueue_failed` 诊断，便于人工排查。

可选 repository 方法：

```go
InsertRawEventAndEnqueueJob(ctx context.Context, event RawEvent, job AsyncJob) error
```

当 `processor.enable_auto_processing=false` 或 `provider=none` 时，只写 raw_event，不 enqueue。

## 18. 事务边界

| 操作 | 事务边界 |
|---|---|
| `memory.observe` | upsert session/task + insert raw_event；enqueue extract_evidence 可同事务或事务后 best-effort |
| `extract_evidence` | insert evidence + enqueue generate candidate |
| `generate_memory_candidate` | insert candidate + enqueue admission |
| `compute_admission` | update candidate + write memory/evidence link/FTS/review checkpoint；用户纠正命中旧 memory 时原地覆盖并写 review |
| `memory.review approve/reject/edit` | 沿用 P1，同事务写 review 和状态 |
| `cleanup_temporary` | 分页处理，每批短事务 |
| `delete_consistency` | 单 memory 一事务 |

禁止在事务内执行：

1. Provider 外部调用。
2. 长文本摘要。
3. 网络请求。
4. 大批量扫描。
5. 长时间 sleep 或重试等待。

P3 一期 `rule_based` Provider 虽然本地执行，也按事务外执行处理，为二期 LLM Provider 保持一致边界。

## 19. 错误码

| 错误码 | 场景 | Retryable |
|---|---|---|
| `JOB_NOT_FOUND` | 查询不存在 job | false |
| `CANDIDATE_NOT_FOUND` | 查询不存在 candidate | false |
| `PROVIDER_NOT_FOUND` | 配置了未知 provider | false |
| `PROVIDER_DISABLED` | provider=none 或自动处理关闭 | false |
| `PROVIDER_FAILED` | Provider 执行失败 | true |
| `ADMISSION_FAILED` | Admission 输入无效或计算失败 | false |
| `AUTOMATION_WRITE_FAILED` | 自动写入 memory 失败 | true |
| `DELETE_CONSISTENCY_FAILED` | 删除一致性无法自动修复 | true |
| `RETENTION_FAILED` | retention job 执行失败 | true |
| `ASYNC_JOB_CONFLICT` | job claim 或状态更新冲突 | true |

现有错误码继续沿用：

```text
VALIDATION_FAILED
CONTENT_TOO_LARGE
SCOPE_INVALID
STORAGE_BUSY
MEMORY_NOT_FOUND
STATE_CONFLICT
FTS_UNAVAILABLE
SESSION_NOT_FOUND
TASK_NOT_FOUND
```

## 20. 日志设计

P3 新增结构化日志：

| 事件 | 字段 |
|---|---|
| job enqueue | `job_id/job_type/target_type/target_id/deduped` |
| job start | `job_id/job_type/retry_count/provider` |
| job success | `job_id/job_type/duration_ms/result_count` |
| job failed | `job_id/job_type/error_code/retry_count/next_run_at` |
| evidence extracted | `raw_event_id/evidence_count/provider` |
| candidate generated | `evidence_id/candidate_count/provider` |
| admission decided | `candidate_id/decision/score/reason_codes` |
| automated memory written | `memory_id/memory_type/scope/state/tier` |
| retention cleanup | `memory_id/action/reason` |

日志不得输出：

1. 完整 content。
2. 完整 evidence statement。
3. 完整 source_refs。
4. 完整工具输出。
5. 完整 diff。

必要时输出 hash 或 id。

## 21. 测试设计

### 21.1 Provider 单元测试

覆盖：

1. `user.declaration` 生成 preference/constraint candidate。
2. `user.correction` 生成 user_confirmed evidence 和 corrected candidate。
3. `agent.decision` 生成 decision candidate。
4. `tool.result.summary` 成功输出不生成 candidate。
5. `tool.result.summary` 失败输出生成 failure/temporary candidate。
6. `file.edit.summary` 只有代码结构事实时不生成长期 memory。
7. 设计复查事件生成 `review_checkpoint` candidate。
8. 输入不足时返回空结果和 reason code。

### 21.2 Admission 单元测试

覆盖：

1. 分数 clamp。
2. 用户显式声明进入 stable。
3. 架构决策进入 pending_review。
4. 安全约束进入 pending_review。
5. 普通成功工具输出 drop。
6. 工具失败临时信息进入 temporary。
7. 重复失败进入 provisional。
8. 冲突候选进入 pending_review。
9. reason codes 完整可解释。

### 21.3 Repository 测试

覆盖：

1. `async_job` migration 幂等。
2. job enqueue dedup。
3. claim pending job 顺序。
4. retry/failed 状态流转。
5. stale running job 恢复。
6. orphan raw_event 查询。
7. evidence 自动写入和去重。
8. candidate 写入和 admission update。
9. 自动 memory 写入同时更新 FTS。
10. review_checkpoint 自动写入。
11. memory_relation supersedes 写入。
12. delete consistency 清理 FTS/relation。

### 21.4 Worker 测试

覆盖：

1. raw_event 到 evidence 的 job 链路。
2. evidence 到 candidate 的 job 链路。
3. candidate 到 memory_item 的 admission 链路。
4. Provider 返回空结果时 job succeeded。
5. Provider 错误时 retry。
6. 超过 max_retries 后 failed。
7. worker context cancellation 后安全退出。
8. stale running job 启动恢复。

### 21.5 MCP 工具测试

覆盖：

1. `memory.jobs.list` 按 status/job_type 查询。
2. `memory.jobs.get` 返回错误摘要。
3. `memory.candidates.list` 返回 dropped/admitted。
4. `memory.candidates.get` 返回 admission reason。
5. `memory.automation.status` 返回 provider 和 job 统计。
6. `memory.retention.run dry_run` 不改写数据库。
7. `memory.retention.run dry_run=false` 清理 temporary。
8. `memory.jobs.reconcile dry_run` 返回 orphan raw_event，不写 job。

### 21.6 集成验收

验收脚本建议：

```text
make test-p3-sqlite
```

脚本流程：

1. 启动临时 SQLite 数据库。
2. 通过 `memory.observe` 写入 session.start。
3. 写入用户声明事件。
4. 等待 worker 处理。
5. 查询 memory.search，确认 stable preference 可检索。
6. 写入架构决策事件。
7. 等待 worker 处理。
8. 查询 memory.review，确认 pending_review decision。
9. approve 后确认 state=stable。
10. 写入普通成功工具输出，确认 candidate dropped 或未生成。
11. 写入失败工具输出，确认 failure/temporary candidate。
12. 写入用户纠正，确认旧 memory 被原地覆盖且 review/evidence 轨迹完整。
13. 写入设计复查结果，确认 review_checkpoint candidate。
14. 手动触发 retention dry_run 和 cleanup。
15. 执行 P1/P2 回归测试。

### 21.7 P3 Golden Set

P3 应新增最小自动写入 golden set，验证 Admission 和自动写入质量，而不只验证链路跑通。

每条用例包含：

```json
{
  "case_id": "ordinary_success_output_drop",
  "raw_event": {
    "event_type": "tool.result.summary",
    "output_summary": "go test ./... passed",
    "source_refs": {"exit_code": 0}
  },
  "expected_candidates": 0,
  "expected_decision": "drop",
  "forbidden_memory_types": ["project_fact", "decision", "failure"]
}
```

P3 golden set 至少覆盖：

1. 用户偏好 stable。
2. 架构决策 pending_review。
3. 普通成功工具输出 drop。
4. 单次失败 temporary。
5. 重复失败 provisional。
6. 用户纠正 stable + 原地覆盖旧 memory。
7. checkpoint schema 完整时生成 review_checkpoint。
8. checkpoint schema 不完整时 reason code 为 `checkpoint_schema_incomplete`。
9. scope invalid 时 candidate failed 且不写 memory。

## 22. 验收标准

| 验收项 | 标准 |
|---|---|
| 自动 evidence 抽取 | `raw_event` 能生成可解释 `evidence` |
| 自动候选生成 | 工具失败事件能生成 failure 或 temporary candidate |
| Admission 生效 | 普通成功工具输出不进入长期记忆 |
| Review 生效 | 架构决策进入 pending_review |
| 用户声明生效 | 普通用户偏好可自动 stable |
| 用户纠正生效 | 旧 memory 原地更新为 stable/user_confirmed，version 增加，review/evidence 轨迹完整 |
| Retention 生效 | temporary 默认 5 天策略可计算并可清理 |
| 删除一致性 | deleted memory 不被 FTS 检索召回 |
| checkpoint 自动生成 | 设计复查会话结束后能生成 `review_checkpoint` candidate |
| 异步可观测 | 可区分 dropped、pending、running、failed、succeeded |
| Provider 架构 | `rule_based` 作为 Provider 接入，`none` 可关闭自动处理 |
| Golden set | P3 自动写入 golden set 通过 |
| P1/P2 回归 | 手动记忆和 capture 测试仍通过 |

P3 退出条件：

```text
系统具备基于结构化事件的自动长期记忆闭环，能够解释为什么写入、为什么丢弃、为什么需要 review，并能清理临时记忆；外部 LLM 抽取能力已通过 Provider 接口预留到二期。
```

## 23. 协作任务拆分

| 任务 ID | 任务 | 输入 | 输出 | 可并行 |
|---|---|---|---|---|
| P3-B1 | automation schema migration | P2 migration | `0005_init_automation.sql` | 否 |
| P3-D1 | processor DTO 和 Provider 接口 | 本设计 Provider 章节 | `internal/processor` types | 是 |
| P3-D2 | rule_based Provider | D1、事件规范 | evidence/candidate rule implementation | 依赖 D1 |
| P3-D3 | Admission Controller | Admission 公式 | score/decision/reason codes | 是 |
| P3-B2 | async repository | B1 | job CRUD、candidate CRUD | 依赖 B1 |
| P3-B3 | automated memory repository | B1、P1 repository | write memory/evidence/link/FTS/relation | 依赖 B1 |
| P3-C1 | automation service | D2/D3/B2/B3 | job handler 链路 | 依赖 B2/B3 |
| P3-C2 | worker runtime | C1 | polling、claim、retry、shutdown | 依赖 C1 |
| P3-C3 | capture observe enqueue | C1/P2 capture | raw_event + async_job best-effort enqueue | 依赖 B2 |
| P3-C4 | MCP diagnostics | C1 | jobs/candidates/status/retention tools | 依赖 C1 |
| P3-C5 | orphan reconcile | C1/B2 | jobs.reconcile 和 raw_event 补偿 | 依赖 C1 |
| P3-R1 | retention service | B3 | score/tier/cleanup | 可并行 |
| P3-E1 | Provider 和 Admission 单元测试 | D2/D3 | rule/admission tests | 依赖 D2/D3 |
| P3-E2 | repository/worker 测试 | B2/B3/C2 | async tests | 依赖 C2 |
| P3-E3 | MCP 工具测试 | C4 | diagnostics tests | 依赖 C4 |
| P3-E4 | P3 验收脚本和 golden set | 全部 | `make test-p3-sqlite` | 依赖全部 |

## 24. 合并顺序建议

```text
P3-B1
  -> P3-D1 + P3-D3
  -> P3-D2
  -> P3-B2 + P3-B3
  -> P3-C1
  -> P3-C2
  -> P3-C3
  -> P3-C4 + P3-C5 + P3-R1
  -> P3-E1 + P3-E2 + P3-E3
  -> P3-E4
  -> P3 release
```

## 25. P3 Done 定义

1. `memory.observe` 成功写入 raw_event 时可 enqueue `extract_evidence` job。
2. `rule_based` Provider 通过 Provider 接口工作。
3. `provider=none` 可关闭自动处理。
4. evidence 自动抽取写入 `evidence.raw_event_id`。
5. candidate 自动生成并写入 `memory_candidate`。
6. Admission 输出 score、decision 和 reason codes。
7. 用户显式声明可自动 stable。
8. 架构决策、安全约束、高影响失败进入 pending_review。
9. 普通成功工具输出不会进入长期记忆。
10. 用户纠正能直接覆盖旧 memory，并保留 correction evidence 与 review 轨迹。
11. 设计复查事件能生成 review_checkpoint candidate。
12. temporary retention 可 dry-run、可执行清理。
13. delete consistency job 能发现或修复 FTS/relation 残留。
14. `memory.jobs.*` 和 `memory.candidates.*` 能解释 pending/running/failed/dropped/admitted。
15. worker 失败不阻塞 Agent 主流程。
16. stale running job 可恢复或失败可诊断。
17. orphan raw_event 可通过 reconcile dry-run 发现，并可手动补 enqueue。
18. P3 自动写入 golden set 通过。
19. P1、P2 回归测试仍通过。
20. P3 验收脚本通过。
21. 文档明确标注二期 LLM Provider 扩展范围。

## 26. 主要风险和控制点

| 风险 | 影响 | 控制点 |
|---|---|---|
| 自动写入污染长期记忆 | 检索注入错误事实 | Admission 保守、pending_review、temporary 默认短 TTL |
| rule_based 抽取质量有限 | 高价值记忆漏写 | 宁可少写；二期 LLM Provider 增强 |
| Provider 边界过宽 | 二期接 LLM 时污染事务和写入策略 | Provider 只产 draft/candidate，不写库、不决策 |
| job enqueue 失败 | raw_event 暂未自动处理 | P3 best-effort enqueue，记录 `automation_enqueue_failed`，不阻塞 Agent 主流程 |
| 异步失败不可见 | 用户不知道为何没生成记忆 | jobs/candidates 诊断工具 |
| 用户纠正未覆盖旧记忆 | 后续仍注入错误事实 | target_memory_id/target_event_id、原地覆盖、review 轨迹 |
| temporary 清理误删长期价值 | 丢失有用经验 | temporary 只 archive 不 hard delete，stable/durable 不自动删 |
| checkpoint 误代替文档事实 | 复查结论过期 | checkpoint 只作历史上下文压缩，P4 再做 doc hash/diff |
| 外部 LLM 过早引入 | 本地可用性和测试稳定性下降 | P3 只预留 Provider，二期实现具体模型 |
