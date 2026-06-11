#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

DB_PATH="${DB_PATH:-${ROOT_DIR}/.theone-data/memory.db}"
SESSION_ID="${SESSION_ID:-sess_codex_wrapper_demo}"
TURN_ID="${TURN_ID:-turn_codex_wrapper_demo}"
TASK_ID="${TASK_ID:-task_codex_wrapper_demo}"

cat <<JSON | "${ROOT_DIR}/bin/theone" observe-envelope -db-path "${DB_PATH}"
{
  "ingest_id": "ing_codex_wrapper_demo_001",
  "protocol_version": "v1",
  "producer": "codex_wrapper",
  "session_id": "${SESSION_ID}",
  "turn_id": "${TURN_ID}",
  "event_type": "agent.response.summary",
  "occurred_at": "2026-05-28T15:00:00+08:00",
  "payload": {
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "agent_type": "codex",
    "task_id": "${TASK_ID}",
    "task_summary": "wrapper 示例接入",
    "user_summary": "【事件】用户请求 Codex wrapper 继续执行下一步。\n【事实】本轮用于演示 observe-envelope 标准写入。",
    "agent_summary": "【结论/决策】Codex wrapper 使用结构化 agent_summary 写入 agent.response.summary。\n【约束】摘要遵守 doc/shared/content-summary-structured.md，不写全文日志或 full_diff。\n【关联】scripts/examples/codex_wrapper_observe_envelope.sh\n【状态】示例 payload 可用于本地 observe-envelope 验证。",
    "is_substantive": true,
    "keywords": ["codex", "wrapper", "observe-envelope", "agent_summary", "content_summary"],
    "salient_spans": [
      "Codex wrapper envelope 使用 agent_summary / user_summary",
      "agent_summary 已包含结构化标签和高价值前置内容",
      "示例不包含全文日志、代码块或 full_diff"
    ]
  }
}
JSON
