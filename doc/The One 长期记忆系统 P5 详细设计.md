# The One 长期记忆系统 P5 详细设计

> 基线来源：
> - `The One 长期记忆系统总体架构设计.md` v0.1 冻结版
> - `The One 长期记忆系统分期迭代研发规划.md`
> - `The One 长期记忆系统 P0-P1 详细设计.md`
> - `The One 长期记忆系统 P2 详细设计.md`
> - `The One 长期记忆系统 P3 详细设计.md`
> - `The One 长期记忆系统 P4 详细设计.md`
> - `当前工程阶段实现状态.md`
>
> 前置说明：P5 是一期 MVP 验收和发布收口阶段，不重新设计 Memory Engine 核心语义。P5 的工程实现必须以 P0-P4 回归通过为前置门槛；若 P4 检索增强、capture diagnostics、automation diagnostics 或 doc/code diagnostics 存在偏差，P5 开工前必须先更新本文档的前置假设和验收口径。

## 1. 设计目标

P5 目标：

```text
让 Codex、Claude Code、Cursor 共享同一个本地 Memory Daemon，并用可重复、可度量、可诊断的 10 个 MVP 验收任务证明一期长期记忆系统达到本地个人工具 MVP 标准。
```

P5 完成后的业务闭环：

```text
MVP Acceptance Runner
  -> start / reuse memoryd
  -> run baseline and candidate tasks
  -> collect agent session / task / raw_event
  -> wait or reconcile async jobs
  -> run memory.search / memory.context
  -> collect retrieval_trace / access_log
  -> collect task outcome and user corrections
  -> compute MVP metrics
  -> generate acceptance report
```

多 Agent 共享闭环：

```text
Agent A session
  -> observe / remember / automation
  -> stable memory + evidence + trace
Agent B session
  -> context/search same project/repo scope
  -> retrieve Agent A memory
Agent C session
  -> context/search same project/repo scope
  -> verify decision/evidence/checkpoint/code_ref
```

P5 的核心交付不是引入新的智能能力，而是将 P0-P4 已完成能力转化为可执行的 MVP 验收体系：验收编排、指标采集、报告生成、三 Agent 接入验证、Level4 覆盖统计和发布前质量门禁。

## 2. 阶段边界

### 2.1 必须交付

1. P5 MVP 验收数据模型和 repository：
   - `mvp_acceptance_run`
   - `mvp_acceptance_task`
   - `mvp_metric_sample`
   - `mvp_agent_capability`
2. MVP 指标采集服务：
   - token savings。
   - 重复上下文说明次数。
   - 历史决策召回准确率。
   - 错误记忆注入率。
   - 检索 P95。
   - 写入非阻塞。
   - 跨 Agent 召回成功率。
   - Level4 capability coverage。
   - event capture completeness。
   - 设计复查历史上下文 token savings。
3. P5 验收编排器：
   - 初始化验收 run。
   - 执行或记录 10 个验收任务。
   - 关联 session/task/raw_event/retrieval_trace/access_log。
   - 生成机器可读 JSON 和 Markdown 报告。
4. 三 Agent 共享 Memory Daemon 的运行配置和接入样例升级：
   - Codex。
   - Claude Code。
   - Cursor。
5. Level4 能力探测和会话质量统计：
   - capability coverage。
   - event capture completeness。
   - 降级原因。
   - 不可观测事件说明。
6. 10 个 MVP 验收任务的场景定义、输入、期望、指标和通过阈值。
7. P5 MCP 诊断工具或 CLI：
   - 创建验收 run。
   - 记录任务结果。
   - 计算指标。
   - 查询报告。
8. `make test-p5-mvp` 和 `scripts/acceptance/p5_mvp.sh`。
9. P5 单元测试、repository 测试、app 集成测试、验收脚本。
10. v1.0.0 发布检查清单。

### 2.2 明确不交付

1. 不做团队版权限、租户隔离、企业审计和备份恢复。
2. 不做完整学习画像。
3. 不做在线 LLM rerank。
4. 不强制接入外部 embedding provider。
5. 不要求 sqlite-vec 可用；无向量环境仍必须完成 MVP 验收。
6. 不实现完整自研 Codegraph。
7. 不保存完整会话、完整工具 output、完整 diff 或完整源码。
8. 不把验收报告当作长期记忆写入 `memory_item`。
9. 不通过伪造 capability 让 Agent 看起来达到 Level4。
10. 不把真实 Agent 原生能力差异隐藏在总分里。

### 2.3 关键设计判断

P5 不应把“真实 Agent 是否能完全被动捕获所有事件”和“Memory Engine 是否能正确记忆、检索、注入”混成一个不可定位的验收结果。

因此 P5 验收拆成两层：

| 层级 | 目标 | 说明 |
|---|---|---|
| Engine MVP | 验证 P0-P4 记忆闭环、检索质量、上下文压缩和污染控制 | 可使用标准 RawEvent 回放和最小 Agent harness 保证可重复 |
| Agent Certification | 验证 Codex、Claude Code、Cursor 在当前环境下的真实捕获能力 | 使用真实 Agent 或 Adapter，单独统计 capability 和 completeness |

最终 v1.0.0 发布必须同时给出两类结果：

1. Engine MVP 是否通过。
2. 每个 Agent 的真实接入等级、不可观测能力和降级原因。

