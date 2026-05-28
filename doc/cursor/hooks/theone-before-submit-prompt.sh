#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DATA_DIR="${ROOT_DIR}/.theone-data"
STATE_DIR="${DATA_DIR}/runtime-state"
CACHE_FILE="${STATE_DIR}/prompt-cache.json"

mkdir -p "${STATE_DIR}"

HOOK_PAYLOAD="$(cat || true)"

python3 - <<'PY' "${HOOK_PAYLOAD}" "${CACHE_FILE}" >/dev/null 2>&1 || true
import json
import os
import sys
from datetime import datetime, timezone

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
    "captured_at": datetime.now(timezone.utc).isoformat()
}
with open(cache_file, "w", encoding="utf-8") as f:
    json.dump(payload, f, ensure_ascii=False)
PY

exit 0

