

> 基线来源：`The One 长期记忆系统总体架构设计.md` v0.1 冻结版。

## 1. 规划目标

本文档用于指导 AI Coding Agent 长期记忆系统从本地 MVP 到一期完整验收的分期研发。

分期原则：

1. 每一期都必须具备基础可用能力，不能只交付内部框架。
2. 每一期都必须有可执行验收标准。
3. 每一期都要为下一期保留数据结构和接口演进空间。
4. 优先保证本地个人工具闭环，再追求多 Agent Level4 捕获完整度。
5. 默认不引入企业级高可用、审计、备份恢复和复杂权限治理。

总体阶段：

```text
P0 工程基座
  -> P1 手动记忆可用
  -> P2 事件捕获可用
  -> P3 自动记忆可用
  -> P4 检索增强可用
  -> P5 多 Agent MVP 验收
```

## 2. 阶段总览

| 阶段  | 目标                  | 用户可用能力                                            | 主要验收                                                  |
| --- | ------------------- | ------------------------------------------------- | ----------------------------------------------------- |
| P0  | 本地 daemon 和存储基座     | 能启动、初始化 DB、暴露 MCP health/status                   | 单二进制启动、schema migration、健康检查                          |
| P1  | 手动记忆和基础检索           | 用户可显式写入记忆和设计复查 checkpoint，Agent 可检索和注入上下文         | `memory.remember/search/context/review` 基本可用          |
| P2  | Agent 事件捕获          | Codex/Claude Code/Cursor 可上报 session、task、tool 摘要 | `memory.observe`、raw_event、agent_task、capture quality |
| P3  | 自动准入和长期记忆           | 系统可从事件中自动生成候选记忆、复查 checkpoint，并进入 review/stable   | Admission、Evidence、Review、Retention 基础闭环              |
| P4  | 检索增强和 Code Index 边界 | 支持关系扩展、code_ref、可选向量、上下文预算、文档 hash/diff-aware 复查  | 历史决策/失败经验/项目约束/复查 checkpoint 召回质量提升                   |
| P5  | 一期 MVP 验收           | 三 Agent 共享记忆，完成 10 个 MVP 验收任务                     | Token savings、任务成功率、错误注入率、Level4 覆盖                   |

## 3. P0：工程基座

### 3.1 目标

建立本地单二进制基础能力，让系统可以作为本地 Memory Daemon 启动、初始化数据库、加载配置并暴露最小 MCP 服务。

### 3.2 可用功能

1. `memoryd` 可以本地启动。
2. 支持默认配置启动，不要求用户先配置 embedding 或外部服务。
3. 自动初始化 SQLite 数据库。
4. 支持 `schema_migration`。
5. 启用 SQLite WAL、foreign key、busy timeout。
6. 暴露最小 MCP 工具：
   - `memory.status`
   - `memory.health`
7. 输出 storage capability：
   - SQLite 可用性
   - FTS5 可用性
   - sqlite-vec 是否可用
   - fallback retrieval 能力

### 3.3 研发范围

| 模块 | 内容 |
|---|---|
| `cmd/memoryd` | daemon 启动入口 |
| `internal/config` | 默认配置、环境变量覆盖 |
| `internal/storage/sqlite` | 连接管理、migration、WAL 参数 |
| `internal/mcp` | MCP server 框架和 health/status |
| `internal/logging` | 本地日志和基础排障信息 |

### 3.4 暂不做

1. 不做自动捕获。
2. 不做 embedding。
3. 不做复杂 UI。
4. 不做 Review Queue。
5. 不做 Code Index。

### 3.5 验收标准

| 验收项 | 标准 |
|---|---|
| 单二进制启动 | `memoryd` 可在本地启动 |
| DB 初始化 | 首次启动自动创建 SQLite 文件和全部基础表 |
| migration 幂等 | 连续启动多次不会重复执行 migration |
| MCP health | `memory.health` 返回 `ok=true` |
| capability | `memory.status` 返回 sqlite、fts5、sqlite_vec、fallback 信息 |
| 无外部依赖 | 未配置 embedding provider 时仍可启动 |

