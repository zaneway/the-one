#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"
DATA_DIR="${DATA_DIR:-/tmp/theone-p3-acceptance}"
DB_PATH="${DATA_DIR}/memory.db"
STATE_DIR="${DATA_DIR}/runtime-state"
CONFIG_PATH="${ROOT_DIR}/theone.yaml"

rm -rf "${DATA_DIR}"
mkdir -p "${STATE_DIR}"

echo "[p3-prefetch] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

export THEONE_ALLOW_SYNTHETIC_SESSION=1

echo "[p3-prefetch] bootstrap session via ingest"
"${BIN_DIR}/theone" ingest -config "${CONFIG_PATH}" -db-path "${DB_PATH}" <<'JSON' >/dev/null
{
  "ingest_id": "ing_p3_sess",
  "protocol_version": "v1",
  "producer": "cursor_hook:sessionStart",
  "agent_type": "cursor",
  "session_id": "conv_p3_prefetch",
  "events": [{
    "kind": "session.lifecycle",
    "event_type": "session.start",
    "payload": {
      "agent_type": "cursor",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "conversation_id": "conv_p3_prefetch",
      "content_summary": "p3 prefetch session"
    }
  }]
}
JSON

echo "[p3-prefetch] prefetch-context with BindTaskFromPrompt"
OUT="$(
  "${BIN_DIR}/theone" prefetch-context -config "${CONFIG_PATH}" -db-path "${DB_PATH}" <<'JSON'
{
  "task": "验收 P3 prefetch 与 task 绑定",
  "workspace_id": "local_default_workspace",
  "project_id": "the-one",
  "repo_id": "the-one",
  "conversation_id": "conv_p3_prefetch",
  "generation_id": "gen_p3_accept_001",
  "agent_type": "cursor",
  "token_budget": 1200,
  "include_code_refs": true,
  "include_evidence_summary": true
}
JSON
)"
echo "${OUT}"

python3 - <<'PY' "${OUT}" "${STATE_DIR}/binding.cursor.json" "${STATE_DIR}/inject-cache.json" "${STATE_DIR}/prefetch.json"
import json
import pathlib
import sys

out = json.loads(sys.argv[1])
binding = json.loads(pathlib.Path(sys.argv[2]).read_text())
inject = json.loads(pathlib.Path(sys.argv[3]).read_text())
prefetch = json.loads(pathlib.Path(sys.argv[4]).read_text())

if not out.get("ok"):
    raise SystemExit(f"[p3-prefetch] prefetch not ok: {out}")
if not out.get("task_bound"):
    raise SystemExit("[p3-prefetch] task_bound=false")
if binding.get("task_id") == "task_cursor_auto":
    raise SystemExit("[p3-prefetch] binding still auto task")
if binding.get("task_from_prompt_pending"):
    raise SystemExit("[p3-prefetch] task_from_prompt_pending still true")
if inject.get("generation_id") != "gen_p3_accept_001":
    raise SystemExit(f"[p3-prefetch] inject generation mismatch: {inject.get('generation_id')}")
if binding.get("task_id") != out.get("task_id"):
    raise SystemExit("[p3-prefetch] binding/task_id mismatch with prefetch output")
if prefetch.get("session_id") != "conv_p3_prefetch":
    raise SystemExit("[p3-prefetch] prefetch session mismatch")
print("[p3-prefetch] acceptance passed")
PY
