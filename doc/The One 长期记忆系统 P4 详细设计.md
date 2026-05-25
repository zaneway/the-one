# The One 长期记忆系统 P4 详细设计

> 基线来源：
> - `The One 长期记忆系统总体架构设计.md` v0.1 冻结版
> - `The One 长期记忆系统分期迭代研发规划.md`
> - `The One 长期记忆系统 P0-P1 详细设计.md`
> - `The One 长期记忆系统 P2 详细设计.md`
> - `The One 长期记忆系统 P3 详细设计.md`
>
> 前置说明：当前 P3 正在同步推进，本文档为了不阻塞后续研发先行给出 P4 详细设计。P4 的工程实现必须以前置 P3 Done 定义通过为准；若 P3 交付内容与 P3 详细设计存在偏差，P4 开工前必须先更新本文档的前置假设和接口边界。

## 1. 设计目标

P4 目标：

```text
在 P3 自动记忆闭环基础上，提升检索质量、上下文构造质量、检索可解释性、代码引用可用性和重复设计复查效率，同时保持 Memory 与 Code Index 的职责边界。
```

P4 完成后的业务闭环：

```text
memory.search / memory.context
  -> infer retrieval intent
  -> scope and metadata filter
  -> FTS retrieve
  -> optional vector retrieve
  -> relation expansion
  -> code_ref resolution
  -> rule-based rerank with score breakdown
  -> context budget packing
  -> write retrieval_trace
  -> write memory_access_log
```

设计复查闭环：

```text
review task
  -> detect target docs
  -> compute document and section hash
  -> compare last review_checkpoint target_hashes
  -> choose unchanged / changed / partial changed strategy
  -> recall checkpoint + changed sections + open risks
  -> build review context pack
  -> record retrieval_trace and access log
```

P4 的核心交付不是“让自动记忆更会写”，而是让“已有记忆更可靠地被找回、解释、压缩和反馈”。自动写入、Admission、Review、Retention 的基础闭环仍归 P3；P4 只在检索路径、访问反馈、关系扩展、代码引用和文档 diff-aware 复查上增强。

## 2. 阶段边界

### 2.1 必须交付

1. `retrieval_trace` 表、repository、service 和诊断查询。
2. `memory_access_log` 表、repository 和检索反馈写入。
3. 统一 `Retrieval Orchestrator`：
   - intent detection。
   - FTS 检索。
   - metadata filter。
   - relation expansion。
   - optional vector retrieval。
   - rule-based rerank。
   - context budget builder。
4. score breakdown：
   - BM25。
   - semantic/vector。
   - task fit。
   - scope fit。
   - retention。
   - relation support。
   - source quality。
   - recency。
   - conflict penalty。
   - staleness penalty。
   - context cost penalty。
5. `memory_relation` 在检索路径中的轻量 expansion。
6. `code_ref` 表、repository 和 MCP 响应字段落地。
7. 轻量 `Code Index Adapter` 抽象和默认本地实现。
8. `Doc Index`：
   - 文档路径识别。
   - Markdown 章节切分。
   - 文档 hash 和章节 hash。
   - 与 `review_checkpoint.target_hashes` 对比。
   - diff-aware 复查策略。
9. `memory.search` 返回真实 `retrieval_trace_id` 和 `score_breakdown`。
10. `memory.context` 按任务类型做 context budget 分配，并记录 `injected` access log。
11. embedding Provider、vector index 抽象和降级能力。
12. P4 单元测试、repository 测试、检索集成测试和验收脚本。

### 2.2 明确不交付

1. 不做在线 LLM rerank。
2. 不把外部 embedding API 放入在线 100ms 检索关键路径。
3. 不做完整自研 Codegraph。
4. 不做精确跨语言调用图。
5. 不做 Neo4j 或图数据库。
6. 不做 Elasticsearch。
7. 不做企业级权限、审计、备份恢复。
8. 不改变 P3 自动写入 Admission 语义。
9. 不把代码结构事实复制进 `memory_item`。
10. 不保存完整源码、完整 diff、完整工具 output 或完整历史对话。

### 2.3 P4 前置验收门槛

P4 可以先做接口和设计评审，但正式实现前必须确认 P3 至少满足：

| 前置项 | 要求 |
|---|---|
| P3 migration | `async_job`、`memory_candidate`、`memory_relation` 可用 |
| 自动 evidence | `raw_event` 能生成可解释 `evidence` |
| candidate 诊断 | 能区分 generated、dropped、admitted、failed |
| Admission | 普通成功工具输出不会进入长期记忆 |
| Review | 架构决策和高影响记忆可进入 pending_review |
| 用户纠正 | 命中旧 memory 时原地覆盖，保留 correction evidence 和 review 轨迹 |
| checkpoint candidate | 设计复查事件可生成 `review_checkpoint` |
| P1/P2 回归 | 手动记忆、检索、context、observe、capture diagnostics 仍通过 |

如果 P3 仅部分完成，P4 实现应优先选择不依赖 P3 异步链路的子任务：

1. `retrieval_trace`。
2. `memory_access_log`。
3. `memory.search/context` score breakdown。
4. `docindex` hash 计算。
5. `code_ref` 手动/显式写入和解析。

关系扩展和自动 code_ref 绑定可以等 P3 自动记忆稳定后再接入。

### 2.4 与 P3 的衔接

P4 复用 P3 能力：

1. `memory_relation` 中的 `supersedes`、`superseded_by`、`supports`、`contradicts` 兼容边。
2. `memory_item.retention_score`、`tier`、`confidence`、`importance`。
3. `memory_candidate` 的 `retrieval_cues_json`、`entities_json`、`tags_json`。
4. `review_checkpoint` 自动候选。
5. `async_job` 执行器。

P4 对 P3 的新增要求：

1. P3 写入 `memory_item` 时必须保证 `search_text` 完整。
2. P3 用户纠正默认不再创建 supersedes 关系；P4 如果遇到手工 seeded 或历史兼容的 supersedes/superseded_by，只在检索层解释，不反推 P3 自动纠正语义。
3. P3 不需要参与 P4 score 计算，但必须提供可被检索层读取的状态、tier、source_quality、review 轨迹和 relation 数据。
4. P3 当前未实现完整 delete consistency job；P4 必须在 P4-B1/B2 范围内补齐删除一致性对 relation、code_ref、embedding、access_log 的处理。

### 2.5 与 P1/P2 的衔接

P4 不重写 P1 的手动记忆接口，也不重写 P2 的 RawEvent 捕获接口。

P4 对 P1/P2 的增强点：

1. P1 `memory.search/context` 中的占位 `retrieval_trace_id` 变为真实持久化 trace。
2. P1 `context` 的简单 token 裁剪升级为按任务意图分配预算。
3. P2 捕获的 `source_refs.file_path/symbol/content_hash` 可被 P4 解析为 `code_ref`。
4. P2/P3 写入的设计复查 checkpoint 可被 P4 用文档 hash 验证是否仍有效。

### 2.6 与当前 memory.Service 的接入方式

P4 不直接在 SQLite repository 内重写 P1 检索逻辑，而是在 `memory.Service` 增加可选 Retrieval Orchestrator 依赖：

```go
type RetrievalOrchestrator interface {
	Search(ctx context.Context, req memory.SearchRequest) (memory.SearchResponse, error)
	Context(ctx context.Context, req memory.ContextRequest) (memory.ContextPack, error)
}
```

接入策略：

1. `memory.Service` 构造时如果注入 Orchestrator，`Search/Context` 走 P4。
2. 未注入 Orchestrator 时保持 P1 原有 FTS repository 路径，用于回归和降级。
3. P4 Orchestrator 复用 P1 scope validator 和请求 DTO，不绕过已有内容边界。
4. P4 响应结构通过向后兼容字段扩展 `SearchResult/ContextPack`，不得破坏 P1/P2/P3 已有调用。
5. `memory.remember/review` 仍由 P1/P3 service/repository 处理，P4 不接管写入。

## 3. 总体架构

```text
memory.search / memory.context
    |
    +-- Retrieval Tool Adapter
    |     - request validation
    |     - scope validator
    |     - content boundary
    |
    v
Retrieval Orchestrator
    |
    +-- Intent Detector
    |     - general search
    |     - continuation
    |     - architecture review
    |     - code task
    |     - failure recall
    |
    +-- Candidate Retrieval
    |     - FTS5
    |     - metadata
    |     - optional vector
    |
    +-- Expansion
    |     - memory_relation
    |     - code_ref resolution
    |     - doc checkpoint matching
    |
    +-- Rerank
    |     - score formula
    |     - conflict/staleness filter
    |     - score_breakdown
    |
    +-- Context Builder
    |     - budget allocation
    |     - compression
    |     - why_included
    |
    +-- Diagnostics
          - retrieval_trace
          - memory_access_log
```

推荐新增代码目录：

```text
internal/retrieval
internal/codeindex
internal/docindex
internal/embedding
internal/storage/sqlite/retrieval_repository.go
internal/storage/sqlite/code_ref_repository.go
internal/storage/sqlite/doc_index_repository.go
internal/mcp/tools/retrieval.go
internal/storage/sqlite/migrations/0006_init_retrieval.sql
```

模块职责：

| 模块 | 职责 |
|---|---|
| `internal/retrieval` | Orchestrator、intent、candidate、rerank、context builder、trace 写入 |
| `internal/codeindex` | Code Index Adapter 抽象、本地轻量符号索引、code_ref resolve |
| `internal/docindex` | 文档章节切分、hash、checkpoint diff-aware 策略 |
| `internal/embedding` | embedding Provider 抽象、none/local 能力探测 |
| `internal/storage` | P4 repository 接口定义 |
| `internal/storage/sqlite` | trace/access log/code_ref/doc_hash/embedding 持久化 |
| `internal/mcp/tools/retrieval.go` | retrieval trace、access log、code ref、docindex 诊断工具 |

## 4. Retrieval Orchestrator 设计

### 4.1 Orchestrator 定位

Retrieval Orchestrator 负责把一次用户任务转换为可解释的记忆召回和上下文注入结果。

它负责：

1. 识别检索意图。
2. 选择检索模式。
3. 执行候选召回。
4. 执行 relation/code/doc 扩展。
5. 计算排序分数。
6. 构造上下文包。
7. 写入 trace 和 access log。

它不负责：

1. 从 raw_event 自动生成记忆。
2. 改变 Admission 决策。
3. 写入新的长期记忆。
4. 调用 LLM 进行在线 rerank。
5. 读取完整源码、完整文档历史或完整工具 output。

### 4.2 核心接口

```go
package retrieval

type Orchestrator interface {
	Search(ctx context.Context, req SearchRequest) (*SearchResult, error)
	BuildContext(ctx context.Context, req ContextRequest) (*ContextResult, error)
}
```

当前 P4-D1 / P4-C1 / P4-C2 已落地：

