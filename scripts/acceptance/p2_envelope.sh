#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"
DATA_DIR="${DATA_DIR:-/tmp/theone-envelope-acceptance}"
DB_PATH="${DATA_DIR}/memory.db"
RUNTIME_STATE_DIR="${DATA_DIR}/runtime-state"

rm -rf "${DATA_DIR}"
mkdir -p "${DATA_DIR}"

echo "[p2-envelope] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

echo "[p2-envelope] creating session boundary"
"${BIN_DIR}/theone" observe -db-path "${DB_PATH}" <<'JSON' >/dev/null
{
  "session_id": "sess_envelope_001",
  "event_type": "session.start",
  "source_channel": "agent_session",
  "workspace_id": "local_default_workspace",
  "project_id": "the-one",
  "repo_id": "the-one",
  "agent_type": "codex",
  "actor": "adapter",
  "content_summary": "envelope acceptance session start",
  "capture_capabilities": {
    "conversation_capture": true,
    "tool_call_capture": false,
    "tool_output_capture": true,
    "file_edit_capture": true,
    "session_lifecycle": true,
    "mcp_observe": true
  },
  "session": {
    "goal_summary": "验收 observe-envelope 入口",
    "status": "active"
  }
}
JSON

echo "[p2-envelope] writing successful envelope"
SUCCESS_OUTPUT="$("${BIN_DIR}/theone" observe-envelope -db-path "${DB_PATH}" <<'JSON'
{
  "ingest_id": "ing_success_001",
  "protocol_version": "v1",
  "producer": "codex_wrapper",
  "session_id": "sess_envelope_001",
  "turn_id": "turn_envelope_001",
  "event_type": "agent.response.summary",
  "occurred_at": "2026-05-28T14:50:00+08:00",
  "payload": {
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "agent_type": "codex",
    "task_id": "task_envelope_001",
    "task_summary": "验证 observe-envelope",
    "user_summary": "继续完成三阶段",
    "agent_summary": "已完成基础接入",
    "is_substantive": true,
    "keywords": ["observe-envelope", "codex", "wrapper"],
    "salient_spans": ["三阶段接入成功"],
    "tool_results": [
      {"tool_name": "go test", "output_summary": "ok", "exit_code": 0}
    ]
  }
}
JSON
)"
echo "${SUCCESS_OUTPUT}"

echo "[p2-envelope] writing failing envelope (tool output too large)"
LONG_OUTPUT="$(python3 - <<'PY'
print("x" * 3001)
PY
)"
FAIL_OUTPUT="$("${BIN_DIR}/theone" observe-envelope -db-path "${DB_PATH}" <<JSON
{
  "ingest_id": "ing_fail_001",
  "protocol_version": "v1",
  "producer": "codex_wrapper",
  "session_id": "sess_envelope_001",
  "turn_id": "turn_envelope_002",
  "event_type": "agent.response.summary",
  "occurred_at": "2026-05-28T14:51:00+08:00",
  "payload": {
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "agent_type": "codex",
    "task_id": "task_envelope_001",
    "task_summary": "验证失败补偿",
    "user_summary": "继续验证失败场景",
    "agent_summary": "工具输出过长",
    "is_substantive": true,
    "tool_results": [
      {"tool_name": "go test", "output_summary": "${LONG_OUTPUT}", "exit_code": 1}
    ]
  }
}
JSON
)"
echo "${FAIL_OUTPUT}"

echo "[p2-envelope] validating outputs and dead letter queue"
python3 - <<'PY' "${SUCCESS_OUTPUT}" "${FAIL_OUTPUT}" "${RUNTIME_STATE_DIR}/dead_letter.jsonl"
import json
import pathlib
import sys

success = json.loads(sys.argv[1])
failure = json.loads(sys.argv[2])
dead_letter_path = pathlib.Path(sys.argv[3])

if success.get("failure_count") != 0:
    raise SystemExit("[p2-envelope] expected success envelope failure_count=0")
if success.get("count", 0) <= 0:
    raise SystemExit("[p2-envelope] expected success envelope count>0")
if failure.get("failure_count", 0) <= 0:
    raise SystemExit("[p2-envelope] expected failing envelope failure_count>0")
if not dead_letter_path.exists():
    raise SystemExit("[p2-envelope] dead_letter.jsonl not found")

lines = [line for line in dead_letter_path.read_text().splitlines() if line.strip()]
if not lines:
    raise SystemExit("[p2-envelope] dead_letter.jsonl is empty")

print(f"[p2-envelope] dead_letter entries={len(lines)}")
print("[p2-envelope] acceptance passed")
PY