这样可以避免某个 Agent 当前版本无法暴露内部工具事件时，掩盖 Memory Engine 本身的质量；也避免通过合成事件伪称真实 Agent 已经 Level4。

## 3. 前置验收门槛

P5 开工前必须确认：

| 前置项 | 要求 |
|---|---|
| P0 | `memoryd` 可启动，health/status 正常，migration 幂等 |
| P1 | `remember/search/context/review` 可用，scope 隔离正确，手动 checkpoint 可召回 |
| P2 | `memory.observe`、agent_session、agent_task、raw_event、capture quality 和诊断工具可用 |
| P3 | evidence/candidate/admission/review/retention/reconcile 基础闭环可用 |
| P4 | retrieval_trace、access_log、relation expansion、code_ref、docindex、context budget 和诊断工具可用 |
| 回归 | `go test -tags sqlite_fts5 ./...` 通过 |
| 验收 | `make test-p2-capture`、`make test-p3-sqlite`、`make test-p4-retrieval` 通过 |

P5 不接受“跳过 P4 直接做多 Agent demo”的路径。没有 trace、access log、capture quality 和 automation diagnostics，P5 指标无法解释，失败也无法定位。

## 4. 总体架构

```text
scripts/acceptance/p5_mvp.sh
    |
    +-- build memoryd
    +-- create temporary data dir / or user selected data dir
    +-- run P0-P4 regression gates
    +-- run MVP acceptance runner
    |
    v
internal/mvp
    |
    +-- Run Service
    |     - create run
    |     - register baseline/candidate task rounds
    |     - bind agent/session/task/trace
    |
    +-- Scenario Registry
    |     - 10 MVP task definitions
    |     - expected recalls
    |     - metric thresholds
    |
    +-- Metric Collector
    |     - capture quality
    |     - retrieval trace/access log
    |     - task outcome
    |     - token and latency samples
    |
    +-- Reporter
          - JSON report
          - Markdown summary
          - pass/fail gate
```

推荐新增代码目录：

```text
internal/mvp
internal/storage/sqlite/mvp_repository.go
internal/mcp/tools/mvp.go
internal/storage/sqlite/migrations/0007_init_mvp.sql
scripts/acceptance/p5_mvp.sh
examples/agents/shared-memoryd
```

模块职责：

| 模块 | 职责 |
|---|---|
| `internal/mvp` | P5 run、scenario、metric、report 领域模型和服务 |
| `internal/storage` | MVP repository 接口定义 |
| `internal/storage/sqlite` | P5 验收 run/task/metric/capability 持久化 |
| `internal/mcp/tools/mvp.go` | MVP run 和报告诊断工具 |
| `internal/app` | 组装 MVP service 和 MCP tools |
| `examples/agents/shared-memoryd` | 三 Agent 共享同一 memoryd 的配置和操作说明 |
| `scripts/acceptance/p5_mvp.sh` | P5 本地验收入口 |

## 5. 数据模型设计

### 5.1 mvp_acceptance_run

`mvp_acceptance_run` 表记录一次完整 MVP 验收。

```sql
create table if not exists mvp_acceptance_run (
  id                         text primary key,
  name                       text not null,
  mode                       text not null,
  workspace_id               text not null,
  project_id                 text,
  repo_id                    text,
  baseline_type              text not null,
  candidate_type             text not null,
  status                     text not null,
  started_at                 datetime not null,
  ended_at                   datetime,
  summary_json               text,
  report_path                text,
  created_at                 datetime not null,
  updated_at                 datetime not null
);
```

推荐索引：

```sql
create index if not exists idx_mvp_acceptance_run_scope
  on mvp_acceptance_run(workspace_id, project_id, repo_id, started_at);

create index if not exists idx_mvp_acceptance_run_status
  on mvp_acceptance_run(status, updated_at);
```

字段规则：

| 字段 | 规则 |
|---|---|
| `mode` | `synthetic`、`real_agent`、`mixed` |
| `baseline_type` | `no_memory`、`full_chat_history`、`summary_only` |
| `candidate_type` | 固定为 `hybrid_memory`，后续可扩展 |
| `status` | `running`、`passed`、`failed`、`partial`、`aborted` |
| `summary_json` | 只保存指标摘要和失败原因，不保存完整对话 |
| `report_path` | 本地报告路径，必须位于 workspace 或 data dir 允许范围内 |

### 5.2 mvp_acceptance_task

`mvp_acceptance_task` 表记录每个验收任务的一轮执行结果。

```sql
create table if not exists mvp_acceptance_task (
  id                         text primary key,
  run_id                     text not null,
  scenario_id                text not null,
  round                      integer not null,
  agent_type                 text not null,
  baseline                   boolean not null default false,
  session_id                 text,
  task_id                    text,
  retrieval_trace_id         text,
  status                     text not null,
  task_success               boolean not null default false,
  expected_json              text,
  observed_json              text,
  failure_reason             text,
  started_at                 datetime not null,
  ended_at                   datetime,
  created_at                 datetime not null,
  updated_at                 datetime not null
);
```

推荐索引：

```sql
create index if not exists idx_mvp_acceptance_task_run
  on mvp_acceptance_task(run_id, scenario_id, round, baseline);

create index if not exists idx_mvp_acceptance_task_session
  on mvp_acceptance_task(session_id, task_id);

create index if not exists idx_mvp_acceptance_task_trace
  on mvp_acceptance_task(retrieval_trace_id);
```