1. `internal/retrieval` 定义上述 Orchestrator、intent/mode、candidate、trace/access log、budget DTO。
2. `internal/memory` 增加可选 `RetrievalOrchestrator` 注入点，但不直接 import `internal/retrieval`，避免包循环。
3. `memory.Service` 未注入 Orchestrator 时继续使用 P1 FTS + metadata 路径；注入后仅 `Search/Context` 委托 P4，`Remember/Review` 仍由 P1/P3 原服务处理。
4. `memory.SearchResult/ContextPack/SearchDiagnostics` 已增加 P4 兼容字段；P4-C1/C2 已通过 `MemoryOrchestrator` 填充 trace、score_breakdown、relation expansion，不破坏 MCP 对外 DTO。
5. P4-C3/C4/C5/C6 已接入 Code Index Adapter、Doc Index review strategy、context budget builder 和诊断工具；`used_code_index/used_doc_index` 会按实际触发路径写入 trace 和 diagnostics。

内部模型：

```go
type RetrievalIntent string

const (
	IntentGeneralSearch       RetrievalIntent = "general_search"
	IntentTaskContinuation    RetrievalIntent = "task_continuation"
	IntentArchitectureReview  RetrievalIntent = "architecture_review"
	IntentCodeTask            RetrievalIntent = "code_task"
	IntentFailureRecall       RetrievalIntent = "failure_recall"
	IntentUserPreference      RetrievalIntent = "user_preference"
)

type RetrievalMode string

const (
	ModeFTSOnly             RetrievalMode = "fts_only"
	ModeFTSMetadata         RetrievalMode = "fts_metadata"
	ModeFTSRelation         RetrievalMode = "fts_relation"
	ModeFTSVectorRelation   RetrievalMode = "fts_vector_relation"
	ModeCheckpointAware     RetrievalMode = "checkpoint_aware"
	ModeCodeAware           RetrievalMode = "code_aware"
)
```

`Candidate`：

```go
type Candidate struct {
	Memory             memory.MemoryItem
	FTSScore           float64
	SemanticScore      float64
	TaskFit            float64
	ScopeFit           float64
	RetentionScore     float64
	RelationSupport    float64
	SourceQuality      float64
	RecencyFit         float64
	ConflictPenalty    float64
	StalenessPenalty   float64
	ContextCostPenalty float64
	FinalScore         float64
	ScoreBreakdown     ScoreBreakdown
	InclusionReasons   []string
	RelatedMemoryIDs   []string
	CodeRefs           []memory.CodeRef
}
```

### 4.3 检索流程

```text
validate request
  -> create retrieval_trace(status=started)
  -> infer intent
  -> resolve scopes
  -> fts retrieve
  -> metadata filter
  -> optional vector retrieve
  -> merge and dedup candidates
  -> relation expansion
  -> code_ref resolve
  -> doc checkpoint matching
  -> compute score breakdown
  -> filter invalid/stale/conflict
  -> rerank
  -> context pack if memory.context
  -> write access logs
  -> update retrieval_trace(status=completed)
```

失败策略：

| 阶段 | 失败处理 |
|---|---|
| trace 写入失败 | 记录日志，检索继续，响应 diagnostics 标记 `trace_unavailable` |
| FTS 失败 | 返回降级错误；P4 不要求无 FTS 检索可用 |
| vector 失败 | 降级为 FTS + metadata + relation |
| relation 查询失败 | 降级为 FTS + metadata，`fallback_reason=relation_failed` |
| code index 失败 | 不阻断检索，code_ref 标记 unresolved |
| doc hash 失败 | 不阻断 checkpoint 召回，复查策略退化为读取当前文档关键章节 |
| access log 写入失败 | 不影响响应，但必须记录日志和 diagnostics |

### 4.4 Intent Detection

P4 使用规则优先，不引入 LLM intent 分类。

| 意图 | 触发信号 | 影响 |
|---|---|---|
| `general_search` | 普通 query | FTS + metadata + relation |
| `task_continuation` | “继续”、“上次”、“当前任务” | 提升 session/task、temporary、failure 权重 |
| `architecture_review` | “复查”、“架构评审”、“详细设计”、“逻辑缺失”、“checkpoint” | 优先 checkpoint + docindex |
| `code_task` | 包含文件、函数、模块、报错栈、symbol | 启用 code_ref 和 Code Index |
| `failure_recall` | “失败”、“报错”、“踩坑”、“为什么又” | 提升 failure/procedure |
| `user_preference` | “偏好”、“以后”、“我的习惯” | 提升 user_global preference/procedure |

## 5. Rerank 设计

### 5.1 排序公式

P4 使用总体架构中的一期公式：

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

实现要求：

1. 所有分数归一化到 `[0,1]`。
2. 如果 embedding provider 或 vector index 不可用，`semantic_score=0`，正向权重按剩余项归一化。
3. `retrieval_score = clamp(raw_score, 0, 1)`。
4. `retrieval_score` 只用于排序，不直接改变 retention。
5. 每条返回结果必须包含 `score_breakdown_json`。
6. P4 不允许用不可解释的黑盒 rerank 替代该公式。

### 5.1.1 Score Component Spec

P4 必须固定各分量的估算方式，避免不同实现产生不可比较的检索结果。

| 分量 | 输入 | 估算方式 | 缺失默认值 |
|---|---|---|---:|
| `semantic_score` | query embedding、memory embedding | vector index 返回的相似度归一化到 `[0,1]` | `0` |
| `bm25_score` | FTS5 rank | 同批候选内 min-max 归一化；只有一个 FTS 命中时取 `0.8` | `0` |
| `task_fit` | task/query、title、content、keywords、retrieval_cues | keyword overlap + memory_type intent boost，clamp 到 `[0,1]` | `0.3` |
| `scope_fit` | request scope、memory scope、workspace/project/repo/session | 精确 scope 命中 `1.0`，上级 user_global preference `0.8`，session continuation `0.7`，否则 `0` | `0` |
| `retention_score` | `memory_item.retention_score` | 直接使用，空值按 tier 推导 | tier 默认 |
| `relation_support` | relation expansion | supports `+0.2`、supersedes newest `+0.3`、contradicts 不加分，clamp 到 `1.0` | `0` |
| `source_quality` | `memory_item.source_quality`、evidence confidence | 直接使用；缺失时按 source_type 估算 | `0.7` |
| `recency_fit` | `updated_at/last_validated_at/last_accessed_at` | 7 天内 `1.0`，30 天内 `0.6`，90 天内 `0.3`，否则 `0.1` | `0.1` |
| `conflict_penalty` | contradicts relation、superseded、同 scope 冲突 | unresolved contradicts `0.7`，疑似冲突 `0.3` | `0` |
| `staleness_penalty` | `valid_until`、superseded、resolve_status | 过期 `0.8`，superseded `1.0`，code_ref stale `0.2` | `0` |
| `context_cost_penalty` | content/token 估算、evidence/code/doc refs 数量 | 当前候选预计 token / token_budget，最大 `1.0` | `0.1` |

tier 默认 retention：

| tier | 默认 retention_score |
|---|---:|
| `temporary` | `0.2` |
| `short_term` | `0.4` |
| `long_term` | `0.7` |
| `durable` | `0.95` |

`task_fit` 的 intent boost：

| intent | boost memory_type |
|---|---|
| `architecture_review` | `review_checkpoint/decision/constraint/open_issue` |
| `code_task` | 带 `code_ref` 的 `failure/procedure/project_fact/decision` |
| `failure_recall` | `failure/procedure` |
| `task_continuation` | `temporary_state/session_summary/failure` |
| `user_preference` | `preference/procedure` |

### 5.2 score_breakdown

```json
{
  "semantic": 0.0,
  "bm25": 0.82,
  "task_fit": 0.74,
  "scope_fit": 1.0,
  "retention": 0.67,
  "relation_support": 0.3,
  "source_quality": 0.8,
  "recency_fit": 0.4,
  "conflict_penalty": 0.0,
  "staleness_penalty": 0.0,
  "context_cost_penalty": 0.1,
  "final": 0.71,
  "fallback": ["vector_disabled"]
}
```

`score_breakdown_json` 用于：

1. 解释为什么被召回。
2. 解释为什么没被注入。
3. 分析误召回和漏召回。
4. 支撑 P5 的历史决策召回准确率和错误注入率统计。

### 5.3 过滤规则

默认过滤：

1. `state=deleted`。
2. `state=archived` 且 `include_archived=false`。
3. scope 不匹配。
4. `valid_until < now`，除非 query 明确查询历史。
5. 通过显式 `superseded_by` relation 标记为已替代的旧记忆。
6. 与 stable 记忆存在 unresolved `contradicts` 且未进入 review 的候选。

降级注入：

| 记忆状态 | 行为 |
|---|---|
| `pending_review` | 可召回，但必须标记未确认 |
| `provisional` | 仅在强相关且无 stable 替代时注入 |
| `temporary` | 只用于 task continuation，不进入长期结论 |
| `archived` | 默认不注入，只在历史查询中作为摘要 |

### 5.4 候选合并和稳定排序

候选合并规则：

1. 候选唯一键为 `memory_id`。
2. FTS、vector、relation expansion 命中同一 memory 时合并为一条 candidate。
3. `bm25_score` 和 `semantic_score` 分别保留各自来源分数，不互相覆盖。
4. relation expansion 只能补充 `relation_support`、`RelatedMemoryIDs` 和 inclusion reason。
5. relation expansion 后的候选总数不得超过 `retrieval.max_candidates_before_rerank`；超出时保留原始 FTS/vector 候选和强关系候选。

稳定排序规则：

```text
final_score desc
state priority stable > pending_review > provisional > temporary
tier priority durable > long_term > short_term > temporary
last_validated_at desc nulls last
updated_at desc
memory_id asc
```

同分排序必须稳定，避免同一 query 在无数据变化时产生上下文抖动。

## 6. Relation Expansion 设计

### 6.1 支持的关系

P4 检索路径使用的关系类型：

| relation_type | 语义 | 检索影响 |
|---|---|---|
| `supersedes` | 新记忆替代旧记忆，主要用于手工 seeded 或历史兼容边 | 提升新记忆，抑制旧记忆 |
| `superseded_by` | 旧记忆被替代，主要用于手工 seeded 或历史兼容边 | 默认过滤旧记忆 |
| `supports` | 支持关系 | 提升 source/target |
| `contradicts` | 冲突关系 | 增加 conflict penalty |

P4 必交付只使用已持久化的四类关系，不在本阶段新增自动关系构建语义。当前 P3 用户纠正默认采用原地覆盖，不会自动产生 supersedes/superseded_by；P4 测试和验收如需验证 supersedes 行为，应通过手工 seeded relation 或历史兼容数据构造。

预留关系类型：

