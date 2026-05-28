#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
THEONE_BIN="${ROOT_DIR}/bin/theone"
CONFIG_PATH="${ROOT_DIR}/theone.yaml"
DATA_DIR="${ROOT_DIR}/.theone-data"
STATE_FILE="${DATA_DIR}/runtime-state/session.json"

if [[ ! -x "${THEONE_BIN}" ]]; then
  exit 0
fi

HOOK_PAYLOAD="$(cat || true)"

END_JSON="$(python3 - <<'PY' "${HOOK_PAYLOAD}" "${STATE_FILE}"
import json
import sys
from datetime import datetime

raw = sys.argv[1] if len(sys.argv) > 1 else ""
state_file = sys.argv[2]
try:
    data = json.loads(raw) if raw.strip() else {}
except Exception:
    data = {}

state = {}
try:
    with open(state_file, "r", encoding="utf-8") as f:
        state = json.load(f)
except Exception:
    state = {}

def pick(dct, keys, default=""):
    for k in keys:
        v = dct.get(k, "")
        if isinstance(v, str) and v.strip():
            return v.strip()
    return default

session_id = pick(data, ["session_id", "sessionId"], pick(state, ["session_id"], ""))
task_id = pick(data, ["task_id", "taskId"], pick(state, ["task_id"], "task_cursor_auto"))
summary = pick(data, ["summary", "result", "message"], "Cursor 会话结束")
if not session_id:
    session_id = f"sess_cursor_auto_{datetime.now().strftime('%Y%m%d')}"

payload = {
    "session_id": session_id,
    "task_id": task_id,
    "agent_type": "cursor",
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "event_type": "session.end",
    "source_channel": "agent_session",
    "actor": "adapter",
    "content_summary": summary[:1600],
    "session": {"status": "completed"},
    "task": {"task_summary": task_id, "status": "completed", "outcome_summary": summary[:1600]}
}
print(json.dumps(payload, ensure_ascii=False))
PY
)"

echo "${END_JSON}" | "${THEONE_BIN}" observe -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" >/dev/null 2>&1 || true
exit 0

