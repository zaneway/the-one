#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DATA_DIR="${ROOT_DIR}/.theone-data"
STATE_DIR="${DATA_DIR}/runtime-state"
CACHE_FILE="${STATE_DIR}/prompt-cache.json"
THEONE_BIN="${ROOT_DIR}/bin/theone"
CONFIG_PATH="${ROOT_DIR}/theone.yaml"
CONTEXT_CACHE_FILE="${STATE_DIR}/context-cache.json"
CONTEXT_ERR_FILE="${STATE_DIR}/context-cache.error.log"

mkdir -p "${STATE_DIR}"

HOOK_PAYLOAD="$(cat || true)"

python3 - <<'PY' "${HOOK_PAYLOAD}" "${CACHE_FILE}" >/dev/null 2>&1 || true
import json
import os
import sys
from datetime import datetime

raw = sys.argv[1] if len(sys.argv) > 1 else ""
cache_file = sys.argv[2]
try:
    data = json.loads(raw) if raw.strip() else {}
except Exception:
    data = {}

def pick(dct, keys, default=""):
    for k in keys:
        v = dct.get(k, "")
        if isinstance(v, str) and v.strip():
            return v.strip()
    return default

prompt = pick(data, ["prompt", "userMessage", "input", "message"], "")
session_id = pick(data, ["session_id", "sessionId"], "")
task_id = pick(data, ["task_id", "taskId"], "")

payload = {
    "user_summary": prompt[:1000] if prompt else "用户输入摘要未直接可见",
    "session_id": session_id,
    "task_id": task_id,
    "captured_at": datetime.now().astimezone().isoformat()
}
with open(cache_file, "w", encoding="utf-8") as f:
    json.dump(payload, f, ensure_ascii=False)
PY

if [[ -x "${THEONE_BIN}" ]]; then
CONTEXT_REQ="$(python3 - <<'PY' "${CACHE_FILE}"
import json
import sys

cache_file = sys.argv[1]
try:
    with open(cache_file, "r", encoding="utf-8") as f:
        cache = json.load(f)
except Exception:
    cache = {}

task = (cache.get("user_summary") or "").strip()
if not task:
    task = "用户输入摘要未直接可见"

session_id = (cache.get("session_id") or "").strip()
task_id = (cache.get("task_id") or "").strip()
if not task_id:
    task_id = "task_cursor_auto"

payload = {
    "task": task[:1000],
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "session_id": session_id,
    "agent_type": "cursor",
    "token_budget": 1200,
    "include_code_refs": True,
    "include_evidence_summary": True
}
print(json.dumps(payload, ensure_ascii=False))
PY
)"

if [[ -n "${CONTEXT_REQ}" ]]; then
  if ! echo "${CONTEXT_REQ}" | "${THEONE_BIN}" context -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" >"${CONTEXT_CACHE_FILE}" 2>"${CONTEXT_ERR_FILE}"; then
    python3 - <<'PY' "${CONTEXT_ERR_FILE}" "${CONTEXT_CACHE_FILE}" >/dev/null 2>&1 || true
import json
import sys
from datetime import datetime

err_file = sys.argv[1] if len(sys.argv) > 1 else ""
out_file = sys.argv[2] if len(sys.argv) > 2 else ""
error_text = ""
if err_file:
    try:
        with open(err_file, "r", encoding="utf-8") as f:
            error_text = f.read().strip()
    except Exception:
        error_text = ""
payload = {
    "ok": False,
    "error_summary": (error_text[-1500:] if error_text else "memory.context 调用失败"),
    "updated_at": datetime.now().astimezone().isoformat()
}
if out_file:
    with open(out_file, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False)
PY
  fi
fi
fi

exit 0