| relation_type | 预留用途 | P4 处理 |
|---|---|---|
| `caused_by` | 失败原因 | 仅预留，不参与默认排序 |
| `precedes` | 时间先后 | 仅预留，不参与默认排序 |
| `refines` | 更精确版本 | 仅预留，不参与默认排序 |
| `related_to` | 弱相关 | 仅预留，不参与默认排序 |
| `linked_to_long_term` | 短期信息关联长期记忆 | 仅预留，不参与默认排序 |

上述预留关系只允许在 enum、诊断展示和后续 migration 兼容层保留，P4 默认不生成、不依赖、不纳入验收。

### 6.2 Expansion 策略

```text
seed candidates
  -> load outgoing/incoming edges depth=1
  -> include strong relation targets
  -> compute relation_support
  -> apply conflict/staleness penalty
  -> rerank with original candidates
```

P4 默认只做 depth=1 关系扩展。

限制：

1. 单次最多扩展 `retrieval.max_relation_expansion` 条，默认 20。
2. `related_to`、`caused_by`、`precedes`、`refines`、`linked_to_long_term` 在 P4 只作为预留关系，不参与默认排序。
3. `contradicts` 默认不直接注入冲突双方，而是标记需要 review 或作为 diagnostics。
4. `supersedes` 是强状态信号，优先于 BM25，但仅适用于显式存在的 relation；不得假设所有用户纠正都有 supersedes 边。

## 7. Code Index 与 code_ref 设计

### 7.1 边界原则

Code Index 负责代码结构事实：

```text
文件、符号、函数、类型、路由、import、轻量结构摘要
```

Memory 负责长期经验：

```text
历史决策、设计原因、用户偏好、项目约束、失败经验、过程规则、review checkpoint
```

P4 必须保证：

1. 调用关系、import 关系、文件结构不得作为普通 `memory_item` 长期保存。
2. 如果某条记忆与代码相关，只保存 `code_ref`。
3. `code_ref` 保存定位、hash 和摘要，不保存源码。
4. 代码结构变化通过 Code Index 重新解析，不通过 Memory 纠正。

### 7.2 Code Index Adapter

```go
package codeindex

type Adapter interface {
	Name() string
	Capabilities(ctx context.Context) (Capabilities, error)
	SearchSymbols(ctx context.Context, req SymbolSearchRequest) ([]SymbolRef, error)
	GetSymbol(ctx context.Context, req GetSymbolRequest) (*SymbolRef, error)
	GetCallers(ctx context.Context, req SymbolGraphRequest) ([]SymbolRef, error)
	GetCallees(ctx context.Context, req SymbolGraphRequest) ([]SymbolRef, error)
	GetImpact(ctx context.Context, req ImpactRequest) (*ImpactResult, error)
	GetFileStructure(ctx context.Context, req FileStructureRequest) (*FileStructure, error)
	BuildTaskContext(ctx context.Context, req TaskContextRequest) (*TaskCodeContext, error)
	ResolveCodeRefs(ctx context.Context, refs []CodeRef) ([]ResolvedCodeRef, error)
}
```

P4 默认实现：

```text
local_basic
  -> git ls-files
  -> file path match
  -> regex / lightweight symbol scan
  -> optional ctags if installed
```

默认实现只保证：

1. 文件路径定位。
2. 简单符号名定位。
3. 文件结构摘要。
4. code_ref resolve。
5. `GetSymbol` 对已解析 symbol 返回定位信息。

默认实现可以降级或返回 unsupported：

1. 精确调用图。
2. 跨语言类型推断。
3. 精确影响面。
4. LSP 生命周期管理。

接口保留 `GetCallers`、`GetCallees`、`GetImpact` 和 `BuildTaskContext` 的原因是保持与总体架构和后续 P5 验收一致。`local_basic` 不需要伪造调用关系；当无法从 ctags 或轻量扫描获得可靠结果时，必须通过 capability 和响应状态明确返回 `unsupported` 或 `degraded`，不得把不可靠调用关系写入 Memory。

### 7.3 code_ref 数据模型

```sql
create table if not exists code_ref (
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
  resolve_status      text not null default 'unresolved',
  resolved_at         datetime,
  created_at          datetime not null,
  updated_at          datetime not null
);

create index if not exists idx_code_ref_memory
  on code_ref(memory_id);

create index if not exists idx_code_ref_repo_file
  on code_ref(repo_id, file_path, symbol);

create index if not exists idx_code_ref_status
  on code_ref(resolve_status, updated_at);
```

`resolve_status`：

```text
unresolved
resolved
stale
missing
ambiguous
```

### 7.4 code_ref 来源

P4 支持三类来源：

| 来源 | 生成方式 | 状态 |
|---|---|---|
| 显式 remember | 用户或 Agent 写入 source_ref file/symbol | 可同步生成 |
| P2 raw_event | `file.edit.summary`、`tool.result.summary` 的 `source_refs_json` | 异步生成 |
| P3 candidate | `retrieval_cues_json`、`entities_json`、`tags_json` 中的 file/symbol hint | 异步生成 |
| P3 evidence | `evidence.source_ref_json` 中的 file/symbol/content_hash | 异步生成 |

P4 不要求 P3 自动候选都带 code_ref。没有 code_ref 的记忆仍可通过 FTS/metadata/relation 工作。

code_ref 来源优先级：

1. `evidence.source_ref_json`。
2. `raw_event.source_refs_json`。
3. `memory_item.retrieval_cues_json/entities_json/tags_json`。
4. `memory_candidate.retrieval_cues_json/entities_json/tags_json`。

当前 P3 `memory_candidate` 不包含 candidate-level `source_ref` 字段，P4 实现不得假设该字段存在；需要 source ref 时必须通过 `evidence_id/raw_event_id` 回表读取。

code_ref 解析可以在线 best-effort 执行，也可以通过异步任务执行：

| 场景 | 执行方式 |
|---|---|
| `memory.context` 返回少量已存在 code_ref | 在线 resolve，超时降级 |
| `memory.remember` 显式 source_ref | 同步写入 code_ref，异步刷新状态 |
| P2/P3 事件或 candidate 解析 | enqueue `resolve_code_ref` |
| repo 文件变化后重校验 | enqueue `refresh_code_ref_status` |

在线 resolve 不得扫描大文件或构建全量索引。超出 `codeindex.max_resolve_refs`、文件过大或 Adapter 不支持时，响应中保留 unresolved/stale 状态，并记录 diagnostics。

### 7.5 P4-C3 阶段范围（摘要）

P4-C3 在 **§17.1** 给出完整阶段设计。本节只保留架构边界；实现细节、接口、验收和与 Orchestrator 的接入点以 §17.1 为准。

C3 必交付：

1. `internal/codeindex` 包与 `local_basic` Adapter。
2. 在线 best-effort `ResolveCodeRefs`，结果写回 `code_ref.resolve_status/content_hash/ref_summary`。
3. `memory.search/context` 在 `code_task` 或 `include_code_refs=true` 时返回已持久化 `code_refs`。
4. `resolve_code_ref` / `refresh_code_ref_status` async job 从“仅落库 payload”升级为真实解析。
5. `staleness_penalty` 纳入 `stale/missing/ambiguous` 的 code_ref 状态。

C3 明确不交付：

1. 精确调用图、影响面、LSP/SCIP Adapter。
2. 把调用关系/import 图写入 `memory_item`。
3. 在线全仓库索引扫描。

## 8. Doc Index 与 diff-aware 复查设计

### 8.1 设计目标

Doc Index 用于解决重复设计复查的上下文压缩问题：

```text
不重复加载完整历史对话，而是用 checkpoint + 当前文档 hash/diff 判断复查重点。
```

Doc Index 不替代文档事实源。每次复查仍必须读取当前文档或变化章节，不能只依赖历史 checkpoint。

### 8.2 文档模型

P4 默认只支持 Markdown 文档。

```go
type DocumentSnapshot struct {
	Path         string
	Role         string
	ContentHash  string
	ModifiedAt   time.Time
	Sections     []DocumentSection
}

type DocumentSection struct {
	SectionID    string
	HeadingPath  []string
	Level        int
	StartLine    int
	EndLine      int
	ContentHash  string
	Summary      string
}
```

章节 hash 计算规则：

1. 统一换行符。
2. 去除行尾空白。
3. 保留标题文本。
4. 不保存完整章节正文到数据库。
5. hash 输入来自当前本地文件内容，但持久层只保存 hash、路径、行号、标题和摘要。

路径安全规则：

1. `doc_path` 必须先 canonicalize，再校验位于当前 `workspace_id/repo_id` 对应根目录内。
2. 拒绝 `..` 路径穿越、绝对路径越界和指向 workspace/repo 外部的 symlink。
3. 只读取配置允许的文档后缀，P4 默认只允许 `.md` 和 `.markdown`。
4. 超过 `docindex.max_doc_size_kb` 的文档只计算路径级 hash，不做章节切分。
5. 诊断工具返回路径、hash、标题和摘要，不返回完整文档正文。

### 8.3 doc_index 数据模型

```sql
create table if not exists doc_snapshot (
  id                  text primary key,
  workspace_id        text not null,
  project_id          text not null default '',
  repo_id             text not null default '',
  doc_path            text not null,
  doc_role            text,
  content_hash        text not null,
  modified_at         datetime,
  section_count       integer not null default 0,
  created_at          datetime not null
);

create index if not exists idx_doc_snapshot_scope
  on doc_snapshot(workspace_id, project_id, repo_id, doc_path, created_at);

create unique index if not exists idx_doc_snapshot_dedup
  on doc_snapshot(workspace_id, project_id, repo_id, doc_path, content_hash);

create table if not exists doc_section_snapshot (
  id                  text primary key,
  snapshot_id         text not null,
  section_id          text not null,
  heading_path_json   text,
  level               integer,
  start_line          integer,
  end_line            integer,
  content_hash        text not null,
  summary             text,
  created_at          datetime not null
);

create index if not exists idx_doc_section_snapshot
  on doc_section_snapshot(snapshot_id, section_id);
```

`summary` 是可选字段，只能保存简短章节摘要，不保存完整章节正文。

snapshot 控制策略：

1. 相同 `workspace_id + project_id + repo_id + doc_path + content_hash` 幂等复用已有 snapshot。
2. `project_id/repo_id` 在持久层统一使用空字符串表达“不适用”，不得存 NULL，避免 SQLite unique index 因 NULL 语义失效。
3. 每个文档默认最多保留最近 `docindex.max_snapshots_per_doc` 个不同 content hash，默认 10。
4. 被 `review_checkpoint.target_hashes` 引用的 snapshot 不自动清理。
5. 清理 snapshot 时必须级联删除对应 `doc_section_snapshot`。
6. snapshot 清理只影响 docindex 诊断，不得删除 `review_checkpoint` 历史结论。

### 8.3.1 target_hashes 兼容 Schema

P4 写入或比较 `review_checkpoint.target_hashes_json` 时使用统一 schema：

