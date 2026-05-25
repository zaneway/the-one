# Codex P2 Capture Example

目标：通过 wrapper 或日志 collector 上报 Codex session、task、工具摘要和文件编辑摘要。

P2 目标等级：Level2+。如果 wrapper 能稳定捕获工具调用和结果摘要，可达到 Level3。

P5-D certification 使用同一个共享 `memoryd`，详见 `../shared-memoryd/README.md`。Codex 在 P5-D 中必须如实记录 capability；如果当前运行环境只能通过主动 `memory.observe` 捕获关键节点，不能把被动工具捕获能力标记为 true。

## 能力声明

```json
{
  "conversation_capture": false,
  "tool_call_capture": true,
  "tool_output_capture": true,
  "file_edit_capture": true,
  "session_lifecycle": true,
  "mcp_observe": true,
  "requires_wrapper": true,
  "requires_rules_injection": false
}
```

如果当前 Codex 运行环境不能捕获工具调用，把 `tool_call_capture` 和 `tool_output_capture` 改为 `false`，不要伪装 Level3。

P5-D capability 建议：

| 能力 | 建议记录 |
|---|---|
| `conversation_capture` | wrapper 能摘要用户输入和 Agent 回复时为 true，否则 false |
| `tool_call_capture` | wrapper 能捕获工具名和参数摘要时为 true |
| `tool_output_capture` | 能捕获 output 摘要、exit code、hash 时为 true |
| `file_edit_capture` | git diff 摘要或 watcher 可用时为 true |
| `session_lifecycle` | wrapper start/end 可用时为 true |
| `memory_observe` | MCP observe 可用时为 true |

降级原因示例：

```json
["conversation_capture_unavailable", "tool_call_only_active_observe"]
```

## wrapper 事件策略

1. wrapper 启动时发送 `session.start`。
2. 每个用户任务开始时可发送 `task.start`，否则由服务端创建 `default_task`。
3. 每个命令执行前发送 `tool.call`。
4. 命令结束后发送 `tool.result.summary`。
5. 文件修改后用 `git diff --stat`、路径列表和内容 hash 生成 `file.edit.summary`。
6. wrapper 退出时发送 `session.end`。

## file.edit.summary

```json
{
  "session_id": "sess_from_session_start",
  "event_type": "file.edit.summary",
  "source_channel": "agent_session",
  "agent_type": "codex",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "adapter",
  "content_summary": "修改 capture service 和 MCP 工具注册，新增 P2-C3 测试。",
  "keywords": ["capture", "memory.observe", "mcp"],
  "salient_spans": [
    "internal/mcp/tools/capture.go added",
    "internal/app/app.go registered capture tools"
  ],
  "source_refs": [
    {
      "source_type": "file_edit_summary",
      "capture_method": "git_diff",
      "file_path": "internal/mcp/tools/capture.go",
      "change_type": "add",
      "after_hash": "sha256:file"
    }
  ],
  "content_hash": "sha256:event"
}
```

禁止把完整 diff 放入 `content_summary`、`salient_spans` 或 `source_refs`。

## task.result

```json
{
  "session_id": "sess_from_session_start",
  "event_type": "task.result",
  "source_channel": "agent_session",
  "agent_type": "codex",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "adapter",
  "task": {
    "task_summary": "推进 P2-C3 研发",
    "status": "succeeded",
    "outcome_summary": "MCP capture tools 已注册，测试通过。"
  },
  "content_summary": "任务成功完成"
}
```

`task_summary` 和 `outcome_summary` 都是摘要，不保存完整对话。