### 3.6 退出条件

P0 完成后，系统虽然还不能记忆业务内容，但已经具备稳定运行、配置加载、数据库初始化和 MCP 连通能力。

## 4. P1：手动记忆可用

### 4.1 目标

先让系统成为一个可用的“本地长期记忆库”。用户或 Agent 可以显式写入高价值记忆，并通过 MCP 检索和构造上下文。

### 4.2 可用功能

1. `memory.remember`：显式写入记忆。
2. `memory.search`：基于 FTS5 + metadata 检索。
3. `memory.context`：构造压缩上下文包。
4. `memory.review`：查看、确认、拒绝、编辑 pending memory。
5. 支持手动修正和撤销：
   - 编辑已有记忆并生成新版本。
   - 归档错误或过期记忆。
   - 删除误写入记忆并触发索引清理。
6. 支持 scope：
   - `user_global`
   - `project_local`
   - `repo_local`
   - `session`
7. 支持 memory type：
   - `preference`
   - `decision`
   - `constraint`
   - `failure`
   - `project_fact`
   - `procedure`
   - `temporary_state`
   - `review_checkpoint`
8. 支持 FTS5 `search_text`。
9. 支持 `memory_item`、`evidence`、`memory_evidence_link`、`memory_review`。
10. 支持手动写入设计复查 checkpoint：
   - 目标文档路径。
   - 复查意图。
   - 复查结论。
   - 用户确认忽略或延期的问题。
   - 下次复查策略。

### 4.3 研发范围

| 模块 | 内容 |
|---|---|
| `internal/mcp` | `remember/search/context/review` |
| `internal/memory` | Memory CRUD、scope validator、状态流转 |
| `internal/retrieval` | FTS5、metadata filter、rule rerank 简化版 |
| `internal/storage/sqlite` | memory/evidence/review 表读写 |
| `internal/ingest` | content minimization 检查、search_text 构建 |

### 4.4 暂不做

1. 不做自动从 raw_event 生成记忆。
2. 不做复杂 relation expansion。
3. 不做 sqlite-vec。
4. 不做完整 Retention Job。
5. 不要求三 Agent 自动捕获。

### 4.5 验收标准

| 验收项 | 标准 |
|---|---|
| 显式偏好写入 | 写入 `user_global/preference` 后可跨项目检索 |
| 项目决策写入 | 写入 `project_local/decision` 后只在对应项目召回 |
| 基础上下文注入 | `memory.context` 在 token budget 内返回压缩上下文 |
| review 流转 | pending memory 可 approve/reject/edit |
| 手动纠错 | stable memory 可 edit/archive/delete，后续检索不再误用旧内容 |
| 复查 checkpoint | 手动写入 `review_checkpoint` 后，下一次设计复查可被 `memory.context` 召回 |
| scope 隔离 | 不同 project 的 project_local 记忆不互相污染 |
| 不保存全文 | 传入超大内容时拒绝或要求摘要化 |

### 4.6 建议验收任务

1. 用户写入“以后技术方案先分析架构边界、风险和工程落地”。
2. 在另一个项目调用 `memory.context`，能召回该偏好。
3. 在项目 A 写入“不引入 Kafka 的历史决策”。
4. 在项目 B 查询 Kafka，不能误召回项目 A 的决策。
5. 手动归档该 Kafka 决策后，默认检索不再注入该记忆。
6. 手动写入一次“总体架构设计已冻结，后续复查只关注重大逻辑缺失”的 `review_checkpoint`。
7. 再次调用 `memory.context` 进行设计复查时，能召回该 checkpoint，并避免重复加载完整历史对话。

### 4.7 退出条件

P1 完成后，系统已经是一个可手动使用、可纠错、可撤销的本地 AI Agent 记忆层，可以开始为真实 Agent 提供基础上下文。

## 5. P2：Agent 事件捕获可用

### 5.1 目标

