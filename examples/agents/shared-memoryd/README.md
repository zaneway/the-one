# Shared Memory Daemon P5-D Certification

本文档用于 P5-D 真实 Agent certification。目标不是重新验证 Engine MVP；Engine MVP 已由 `make test-p5-mvp` 的 synthetic 模式覆盖。P5-D 只验证 Codex、Claude Code、Cursor 在当前本地环境中是否能共享同一个 `memoryd`，并如实记录 Level4 capability coverage、event capture completeness 和降级原因。

## 1. 前置条件

先完成本地回归：

```bash
make test-p5-mvp
```

启动同一个 Memory Daemon：

```bash
bin/memoryd serve --data-dir /tmp/the-one-p5-real-agent
```

三 Agent 必须使用同一组 scope：

| 字段 | 示例 |
|---|---|
| `workspace_id` | `ws_p5_real` |
| `project_id` | `proj_the_one` |
| `repo_id` | `repo_the_one` |

不同 Agent 必须使用不同 `session_id`。如果 Agent 或 adapter 无法被动捕获某类事件，不要伪装能力；在 P5 capability 记录中写降级原因。

## 2. 创建 mixed run

通过 MCP 调用：

```json
{
  "tool": "memory.mvp.run.start",
  "params": {
    "name": "P5-D mixed real-agent certification",
    "mode": "mixed",
    "workspace_id": "ws_p5_real",
    "project_id": "proj_the_one",
    "repo_id": "repo_the_one",
    "baseline_type": "summary_only",
    "candidate_type": "hybrid_memory"
  }
}
```

保存返回的 `run_id`。后续三 Agent certification 记录必须使用同一个 `run_id`。

## 3. Agent certification 采集口径

每个 Agent 需要形成一条 `mvp_agent_capability` 记录，字段含义如下：

| 字段 | 通过条件 |
|---|---|
| `conversation_capture` | 能捕获用户消息和 Agent 回复摘要 |
| `tool_call_capture` | 能捕获工具名、调用时间和参数摘要 |
| `tool_output_capture` | 能捕获输出摘要、错误签名、exit code、hash |
| `file_edit_capture` | 能捕获文件路径、符号、diff 摘要和 content hash |
| `session_lifecycle` | 能捕获 session start/end、任务目标和任务结果 |
| `memory_observe` | 能主动上报标准 RawEvent |
| `completeness` | 本次 certification 中实际捕获事件数 / 期望可观测事件数 |

必需 Agent：

1. `codex`
2. `claude_code`
3. `cursor`

P5-D 聚合逻辑会把缺失 Agent 自动判定为 certification 失败；不能只记录一个通过的 Agent 后宣称整体通过。

## 4. 手工验收流程

### 4.1 Claude Code

1. 使用 `../claude-code/README.md` 的 hooks 或等价包装方式连接同一个 `memoryd`。
2. 上报 `session.start`，capability 中如实声明六项能力。
3. 执行一个包含工具调用、测试结果和文件编辑的短任务。
4. 上报 `tool.call`、`tool.result.summary`、`file.edit.summary`、`task.result`、`session.end`。
5. 根据 capture diagnostics 计算 completeness。

### 4.2 Codex

1. 使用 `../codex/README.md` 的 wrapper/log collector 策略连接同一个 `memoryd`。
2. 执行一个短工程任务，至少覆盖 session lifecycle、tool result summary、file edit summary 和 memory observe。
3. 如果当前 Codex 环境无法被动捕获工具调用，`tool_call_capture=false`，并在降级原因中写明。

### 4.3 Cursor

1. 使用 `../cursor/README.md` 的 rules 主动调用 MCP。
2. 执行一个短工程任务，至少覆盖用户声明、文件编辑摘要、任务结果和 session end。
3. 如果无法捕获内部工具调用，不要把 `file.edit.summary` 伪装成 `tool.call`。

## 5. 记录 capability 示例

P5-D 手工验收通过 `memory.mvp.capability.record` 写入每个 Agent 的 capability 快照。该工具会校验 run 是否存在、Agent 是否属于 `codex` / `claude_code` / `cursor`、`capture_level` 是否在 1 到 4 之间，以及降级能力是否提供原因。

```json
{
  "tool": "memory.mvp.capability.record",
  "params": {
    "run_id": "mvp_run_xxx",
    "agent_type": "codex",
    "adapter_name": "wrapper",
    "adapter_version": "local",
    "capture_level": 3,
    "conversation_capture": false,
    "tool_call_capture": true,
    "tool_output_capture": true,
    "file_edit_capture": true,
    "session_lifecycle": true,
    "memory_observe": true,
    "completeness": 0.91,
    "degradation_reasons": ["conversation_capture_unavailable"]
  }
}
```

## 6. 生成报告

完成三 Agent capability 和至少一个 cross-agent task 记录后，调用：

```json
{
  "tool": "memory.mvp.metrics.compute",
  "params": {
    "run_id": "mvp_run_xxx",
    "recompute": true
  }
}
```

然后生成报告：

```json
{
  "tool": "memory.mvp.report",
  "params": {
    "run_id": "mvp_run_xxx",
    "format": "markdown",
    "include_failures": true
  }
}
```

报告必须显示：

1. Engine MVP 结果。
2. 每个 Agent 的 coverage 和 completeness。
3. 缺失能力或缺失 Agent 的失败指标。
4. 降级原因。

## 7. P5-D 通过条件

| 条件 | 标准 |
|---|---|
| 共享 daemon | 三 Agent 连接同一个 `memoryd` 和 data dir |
| scope 对齐 | 三 Agent 使用同一 workspace/project/repo |
| capability coverage | 三 Agent 分别统计，不能只看平均值 |
| completeness | 每个 Agent `>= 0.90`，不可观测能力必须写降级原因 |
| 缺失 Agent | 任一 Agent 未记录则 certification 失败 |
| 数据边界 | 不保存完整对话、完整工具输出、完整 diff、完整源码 |

P5-D 可以允许某个 Agent 当前只能达到 Level3，但 release notes 必须明确该限制。只有三个 Agent 均达到 Level4 coverage 且 completeness 达标，`agent_certification_passed` 才能为 true。