字段规则：

1. `expected_json` 保存结构化期望，例如必须召回的 memory type、scope、code_ref、checkpoint。
2. `observed_json` 保存结构化结果摘要，例如召回 memory id、注入数量、错误注入数量。
3. `failure_reason` 必须是短文本，不保存完整 Agent 输出。
4. `round=1` 表示制造上下文；`round=2` 表示跨 session 或跨 Agent 验证。

### 5.3 mvp_metric_sample

`mvp_metric_sample` 表保存按 run/task/agent 聚合后的指标样本。

```sql
create table if not exists mvp_metric_sample (
  id                         text primary key,
  run_id                     text not null,
  scenario_id                text,
  task_result_id             text,
  agent_type                 text,
  metric_name                text not null,
  metric_value               real not null,
  numerator                  real,
  denominator                real,
  unit                       text not null,
  threshold_value            real,
  threshold_operator         text,
  passed                     boolean not null default false,
  source_json                text,
  created_at                 datetime not null
);
```

推荐索引：

```sql
create index if not exists idx_mvp_metric_run
  on mvp_metric_sample(run_id, metric_name, scenario_id);

create index if not exists idx_mvp_metric_agent
  on mvp_metric_sample(run_id, agent_type, metric_name);
```

指标命名：

| metric_name | unit | 目标 |
|---|---|---:|
| `task_success_rate` | ratio | `= 1.00` |
| `token_savings` | ratio | `>= 0.30` |
| `repeated_explanation_reduction` | ratio | `>= 0.50` |
| `decision_recall_accuracy` | ratio | `>= 0.80` |
| `wrong_memory_injection_rate` | ratio | `<= 0.05` |
| `retrieval_latency_p95_ms` | ms | `<= 100` |
| `cross_agent_recall_success_rate` | ratio | `>= 0.80` |
| `event_capture_completeness` | ratio | `>= 0.90` |
| `level4_capability_coverage` | ratio | `= 1.00` |
| `review_context_token_savings` | ratio | `>= 0.60` |
| `write_blocking_error_count` | count | `= 0` |

### 5.4 mvp_agent_capability

`mvp_agent_capability` 表保存某个 Agent 在某次验收中的真实能力快照。

```sql
create table if not exists mvp_agent_capability (
  id                         text primary key,
  run_id                     text not null,
  agent_type                 text not null,
  adapter_name               text,
  adapter_version            text,
  capture_level              integer not null,
  conversation_capture       boolean not null default false,
  tool_call_capture          boolean not null default false,
  tool_output_capture        boolean not null default false,
  file_edit_capture          boolean not null default false,
  session_lifecycle          boolean not null default false,
  memory_observe             boolean not null default false,
  capability_coverage        real not null default 0,
  completeness               real not null default 0,
  degradation_reasons_json   text,
  created_at                 datetime not null
);
```

唯一约束：

```sql
create unique index if not exists idx_mvp_agent_capability_unique
  on mvp_agent_capability(run_id, agent_type);
```

设计约束：

1. capability 来自 Adapter 声明和 P2 `capture_capabilities_json`。
2. completeness 来自 `capture_quality_json` 和实际 raw_event 统计。
3. 不允许手工把 capability 覆盖为 true 以通过验收。
4. 如果某能力在当前 Agent 无法观测，必须写入 `degradation_reasons_json`。

## 6. MVP 指标计算

### 6.1 Token savings

```text
token_savings =
  (baseline_context_tokens - candidate_context_tokens)
  / baseline_context_tokens
```

字段来源：

| 字段 | 来源 |
|---|---|
| `baseline_context_tokens` | baseline run 中手工上下文、完整历史或 summary 的估算 token |
| `candidate_context_tokens` | 用户输入 token + memory context token + 必要系统上下文 token |
| `memory_context_token_count` | `memory.context` 返回结果或 context builder 估算 |

实现要求：

1. P5 默认使用本地近似 token 估算，避免引入外部 tokenizer 依赖。
2. 同一 run 内必须使用同一种估算算法。
3. 报告中必须标明 token 估算方式和误差风险。
4. 设计复查场景单独计算 `review_context_token_savings`。

### 6.2 历史决策召回准确率

```text
decision_recall_accuracy =
  matched_expected_decision_count / expected_decision_count
```

匹配规则：

1. 必须命中正确 scope。
2. 必须命中 `memory_type=decision` 或等价 checkpoint 中的决策结论。
3. 必须包含 evidence 或 `why_included` 可解释原因。
4. pending_review 记忆可以计为召回，但必须标记未确认状态。

### 6.3 错误记忆注入率

```text
wrong_memory_injection_rate =
  wrong_memory_injected_count / injected_memory_count
```

错误注入定义：

1. scope 不匹配。
2. 已 archived/deleted/superseded 的记忆被作为当前结论注入。
3. 代码结构事实被当作普通 memory 注入。
4. 与当前用户纠正相反的旧偏好被直接使用。
5. checkpoint 已被文档 hash 证明过期，但仍作为当前事实使用。

当 `injected_memory_count=0` 时，不计入该指标分母，但必须在召回失败中记录。

### 6.4 检索 P95