```json
{
  "doc_path": "doc/The One 长期记忆系统 P4 详细设计.md",
  "doc_role": "implementation_design",
  "hash_algo": "sha256",
  "normalization": "lf_trim_trailing_space",
  "content_hash": "sha256:doc_hash",
  "sections": [
    {
      "section_id": "8.3",
      "heading_path": ["8. Doc Index 与 diff-aware 复查设计", "8.3 doc_index 数据模型"],
      "section_hash": "sha256:section_hash"
    }
  ],
  "normalized_at": "2026-05-24T00:00:00Z"
}
```

兼容规则：

1. P3 事件中只有 `content_hash` 时，P4 只能做文档级比较。
2. P3 事件中没有 `hash_algo` 时，默认解释为 `sha256`，但 diagnostics 标记 `hash_algo_inferred`。
3. `doc_path` 必须按 P4 路径安全规则 canonicalize 后再比较。
4. section hash 缺失时，不得声称已完成 changed-section 精确定位，只能返回 `doc_changed_unknown_sections`。

### 8.4 checkpoint 匹配策略

输入：

1. 当前任务文本。
2. 当前 repo/project。
3. 目标文档路径。
4. 最近相关 `review_checkpoint`。
5. 当前文档 snapshot。

匹配流程：

```text
find target docs
  -> compute current document snapshot
  -> load latest review_checkpoint for same doc/project
  -> compare document hash
  -> compare section hashes
  -> build review strategy
```

策略：

| 场景 | 策略 |
|---|---|
| 文档 hash 未变化 | checkpoint first，复查重点转向未覆盖风险、新需求和开放项 |
| 文档 hash 变化但章节 hash 少量变化 | checkpoint + changed sections + impacted nearby sections |
| 文档结构大幅变化 | checkpoint 只作历史摘要，重新读取当前文档关键章节 |
| 无 checkpoint | 执行完整当前文档复查，并建议生成 checkpoint |
| checkpoint 指向文档不存在 | 标记 stale，降级为普通文档复查 |

P4-C4 当前实现边界：

1. `docindex.BuildMarkdownSnapshot` 在线读取当前 Markdown 文档，执行路径安全校验、换行归一化、行尾空白裁剪、文档 hash 和章节 hash 计算。
2. `build_doc_snapshot` 异步任务在 payload 未携带 `content_hash` 时可按 `doc_path + repo_id` 直接构建 snapshot；payload 已携带预计算 snapshot 时保持兼容写入。
3. `memory.context` 仅在架构复查任务且任务文本显式包含 `.md/.markdown` 路径时启用 Doc Index，返回 `context_pack.review_strategy`。
4. `memory.context` 优先使用命中的 `review_checkpoint.target_hashes_json` 作为复查基线；checkpoint 只有文档级 hash 且缺少 section hash 时，可降级使用历史 `doc_snapshot` 计算 changed sections。
5. 当前 strategy 支持 `full_document`、`changed_sections`、`checkpoint_only` 三类模式，并写入 `retrieval_trace.used_doc_index` 与 `diagnostics.used_doc_index`。
6. C4 不实现 vector retrieval、不读取或持久化完整文档正文、不做复杂语义 diff；章节摘要只来自标题路径。

### 8.5 设计复查 context budget

设计复查任务默认预算：

| 内容 | 默认预算 |
|---|---:|
| 冻结设计基线和相关架构决策 | 25% |
| 最近 `review_checkpoint` | 30% |
| 用户确认忽略或延期的问题 | 15% |
| 当前文档变化章节或章节摘要 | 20% |
| evidence 和 code_ref 摘要 | 10% |

如果文档 hash 未变化：

1. 降低当前文档摘要预算。
2. 提升开放风险、未覆盖边界、近期新增记忆预算。
3. 不重复提出用户已确认忽略的问题。

如果文档 hash 变化：

1. 变化章节优先进入上下文。
2. 上次 checkpoint 的结论只能作为历史参考。
3. 对变化章节相关的历史决策和 open_issue 增加权重。

## 9. Embedding 与 Vector Index 设计

### 9.1 定位

P4 将 embedding 和 vector index 定义为两个独立维度，二者都是可选增强，不作为必交付成功条件。

必交付路径：

```text
FTS5 + metadata + relation + rule rerank
```

增强路径：

```text
FTS5 + metadata + embedding provider + vector index + relation + rule rerank
```

### 9.2 Provider 抽象

```go
package embedding

type Provider interface {
	Name() string
	Dim() int
	EmbedMemory(ctx context.Context, input EmbedInput) ([]float32, error)
	EmbedQuery(ctx context.Context, input EmbedInput) ([]float32, error)
}
```

P4 embedding provider：

| provider | 行为 |
|---|---|
| `none` | 默认，不生成向量 |
| `local_stub` | 测试用固定向量，不用于生产质量评估 |

外部 Provider 如 OpenAI、DeepSeek、Minimax 不进入 P4 在线关键路径。若后续实现，只能用于异步 embedding 构建或显式低频操作。

vector index backend：

| backend | 行为 |
|---|---|
| `none` | 默认，不做向量存储和检索 |
| `blob` | 仅保存向量 blob，P4 不要求高效近邻检索 |
| `sqlite_vec` | 使用 sqlite-vec virtual table 或等价封装执行向量检索 |

`sqlite_vec` 是 vector index 能力，不是 embedding provider。只有当 `embedding.provider != none` 且 `vector_index.backend` 可用时，在线检索才可能产生非零 `semantic_score`。

### 9.3 memory_embedding 数据模型

```sql
create table if not exists memory_embedding (
  memory_id           text not null,
  embedding_model     text not null,
  embedding_dim       integer not null,
  embedding           blob not null,
  created_at          datetime not null,
  updated_at          datetime not null,
  primary key(memory_id, embedding_model)
);

create index if not exists idx_memory_embedding_model
  on memory_embedding(embedding_model, updated_at);
```

如果 sqlite-vec 可用，可以用 virtual table 替代 blob 存储，但 repository 接口必须保持一致。主键必须允许同一 memory 并存多个 embedding model，便于模型升级、灰度和回滚；默认检索使用配置中声明的 active model。

### 9.4 在线降级

```text
if embedding.provider == none or vector_index.backend == none:
    semantic_score = 0
    used_vector = false
    fallback_reason includes vector_disabled
else if query embedding cache hit:
    use vector retrieval
else if local embedding latency within online budget:
    compute query embedding
else:
    fallback to FTS + metadata + relation
```

约束：

1. 不为 query embedding 建持久化表。
2. query embedding 只进进程内 LRU。
3. 外部网络 embedding 不进入 `memory.context` 默认路径。
4. `memory.status` 必须分别展示 embedding provider、vector index capability 和 fallback retrieval。

## 10. 核心数据模型

### 10.1 retrieval_trace

```sql
create table if not exists retrieval_trace (
  id                  text primary key,
  session_id          text,
  task_id             text,
  workspace_id        text,
  project_id          text,
  repo_id             text,
  query               text,
  task                text,
  retrieval_intent    text,
  retrieval_mode      text,
  used_fts            boolean not null default false,
  used_vector         boolean not null default false,
  used_relation       boolean not null default false,
  used_code_index     boolean not null default false,
  used_doc_index      boolean not null default false,
  fallback_reason     text,
  candidate_count     integer not null default 0,
  injected_count      integer not null default 0,
  latency_ms          integer,
  status              text not null default 'completed',
  created_at          datetime not null
);

create index if not exists idx_retrieval_trace_scope
  on retrieval_trace(workspace_id, project_id, repo_id, created_at);

create index if not exists idx_retrieval_trace_task
  on retrieval_trace(session_id, task_id, created_at);
```

约束：

1. `query` 和 `task` 只保存归一化短文本。
2. 不保存完整 prompt、完整代码、完整文档正文或完整工具输出。
3. trace 是 request-level 诊断记录，允许同一请求内从 `started` 更新为 `completed/failed`。
4. 已完成的 trace 不得被后续请求改写；如需更细粒度事件流，后续可新增 `retrieval_trace_event`。

### 10.2 memory_access_log

```sql
create table if not exists memory_access_log (
  id                    text primary key,
  memory_id             text not null,
  session_id            text,
  task_id               text,
  retrieval_trace_id    text,
  event_type            text not null,
  event_weight          real not null,
  source_type           text,
  source_quality        real not null default 0.7,
  query                 text,
  rank                  integer,
  score                 real,
  score_breakdown_json  text,
  inclusion_reason_json text,
  used_in_context       boolean not null default false,
  feedback              text,
  created_at            datetime not null
);

create index if not exists idx_memory_access_log_memory
  on memory_access_log(memory_id, created_at);

create index if not exists idx_memory_access_log_trace
  on memory_access_log(retrieval_trace_id, rank);

create index if not exists idx_memory_access_log_task
  on memory_access_log(session_id, task_id, created_at);
```

推荐事件：

| event_type | event_weight |
|---|---:|
| `retrieved` | 0.2 |
| `injected` | 0.5 |
| `cited_by_agent` | 1.0 |
| `user_confirmed` | 2.0 |
| `task_success` | 1.5 |
| `ignored` | -0.5 |
| `task_failure` | -1.5 |
| `user_rejected` | -3.0 |

P4 必须实现 `retrieved` 和 `injected` 的写入。其他事件在 P4 定义接入口和归因规则，允许先通过 MCP 诊断工具、`memory.review` 或后续 P5 指标采集链路写入。

反馈事件接入边界：

| event_type | P4 要求 | 典型来源 |
|---|---|---|
| `retrieved` | 必须实现 | `memory.search/context` 候选结果 |
| `injected` | 必须实现 | `memory.context` 实际上下文包 |
| `cited_by_agent` | 接口预留，可选实现 | Agent 主动上报或 response summary |
| `user_confirmed` | 接口预留，可由 review 写入 | `memory.review approve` |
| `task_success` | 接口预留，P5 完整接入 | task result 与 trace 聚合 |
| `task_failure` | 接口预留，P5 完整接入 | task result 与 trace 聚合 |
| `user_rejected` | 接口预留，可由 review/correction 写入 | `memory.review reject` 或 `user.correction` |

归因规则：

1. `task_success` 和 `task_failure` 只能归因给同一 `retrieval_trace_id` 下 `used_in_context=true` 或后续明确 `cited_by_agent` 的记忆。
2. 单纯 `retrieved` 不因任务成功获得额外强化。
3. 用户拒绝或纠正已注入记忆时，应写入 `user_rejected`；命中旧 memory 的纠正由 P3 原地覆盖处理，显式 supersedes relation 只处理历史兼容数据。
4. P4 不要求完成任务结果自动归因，但必须保证 `memory_access_log` schema 和 repository 可承载这些事件。

当前 P3 用户纠正采用原地覆盖旧 memory；因此 `user_rejected` 的后续状态处理应解释为：

1. 命中 `target_memory_id/target_event_id` 时，由 P3 correction overwrite 更新旧 memory。
2. P4 access log 记录该 memory 曾被错误注入或拒绝，用于后续 reinforcement/penalty。
3. 只有显式存在 supersedes/superseded_by relation 的历史兼容数据，才走 relation staleness 逻辑。