让 Codex、Claude Code、Cursor 至少能通过统一 MCP 和轻量 Adapter 上报事件，为后续自动记忆生成准备原始事件层。

### 5.2 可用功能

1. `memory.observe` 可接收标准 RawEvent。
2. 支持 `agent_session`。
3. 支持 `agent_task`。
4. 支持 `raw_event` append-only 写入。
5. 支持事件去重。
6. 支持 capture capability 上报。
7. 支持 session/task 生命周期事件。
8. 支持工具调用和工具结果摘要事件。
9. 支持文件编辑摘要事件。
10. 支持捕获诊断查询：
   - session 列表。
   - task 列表。
   - raw_event 列表和过滤。
   - capture quality 查看。
11. 三个 Agent 至少达到 Level2，优先向 Level3/Level4 演进。

### 5.3 研发范围

| 模块 | 内容 |
|---|---|
| `internal/capture` | Capture Adapter 抽象和 capability |
| `internal/mcp` | `memory.observe` |
| `internal/ingest` | RawEvent normalization、dedup、content minimization |
| `internal/storage/sqlite` | agent_session、agent_task、raw_event |
| `internal/diagnostics` | session/task/raw_event/capture quality 查询 |
| Agent 配置 | Codex/Claude Code/Cursor 的 MCP 接入样例 |

### 5.4 Agent 接入目标

| Agent | P2 目标 |
|---|---|
| Claude Code | 优先 hooks + MCP，目标 Level3+ |
| Codex | MCP + wrapper/log collector，目标 Level2+ |
| Cursor | MCP + rules + 文件 diff 摘要，目标 Level2+ |

### 5.5 暂不做

1. 不要求自动生成长期记忆。
2. 不要求所有 Agent 达到 Level4。
3. 不做复杂任务边界识别，允许 default_task。
4. 不做自动 Retention。

### 5.6 验收标准

| 验收项 | 标准 |
|---|---|
| session 捕获 | 每个 Agent 能创建 session start/end |
| task 捕获 | 能创建 default_task 或明确 task |
| tool 捕获 | 至少能捕获工具名、输入摘要、输出摘要、exit code/hash |
| 文件编辑捕获 | 能捕获文件路径和 diff 摘要 |
| 去重 | 相同 content_hash 事件不会重复写入 |
| content boundary | 不保存完整 output 和完整 diff |
| capability | 每个 session 写入 capture_level 和 capture_quality |
| 捕获可观测 | 可按 agent/session/task/project 查询 raw_event 和 capture quality |

### 5.7 建议验收任务

1. Claude Code 运行一次测试失败，系统保存 tool result summary。
2. Codex 完成一次文件修改，系统保存 file edit summary。
3. Cursor 通过 rules 主动调用 `memory.observe` 上报用户声明。
4. 查询 raw_event，能按 session/task/project 过滤。
5. 查询 capture quality，能看出该 session 实际达到 Level2、Level3 或 Level4。

### 5.8 退出条件

P2 完成后，系统具备自动捕获事件、诊断捕获质量、验证事件边界的基础能力，但还不承诺自动生成高质量长期记忆。

## 6. P3：自动记忆可用

### 6.1 目标

从捕获事件中自动生成候选记忆，经过 Evidence、Admission、Review 和基础 Retention 进入可用长期记忆。

### 6.2 可用功能

1. 从 `raw_event` 抽取 `evidence`。
2. 从 evidence 生成 memory candidate。
3. 实现 Admission Control 公式。
4. 支持 `write_temporary`、`write_provisional`、`write_pending_review`、`write_stable`。
5. 高影响记忆进入 Review Queue。
6. 用户显式声明可自动 stable/durable。
7. 用户纠正可创建 supersedes 关系。
8. 基础 Retention Job：
   - access log 聚合
   - retention score 重算
   - temporary 清理
9. 支持 delete consistency。
10. 支持从设计复查类会话自动生成 `review_checkpoint` candidate：
   - 识别复查目标文档。
   - 抽取复查意图和结论。
   - 抽取已确认忽略或延期的问题。
   - 写入下次复查策略。
