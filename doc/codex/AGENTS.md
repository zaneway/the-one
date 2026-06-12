# The One 记忆（Codex 项目说明片段）

> 建议将本节合并进仓库根目录的 `AGENTS.md`，与系统 Hook 配合使用。  
> Hook 负责被动 ingest / prefetch；**MCP `theone` 须连接**后 Agent 才能主动 `memory_observe` / `memory_remember`。

## 线程级常量（同一会话复用）

| 字段 | 值 |
|------|-----|
| `agent_type` | `codex` |
| `workspace_id` | `local_default_workspace` |
| `project_id` / `repo_id` | 默认取 Hook payload 中当前对话工作目录名；可用 `THEONE_PROJECT_ID` / `THEONE_REPO_ID` 覆盖 |
| `source_channel` | `agent_session` |
| `session_id` | Hook `SessionStart` 后 binding 中的 `session_id` |
| `task_id` | prefetch 后 binding 中的 `task_id` |

除 `session.start` 外，调用 `memory_observe` 时 `session_id` **必填**。不要用 `memory_observe` 主动写 `session.start`。

## 何时调用 MCP `memory_observe`

有实质工作时优先记录本轮 `turn.completed`（Hook `Stop` 也会代劳）。不要把同一轮用户请求和助手应答拆成 `conversation.message` + `agent.response.summary` 两条 raw_event。

入库须满足关键字段（L1 归属 + L2 检索字段），字段规范与 Cursor rule 一致，仅 `agent_type=codex` 不同。

通过 MCP 主动调用 `memory_observe` 时，`source_refs.capture_method` 使用 `manual_mcp_call`；Hook 被动采集事件由 Driver 自动写入 `producer=codex_hook:<HookEvent>`，不要在 MCP payload 中伪造 hook producer。

`memory_observe` / Hook 只保存 `raw_event`，不在写入前调用外部 AI 做语义简化；evidence / candidate 抽取由 raw_event 落库后的自动处理链路完成。

结构化 `content_summary` 统一遵守 `doc/shared/content-summary-structured.md`：

- `content_summary` 使用结构化索引卡，不写自由文本流水账。
- 固定标签、`salient_spans`、`keywords`、`memory_remember.content` 的分工以 shared 文档为准。
- 当前 `memory.context` 仍可能头部截断，因此高价值结论和约束必须前置。
- 需要保留原始事实时，使用 `raw_payload_json` + `payload_schema` + `redaction_state=raw`；当前暂不做脱敏，默认上限 1MiB，尽量不截断。

## 何时调用 `memory_remember`

用户明确偏好、架构结论、可复用排障步骤等长期信息。

## 自动注入的记忆上下文

- 每轮 `UserPromptSubmit` 前系统会 prefetch，并写入 `.codex/theone-context.md`。
- 同时通过 Hook 的 `hookSpecificOutput.additionalContext` 注入当轮上下文；**以 Hook 输出为准**。

## 安装与排障

见 `doc/codex/README.md`。