## 11. MCP 工具与响应变更

### 11.1 memory.search 响应增强

P4 后 `memory.search` 必须返回：

```json
{
  "request_id": "req_search_001",
  "retrieval_trace_id": "rt_001",
  "results": [
    {
      "memory_id": "mem_001",
      "memory_type": "decision",
      "scope": "project_local",
      "title": "auth 模块暂不引入异步消息",
      "content": "认证链路要求请求内完成身份校验，历史决策是不引入异步消息以避免一致性和排障复杂度。",
      "score": 0.86,
      "score_breakdown": {
        "bm25": 0.78,
        "semantic": 0.0,
        "task_fit": 0.8,
        "scope_fit": 1.0,
        "relation_support": 0.2,
        "final": 0.86
      },
      "why_included": ["task_match", "scope_match", "decision_memory"],
      "code_refs": []
    }
  ],
  "diagnostics": {
    "retrieval_mode": "fts_relation",
    "used_fts": true,
    "used_vector": false,
    "used_relation": true,
    "used_code_index": false,
    "fallback_reason": ["vector_disabled"],
    "latency_ms": 42
  }
}
```

### 11.2 memory.context 响应增强

P4 后 `memory.context` 必须返回：

1. `retrieval_trace_id`。
2. `context_pack.summary`。
3. `context_pack.memories[].why_included`。
4. `context_pack.memories[].score_breakdown`。
5. `context_pack.code_refs`。
6. `context_pack.review_strategy`，仅设计复查任务返回。
7. `diagnostics.budget_allocation`。

设计复查示例：

```json
{
  "context_pack": {
    "summary": "本次复查应基于最近 P3 checkpoint 和当前 P4 文档变化章节进行，不重复展开已确认忽略项。",
    "review_strategy": {
      "mode": "changed_sections",
      "checkpoint_id": "mem_cp_001",
      "target_docs": ["doc/The One 长期记忆系统 P4 详细设计.md"],
      "changed_sections": ["8. Doc Index 与 diff-aware 复查设计"],
      "ignored_items_policy": "do_not_repeat_confirmed_ignored_items"
    }
  },
  "retrieval_trace_id": "rt_002"
}
```

### 11.2.1 Context Budget 算法

P4 context builder 使用确定性预算分配：

```text
if request.token_budget <= 0:
    effective_budget = retrieval.default_token_budget
else:
    effective_budget = min(request.token_budget, retrieval.default_token_budget)
reserve 10% for summary and diagnostics
allocate by intent profile
fill required buckets by final_score
trim each memory to compressed summary
drop overflow by lowest final_score and weakest state
```

token 估算：

```text
estimated_tokens = ceil(utf8_rune_count / 2)
```

P4 不要求精确 tokenizer，但必须使用同一估算函数完成预算判断和测试。

默认 bucket 限制：

| bucket | max items | 单项最大 tokens |
|---|---:|---:|
| stable constraints/decisions | 6 | 180 |
| preferences/procedures | 4 | 120 |
| failure memories | 5 | 160 |
| recent/session state | 4 | 120 |
| code_refs | 8 | 80 |
| review_checkpoint | 2 | 260 |
| doc changed sections | 6 | 160 |

裁剪顺序：

1. 先丢弃 `temporary` 中非 task continuation 的候选。
2. 再丢弃 `provisional` 且 score 低于 stable 替代项的候选。
3. 再压缩 evidence/code/doc 摘要。
4. 最后按稳定排序规则从低到高裁剪。

注入格式约束：

1. `pending_review` 必须标记 `unconfirmed=true`。
2. `provisional` 必须标记 `confidence` 和 `why_included`。
3. `temporary` 必须标记 `valid_until` 或 `session_only=true`。
4. archived memory 只在历史查询中以 `historical=true` 摘要出现。

P4-C5 当前实现边界：

1. `internal/retrieval/context_builder.go` 已替换 C1-C4 的顺序字符裁剪，使用确定性 token 估算 `ceil(rune_count/2)`。
2. 当前按 intent profile 分配 `stable_design`、`preferences_procedures`、`failure_memories`、`recent_session_state`、`code_refs`、`review_checkpoint`、`doc_changed_sections` bucket。
3. `memory.context.diagnostics.budget_allocation` 返回 `total/reserved/available/used/remaining/memory_count` 以及每个 bucket 的 `budget/used/items`。
4. `pending_review/provisional` 注入时标记 `unconfirmed=true`，`session` 注入时标记 `session_only=true`，`archived` 注入时标记 `historical=true`。
5. C5 不实现精确 tokenizer、不生成完整 prompt 模板、不改变 FTS/relation/code/doc 召回边界；后续可在同一 builder 内替换 tokenizer 或调整 profile。

### 11.3 新增诊断工具

推荐新增 MCP 诊断工具：

| 工具 | 用途 |
|---|---|
| `memory.retrieval.traces` | 查询检索 trace |
| `memory.retrieval.access_logs` | 查询某次 trace 的 memory access log |
| `memory.code_refs` | 查询 memory 关联的 code_ref |
| `memory.docindex.snapshots` | 查询文档 snapshot |
| `memory.docindex.diff` | 比较当前文档与 checkpoint/snapshot |

诊断工具只返回摘要、hash、分数和引用，不返回完整文档正文或源码。

诊断工具最小入参：

| 工具 | 必填/过滤字段 | 约束 |
|---|---|---|
| `memory.retrieval.traces` | `workspace_id`，可选 `project_id/repo_id/session_id/task_id/limit` | 不跨 workspace 查询 |
| `memory.retrieval.access_logs` | `retrieval_trace_id` 或 `memory_id` | 返回 score breakdown 和 inclusion reason，不返回完整 memory content |
| `memory.code_refs` | `memory_id` 或 `repo_id + file_path` | 只返回路径、symbol、hash、resolve_status |
| `memory.docindex.snapshots` | `workspace_id + doc_path` | `doc_path` 必须通过路径安全校验 |
| `memory.docindex.diff` | `doc_path`，可选 `snapshot_id/checkpoint_id` | 只返回 changed section metadata 和 hash |

所有诊断工具必须执行 scope validator。缺少 scope 时返回 `VALIDATION_FAILED`，不得退化为全库扫描。

P4-C6 当前实现边界：

1. 已注册 `memory.retrieval.traces`、`memory.retrieval.access_logs`、`memory.code_refs`、`memory.docindex.snapshots`、`memory.docindex.diff` 五个 MCP 诊断工具。
2. trace 查询必须携带 `workspace_id`；access log 查询必须携带 `retrieval_trace_id` 或 `memory_id`；code_ref 查询必须携带 `memory_id` 或 `repo_id + file_path`；doc snapshot/diff 必须携带 `workspace_id + doc_path`。
3. 所有列表工具默认最多返回 100 条，超出请求返回 `diagnostics=["limit_truncated"]`。
4. 工具响应只返回短摘要、hash、score breakdown、inclusion reason、resolve status、section metadata，不返回完整 memory content、源码、完整文档正文或完整 prompt。
5. `memory.docindex.diff` 默认比较最新 snapshot 与上一版 snapshot，也支持显式 `base_snapshot_id`；diff 只返回 `added/modified/removed` section metadata 和 hash。
6. C6 不实现 access log 聚合统计、不实现敏感删除脱敏、不执行在线文档重建；这些仍属于后续 P4 剩余验收项。

## 12. 配置设计

新增配置：

```yaml
retrieval:
  default_limit: 10
  default_token_budget: 1800
  online_timeout_ms: 100
  max_relation_expansion: 20
  max_candidates_before_rerank: 80
  enable_trace: true
  enable_access_log: true
  enable_relation_expansion: true
  enable_code_ref_resolution: true
  enable_doc_index: true

embedding:
  provider: none
  model: ""
  query_cache_size: 256
  online_query_embedding_enabled: false

vector_index:
  backend: none
  sqlite_vec_enabled: auto

codeindex:
  provider: local_basic
  enable_ctags: false
  max_file_size_kb: 512
  max_resolve_refs: 30

docindex:
  enabled: true
  max_doc_size_kb: 512
  max_sections: 200
  max_snapshots_per_doc: 10
  store_section_summary: true

access_log:
  retention_days_retrieved: 30
  retention_days_injected: 180
  aggregate_before_cleanup: true
```

与当前配置的兼容策略：

1. 当前 `storage.sqlite_vec_enabled` 保留为 deprecated alias；P4 读取配置时优先使用 `vector_index.sqlite_vec_enabled`，为空时回退到 `storage.sqlite_vec_enabled`。
2. 当前 `embedding.provider/model` 保持兼容；P4 新增 `query_cache_size` 和 `online_query_embedding_enabled` 时不得破坏旧配置文件。
3. `embedding.provider=local` 如果已存在于用户配置中，P4 应映射为 `local_stub` 或返回明确的 `CONFIG_INVALID`，不能静默切到外部 provider。
4. `retrieval.default_limit/online_timeout_ms` 沿用当前 `RetrievalConfig` 字段，新增字段按默认值补齐。
5. `codeindex/docindex/access_log/vector_index` 是新增配置结构；缺省时必须保持无外部依赖可启动。

默认值：

| 配置 | 默认值 | 说明 |
|---|---:|---|
| `retrieval.online_timeout_ms` | `100` | 在线轻量检索目标 |
| `retrieval.max_relation_expansion` | `20` | 单次关系扩展上限 |
| `retrieval.max_candidates_before_rerank` | `80` | rerank 前候选上限 |
| `embedding.provider` | `none` | 默认不启用向量 |
| `embedding.online_query_embedding_enabled` | `false` | 默认不在线生成 query embedding |
| `vector_index.backend` | `none` | 默认不启用向量索引 |
| `codeindex.provider` | `local_basic` | 本地轻量实现 |
| `docindex.max_doc_size_kb` | `512` | 超限降级为路径级 hash |
| `docindex.max_snapshots_per_doc` | `10` | 单文档保留的不同 hash 快照数 |
| `access_log.retention_days_retrieved` | `30` | retrieved 明细保留天数 |
| `access_log.retention_days_injected` | `180` | injected 明细保留天数 |

## 12.1 Retrieval Profiles

P4 默认使用内置 profile 调整 bucket 预算和轻量权重，不开放用户自定义配置，避免早期调参复杂化。

| profile | 适用 intent | 调整 |
|---|---|---|
| `default` | `general_search` | 使用默认 rerank 权重和通用 context budget |
| `code_task` | `code_task` | 提升 `code_ref`、`failure`、`procedure` bucket；启用 Code Index |
| `architecture_review` | `architecture_review` | 提升 `review_checkpoint`、`decision`、`constraint`、doc changed sections |
| `failure_recall` | `failure_recall` | 提升 `failure/procedure`，降低普通 preference |
| `task_continuation` | `task_continuation` | 提升 session/recent state 和 temporary |