11. 支持异步任务诊断：
   - 查看 pending/running/failed job。
   - 查看候选记忆生成结果。
   - 区分 Admission 丢弃和 worker 失败。

### 6.3 研发范围

| 模块 | 内容 |
|---|---|
| `internal/ingest` | evidence extraction、candidate generation |
| `internal/memory` | admission、状态流转、版本化 |
| `internal/retention` | retention score、tier、cleanup |
| `internal/storage/sqlite` | async_job、memory_relation、review_checkpoint、tombstone |
| `internal/mcp` | review 完整动作 |
| `internal/diagnostics` | async_job、candidate、admission reason 查询 |

### 6.4 自动写入策略

| 来源 | 默认动作 |
|---|---|
| 用户声明 | stable 或 pending_review |
| 用户纠正 | stable，旧记忆降权或 superseded |
| 架构决策 | pending_review |
| 安全约束 | pending_review |
| 高影响失败 | pending_review |
| 普通工具输出 | temporary |
| 重复失败模式 | provisional |
| session summary | short_term |
| 设计复查结论 | review_checkpoint candidate，重大结论 pending_review |

### 6.5 暂不做

1. 不做复杂 LLM 多轮反思。
2. 不做企业级审批流。
3. 不做完整学习画像。
4. 不强制保证异步任务最终成功。

### 6.6 验收标准

| 验收项 | 标准 |
|---|---|
| 自动候选生成 | 工具失败事件能生成 failure candidate |
| Admission 生效 | 普通成功工具输出不进入长期记忆 |
| Review 生效 | 架构决策进入 pending_review |
| 用户纠正生效 | 新记忆 stable，旧记忆 archived 或 superseded |
| Retention 生效 | temporary 默认 5 天策略可计算 |
| 删除一致性 | deleted memory 不被 FTS 检索召回 |
| checkpoint 自动生成 | 设计复查会话结束后能生成 `review_checkpoint` candidate |
| 异步可观测 | 自动候选未生成时可区分 dropped、pending、failed |

### 6.7 建议验收任务

1. 运行多次测试失败，只保存错误签名和摘要。
2. 用户纠正数据库从 MySQL 迁移到 PostgreSQL。
3. 系统后续回答当前数据库时返回 PostgreSQL，并把 MySQL 作为历史信息。
4. 架构决策自动进入 pending_review，用户确认后变 stable。
5. 制造一个 extraction 失败任务，能在 async job 诊断中看到 failed，但不阻塞 Agent 主流程。
6. 完成一次设计文档复查后，系统自动生成 checkpoint candidate，用户确认后后续复查可召回。

### 6.8 退出条件

P3 完成后，系统开始具备真正的“自动长期记忆层”能力，并能解释自动写入链路的主要结果；检索质量仍以 FTS + 规则为主。

## 7. P4：检索增强和 Code Index 边界可用

### 7.1 目标

提升检索质量、上下文构造质量和代码相关任务连续性，同时保持 Code Index 与 Memory 的边界清晰。

### 7.2 可用功能

1. 实现 rerank 公式。
2. 支持 `retrieval_trace`。
3. 支持 `memory_access_log` score breakdown。
4. 支持 relation expansion。
5. 支持 code_ref resolution。
6. 实现轻量 Code Index Adapter。
7. 可选支持 sqlite-vec。
8. 支持 fallback：
   - FTS-only
   - FTS + metadata
   - FTS + relation
   - FTS + vector + relation
9. `memory.context` 支持 context budget 分配。
10. 支持设计复查场景的 checkpoint-aware 上下文构建。
11. 支持文档章节 hash 和 diff-aware 复查策略：
   - 文档未变化时，优先加载 checkpoint 和未覆盖风险。
   - 文档变化时，优先检查变化章节和受影响章节。

### 7.3 研发范围