```text
retrieval_latency_p95_ms =
  p95(retrieval_trace.latency_ms)
```

统计口径：

1. 只统计 `memory.search` 和 `memory.context` 在线路径。
2. 不包含异步 evidence extraction、candidate generation、retention 和 reconcile。
3. vector provider、Code Index、Doc Index 降级时仍计入本次 trace。
4. P95 目标为 `<= 100ms`。

### 6.5 Level4 capability coverage

```text
level4_capability_coverage =
  supported_required_capability_count / 6
```

六项能力：

1. conversation capture。
2. tool call capture。
3. tool output capture。
4. file edit capture。
5. session lifecycle。
6. memory observe。

P5 报告必须按 Agent 分开展示，不允许只给总平均值。

### 6.6 Event capture completeness

```text
event_capture_completeness =
  captured_required_event_count / expected_required_event_count
```

统计规则：

1. `expected_required_event_count` 按本次 scenario 中可观测的事件计算。
2. Agent 明确不支持的 capability 不计入 completeness 分母，但会降低 capability coverage。
3. 内容边界拒绝计入质量诊断，不直接计入 captured。
4. deduped 事件不重复计数。

## 7. P5 MCP 工具设计

P5 MCP 工具定位为验收和诊断工具，不进入 Agent 正常任务路径。

### 7.1 memory.mvp.run.start

用途：创建一次 MVP 验收 run。

请求：

```json
{
  "name": "v1.0.0 local mvp acceptance",
  "mode": "mixed",
  "workspace_id": "ws_001",
  "project_id": "proj_001",
  "repo_id": "repo_001",
  "baseline_type": "summary_only",
  "candidate_type": "hybrid_memory"
}
```

响应：

```json
{
  "request_id": "req_001",
  "run_id": "mvp_run_001",
  "status": "running"
}
```

约束：

1. `workspace_id` 必填。
2. `mode` 必须是 `synthetic`、`real_agent`、`mixed` 之一。
3. 同一个 run 不负责启动真实 Agent，只记录和聚合验收数据。

### 7.2 memory.mvp.task.record

用途：记录单个 scenario 的一次执行结果。

请求：

```json
{
  "run_id": "mvp_run_001",
  "scenario_id": "mvp_03_decision_recall",
  "round": 2,
  "agent_type": "codex",
  "baseline": false,
  "session_id": "sess_001",
  "task_id": "task_001",
  "retrieval_trace_id": "rt_001",
  "task_success": true,
  "expected": {
    "memory_types": ["decision"],
    "required_scope": "project_local"
  },
  "observed": {
    "retrieved_memory_count": 3,
    "injected_memory_count": 2,
    "wrong_memory_injected_count": 0
  }
}
```

响应：

```json
{
  "request_id": "req_002",
  "task_result_id": "mvp_task_001",
  "accepted": true
}
```

约束：

1. `run_id` 必须对应已存在的 P5 run，禁止写入孤儿 task。
2. `agent_type` 必须是 `codex`、`claude_code`、`cursor` 之一。
3. `task_success=false` 或 `status=failed/skipped/running` 会生成失败的 `task_success_rate` 指标，并使该 task 派生指标不能通过。
4. 绑定的 `retrieval_trace_id` 缺失时，`retrieval_latency_p95_ms` 指标按失败处理，避免跳过性能门禁。

### 7.3 memory.mvp.capability.record

用途：记录单个 Agent 在本次 P5-D 验收中的捕获能力快照。

请求：

```json
{
  "run_id": "mvp_run_001",
  "agent_type": "codex",
  "adapter_name": "wrapper",
  "adapter_version": "local",
  "capture_level": 3,
  "conversation_capture": false,
  "tool_call_capture": true,
  "tool_output_capture": true,
  "file_edit_capture": true,
  "session_lifecycle": true,
  "memory_observe": true,
  "completeness": 0.91,
  "degradation_reasons": ["conversation_capture_unavailable"]
}
```

响应：

```json
{
  "request_id": "req_003",
  "capability_id": "mvp_cap_001",
  "agent_type": "codex",
  "capability_coverage": 0.8333,
  "completeness": 0.91,
  "accepted": true
}
```

约束：

1. `run_id` 必须存在。
2. `agent_type` 必须属于 P5-D 三类 Agent。
3. `capture_level` 取值范围为 1 到 4。
4. `completeness` 取值范围为 0 到 1。
5. capability coverage 或 completeness 未达标时，必须提供 `degradation_reasons`。

### 7.4 memory.mvp.metrics.compute

用途：根据 P2/P3/P4 诊断数据和任务记录计算 MVP 指标。

请求：

```json
{
  "run_id": "mvp_run_001",
  "recompute": true
}
```

响应：

```json
{
  "request_id": "req_004",
  "run_id": "mvp_run_001",
  "status": "partial",
  "metrics": [
    {
      "metric_name": "retrieval_latency_p95_ms",
      "metric_value": 42,
      "threshold_operator": "<=",
      "threshold_value": 100,
      "passed": true
    }
  ]
}
```

### 7.5 memory.mvp.report

用途：查询或生成验收报告。

请求：

```json
{
  "run_id": "mvp_run_001",
  "format": "markdown",
  "include_failures": true
}
```

响应：