profile 只能调整各 bucket 的预算和少量 intent boost，不得改变 scope validator、状态过滤和安全边界。

## 13. Storage 与事务边界

### 13.1 migration

新增 migration：

```text
0006_init_retrieval.sql
```

包含：

1. `retrieval_trace`。
2. `memory_access_log`。
3. `code_ref`。
4. `memory_embedding`。
5. `doc_snapshot`。
6. `doc_section_snapshot`。

如果 P3 已经创建 `memory_relation`，P4 不重复创建，只补必要索引。

### 13.2 P4 async_job 类型

P4 复用 P3 `async_job`，新增任务类型：

| job_type | target_type | 触发来源 | 处理内容 |
|---|---|---|---|
| `resolve_code_ref` | `memory_item` 或 `raw_event` | remember、raw_event、candidate | 从 source_ref/retrieval_cues 生成或刷新 code_ref |
| `refresh_code_ref_status` | `repo` 或 `code_ref` | repo 文件变化、手动诊断 | 重新解析 code_ref，标记 stale/missing/ambiguous |
| `build_doc_snapshot` | `doc_path` | 设计复查、诊断工具 | 计算文档 hash 和章节 hash |
| `compute_embedding` | `memory_item` | memory 写入或更新 | 生成 memory embedding 并写入 vector index |
| `cleanup_access_log` | `workspace` | 定期任务或手动诊断 | 聚合并清理低价值 access log 明细 |

任务约束：

1. `resolve_code_ref` 和 `build_doc_snapshot` 不得保存源码或完整文档正文。
2. `compute_embedding` 仅在 `embedding.provider != none` 时 enqueue。
3. `build_doc_snapshot` 必须先通过路径安全校验，再读取文件。
4. 任务失败写入 `async_job.last_error`，不阻塞在线检索。
5. P4 诊断工具应能通过 `memory.jobs.*` 或 retrieval diagnostics 看到上述 job 的 pending/running/failed 状态。

### 13.2.1 Worker Handler Registry

当前 P3 worker 调用 `automation.Service.RunJob` 的固定 switch。P4 若继续直接扩展该 switch，会让 automation service 同时承载自动记忆、code_ref、docindex、embedding 和 access log 清理，职责会失控。

P4 应在不破坏 P3 job 行为的前提下引入轻量 handler registry：

```go
type JobHandler interface {
	CanHandle(jobType string) bool
	RunJob(ctx context.Context, job automation.AsyncJob) (map[string]any, error)
}

type JobDispatcher struct {
	handlers []JobHandler
}
```

接入规则：

1. P3 `extract_evidence/generate_memory_candidate/compute_admission` 作为默认 handler 注册。
2. P4 `resolve_code_ref/build_doc_snapshot/compute_embedding/cleanup_access_log` 作为独立 handler 注册。
3. worker 只负责 claim、retry、failed/succeeded 状态流转，不理解具体业务 job。
4. 未注册 job_type 返回 `PROVIDER_NOT_FOUND` 或 `VALIDATION_FAILED`，并走现有 retry/failed 策略。
5. P4 开工时应先完成该 registry 改造，再接入 P4-B5 job handlers。

### 13.2.2 access log 清理和聚合

`memory_access_log` 是高增长表，P4 必须定义最小清理策略：

1. `retrieved` 明细默认保留 30 天。
2. `injected` 明细默认保留 180 天。
3. `user_confirmed/user_rejected/task_success/task_failure` 默认长期保留，除非敏感删除。
4. 清理前可按 `memory_id + event_type + day` 聚合到 memory_item 的 `last_accessed_at/reinforcement_count/effective_reinforcement` 或后续统计表。
5. 清理任务通过 `cleanup_access_log` 执行，失败只影响空间回收，不影响在线检索。
6. 敏感删除优先级高于普通保留策略，必须清理或脱敏 query、feedback 和可能泄露路径的字段。

### 13.3 事务边界

| 操作 | 事务边界 |
|---|---|
| `memory.search` | 候选检索使用只读事务；trace/access log 使用独立短写事务 |
| `memory.context` | 候选检索使用只读事务；上下文构造在事务外完成；injected access log 使用独立短写事务 |
| code_ref 生成 | 写 code_ref 独立短事务 |
| doc snapshot | 一个文档 snapshot 和 sections 同一事务 |
| embedding 回填 | 单 memory 或小批量事务 |
| delete consistency | memory 状态 + FTS + relation + code_ref 清理同一事务 |

SQLite 约束：

1. 在线检索读路径不得持有长事务。
2. trace/access log 写入必须短事务。
3. docindex 读取文件和 hash 计算不得在 DB 事务内执行。
4. embedding 计算不得在 DB 事务内执行。
5. trace/access log 写入失败只影响 diagnostics，不回滚检索结果。
6. 同一次请求可以先 best-effort 写入 `retrieval_trace(status=started)`，请求结束后再短事务更新为 `completed/failed`；如果启动 trace 写入失败，响应仍应带 `trace_unavailable` diagnostics。

## 14. Delete Consistency 扩展

当前代码层 P3 尚未实现完整 `delete_consistency` job；P4 应把删除一致性作为 P4-B1/B2 的基础能力补齐，而不是假设已有 P3 job 可扩展。

P4 后删除一致性需要覆盖：

| 表 | 处理 |
|---|---|
| `memory_item_fts` | 删除对应 FTS entry |
| `memory_relation` | 删除 source/target 指向该 memory 的边 |
| `code_ref` | 删除对应 code_ref |
| `memory_embedding` | 删除对应 embedding |
| `memory_access_log` | 默认保留统计；敏感删除时清理或脱敏 |
| `retrieval_trace` | 默认保留 trace；敏感删除时清理 query/task 摘要 |

普通删除：

1. `memory_item.state=deleted`。
2. 写 `memory_tombstone`。
3. 清理 FTS、relation、code_ref、embedding。
4. access log 保留最小统计，不再返回记忆内容。

敏感删除：

1. 执行普通删除。
2. 清理或脱敏 access log 中的 query、feedback、score_breakdown。
3. 清理 doc/code 引用中可能泄露路径的字段。

## 15. 测试设计

### 15.1 单元测试

| 模块 | 测试重点 |
|---|---|
| `retrieval` | intent detection、score formula、filter、budget allocation |
| `codeindex` | file/symbol resolve、missing/ambiguous/stale |
| `docindex` | Markdown section split、hash stability、changed section detection |
| `embedding` | provider none、fallback、query cache |

### 15.2 Repository 测试

1. `retrieval_trace` insert/list。
2. `memory_access_log` insert/list。
3. `code_ref` insert/update/list/delete。
4. `doc_snapshot` 和 `doc_section_snapshot` 原子写入。
5. `memory_embedding` upsert/delete。
6. delete consistency 覆盖新增表。

### 15.3 集成测试

1. `memory.search` 写入真实 trace。
2. `memory.search` 返回 score breakdown。
3. `memory.context` 写入 injected access log。
4. 手工 seeded supersedes 关系影响排序。
5. contradicts 关系增加 penalty。
6. 禁用 vector index 后仍可 FTS + relation。
7. code_ref resolve 失败不阻断 context。
8. 文档 hash 未变化时返回 checkpoint-first strategy。
9. 文档章节变化时返回 changed-sections strategy。
10. doc path 越界、symlink 越界和超大文档触发安全降级或拒绝。
11. `resolve_code_ref`、`build_doc_snapshot`、`compute_embedding` job 失败可诊断。
12. P1/P2/P3 回归测试仍通过。

### 15.4 验收脚本

推荐新增：

```text
scripts/acceptance/p4_retrieval.sh
make test-p4-retrieval
```

验收脚本应覆盖：

1. 准备多条 decision/failure/constraint/preference。
2. 准备 supports/contradicts relation，并可选准备手工 seeded supersedes relation。
3. 执行 `memory.search` 验证 trace 和 breakdown。
4. 执行 `memory.context` 验证 budget 和 access log。
5. 准备带 code_ref 的 memory，验证 code_ref 返回。
6. 修改 Markdown 文档章节，验证 docindex diff strategy。
7. 禁用 vector index，验证 fallback。
8. 构造越界文档路径，验证 docindex 拒绝读取。

### 15.5 Retrieval Golden Set

P4 应新增最小检索 golden set，用于验证召回质量而不只验证 trace 存在。

每条用例包含：

```json
{
  "case_id": "decision_kafka_001",
  "query": "为什么这个项目没有用 Kafka？",
  "workspace_id": "ws",
  "project_id": "proj",
  "expected_memory_ids": ["mem_decision_kafka"],
  "forbidden_memory_ids": ["mem_other_project_kafka"],
  "expected_reasons": ["decision_memory", "scope_match"],
  "max_latency_ms": 100
}
```

P4 golden set 至少覆盖：

1. 历史决策召回。
2. 失败经验召回。
3. scope 隔离。
4. 用户纠正覆盖后旧内容不注入。
5. 手工 seeded supersedes 后旧记忆不注入。
6. review checkpoint 召回。
7. vector disabled fallback。

## 16. 验收标准

| 验收项 | 标准 |
|---|---|
| 检索 trace | 每次 `memory.search/context` 生成 `retrieval_trace` |
| access log | 被召回和注入的 memory 写入 `memory_access_log` |
| score breakdown | 每条结果包含可解释分数拆解 |
| relation expansion | `supports/contradicts` 影响排序或 penalty；显式存在的 `supersedes/superseded_by` 影响旧记忆过滤 |
| code_ref | 记忆可关联 repo/file/symbol/hash，并在 context 中返回 |
| Code Index 边界 | 调用关系、文件结构事实不写入普通 `memory_item` |
| checkpoint-aware context | 设计复查任务优先召回相关 `review_checkpoint` |
| 文档 hash | 文档未变化时不强制全文重读，变化时定位变化章节 |
| docindex 安全 | 越界路径、symlink 越界和超大文档不会被全文读取 |
| vector 降级 | embedding 或 vector index 不可用时 FTS + metadata + relation 仍可用 |
| 延迟 | FTS + metadata + relation 轻量路径 P95 <= 100ms |
| 回归 | P1/P2/P3 验收测试仍通过 |
| golden set | P4 检索 golden set 通过 |

P4 退出条件：

```text
系统检索和上下文构造具备可解释排序、访问反馈、关系扩展、代码引用和文档 hash/diff-aware 复查能力；embedding 和 vector index 作为可选增强，不影响无向量环境下的一期 MVP 验收。
```

## 17. 协作任务拆分