| 模块 | 内容 |
|---|---|
| `internal/retrieval` | hybrid retrieval、rerank、context builder |
| `internal/codeindex` | 轻量 Git + tree-sitter/ctags Adapter |
| `internal/embedding` | embedding provider 抽象、sqlite-vec 可选 |
| `internal/storage/sqlite` | retrieval_trace、access_log、embedding |
| `internal/memory` | relation builder |
| `internal/docindex` | 文档路径、章节、hash、diff 摘要 |

### 7.4 Code Index 一期边界

一期只要求：

1. 文件路径定位。
2. 符号名定位。
3. 简单 symbol search。
4. code_ref resolve。
5. 文件结构摘要。

不强制要求：

1. 完整调用图。
2. 精确影响面分析。
3. 跨语言语义索引。
4. 大规模图数据库。

### 7.5 暂不做

1. 不做在线 LLM rerank。
2. 不做 Neo4j。
3. 不要求 embedding 必须启用。
4. 不做完整 Codegraph 自研。

### 7.6 验收标准

| 验收项 | 标准 |
|---|---|
| 检索 trace | 每次 search/context 生成 retrieval_trace |
| score breakdown | 被召回记忆有 score_breakdown |
| relation expansion | supersedes、supports、contradicts 可影响排序 |
| code_ref | 记忆能关联 repo/file/symbol/hash |
| Code Index 边界 | 调用关系不进入普通 memory_item |
| checkpoint-aware context | 设计复查任务优先召回最近相关 `review_checkpoint` |
| 文档 hash | 文档 hash 未变化时不强制全文重读，hash 变化时定位变化章节 |
| 降级 | sqlite-vec 不可用时系统仍可检索 |
| 延迟 | 轻量路径 P95 <= 100ms |

### 7.7 建议验收任务

1. 查询“为什么没有用 Kafka”，召回历史决策和 evidence。
2. 查询“继续 auth 问题”，召回失败经验、任务状态和 code_ref。
3. 分析代码调用关系时，调用关系进入 Code Index，不进入 Memory。
4. 禁用 sqlite-vec 后，FTS + relation 仍能返回可用上下文。
5. 对同一设计文档重复复查时，召回 checkpoint；若文档 hash 未变，避免加载完整历史对话。

### 7.8 退出条件

P4 完成后，系统检索和上下文构造能力进入可用状态，可以开始执行完整 MVP 验收集。

## 8. P5：多 Agent MVP 验收

### 8.1 目标

完成一期 MVP：Codex、Claude Code、Cursor 共享同一个本地 Memory Daemon，具备 Level4 目标捕获能力，并通过 10 个验收任务。

### 8.2 可用功能

1. 三个 Agent 都能接入同一个 Memory Daemon。
2. 三个 Agent 都能调用 MCP 工具。
3. 三个 Agent 都能上报 RawEvent。
4. 支持跨 Agent 共享 project/repo memory。
5. 支持 task/session 级归因。
6. 支持自动写入、review、检索、上下文注入。
7. 支持 MVP 指标采集。
8. 支持重复设计复查的 checkpoint 召回和历史上下文压缩。
9. 支持基础排障：
   - status
   - health
   - storage capability
   - capture quality
   - retrieval trace
   - failed async jobs

### 8.3 Level4 目标

| 能力 | 验收目标 |
|---|---|
| conversation capture | 捕获用户消息和 Agent 回复摘要 |
| tool call capture | 捕获工具名、调用时间、参数摘要 |
| tool output capture | 捕获 output 摘要、错误签名、exit code、hash |
| file edit capture | 捕获文件路径、符号、diff 摘要、content hash |
| session lifecycle | 捕获 session start/end、任务目标、任务结果 |
| memory observe | 能主动上报标准 RawEvent |

### 8.4 MVP 指标

| 指标 | 目标 |
|---|---:|
| Token savings | >= 30% |
| 重复上下文说明次数 | 降低 >= 50% |
| 历史决策召回准确率 | >= 80% |
| 错误记忆注入率 | <= 5% |
| 检索延迟 | P95 <= 100ms |
| 写入阻塞 | 不阻塞 Agent 主流程 |
| 跨 Agent 召回成功率 | >= 80% |
| Event capture completeness | >= 90% |
| 设计复查历史上下文 Token savings | >= 60% |