```json
{
  "request_id": "req_005",
  "run_id": "mvp_run_001",
  "status": "failed",
  "report_path": "reports/mvp/mvp_run_001.md",
  "summary": {
    "passed_metrics": 8,
    "failed_metrics": 2,
    "engine_mvp_passed": true,
    "agent_certification_passed": false
  }
}
```

## 8. 验收编排模式

### 8.1 synthetic 模式

synthetic 模式使用标准 RawEvent 回放和 MCP 工具调用验证 Memory Engine。

适用场景：

1. CI 或本地快速回归。
2. 验证 P0-P4 能力没有回归。
3. 稳定复现 10 个 MVP scenario。

特点：

1. 不依赖真实 Agent UI 或私有日志格式。
2. 可以精确制造 scope、session、task、event、candidate 和 relation。
3. 不能证明真实 Agent 已达到 Level4。

### 8.2 real_agent 模式

real_agent 模式使用真实 Codex、Claude Code、Cursor 接入同一个 Memory Daemon。

适用场景：

1. 发布前手工验收。
2. 验证 Adapter、hook、wrapper、rules 的真实捕获能力。
3. 评估不可观测能力和降级原因。

特点：

1. 可以证明当前环境下的真实 Agent 接入质量。
2. 结果受 Agent 版本、运行方式、权限和日志可见性影响。
3. 必须在报告中记录环境和降级原因。

### 8.3 mixed 模式

mixed 模式用于 v1.0.0 推荐验收：

1. 使用 synthetic 模式验证 Engine MVP 必过。
2. 使用 real_agent 模式验证三 Agent certification。
3. 报告中分别给出 Engine、Codex、Claude Code、Cursor 的结论。

## 9. 三 Agent 接入设计

### 9.1 共享 Memory Daemon

三个 Agent 必须连接同一个 `memoryd` 实例和同一个 SQLite data dir。

推荐启动：

```bash
memoryd serve --data-dir /tmp/the-one-mvp --config memoryd.yaml
```

共享约束：

1. `workspace_id` 必须一致。
2. 同一项目的 `project_id` 必须一致。
3. 同一仓库的 `repo_id` 必须一致。
4. 不同 Agent 使用不同 `session_id`。
5. task 边界可以不同，但必须可通过 project/repo scope 召回。

### 9.2 Claude Code

目标：

| 能力 | P5 目标 |
|---|---|
| conversation capture | hooks 捕获 prompt 和 response 摘要 |
| tool call capture | hooks 捕获 tool name、input summary、timestamp |
| tool output capture | hooks 捕获 output summary、error signature、exit code、hash |
| file edit capture | tool/use 或 git diff 摘要 |
| session lifecycle | session start/end hooks |
| memory observe | MCP tool 调用 |

降级策略：

1. 如果 tool output 原始内容不可取，只保留 exit code、错误签名和 hash。
2. 如果文件编辑事件无法直接捕获，使用 git diff 摘要补偿。
3. 所有降级必须写入 `capture_capabilities_json` 和 P5 capability report。

### 9.3 Codex

目标：

| 能力 | P5 目标 |
|---|---|
| conversation capture | wrapper 或 session summary 捕获 |
| tool call capture | wrapper / shell log collector / MCP observe |
| tool output capture | 摘要、exit code、hash，不保存完整 output |
| file edit capture | git diff 摘要或 workspace watcher |
| session lifecycle | wrapper start/end |
| memory observe | MCP tool 调用 |

降级策略：

1. Codex 环境差异较大，不应假定所有工具事件都可被动捕获。
2. 如果只能通过主动 `memory.observe` 捕获关键节点，则 capability coverage 必须如实反映被动捕获缺口。
3. P5 允许 Codex 以 mixed 模式参与 Engine 验收，但 real_agent certification 必须独立报告。

### 9.4 Cursor

目标：

| 能力 | P5 目标 |
|---|---|
| conversation capture | rules/plugin/log 摘要 |
| tool call capture | rules 引导或可用日志捕获 |
| tool output capture | 摘要、错误签名、hash |
| file edit capture | git diff / filesystem watcher |
| session lifecycle | rules 或 wrapper start/end |
| memory observe | MCP tool 调用 |

降级策略：

1. Cursor rules 更适合主动调用 MCP，内部工具链可见性依赖版本。
2. 文件编辑捕获优先独立于对话捕获，避免工具可见性不足影响代码变更记忆。
3. 无法捕获内部工具调用时，不能把 file edit summary 伪装成 tool call capture。

## 10. 10 个 MVP 验收任务设计

### 10.1 mvp_01_task_continuation

目标：验证跨 session 继续同一项目任务。

第一轮：

```text
用户要求实现 auth token 过期边界修复，并说明项目采用 Go、PostgreSQL，认证模块要求请求内同步完成校验。
Agent 运行测试后发现 TestTokenExpiry 在精确过期时间失败。
```

第二轮：

```text
用户只说：继续上次 auth 的问题。
```

期望：

1. 召回 auth token 过期边界问题。
2. 召回同步校验约束。
3. 返回相关 code_ref。
4. Agent 不要求用户重新解释背景。

通过指标：

| 指标 | 目标 |
|---|---:|
| `repeated_explanation_count` | `0` |
| `task_state_recall` | `1` |
| `token_savings` | `>= 0.30` |

### 10.2 mvp_02_user_preference

目标：验证 `user_global` 偏好跨项目生效。

期望：

