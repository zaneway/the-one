# The One 记忆（Claude Code 项目说明片段）

> 建议将本节合并进仓库根目录的 `CLAUDE.md`，与系统 Hook 配合使用。  
> Hook 负责被动 ingest / prefetch；**MCP `theone` 须连接**后 Agent 才能主动 `memory_observe` / `memory_remember`。

## 线程级常量（同一会话复用）

| 字段 | 值 |
|------|-----|
| `agent_type` | `claude_code` |
| `workspace_id` | `local_default_workspace` |
| `project_id` / `repo_id` | `the-one`（按实际项目修改） |
| `source_channel` | `agent_session` |
| `session_id` | Hook `SessionStart` 后 binding 中的 `session_id`（与 Claude `session_id` 同值） |
| `task_id` | prefetch 后 binding 中的 `task_id` |

除 `session.start` 外，调用 `memory_observe` 时 `session_id` **必填**。

## 何时调用 MCP `memory_observe`

有实质工作时至少：`session.start`（或由 Hook 代劳）+ 用户约束/问题 + 本轮 `agent.response.summary`。

入库须满足关键字段（L1 归属 + L2 检索字段），详见仓库 `.cursor/rules/theone-memory-observe.mdc`（字段要求与 agent_type 无关，将 `cursor` 改为 `claude_code` 即可）。

## 何时调用 `memory_remember`

用户明确偏好、架构结论、可复用排障步骤等长期信息；见 Rule 第 9 节。

## 自动注入的记忆上下文

- 每轮 `UserPromptSubmit` 前系统会 prefetch，并写入 `.claude/theone-context.md`。
- 同时通过 Hook 的 `additionalContext` 注入当轮上下文；**以 Hook 输出为准**。

## 安装与排障

见 `doc/claude/README.md`。