| 任务 ID | 任务 | 输入 | 输出 | 可并行 |
|---|---|---|---|---|
| P4-B1 | retrieval schema migration | P3 migration | `0006_init_retrieval.sql` | 否 |
| P4-D1 | retrieval DTO 和 Orchestrator 接口 | 本设计 | `internal/retrieval` types | 是 |
| P4-D2 | intent detection 和 score formula | D1 | intent/rerank tests | 依赖 D1 |
| P4-B2 | trace/access log repository | B1 | retrieval repository | 依赖 B1 |
| P4-B3 | code_ref repository | B1 | code_ref CRUD | 依赖 B1 |
| P4-B4 | docindex repository | B1 | doc snapshot CRUD | 依赖 B1 |
| P4-B5 | P4 async job handlers | P3 async_job | resolve_code_ref/build_doc_snapshot/compute_embedding | 依赖 B2-B4 |
| P4-C1 | Orchestrator FTS + metadata 接入 | D1/B2 | search/context trace | 依赖 D1/B2 |
| P4-C2 | relation expansion | C1/P3 relation | relation-aware rerank | 依赖 C1 |
| P4-C3 | Code Index Adapter + Orchestrator 接入 | B3/C1/C2 | `internal/codeindex`、在线 resolve、`code_aware` trace | 依赖 C2，可与 C4 并行 |
| P4-C4 | Doc Index | B4 | hash/diff-aware strategy | 可并行 |
| P4-C5 | context budget builder | C1/D2 | context allocation | 依赖 C1 |
| P4-C6 | MCP diagnostics | B2/B3/B4 | traces/access/code/doc tools | 依赖 repositories |
| P4-E1 | unit tests | D2/C3/C4 | unit coverage | 并行 |
| P4-E2 | integration tests | C1-C6 | retrieval integration | 依赖核心实现 |
| P4-E3 | acceptance script | 全部 | `make test-p4-retrieval` | 依赖全部 |

P4-E1 当前实现边界：

1. `internal/retrieval` 单元测试覆盖 intent hint、intent 优先级、score formula、stable ordering、scope fallback、penalty 和 context budget allocation。
2. `internal/codeindex` 单元测试覆盖 `local_basic` 的 resolved/stale/ambiguous/missing/unsafe path/large file/max_resolve_refs，不伪造调用图或源码摘要。
3. `internal/docindex` 单元测试覆盖 Markdown section split、hash normalization、large file hash-only、unsafe path、symlink escape 和 max_sections。
4. E1 只做单元覆盖收口；跨模块真实 MCP/SQLite/Orchestrator 串联归 P4-E2，acceptance script 归 P4-E3。

P4-E2 当前实现边界：

1. `internal/app/p4_retrieval_integration_test.go` 使用真实 App、MCP registry、SQLite、FTS5、P4 Orchestrator 和 repositories 串联验证。
2. golden set 覆盖历史决策召回、失败经验召回、project scope 隔离和显式 `supersedes` 后旧记忆默认过滤。
3. context 集成覆盖 CodeRef 在线 resolve、DocIndex changed-sections strategy、checkpoint-aware diagnostics、budget allocation 和 injected access log。
4. `internal/app/p4_diagnostics_tools_test.go` 覆盖 C6 诊断工具在 app 层的注册、查询、limit 截断和 doc diff。
5. E2 不启动长期 daemon、不跑 shell acceptance script；`make test-p4-retrieval` 仍归 P4-E3。

P4-E3 当前实现边界：

1. 已新增 `scripts/acceptance/p4_retrieval.sh` 和 `make test-p4-retrieval`。
2. 脚本按 P4 验收分层执行 retrieval/codeindex/docindex 单元目标、SQLite repository、app integration/diagnostics、`sqlite_fts5` 全量回归和 memoryd build。
3. 脚本默认使用 `GO_TAGS=sqlite_fts5`，确保 FTS5 路径和 app 层 P4 integration tests 被执行。
4. E3 不引入外部 embedding/vector 依赖；vector retrieval 仍是 P4+ 可选增强，不影响无向量环境下的 P4 验收。
5. E3 不长期启动 daemon，不依赖网络和外部服务；验收仍以本地 SQLite + Go test + build 为准。

### 17.1 P4-C3：Code Index Adapter 阶段设计

#### 17.1.1 阶段目标

P4-C3 在 P4-B3 `code_ref` repository 和 P4-C1/C2 Orchestrator 在线检索骨架之上，补齐 **Code Index 与 Memory 的分层边界**：

```text
memory.search / memory.context (code_task 或显式 include_code_refs)
  -> 读取 seed memory 已持久化的 code_ref
  -> Code Index Adapter(local_basic) best-effort resolve
  -> 更新 code_ref.resolve_status / content_hash / ref_summary
  -> 合并到 SearchResult/ContextPack.code_refs
  -> staleness_penalty 纳入 rerank
  -> retrieval_trace.used_code_index = true
```

C3 不改变 P3 Admission、不新增自动 memory 写入，不把代码结构事实复制进 `memory_item`。

#### 17.1.2 前置条件与依赖

| 依赖项 | 状态要求 | C3 使用方式 |
|---|---|---|
| P4-B3 `code_ref` repository | 已完成 | CRUD、`UpdateCodeRefResolveStatus`、`ListCodeRefs`；C3 扩展 `ListCodeRefsByMemoryIDs` 批量查询 |
| P4-B5 `resolve_code_ref` job | 已完成（当前仅落库 payload） | C3 升级为调用 Adapter 后写回状态 |
| P4-C1 Orchestrator | 已完成 | 注入 `CodeRefRepository` + `CodeIndexResolver` |
| P4-C2 relation expansion | 已完成 | C3 不改变 relation 语义，只在 rerank 前附加 code_ref 元数据 |
| P1 `repo` 表 / workspace 根路径 | 已有 | 解析 repo 根目录、拒绝越界路径 |
| P2/P3 `source_refs` / evidence | 已有 | 异步 job 从 event/candidate 提取 hint，不在 C3 重做 Admission |

若 `codeindex.provider=none` 或 `retrieval.enable_code_ref_resolution=false`，系统必须可启动；在线路径降级为不 resolve、不返回 code_refs，diagnostics 标记 `code_index_disabled`。

#### 17.1.3 代码目录与职责

```text
internal/codeindex/
  adapter.go          # Adapter 接口、Capabilities、错误码
  local_basic.go      # 默认实现：git ls-files + 路径/符号定位 + 可选 ctags
  resolver.go         # ResolveCodeRefs 编排：超时、批量上限、状态机
  types.go            # SymbolRef、ResolvedCodeRef、ResolveRequest
  path_guard.go       # repo 相对路径校验、拒绝 ../ 与 workspace 外读取
  local_basic_test.go
  resolver_test.go
```

| 组件 | 职责 |
|---|---|
| `Adapter` | 对仓库只读：定位文件、符号、行号、content hash、结构摘要 |
| `Resolver` | 把 `memory.CodeRef` 转为 `ResolvedCodeRef`，决定 `resolve_status` |
| `path_guard` | 与 docindex 类似的路径安全；code_ref 只允许 repo 内相对路径 |
| `MemoryOrchestrator` | 在线 attach code_refs、写 trace.used_code_index、触发 rerank penalty |

`internal/retrieval` 只依赖窄接口，不 import 具体 `local_basic` 实现：

```go
// internal/retrieval/code_ref.go

type CodeRefRepository interface {
	ListCodeRefsByMemoryIDs(ctx context.Context, memoryIDs []string, limit int) ([]memory.CodeRef, error)
	UpdateCodeRefResolveStatus(ctx context.Context, id, status, contentHash, refSummary string) (memory.CodeRef, error)
}

type CodeIndexResolver interface {
	ResolveCodeRefs(ctx context.Context, repoRoot string, refs []memory.CodeRef) ([]memory.CodeRef, error)
	Capabilities(ctx context.Context) (codeindex.Capabilities, error)
}
```

#### 17.1.4 Adapter 接口与 local_basic 行为

C3 实现 §7.2 的 `codeindex.Adapter`，默认 provider 为 `local_basic`。

**Capabilities 响应（必返回）**

```json
{
  "provider": "local_basic",
  "symbol_search": true,
  "file_structure": true,
  "call_graph": false,
  "impact_analysis": false,
  "ctags_available": false
}
```

**local_basic 必支持**

| 能力 | 行为 |
|---|---|
| `ResolveCodeRefs` | 对每条 ref：校验路径 → 读文件（有大小上限）→ 计算 `content_hash` → 定位 symbol/行号 → 生成 `ref_summary` |
| `GetSymbol` | 在已解析 symbol 上返回 file/line/hash；未解析则 `unsupported` |
| `GetFileStructure` | 返回顶层声明名列表（package/func/type 级 regex 或 ctags） |
| `SearchSymbols` | 按 symbol 名或路径片段在 `git ls-files` 结果中模糊匹配，上限 `codeindex.max_symbol_search`（默认 20） |

**local_basic 可降级 / unsupported（不得伪造）**

| 能力 | C3 行为 |
|---|---|
| `GetCallers` / `GetCallees` | 返回 `unsupported`；Capabilities 中 `call_graph=false` |
| `GetImpact` | 返回 `unsupported` |
| `BuildTaskContext` | 仅聚合已 resolve 的 code_ref + 文件结构摘要，不构建调用图 |

**content_hash 规则（与 P4 docindex 对齐）**

```text
hash_algo = sha256
normalization = lf_trim_trailing_space
输入 = 文件全文（超 max_file_size_kb 时只对前 N KB 做 hash，并标记 diagnostics=file_truncated_for_hash）
```

**resolve_status 状态机**

```text
unresolved
  -> resolved          文件存在且 symbol/行号匹配，hash 一致或首次写入
  -> stale             文件存在但 content_hash 与 ref 中记录不一致
  -> missing           文件不存在或 symbol 在文件中找不到
  -> ambiguous         同 symbol 多匹配且无法安全消歧
```

状态转换由 `Resolver` 统一判定；repository 只持久化结果，不在 SQL 层做推断。

**ref_summary 约束**

1. 最多 `512` rune（与 `code_ref_repository` 一致）。
2. 只允许：`package` 名、symbol 签名一行、行号范围、hash 前缀。
3. 禁止写入函数体、import 列表全文或超过 3 行的代码片段。

#### 17.1.5 路径与读取安全

code_ref 路径安全规则与 §8.2 docindex 同级，但根目录为 **repo root** 而非 workspace doc root：

1. `file_path` 必须 canonicalize 为 repo 内相对路径。
2. 拒绝 `..`、绝对路径、指向 repo 外部的 symlink。
3. 单文件读取上限 `codeindex.max_file_size_kb`，默认 `512`。
4. 禁止读取 `.git/`、`node_modules/` 等配置排除目录（通过 `codeindex.exclude_globs` 扩展）。
5. 在线 resolve 总耗时受 `retrieval.online_timeout_ms` 约束；超时后保留原 `resolve_status`，diagnostics 增加 `code_resolve_timeout`。

#### 17.1.6 Orchestrator 接入

**触发条件**

| 条件 | 行为 |
|---|---|
| `DetectIntent == code_task` | 对 top-N 候选 memory 加载并 resolve code_ref；`retrieval_mode` 至少为 `code_aware`（可与 `fts_relation` 组合为 `fts_relation` + `used_code_index=true`） |
| `memory.SearchRequest.include_code_refs == true` | 同上，不强制 intent |
| 其他 intent | 不在线 resolve；已有 code_ref 也不默认返回（减少噪声） |