1. 召回用户偏好。
2. 输出结构体现架构边界、风险和工程落地。
3. 不污染 project/repo scope。

通过指标：

| 指标 | 目标 |
|---|---:|
| `preference_recall_accuracy` | `1.00` |
| `repeated_explanation_count` | `0` |
| `wrong_scope_injection_count` | `0` |

### 10.3 mvp_03_decision_recall

目标：验证历史架构决策和 evidence 召回。

期望：

1. 召回“不引入 Kafka”的决策。
2. 召回原因和适用边界。
3. 返回 evidence 摘要。
4. pending_review 状态必须显式标记。

通过指标：

| 指标 | 目标 |
|---|---:|
| `decision_recall_accuracy` | `>= 0.80` |
| `evidence_faithfulness` | `>= 0.90` |
| `pending_state_mark_rate` | `1.00` |

### 10.4 mvp_04_failure_recall

目标：验证失败经验和 procedure 能改变后续行为。

期望：

1. 召回慢请求排查失败经验。
2. 优先建议 metrics、trace、DB pool。
3. 旧错误策略不再作为首要建议。

通过指标：

| 指标 | 目标 |
|---|---:|
| `failure_memory_recall` | `1` |
| `user_correction_reduction` | `>= 0.50` |
| `old_wrong_strategy_reuse_count` | `0` |

### 10.5 mvp_05_temporal_validity

目标：验证过期项目事实识别。

期望：

1. 当前事实返回 PostgreSQL。
2. MySQL 旧记忆 archived 或 superseded。
3. 旧记忆只能作为历史信息出现。

通过指标：

| 指标 | 目标 |
|---|---:|
| `temporal_correctness` | `1.00` |
| `stale_memory_misuse_count` | `0` |
| `supersedes_link_present` | `1` |

### 10.6 mvp_06_cross_agent_sharing

目标：验证 Codex、Claude Code、Cursor 共享同一项目上下文。

流程：

```text
Claude Code 写入架构决策。
Codex 在同一项目继续实现。
Cursor 在同一项目询问历史决策。
```

期望：

1. 三个 Agent 使用同一 project/repo scope。
2. Codex 和 Cursor 召回 Claude Code 写入的稳定记忆。
3. session 不同但 project/repo 对齐。

通过指标：

| 指标 | 目标 |
|---|---:|
| `cross_agent_recall_success_rate` | `>= 0.80` |
| `scope_error_count` | `0` |
| `level4_capability_coverage` | `1.00` |
| `event_capture_completeness` | `>= 0.90` |

### 10.7 mvp_07_no_tool_output_pollution

目标：验证临时工具输出不污染长期记忆。

期望：

1. 不保存完整 output。
2. 普通成功输出不进入长期记忆。
3. 失败输出只保存摘要、错误签名、关键词和 tool ref。
4. 未被重复引用的 temporary 信息可被 retention 清理或归档。

通过指标：

| 指标 | 目标 |
|---|---:|
| `full_output_storage_count` | `0` |
| `temporary_output_long_term_rate` | `<= 0.05` |
| `error_signature_accuracy` | `>= 0.80` |

### 10.8 mvp_08_code_index_boundary

目标：验证源码结构事实不混入普通 Memory。

期望：

1. 调用关系进入 Code Index 或 code_ref 解析，不作为普通 `memory_item`。
2. 设计原因进入 Memory。
3. Memory 只保存 code_ref，不复制代码结构事实。

通过指标：

| 指标 | 目标 |
|---|---:|
| `code_structure_fact_memory_count` | `0` |
| `code_ref_completeness` | `>= 0.90` |
| `design_reason_memory_accuracy` | `>= 0.80` |

### 10.9 mvp_09_user_correction

目标：验证用户纠正后后续行为改变。

期望：

1. 旧 Redis 偏好被降权、覆盖或 superseded。
2. 新偏好 stable。
3. 低频配置缓存场景优先分析本地缓存是否足够。

通过指标：

| 指标 | 目标 |
|---|---:|
| `corrected_preference_hit_rate` | `1.00` |
| `old_preference_misuse_count` | `0` |
| `supersedes_or_override_present` | `1` |

### 10.10 mvp_10_review_checkpoint_compression

目标：验证重复设计复查上下文压缩。

期望：

1. 召回最近 `review_checkpoint`。
2. 用文档 hash/diff 校验事实源。
3. 已确认忽略项不重复作为重大缺陷。
4. 文档未变化时不重复展开完整历史。

通过指标：

| 指标 | 目标 |
|---|---:|
| `review_context_token_savings` | `>= 0.60` |
| `ignored_issue_repeated_count` | `0` |
| `checkpoint_recall_accuracy` | `>= 0.90` |
| `unchanged_doc_full_read_rate` | `<= 0.30` |

## 11. 报告设计

P5 报告必须同时生成 JSON 和 Markdown。

### 11.1 JSON 报告

用途：CI、脚本和后续自动分析。

结构：

```json
{
  "run_id": "mvp_run_001",
  "status": "partial",
  "engine_mvp_passed": true,
  "agent_certification_passed": false,
  "metrics": [],
  "agents": [],
  "scenarios": [],
  "failures": []
}
```

### 11.2 Markdown 报告

用途：研发负责人评审和 v1.0.0 发布决策。

报告章节：

