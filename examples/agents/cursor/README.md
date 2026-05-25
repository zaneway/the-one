# Cursor P2 Capture Example

目标：通过 Cursor rules 引导 Agent 主动调用 `memory.observe`，并用 Git diff 摘要补充文件编辑事件。

P2 目标等级：Level2+。Cursor 内部工具调用是否可见取决于运行环境，P2 不依赖私有接口。

P5-D certification 使用同一个共享 `memoryd`，详见 `../shared-memoryd/README.md`。Cursor 的 rules 更适合主动上报关键节点；如果内部工具调用不可见，应保持 tool call capability 为 false，并在报告中说明。

## 能力声明

```json
{
  "conversation_capture": false,
  "tool_call_capture": false,
  "tool_output_capture": true,
  "file_edit_capture": true,
  "session_lifecycle": true,
  "mcp_observe": true,
  "requires_wrapper": false,
  "requires_rules_injection": true
}
```

如果只能由 Agent 主动上报关键节点，而无法被动捕获工具事件，保留 Level2，不要把 `tool_call_capture` 标记为 `true`。

P5-D capability 建议：

| 能力 | 建议记录 |
|---|---|
| `conversation_capture` | rules/plugin/log 能形成消息摘要时为 true |
| `tool_call_capture` | 能捕获内部工具调用摘要时为 true，否则 false |
| `tool_output_capture` | 能主动上报测试/构建输出摘要时为 true |
| `file_edit_capture` | git diff 摘要或 watcher 可用时为 true |
| `session_lifecycle` | rules/wrapper 能上报 start/end 时为 true |
| `memory_observe` | MCP observe 可用时为 true |

降级原因示例：

```json
["tool_call_capture_unavailable_in_current_cursor_environment"]
```

## 建议 Cursor Rule 片段

```text
在开始一个较长工程任务时，调用 memory.observe 上报 session.start。
在运行关键测试、构建、lint 后，调用 memory.observe 上报 tool.result.summary。
在完成文件修改后，用文件路径、符号、diff 摘要和 hash 上报 file.edit.summary。
在任务完成或中断时，调用 memory.observe 上报 task.result 和 session.end。
不要上报完整源码、完整 diff、完整工具输出或完整对话。
```

## 用户声明事件

```json
{
  "session_id": "sess_from_session_start",
  "event_type": "user.declaration",
  "source_channel": "mcp_tool",
  "agent_type": "cursor",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "user",
  "content_summary": "用户要求后续技术方案先分析架构边界、风险和工程落地。",
  "keywords": ["用户偏好", "架构边界", "工程落地"],
  "salient_spans": [
    "以后技术方案先分析架构边界、风险和工程落地"
  ],
  "source_refs": [
    {
      "source_type": "user_declared",
      "capture_method": "cursor_rule"
    }
  ],
  "content_hash": "sha256:event"
}
```

该事件只进入 `raw_event`。如果需要立即形成稳定长期记忆，应另行调用 `memory.remember`。

## 用户纠正事件

```json
{
  "session_id": "sess_from_session_start",
  "event_type": "user.correction",
  "source_channel": "mcp_tool",
  "agent_type": "cursor",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "user",
  "content_summary": "用户纠正：低频配置缓存优先考虑本地进程缓存，不是所有缓存都用 Redis。",
  "keywords": ["用户纠正", "缓存", "Redis", "本地缓存"],
  "source_refs": [
    {
      "source_type": "user_correction",
      "capture_method": "cursor_rule",
      "target_event_id": "evt_previous"
    }
  ],
  "content_hash": "sha256:event"
}
```

`target_event_id` 用于 P3 把纠正和原始事件关联起来。
