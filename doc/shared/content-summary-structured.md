# 结构化 content_summary 规范

本规范是 cursor、claude_code、codex 三端 Agent 写入 `content_summary` 的单一真相。目标是在捕获期把高价值信息前置，缩小入库体积，降低 FTS 噪声，减少 `memory.content` 注入 token 浪费。

当前 `memory.context` 仍可能按头部截断注入内容；本规范不修改 retrieval / `memory.context` 截断策略，而是通过结构化索引卡降低无关内容进入头部的概率。

链路边界：`memory.observe` / Hook 负责构造最小事实事件；进入 `raw_event` 后，外部 AI 或规则模型再从 `raw_payload_json` 与索引字段中抽取 evidence / candidate，不在写入前改写原始事实。部分低价值控制面事件会被 ingest 抑制，不写 `raw_event`，例如 Cursor 的 `session.start` 和 `tool.result.summary`。

## 字段分工

| 字段 | 分工 |
|------|------|
| `content_summary` | 结构化索引卡，建议 200-800 字，硬上限 6000；高价值信息必须前置。 |
| `raw_payload_json` | 可选的有界原始/准原始事实载荷；默认上限 1MiB，尽量不截断；用于后续 AI evidence 抽取和审计重放，不直接进入 `memory_item`。 |
| `payload_schema` | `raw_payload_json` 的 schema 版本，例如 `turn.completed.v1`、`tool_result.v1`、`file_edit.v1`。 |
| `raw_payload_hash` | `raw_payload_json` 的 SHA256；为空时服务端自动计算。 |
| `redaction_state` | 当前暂用 `raw`；后续如接入脱敏再使用 `redacted` / `minimized`。 |
| `truncation` | 截断元数据；默认不截断，只有超过 raw payload 上限时才标记 `truncated=true`。 |
| `salient_spans` | 1-5 条原子事实，每条 <=500 字；不得粘贴全文。 |
| `keywords` | 3-8 个检索锚点，优先模块名、文件名、概念、错误码、决策词。 |
| `memory_remember.content` | 只写长期结论、偏好、约束、流程或失败经验，建议 <=1500 字。 |

## 固定标签

`content_summary` 使用以下固定标签；无内容的标签段可省略：

- `【事件】`
- `【事实】`
- `【结论/决策】`
- `【约束】`
- `【关联】`
- `【状态】`

服务端质量校验要求 `content_summary` 至少包含 `【事件】`、`【事实】` 或 `【结论` 之一。超过 400 字时，前 400 字内必须出现 `【结论` 或 `【约束】`，确保头部截断仍保留高价值信息。

## 禁止项

- 禁止对话复述。
- 禁止代码块。
- 禁止 `full_diff`。
- 禁止长日志。
- 禁止流水账。
- 禁止为填满 6000 字写长文。

注意：禁止项约束的是 `content_summary`、`salient_spans` 和 `source_refs`。如果需要保留原始事实，应放入 `raw_payload_json`，并携带 `payload_schema`、`redaction_state`、`truncation` 元数据。

## event_type 写作要点

| event_type | content_summary 要点 | salient_spans 要点 |
|------------|----------------------|--------------------|
| `session.start` | 控制面事件，默认不写 `raw_event`；仅维护 session/task binding。 | 不适用。 |
| `conversation.message` | 用户新问题、约束或纠正；长内容必须把约束前置。 | 1-3 条用户原子需求。 |
| `user.declaration` / `user.correction` | `【事实】` 用户声明；若是长期约束用 `【约束】` 前置。 | 用户明确表达的偏好/约束。 |
| `agent.response.summary` | `【结论/决策】` 本轮结果先写；再写关键事实、测试状态、关联文件。 | 1-5 条完成项、风险或验证结果。 |
| `agent.decision` | 技术取舍、理由、约束、适用范围。 | 决策和关键理由分条。 |
| `tool.result.summary` | Cursor Hook 默认抑制，不写 `raw_event`；高价值工具结论应汇入 `turn.completed` / `agent.decision` / `memory_remember`。 | 不适用。 |
| `file.edit.summary` | Hook 默认抑制，不写 `raw_event`；文件路径、hash、change_type 仅用于 Hook 链路诊断。高价值代码变更应汇入 `turn.completed` / `agent.decision` / `memory_remember`。 | 不适用。 |
| `task.result` | `【结论/决策】` 任务结果；`【状态】` succeeded/failed/interrupted。 | 结果、验证、遗留风险。 |
| `session.end` | `【结论/决策】` 会话收口；`【状态】completed/failed。 | 会话最终结果或未完成项。 |

## 坏例 / 好例

坏例：自由文本、流水账、无检索锚点。

```json
{
  "event_type": "agent.response.summary",
  "content_summary": "我先看了几个文件，然后改了一些代码，接着跑了测试，发现还有问题，又继续调整，最后应该可以了。",
  "keywords": ["代码"],
  "salient_spans": []
}
```

好例：高价值信息前置，事实原子化，span 有界。

```json
{
  "event_type": "agent.response.summary",
  "content_summary": "【结论/决策】三端 Agent 的 content_summary 统一改为结构化索引卡，服务端会拒绝无标签摘要。\n【约束】不新增 DB 字段，不修改 search_text 拼接，不调整 memory.context 截断策略。\n【事实】Hook 使用模板化 formatter 生成 800 字以内摘要，并输出 1-5 条 salient_spans。\n【关联】internal/capture/minimize.go; drivers/shared/theone-runtime-lib.py\n【状态】go test ./internal/capture/... 通过",
  "keywords": ["content_summary", "structured", "capture", "memory.context", "salient_spans"],
  "salient_spans": [
    "服务端拒绝无结构化标签的 content_summary",
    "超过 1200 字且无 salient_spans 的 conversation.message 会被拒绝",
    "三端文档以 doc/shared/content-summary-structured.md 为单一真相"
  ]
}
```