1. 总体结论。
2. 环境信息。
3. P0-P4 前置门禁结果。
4. Engine MVP 指标。
5. 三 Agent certification 结果。
6. 10 个 scenario 结果。
7. 失败项和定位建议。
8. 一期边界确认。
9. v1.0.0 发布建议。

报告必须明确：

1. 哪些任务通过。
2. 哪些任务失败。
3. 失败是 Engine 问题、Agent capability 问题、测试数据问题，还是环境问题。
4. 是否允许带已知限制发布。

## 12. 验收脚本设计

### 12.1 Makefile

新增：

```makefile
test-p5-mvp:
	GO_TAGS="sqlite_fts5" BIN_DIR="$(BIN_DIR)" scripts/acceptance/p5_mvp.sh
```

### 12.2 scripts/acceptance/p5_mvp.sh

脚本执行顺序：

```text
1. go test -tags sqlite_fts5 ./internal/mvp
2. go test -tags sqlite_fts5 ./internal/storage/sqlite
3. go test -tags sqlite_fts5 ./internal/app
4. go test -tags sqlite_fts5 ./...
5. go build -tags sqlite_fts5 -o bin/memoryd ./cmd/memoryd
6. run synthetic MVP acceptance
7. emit report path
```

脚本约束：

1. 默认不依赖网络。
2. 默认不启动真实 Codex/Claude Code/Cursor。
3. 默认使用临时 data dir。
4. 不删除用户指定 data dir。
5. real_agent 模式必须由用户显式开启。

推荐环境变量：

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `P5_MVP_MODE` | `synthetic` | `synthetic`、`real_agent`、`mixed` |
| `P5_DATA_DIR` | 临时目录 | 验收数据库目录 |
| `P5_REPORT_DIR` | `reports/mvp` | 报告输出目录 |
| `P5_KEEP_DATA` | `false` | 是否保留临时验收数据 |
| `P5_REAL_AGENT` | `false` | 是否允许真实 Agent certification |

## 13. 测试设计

### 13.1 单元测试

| 模块 | 用例 |
|---|---|
| metric calculator | token savings、错误注入率、P95、空分母处理 |
| capability calculator | capability coverage、completeness、降级原因 |
| scenario registry | 10 个 scenario id 唯一，阈值完整 |
| reporter | JSON/Markdown 输出不包含完整 prompt/output |

### 13.2 Repository 测试

| 表 | 用例 |
|---|---|
| `mvp_acceptance_run` | 创建、结束、状态更新、scope 查询 |
| `mvp_acceptance_task` | task 记录、trace/session 关联、round 查询 |
| `mvp_metric_sample` | 指标 upsert、阈值判断、按 run 聚合 |
| `mvp_agent_capability` | 每 Agent 唯一、降级原因保存 |

### 13.3 App 集成测试

集成测试必须覆盖：

1. P5 MCP tools 注册。
2. 创建 run。
3. 记录任务。
4. 计算指标。
5. 生成报告。
6. 与 P2 capture diagnostics 聚合。
7. 与 P4 retrieval_trace/access_log 聚合。

### 13.4 验收测试

`make test-p5-mvp` 至少覆盖 synthetic 模式：

1. 构造 10 个 scenario 的标准事件。
2. 等待或手动推进 P3 async worker。
3. 执行 P4 `memory.context/search`。
4. 记录 P5 task result。
5. 计算指标。
6. 生成报告。
7. P5 synthetic Engine MVP 必须通过。

real_agent 模式不进入默认 CI，但必须提供手工验收清单。

## 14. 发布门禁

P5 Done 后才允许标记 `v1.0.0`。

### 14.1 必须通过

1. `go test -tags sqlite_fts5 ./...`
2. `make test-p2-capture`
3. `make test-p3-sqlite`
4. `make test-p4-retrieval`
5. `make test-p5-mvp`
6. P5 synthetic Engine MVP 通过。
7. P5 报告生成成功。
8. 无完整源码、完整 output、完整 diff、完整历史对话入库。
9. 检索 P95 `<= 100ms`。
10. 错误记忆注入率 `<= 5%`。

### 14.2 可带限制发布

以下情况允许发布，但必须在 v1.0.0 release notes 中明确：

1. 某真实 Agent 当前只能达到 Level3，但 Engine MVP 通过。
2. sqlite-vec 不可用。
3. Code Index 仅 `local_basic`，调用图能力 degraded。
4. real_agent certification 需要用户手工运行。
5. token 估算为近似算法。

### 14.3 不允许发布

以下情况不允许标记 v1.0.0：

1. P5 synthetic Engine MVP 未通过。
2. P1/P2/P3/P4 任一回归失败。
3. scope 错误导致跨项目记忆污染。
4. archived/deleted/superseded 记忆被作为当前结论注入。
5. 完整工具 output、完整 diff 或完整源码进入持久层。
6. `memory.observe` 写入阻塞 Agent 主流程。
7. P5 报告无法定位失败原因。

## 15. 分步实现计划

### P5-A：验收模型和指标基础

1. 新增 `0007_init_mvp.sql`。
2. 新增 `internal/mvp` DTO、metric calculator、scenario registry。
3. 新增 SQLite repository 和测试。
4. 新增 MCP tools skeleton。

Done 定义：

1. P5 表 migration 幂等。
2. 指标计算单元测试通过。
3. run/task/metric/capability repository 测试通过。