### 8.5 MVP 验收任务

| 任务 | 目标 |
|---|---|
| 1. 跨 Session 继续同一项目任务 | 验证任务连续性 |
| 2. 用户架构偏好应用 | 验证 `user_global` 偏好 |
| 3. 历史架构决策召回 | 验证 decision + evidence |
| 4. 避免重复踩坑 | 验证 failure/procedure 影响行为 |
| 5. 识别过期项目事实 | 验证 supersedes 和 temporal validity |
| 6. 多 Agent 共享同一项目上下文 | 验证 Codex/Claude/Cursor 共享 |
| 7. 临时工具输出不污染长期记忆 | 验证 Admission/Retention |
| 8. 源码结构事实不混入 Memory | 验证 Code Index 边界 |
| 9. 用户纠正后后续行为改变 | 验证负强化和版本化 |
| 10. 重复设计复查上下文压缩 | 验证 `review_checkpoint` 和文档 hash/diff-aware 复查 |

### 8.6 暂不做

1. 不做团队版权限。
2. 不做企业审计。
3. 不做备份恢复。
4. 不做学习画像。
5. 不做在线 LLM rerank。
6. 不做完整自研 Codegraph。

### 8.7 退出条件

P5 完成后，一期本地个人工具达到 MVP 标准，可以进入：

1. 小团队版本设计。
2. 企业平台能力设计。
3. 学习画像二期设计。
4. 更完整 Codegraph 集成。

## 9. 版本节奏建议

建议版本号：

| 版本 | 阶段 | 建议定位 |
|---|---|---|
| `v0.1.0` | P0 | 本地 daemon 技术预览 |
| `v0.2.0` | P1 | 手动记忆可用 |
| `v0.3.0` | P2 | 事件捕获可用 |
| `v0.4.0` | P3 | 自动记忆可用 |
| `v0.5.0` | P4 | 检索增强可用 |
| `v1.0.0` | P5 | 一期 MVP |

每个版本都应包含：

1. 可运行二进制。
2. migration。
3. 最小配置说明。
4. MCP 工具说明。
5. 验收脚本或验收清单。
6. 已知限制。

## 10. 研发优先级建议

优先级从高到低：

1. 数据模型和 migration 稳定。
2. MCP 工具协议稳定。
3. scope 隔离正确。
4. 不保存完整代码和完整 output。
5. FTS 检索可用。
6. Review 和用户纠错闭环。
7. Capture Adapter 覆盖率。
8. Retention/Admission 自动化。
9. Code Index 边界。
10. sqlite-vec 和 embedding 增强。

不建议提前投入：

1. 复杂 UI。
2. 企业权限模型。
3. 高可用部署。
4. 图数据库。
5. 在线 LLM rerank。
6. 完整学习画像。

## 11. 主要风险和控制点

| 风险 | 影响 | 控制点 |
|---|---|---|
| Agent 捕获能力不一致 | Level4 难以一次达成 | capability 探测、降级、验收分开统计 |
| 自动写入污染长期记忆 | 错误上下文反复注入 | Admission、pending_review、负强化 |
| 检索质量不稳定 | token savings 和任务成功率不达标 | retrieval_trace、score_breakdown、验收任务集 |
| SQLite 写锁阻塞 | 影响在线检索 | WAL、短事务、异步批处理 |
| Code Index 和 Memory 混淆 | 代码事实过期污染记忆 | code_ref 边界、验收任务 8 |
| 用户不处理 review | pending 堆积 | P3 后补 review backlog 提醒和默认降级 |

## 12. 推荐下一步

当前最适合进入 P0/P1 的实现设计，建议优先输出以下文档：

1. `P0 工程基座实现设计`
2. `SQLite Schema 与 Migration 设计`
3. `MCP 工具 JSON Schema 设计`
4. `P1 手动记忆可用验收清单`
