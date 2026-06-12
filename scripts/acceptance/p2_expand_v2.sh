#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"
DATA_DIR="${DATA_DIR:-/tmp/theone-p2-acceptance}"
DB_PATH="${DATA_DIR}/memory.db"
STATE_DIR="${DATA_DIR}/runtime-state"
CONFIG_PATH="${ROOT_DIR}/theone.yaml"

rm -rf "${DATA_DIR}"
mkdir -p "${STATE_DIR}"

echo "[p2-v2] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

export THEONE_ALLOW_SYNTHETIC_SESSION=1

ingest() {
  "${BIN_DIR}/theone" ingest -config "${CONFIG_PATH}" -db-path "${DB_PATH}" "$@"
}

echo "[p2-v2] session start"
ingest <<'JSON' >/dev/null
{
  "ingest_id": "ing_p2_sess",
  "protocol_version": "v1",
  "producer": "cursor_hook:sessionStart",
  "agent_type": "cursor",
  "session_id": "conv_p2_v2",
  "events": [{
    "kind": "session.lifecycle",
    "event_type": "session.start",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "conversation_id": "conv_p2_v2",
      "content_summary": "p2 v2 session"
    }
  }]
}
JSON

echo "[p2-v2] atomic agent.decision"
OUT_FILE="$(ingest <<'JSON'
{
  "ingest_id": "ing_p2_file_1",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterAgentResponse",
  "agent_type": "cursor",
  "session_id": "conv_p2_v2",
  "events": [{
    "kind": "capture.atomic",
    "event_type": "agent.decision",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "content_summary": "【结论/决策】p2 atomic decision once"
    }
  }]
}
JSON
)"
echo "${OUT_FILE}"

echo "[p2-v2] atomic agent.decision retry (atomic-dedup)"
OUT_FILE2="$(ingest <<'JSON'
{
  "ingest_id": "ing_p2_file_2",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterAgentResponse",
  "agent_type": "cursor",
  "session_id": "conv_p2_v2",
  "events": [{
    "kind": "capture.atomic",
    "event_type": "agent.decision",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "content_summary": "【结论/决策】p2 atomic decision once"
    }
  }]
}
JSON
)"
echo "${OUT_FILE2}"

echo "[p2-v2] turn.completed without file_edits (v2)"
OUT_TURN="$(ingest <<'JSON'
{
  "ingest_id": "ing_p2_turn",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterAgentResponse",
  "agent_type": "cursor",
  "session_id": "conv_p2_v2",
  "events": [{
    "kind": "turn.completed",
    "event_type": "agent.response.summary",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "user_summary": "hello p2",
      "agent_summary": "done p2",
      "is_substantive": true
    }
  }]
}
JSON
)"
echo "${OUT_TURN}"

python3 - <<'PY' "${OUT_FILE}" "${OUT_FILE2}" "${OUT_TURN}" "${STATE_DIR}/atomic-dedup.json"
import json
import pathlib
import sys

first = json.loads(sys.argv[1])
second = json.loads(sys.argv[2])
turn = json.loads(sys.argv[3])
dedup_path = pathlib.Path(sys.argv[4])

if first.get("accepted", 0) < 1:
    raise SystemExit(f"[p2-v2] first atomic accepted={first.get('accepted')}")
if second.get("deduped", 0) < 1:
    raise SystemExit(f"[p2-v2] second atomic deduped={second.get('deduped')}, want atomic-dedup")
if turn.get("accepted", 0) < 2:
    raise SystemExit(f"[p2-v2] turn accepted={turn.get('accepted')}, want user+agent")
if not dedup_path.is_file():
    raise SystemExit("[p2-v2] atomic-dedup.json missing")
print("[p2-v2] acceptance passed")
PY