### P5-B：诊断聚合和报告

1. 接入 P2 capture diagnostics。
2. 接入 P4 retrieval_trace/access_log。
3. 接入 task outcome 和 user correction 统计。
4. 实现 JSON/Markdown reporter。

Done 定义：

1. 能生成包含 Engine 和 Agent certification 分区的报告。
2. 报告不包含完整敏感原文。
3. 失败项能关联到 session/task/trace。

### P5-C：10 个 scenario synthetic 验收

1. 实现 10 个 scenario 的事件 fixture。
2. 编排 `observe -> automation -> context/search -> metric`。
3. 固化阈值和 pass/fail 判断。
4. 新增 `scripts/acceptance/p5_mvp.sh`。

Done 定义：

1. synthetic 模式 `make test-p5-mvp` 通过。
2. 10 个 scenario 均有独立结果。
3. P5 报告输出可复核。

### P5-D：真实 Agent certification

1. 升级 `examples/agents/*` 为 shared memoryd 模式。
2. 增加 Codex、Claude Code、Cursor capability 采集说明。
3. 增加 real_agent 手工验收清单。
4. 支持 mixed 模式报告合并。

Done 定义：

1. 三 Agent 均可连接同一个 Memory Daemon。
2. 报告按 Agent 展示 capability coverage 和 completeness。
3. 降级原因可解释。

### P5-E：v1.0.0 收口

1. 跑完整发布门禁。
2. 更新 `当前工程阶段实现状态.md`。


Done 定义：

1. P5 Done 定义全部满足。
2. v1.0.0 release notes 明确边界和已知限制。
3. 不存在未解释的验收失败项。

## 16. P5 Done 定义

1. P5 migration、repository、service、MCP tools 和报告生成能力完成。
2. `memory.mvp.run.start` 可创建验收 run。
3. `memory.mvp.task.record` 可记录 scenario 结果。
4. `memory.mvp.metrics.compute` 可计算全部 MVP 指标。
5. `memory.mvp.report` 可生成 JSON 和 Markdown 报告。
6. 10 个 MVP scenario 均有标准输入、期望、指标和阈值。
7. synthetic Engine MVP 验收通过。
8. Codex、Claude Code、Cursor 均能连接同一 memoryd。
9. 三 Agent capability coverage 和 event completeness 可统计。
10. P5 报告区分 Engine MVP 和 Agent certification。
11. 跨 Agent 召回成功率 `>= 80%`。
12. Token savings `>= 30%`。
13. 设计复查历史上下文 Token savings `>= 60%`。
14. 历史决策召回准确率 `>= 80%`。
15. 错误记忆注入率 `<= 5%`。
16. 检索 P95 `<= 100ms`。
17. `memory.observe` 不阻塞 Agent 主流程。
18. Level4 capability coverage 按 Agent 单独展示，不能用平均值掩盖短板。
19. Event capture completeness `>= 90%`，不可观测能力必须说明。
20. P1/P2/P3/P4 回归全部通过。
21. `make test-p5-mvp` 通过。
22. 无完整源码、完整 output、完整 diff、完整历史对话入库。
23. Code Index 和 Memory 边界在验收任务 8 中通过。
24. review checkpoint 和 doc hash/diff-aware 复查在验收任务 10 中通过。
25. v1.0.0 发布检查清单完成。

## 17. 风险和控制点

| 风险 | 影响 | 控制点 |
|---|---|---|
| 真实 Agent 能力不可控 | Level4 难以稳定通过 | 拆分 Engine MVP 和 Agent certification，capability 单独报告 |
| synthetic 验收过度理想化 | 不能代表真实使用 | mixed 模式发布验收，real_agent 手工清单补充 |
| token 估算不准 | savings 指标有偏差 | 同 run 固定算法，报告标明估算方式 |
| 指标被平均值掩盖 | 单 Agent 短板不可见 | 所有 Agent 指标单独展示，禁止只看总平均 |
| 验收数据污染用户库 | 本地记忆被测试数据污染 | 默认临时 data dir，真实 data dir 需显式指定 |
| 报告泄露敏感内容 | 本地报告保存完整 prompt/output | 只保存摘要、计数、id、hash 和失败原因短文本 |
| P5 引入核心语义漂移 | 破坏 P0-P4 稳定性 | P5 只做验收和度量，不改变 Admission/Retrieval 语义 |
| 10 个任务维护成本高 | 后续演进困难 | scenario registry 结构化定义，阈值集中管理 |
| 检索延迟偶发抖动 | P95 不稳定 | 固定本地环境，排除异步任务耗时，报告样本数 |
| scope 归因错误 | 跨 Agent 共享污染 | run/task/session 全链路绑定 workspace/project/repo |

## 18. 与后续阶段边界

P5 完成后，一期 MVP 可以进入 v1.0.0。本阶段不展开二期能力，但为后续保留以下扩展点：

1. 团队版 workspace/project 权限模型。
2. 企业审计和备份恢复。
3. 更完整 Codegraph/LSP/SCIP Adapter。
4. 外部 embedding provider 和向量检索增强。
5. 用户学习画像和长期行为偏好建模。
6. UI 化 review queue 和验收 dashboard。

P5 的持久化数据可以作为后续质量评估样本，但不应直接作为用户长期记忆使用。
