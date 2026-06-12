#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"
DATA_DIR="${DATA_DIR:-/tmp/theone-ingest-acceptance}"
DB_PATH="${DATA_DIR}/memory.db"
RUNTIME_STATE_DIR="${DATA_DIR}/runtime-state"

rm -rf "${DATA_DIR}"
mkdir -p "${DATA_DIR}"

echo "[p0-ingest] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

echo "[p0-ingest] ingest retry dedup + bootstrap + task upgrade path"
export THEONE_ALLOW_SYNTHETIC_SESSION=1

OUT1="$("${BIN_DIR}/theone" ingest -db-path "${DB_PATH}" <<'JSON'
{
  "ingest_id": "ing_p0_batch_001",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterFileEdit",
  "agent_type": "cursor",
  "session_id": "sess_p0_conv_001",
  "events": [
    {
      "kind": "capture.atomic",
      "event_type": "file.edit.summary",
      "payload": {
        "workspace_id": "local_default_workspace",
        "project_id": "the-one",
        "repo_id": "the-one",
        "agent_type": "cursor",
        "file_path": "internal/adapter/kind.go",
        "change_type": "modify",
        "content_summary": "p0 ingest acceptance edit"
      }
    }
  ]
}
JSON
)"
echo "${OUT1}"

OUT2="$("${BIN_DIR}/theone" ingest -db-path "${DB_PATH}" <<'JSON'
{
  "ingest_id": "ing_p0_batch_001",
  "protocol_version": "v1",
  "producer": "cursor_hook:afterFileEdit",
  "agent_type": "cursor",
  "session_id": "sess_p0_conv_001",
  "events": [
    {
      "kind": "capture.atomic",
      "event_type": "file.edit.summary",
      "payload": {
        "workspace_id": "local_default_workspace",
        "project_id": "the-one",
        "repo_id": "the-one",
        "agent_type": "cursor",
        "file_path": "internal/adapter/kind.go",
        "change_type": "modify",
        "content_summary": "p0 ingest acceptance edit"
      }
    }
  ]
}
JSON
)"
echo "${OUT2}"

python3 - <<'PY' "${OUT1}" "${OUT2}" "${RUNTIME_STATE_DIR}/ingest-ledger.json" "${RUNTIME_STATE_DIR}/binding.cursor.json"
import json
import pathlib
import sys

first = json.loads(sys.argv[1])
second = json.loads(sys.argv[2])
ledger_path = pathlib.Path(sys.argv[3])
binding_path = pathlib.Path(sys.argv[4])

if first.get("suppressed", 0) < 1:
    raise SystemExit(f"[p0-ingest] first suppressed={first.get('suppressed')}, want >=1 (file.edit.summary default suppressed)")
if second.get("deduped", 0) < 1:
    raise SystemExit(f"[p0-ingest] second deduped={second.get('deduped')}, want >=1")
if not ledger_path.is_file():
    raise SystemExit("[p0-ingest] ingest-ledger.json missing")
if not binding_path.is_file():
    raise SystemExit("[p0-ingest] binding.cursor.json missing")
binding = json.loads(binding_path.read_text())
if binding.get("task_id") != "task_cursor_auto":
    raise SystemExit(f"[p0-ingest] unexpected task_id={binding.get('task_id')}")
if binding.get("task_from_prompt_pending") is not True:
    raise SystemExit("[p0-ingest] want task_from_prompt_pending=true after bootstrap")

print("[p0-ingest] acceptance passed")
PY