**在线流程（插入点）**

```text
FTS seed
  -> relation expansion (C2)
  -> merge candidates
  -> [C3] load code_refs for candidate memory_ids (limit = codeindex.max_resolve_refs)
  -> [C3] ResolveCodeRefs(best-effort, sub-timeout)
  -> [C3] persist status updates (short write tx per ref or small batch)
  -> rerank (staleness_penalty += f(resolve_status))
  -> attach code_refs to SearchResult / ContextPack
  -> trace.used_code_index = true
```

**rerank 与 code_ref 的关系**

| resolve_status | staleness_penalty 增量 | why_included |
|---|---|---|
| `resolved` | `0` | 可附加 `code_ref_resolved` |
| `stale` | `+0.2`（与 §5.1.1 一致） | `code_ref_stale` |
| `missing` | `+0.2`，且默认不注入 context | `code_ref_missing` |
| `ambiguous` | `+0.1` | `code_ref_ambiguous` |
| `unresolved` | `0`（仅 diagnostics） | 不加分也不强惩罚 |

`task_fit` 在 `code_task` intent 下对带 `resolved` code_ref 的 `failure/procedure/decision/project_fact` 额外 `+0.1`（clamp 后计入）。

**失败降级**

| 失败 | 处理 |
|---|---|
| `CodeRefRepository` 不可用 | 跳过 code_ref，`fallback_reason=code_ref_unavailable` |
| Adapter 初始化失败 | 同上，`code_index_unavailable` |
| 单条 resolve 失败 | 保留原状态，diagnostics 记录 `code_resolve_partial_failed` |
| 批量超时 | 已完成的写回，未完成保持原状，`code_resolve_timeout` |

resolve 失败 **不得** 阻断 FTS/relation 主路径。

#### 17.1.7 写入与异步任务

**memory.remember 显式 source_ref**

```text
remember 校验 source_ref
  -> WriteCodeRef(resolve_status=unresolved)
  -> 若 code_task 或请求带 resolve_code_refs=true：同步 best-effort resolve（受 online_timeout 约束）
  -> 否则 enqueue resolve_code_ref
```

**P4-B5 job 升级**

`resolve_code_ref` payload 扩展（向后兼容）：

```json
{
  "memory_id": "mem_001",
  "repo_id": "repo_001",
  "repo_root": "/abs/path/to/repo",
  "code_refs": [],
  "resolve_mode": "adapter"
}
```

| `resolve_mode` | 行为 |
|---|---|
| 缺失或 `persist_only` | 保持当前 B5 行为：只 `WriteCodeRef`，不调用 Adapter（兼容测试与手工灌数） |
| `adapter` | 调用 `Resolver` → `UpdateCodeRefResolveStatus` |

新增 `refresh_code_ref_status` job（C3 必交付）：

```text
target_type = repo | code_ref
  -> 列出 stale/unresolved/resolved 的 code_ref（带上限）
  -> 批量 re-resolve
  -> 不修改 memory_item 正文
```

触发来源：手动诊断、repo 文件变更后的 best-effort enqueue（C3 不要求文件 watcher，只定义 job 契约）。

**code_ref 来源优先级（异步提取）**

与 §7.4 一致：`evidence.source_ref_json` > `raw_event.source_refs_json` > `memory_item.retrieval_cues/entities/tags` > `memory_candidate.*`。C3 的 `resolve_code_ref` handler 负责按优先级构造初始 `CodeRef` 再 resolve，不在线扫描 candidate 全表。

#### 17.1.8 配置与 memory.status

C3 需要在 `internal/config` 增加 `CodeIndexConfig`（与 §12 YAML 对齐）：

```yaml
codeindex:
  provider: local_basic   # local_basic | none
  enable_ctags: false
  max_file_size_kb: 512
  max_resolve_refs: 30
  max_symbol_search: 20
  exclude_globs:
    - "**/.git/**"
    - "**/node_modules/**"
```

`retrieval` 增加：

```yaml
retrieval:
  enable_code_ref_resolution: true
```

`memory.status` 扩展字段：

```json
{
  "code_index": {
    "provider": "local_basic",
    "capabilities": { "call_graph": false, "ctags_available": false },
    "enabled": true
  }
}
```

#### 17.1.9 测试与验收（C3 子集）

| 类型 | 用例 |
|---|---|
| 单元 | 路径穿越拒绝、超大文件截断 hash、symbol 唯一→resolved、symbol 重复→ambiguous、文件删除→missing、hash 变化→stale |
| repository | `ListCodeRefsByMemoryIDs` 批量查询、`UpdateCodeRefResolveStatus` 与 resolved_at |
| orchestrator | `code_task` intent 返回 code_refs；resolve 超时降级；`used_code_index=true` |
| job | `resolve_mode=adapter` 写回状态；`persist_only` 回归仍通过 |
| 集成 | `memory.context` 含 resolved code_ref；missing 不注入 context；trace 记录 fallback |

C3 阶段 **不要求** 完整 `scripts/acceptance/p4_retrieval.sh`（归 P4-E3），但应新增：

```text
go test ./internal/codeindex/...
go test ./internal/retrieval/... -run CodeRef
```

#### 17.1.10 P4-C3 Done 定义

1. `internal/codeindex` 存在且默认 `local_basic` 可实例化。
2. `Capabilities` 诚实反映 call_graph/impact 不可用，不返回伪造调用关系。
3. `memory.search` 在 `code_task` 或 `include_code_refs=true` 时返回非空 `code_refs`（当 memory 上存在 code_ref 时）。
4. `memory.context` 返回 `context_pack.code_refs`，且 `resolved` 项进入 rerank 后仍可注入（受 C1 预算约束）。
5. `retrieval_trace.used_code_index` 在发生 resolve 时为 `true`。
6. `stale/missing/ambiguous` 影响 `staleness_penalty` 或注入过滤，且 `why_included` 可解释。
7. `resolve_code_ref` job 支持 `resolve_mode=adapter` 并写回状态。
8. `refresh_code_ref_status` job 可批量刷新状态。
9. Code Index 不把调用图/import 图写入 `memory_item`。
10. resolve 失败或超时不会阻断 FTS + relation 主路径。
11. P4-C1/C2 与 P1/P2/P3 现有测试仍通过。

#### 17.1.11 与后续阶段边界

| 能力 | C3 | C4/C5/C6 |
|---|---|---|
| code_ref 在线 attach + resolve | 是 | 诊断工具 `memory.code_refs` |
| doc hash / review_strategy | 否 | C4 |
| 多 bucket context budget | 否 | C5 |
| MCP traces/access/doc 工具 | 否 | C6 |
| vector / embedding 检索 | 否 | 可选增强，非 C3 |

## 18. 合并顺序建议

```text
P4-B1
  -> P4-D1 + P4-D2
  -> P4-B2 + P4-B3 + P4-B4
  -> P4-B5
  -> P4-C1
  -> P4-C2
  -> P4-C3 + P4-C4
  -> P4-C5
  -> P4-C6
  -> P4-E1 + P4-E2
  -> P4-E3
  -> P4 release
```

## 19. P4 Done 定义

1. `memory.search` 和 `memory.context` 返回真实 `retrieval_trace_id`。
2. `retrieval_trace` 记录检索模式、fallback、候选数、注入数和延迟。
3. `memory_access_log` 记录 retrieved/injected。
4. 每条检索结果包含 `score_breakdown` 和 `why_included`。
5. embedding 或 vector index 不可用时检索自动降级。
6. 显式存在 `supersedes/superseded_by` 关系时默认过滤旧记忆；用户纠正覆盖后的旧内容不再可被默认召回。
7. `supports` 关系提升相关记忆。
8. `contradicts` 关系产生冲突 penalty 或 diagnostics。
9. P4 relation expansion 必交付只依赖 `supersedes/superseded_by/supports/contradicts`。
10. `code_ref` 可写入、查询和解析。
11. `memory.context` 可返回 code_refs。
12. Code Index 结构事实不写入普通 Memory。
13. Code Index Adapter 保留 callers/callees/impact/task context 接口，并能明确返回 degraded/unsupported。
14. Markdown 文档可生成 doc snapshot 和 section hash。
15. docindex 拒绝 workspace/repo 外路径和 symlink 越界。
16. 设计复查任务可根据 checkpoint 和文档 hash 选择复查策略。
17. 文档 hash 未变化时，context 不重复展开完整历史。
18. 文档 hash 变化时，context 优先变化章节和受影响 checkpoint。
19. P4 诊断工具可查询 traces、access logs、code refs、doc snapshots。
20. delete consistency 覆盖 code_ref 和 memory_embedding。
21. FTS + metadata + relation 轻量路径 P95 <= 100ms。
22. P1/P2/P3 回归测试通过。
23. P4 retrieval golden set 通过。
24. `make test-p4-retrieval` 通过。

## 19.1 P4+ 可选增强

以下增强对 P4 有价值，但不进入 P4 Done：

1. `retrieval_trace_event`：记录更细粒度阶段耗时和失败点。
2. `bad_injection/missing_recall` 反馈入口：支持用户标记错误注入和漏召回。
3. 文档 rename/move 检测：基于相同 `content_hash` 发现同内容不同路径。
4. LSP/SCIP Adapter：提升 Code Index 调用关系和影响面质量。
5. access log 聚合统计表：减少长期明细存储压力。

## 20. 主要风险和控制点

| 风险 | 影响 | 控制点 |
|---|---|---|
| P3 未完全稳定 | P4 relation/retention 依赖变动 | P4 前置验收门槛，先实现 trace/access/doc/code 基础 |
| score 公式过复杂 | 检索行为难调试 | 固定 breakdown，测试每个分量，先规则化 |
| relation expansion 引入噪声 | 错误记忆被放大 | 默认 depth=1、限制数量、contradicts penalty |
| Code Index 与 Memory 混淆 | 代码结构事实过期污染记忆 | 只保存 code_ref，不保存调用关系进 memory_item |
| doc checkpoint 误代替事实源 | 复查遗漏新变更 | 每次复查读取当前文档或变化章节 |
| docindex 路径越界 | 读取 workspace/repo 外部敏感文件 | canonicalize、拒绝路径穿越和 symlink 越界 |
| sqlite-vec 依赖不稳定 | 本地启动和跨平台验收失败 | embedding/vector index 可选，FTS + relation 为必交付路径 |
| access log 膨胀 | SQLite 体积增长 | 后续 retention/cleanup 策略，P4 先保留最小字段 |
| trace 泄露敏感 query | 本地日志和 DB 存储风险 | query/task 归一化短文本，敏感删除支持脱敏 |
| 在线路径超时 | Agent 主流程受阻 | 100ms budget、vector/code/doc 失败降级 |
