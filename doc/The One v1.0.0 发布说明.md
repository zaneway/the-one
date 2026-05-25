# The One v1.0.0 发布说明

> 发布定位：一期本地个人工具 MVP。
>
> 发布日期：2026-05-25

## 1. 总体结论

v1.0.0 完成一期本地 AI Coding Agent 长期记忆系统的主干闭环：

```text
P0 工程基座
  -> P1 手动记忆
  -> P2 Agent 事件捕获
  -> P3 自动记忆
  -> P4 检索增强
  -> P5 MVP 验收
```

当前版本可以作为本地 `memoryd` MVP 使用。默认运行方式仍是本地单二进制 + SQLite + FTS5，embedding/vector 能力保持可选，不是 v1.0.0 必需依赖。

## 2. 已完成能力

### 2.1 P0-P1 基础能力

1. `memoryd` 本地启动、health/status、SQLite migration。
2. SQLite WAL、foreign key、busy timeout。
3. `memory.remember`、`memory.search`、`memory.context`、`memory.review`。
4. scope 隔离：
   - `user_global`
   - `project_local`
   - `repo_local`
   - `session`
5. 手动 `review_checkpoint` 写入和召回。
6. archive/delete 后默认不再检索。

### 2.2 P2 事件捕获

1. `memory.observe`。
2. `agent_session`、`agent_task`、`raw_event` append-only 事件层。
3. session/task/tool/file edit/conversation/user correction 等事件类型。
4. content minimization：拒绝完整对话、完整 output、完整 diff、完整源码。
5. capture quality 统计和诊断工具。
6. Codex、Claude Code、Cursor 接入样例。

### 2.3 P3 自动记忆

1. `async_job` worker。
2. rule-based processor。
3. raw_event -> evidence -> memory candidate。
4. Admission Control。
5. pending review、stable、temporary/provisional 等状态流转。
6. retention cleanup/recompute。
7. orphan raw_event reconcile。

### 2.4 P4 检索增强

1. `retrieval_trace`。
2. `memory_access_log`。
3. P4 Retrieval Orchestrator。
4. score breakdown 和 `why_included`。
5. relation expansion。
6. `code_ref` repository 和 `local_basic` Code Index Adapter。
7. Markdown Doc Index、section hash、review checkpoint diff-aware 策略。
8. context budget builder。
9. retrieval/code/doc 诊断工具。

### 2.5 P5 MVP 验收

1. MVP 验收数据模型：
   - `mvp_acceptance_run`
   - `mvp_acceptance_task`
   - `mvp_metric_sample`
   - `mvp_agent_capability`
2. MVP 工具：
   - `memory.mvp.run.start`
   - `memory.mvp.task.record`
   - `memory.mvp.capability.record`
   - `memory.mvp.metrics.compute`
   - `memory.mvp.report`
3. 10 个 synthetic MVP scenario fixture。
4. `make test-p5-mvp`。
5. Engine MVP 与 Agent certification 分层报告，支持 `include_failures=true` 输出失败 task 明细。
6. 缺失 Codex、Claude Code、Cursor 任一 Agent capability 时，Agent certification 自动失败。
7. 共享 memoryd real_agent/mixed 手工验收清单。

## 3. 验收结果

v1.0.0 发布门禁：

| 验收项 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| `go test -tags sqlite_fts5 ./...` | 通过 |
| `git diff --check` | 通过 |
| `make test-p2-capture` | 通过 |
| `make test-p3-sqlite` | 通过 |
| `make test-p4-retrieval` | 通过 |
| `make test-p5-mvp` | 通过 |

P5 synthetic Engine MVP：

| 指标 | 目标 | 结果 |
|---|---:|---|
| Token savings | `>= 30%` | synthetic fixture 通过 |
| 重复上下文说明次数 | 降低 `>= 50%` 或计数为 0 | synthetic fixture 通过 |
| 历史决策召回准确率 | `>= 80%` | synthetic fixture 通过 |
| 错误记忆注入率 | `<= 5%` | synthetic fixture 通过 |
| 检索 P95 | `<= 100ms` | synthetic fixture 通过 |
| 跨 Agent 召回成功率 | `>= 80%` | synthetic fixture 通过 |
| 设计复查历史上下文 Token savings | `>= 60%` | synthetic fixture 通过 |

说明：

1. `make test-p5-mvp` 默认只运行 synthetic 模式，不启动真实 Codex、Claude Code 或 Cursor。
2. real_agent/mixed certification 需要按 `examples/agents/shared-memoryd/README.md` 在本地真实 Agent 环境中手工执行。
3. P2/P3 acceptance 在 sandbox 下可能出现 Go module stat cache 的非致命 `operation not permitted` 提示；脚本最终退出码为 0 且输出 `acceptance passed`。

## 4. 使用方式

构建：

```bash
make build
```

启动：

```bash
bin/memoryd serve --data-dir /tmp/the-one-memoryd
```

健康检查：

```bash
make run-health DATA_DIR=/tmp/the-one-memoryd
make run-status DATA_DIR=/tmp/the-one-memoryd
```

验收：

```bash
make test-p2-capture
make test-p3-sqlite
make test-p4-retrieval
make test-p5-mvp
```

真实 Agent certification：

```text
examples/agents/shared-memoryd/README.md
```

## 5. 已知限制

以下限制不阻塞 v1.0.0 本地 MVP，但必须在使用和后续规划中明确：

1. `sqlite-vec` / vector retrieval 未作为默认必交付能力启用；无向量环境下通过 FTS + metadata + relation 工作。
2. Code Index 默认是 `local_basic`，不提供完整跨语言调用图和影响面分析。
3. 不实现在线 LLM rerank。
4. 不做团队权限、企业审计、备份恢复。
5. 不做完整学习画像。
6. real_agent certification 需要用户在本地真实 Agent 环境中手工运行。
7. token 统计是本地近似口径，不是模型官方 tokenizer 精确值。
8. P5 report 当前由 MCP 返回内容，文件落盘由脚本、CLI 或后续发布工具处理。

## 6. 数据和安全边界

v1.0.0 仍遵守一期边界：

1. 不保存完整源码。
2. 不保存完整工具 output。
3. 不保存完整 diff。
4. 不保存完整历史对话。
5. 自动写入必须经过 evidence、candidate、Admission 和 Review/Retention 边界。
6. `memory.observe` 仍以 `raw_event` append-only 作为事实层。
7. Code Index 与 Memory 分层，Memory 不复制代码结构事实。

## 7. 后续方向

推荐后续进入：

1. 小团队版本设计。
2. 更完整 Code Index Adapter，例如 LSP/SCIP。
3. 可选 embedding/vector retrieval 增强。
4. UI 化 review queue 和 MVP dashboard。
5. 企业权限、审计、备份恢复。
