# The One v1.0.0 发布检查清单

> 用途：v1.0.0 发布前最终人工复核。
>
> 状态：Engine MVP 已通过 synthetic 验收；真实 Agent certification 需按本地环境手工执行。

## 1. 发布门禁

| 检查项 | 命令或依据 | 状态 |
|---|---|---|
| Go 全量测试 | `go test ./...` | 通过 |
| SQLite FTS5 全量测试 | `go test -tags sqlite_fts5 ./...` | 通过 |
| Diff 空白检查 | `git diff --check` | 通过 |
| P2 capture 验收 | `make test-p2-capture` | 通过 |
| P3 sqlite 验收 | `make test-p3-sqlite` | 通过 |
| P4 retrieval 验收 | `make test-p4-retrieval` | 通过 |
| P5 synthetic MVP 验收 | `make test-p5-mvp` | 通过 |

## 2. P5 Done 复核

| Done 项 | 状态 |
|---|---|
| P5 migration、repository、service、MCP tools 和报告生成能力完成 | 通过 |
| `memory.mvp.run.start` 可创建验收 run | 通过 |
| `memory.mvp.task.record` 可记录 scenario 结果 | 通过 |
| `memory.mvp.capability.record` 可记录三 Agent capability | 通过 |
| `memory.mvp.metrics.compute` 可计算 MVP 指标 | 通过 |
| `memory.mvp.report` 可生成 JSON、Markdown 和失败明细报告 | 通过 |
| 10 个 MVP scenario 有标准输入、期望、指标和阈值 | 通过 |
| synthetic Engine MVP 验收通过 | 通过 |
| 三 Agent capability coverage 和 event completeness 可统计 | 通过 |
| 缺失任一 Agent capability 时 Agent certification 失败 | 通过 |
| P5 报告区分 Engine MVP 和 Agent certification | 通过 |
| `make test-p5-mvp` 通过 | 通过 |

## 3. 真实 Agent certification

P5-D 已交付真实 Agent certification 清单：

```text
examples/agents/shared-memoryd/README.md
```

发布前需要人工记录：

| Agent | 是否连接同一 memoryd | capability coverage | completeness | 降级原因 |
|---|---|---:|---:|---|
| Codex | 待本地执行 | 待填 | 待填 | 待填 |
| Claude Code | 待本地执行 | 待填 | 待填 | 待填 |
| Cursor | 待本地执行 | 待填 | 待填 | 待填 |

如果任一 Agent 未达到 Level4，允许作为 v1.0.0 已知限制发布，但 release notes 必须说明。

## 4. 安全和数据边界

| 边界 | 状态 |
|---|---|
| 不保存完整源码 | 通过 |
| 不保存完整工具 output | 通过 |
| 不保存完整 diff | 通过 |
| 不保存完整历史对话 | 通过 |
| checkpoint 不替代当前文档事实源 | 通过 |
| Code Index 不把调用图写入普通 memory_item | 通过 |
| `memory.observe` 不阻塞 Agent 主流程 | 通过 |

## 5. 可带限制发布项

| 限制 | 说明 |
|---|---|
| sqlite-vec / vector retrieval | 非默认必交付，当前通过 FTS + metadata + relation 工作 |
| Code Index | 默认 `local_basic`，调用图/影响面能力 degraded |
| 在线 LLM rerank | v1.0.0 不实现 |
| real_agent certification | 需要用户本地手工执行 |
| token 统计 | 近似估算，不是官方 tokenizer 精确口径 |
| 企业能力 | 不包含团队权限、审计、备份恢复 |

## 6. 发布结论

当前代码和文档满足 v1.0.0 本地个人工具 MVP 的发布条件。

推荐发布口径：

```text
The One v1.0.0 是本地个人 memoryd MVP。
Engine MVP 已通过 synthetic 验收。
真实 Codex、Claude Code、Cursor certification 具备清单和报告机制，需要在用户本地环境按需执行。
```
