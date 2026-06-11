#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"
DATA_DIR="${DATA_DIR:-/tmp/theone-p1-acceptance}"
DB_PATH="${DATA_DIR}/memory.db"
STATE_DIR="${DATA_DIR}/runtime-state"

rm -rf "${DATA_DIR}"
mkdir -p "${STATE_DIR}"

echo "[p1-binding] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

export THEONE_ALLOW_SYNTHETIC_SESSION=1

echo "[p1-binding] session A ingest"
"${BIN_DIR}/theone" ingest -db-path "${DB_PATH}" <<'JSON' >/dev/null
{
  "ingest_id": "ing_p1_sess_a",
  "protocol_version": "v1",
  "producer": "cursor_hook:sessionStart",
  "agent_type": "cursor",
  "session_id": "conv_p1_a",
  "events": [{
    "kind": "session.lifecycle",
    "event_type": "session.start",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "conversation_id": "conv_p1_a",
      "content_summary": "p1 session a"
    }
  }]
}
JSON

echo "[p1-binding] seed turn dedup on session A"
"${BIN_DIR}/theone" ingest -db-path "${DB_PATH}" <<'JSON' >/dev/null
{
  "ingest_id": "ing_p1_turn_a",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterAgentResponse",
  "agent_type": "cursor",
  "session_id": "conv_p1_a",
  "events": [{
    "kind": "turn.completed",
    "event_type": "agent.response.summary",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "user_summary": "hello p1",
      "agent_summary": "done p1",
      "is_substantive": true
    }
  }]
}
JSON

echo "[p1-binding] session B reset binding + clear dedup"
"${BIN_DIR}/theone" ingest -db-path "${DB_PATH}" <<'JSON' >/dev/null
{
  "ingest_id": "ing_p1_sess_b",
  "protocol_version": "v1",
  "producer": "cursor_hook:sessionStart",
  "agent_type": "cursor",
  "session_id": "conv_p1_b",
  "events": [{
    "kind": "session.lifecycle",
    "event_type": "session.start",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "conversation_id": "conv_p1_b",
      "content_summary": "p1 session b"
    }
  }]
}
JSON

python3 - <<'PY' "${STATE_DIR}/binding.cursor.json" "${STATE_DIR}/turn-dedup.json"
import json
import pathlib
import sys

binding_path = pathlib.Path(sys.argv[1])
dedup_path = pathlib.Path(sys.argv[2])
if not binding_path.is_file():
    raise SystemExit("[p1-binding] binding.cursor.json missing")
binding = json.loads(binding_path.read_text())
if binding.get("session_id") != "conv_p1_b":
    raise SystemExit(f"[p1-binding] want session conv_p1_b, got {binding.get('session_id')}")
if binding.get("external_session_key") != "conv_p1_b":
    raise SystemExit("[p1-binding] external_session_key mismatch")
if not dedup_path.is_file():
    raise SystemExit("[p1-binding] turn-dedup.json missing")
dedup = json.loads(dedup_path.read_text())
if dedup.get("last_turn_id"):
    raise SystemExit(f"[p1-binding] turn-dedup should be cleared, got {dedup}")
if "session_id" in dedup or "task_id" in dedup:
    raise SystemExit("[p1-binding] turn-dedup must not contain session_id/task_id")
print("[p1-binding] acceptance passed")
PY
