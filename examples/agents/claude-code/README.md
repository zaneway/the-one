# Claude Code P2 Capture Example

目标：通过 Claude Code hooks 或等价包装脚本，把会话生命周期、工具调用和工具结果摘要上报到 `memory.observe`。

P2 目标等级：Level3+。

P5-D certification 使用同一个共享 `memoryd`，详见 `../shared-memoryd/README.md`。Claude Code 优先通过 hooks 证明 Level4 capability；如果文件编辑或对话摘要在当前 hook 环境不可观测，必须在 capability 降级原因中说明。

## 能力声明

```json
{
  "conversation_capture": false,
  "tool_call_capture": true,
  "tool_output_capture": true,
  "file_edit_capture": false,
  "session_lifecycle": true,
  "mcp_observe": true,
  "requires_wrapper": false,
  "requires_rules_injection": false
}
```

如果 hook 能稳定拿到文件编辑摘要，可把 `file_edit_capture` 改为 `true`。

P5-D capability 建议：

| 能力 | 建议记录 |
|---|---|
| `conversation_capture` | prompt/response 摘要 hook 可用时为 true |
| `tool_call_capture` | tool use hook 可用时为 true |
| `tool_output_capture` | tool result 摘要、exit code、hash 可用时为 true |
| `file_edit_capture` | edit hook 或 git diff 摘要可用时为 true |
| `session_lifecycle` | start/end hook 可用时为 true |
| `memory_observe` | MCP observe 可用时为 true |

降级原因示例：

```json
["file_edit_capture_uses_git_diff_fallback"]
```

## session.start

```json
{
  "event_type": "session.start",
  "source_channel": "agent_session",
  "agent_type": "claude_code",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "adapter",
  "session": {
    "goal_summary": "执行当前 Claude Code 任务",
    "status": "active"
  },
  "capture_capabilities": {
    "conversation_capture": false,
    "tool_call_capture": true,
    "tool_output_capture": true,
    "file_edit_capture": false,
    "session_lifecycle": true,
    "mcp_observe": true
  }
}
```

响应中的 `session_id` 必须缓存到本次 Claude Code 会话上下文，后续事件都带上该值。

## tool.call

```json
{
  "session_id": "sess_from_session_start",
  "event_type": "tool.call",
  "source_channel": "agent_session",
  "agent_type": "claude_code",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "tool",
  "tool_name": "go test",
  "input_summary": "运行 Go 全量测试",
  "keywords": ["go test", "verification"],
  "source_refs": [
    {
      "source_type": "tool_call",
      "capture_method": "adapter_hook",
      "tool_name": "go test",
      "command_hash": "sha256:command"
    }
  ],
  "content_hash": "sha256:event"
}
```

## tool.result.summary

```json
{
  "session_id": "sess_from_session_start",
  "event_type": "tool.result.summary",
  "source_channel": "agent_session",
  "agent_type": "claude_code",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "tool",
  "tool_name": "go test",
  "input_summary": "运行 Go 全量测试",
  "output_summary": "测试通过",
  "keywords": ["go test", "pass"],
  "salient_spans": ["go test ./...: pass"],
  "source_refs": [
    {
      "source_type": "tool_output",
      "capture_method": "adapter_hook",
      "tool_name": "go test",
      "command_hash": "sha256:command",
      "exit_code": 0
    }
  ],
  "content_hash": "sha256:event"
}
```

只保存摘要、错误签名、exit code 和 hash，不保存完整 stdout/stderr。

## session.end

```json
{
  "session_id": "sess_from_session_start",
  "event_type": "session.end",
  "source_channel": "agent_session",
  "agent_type": "claude_code",
  "workspace_id": "ws_local",
  "project_id": "proj_the_one",
  "repo_id": "repo_the_one",
  "actor": "adapter",
  "session": {
    "status": "completed"
  },
  "content_summary": "Claude Code 会话结束"
}
```
