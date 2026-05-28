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

SESSION_JSON="$(python3 - <<'PY' "${HOOK_PAYLOAD}"
import json
import sys
from datetime import datetime

raw = sys.argv[1] if len(sys.argv) > 1 else ""
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

session_id = pick(data, ["session_id", "sessionId"], f"sess_cursor_auto_{datetime.now().strftime('%Y%m%d')}")
task_id = pick(data, ["task_id", "taskId"], "task_cursor_auto")
goal = pick(data, ["goal", "goal_summary", "summary"], "Cursor hook auto session start")

payload = {
    "session_id": session_id,
    "task_id": task_id,
    "agent_type": "cursor",
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "event_type": "session.start",
    "source_channel": "agent_session",
    "actor": "adapter",
    "content_summary": "Cursor session start",
    "capture_capabilities": {
        "conversation_capture": True,
        "tool_call_capture": True,
        "tool_output_capture": True,
        "file_edit_capture": True,
        "session_lifecycle": True,
        "mcp_observe": True
    },
    "session": {"goal_summary": goal, "status": "active"},
    "task": {"task_summary": task_id, "status": "active", "outcome_summary": ""}
}
print(json.dumps(payload, ensure_ascii=False))
PY
)"

python3 - <<'PY' "${SESSION_JSON}" "${STATE_FILE}" >/dev/null 2>&1 || true
import json
import os
import sys
from datetime import datetime

raw = sys.argv[1] if len(sys.argv) > 1 else ""
state_file = sys.argv[2] if len(sys.argv) > 2 else ""
try:
    data = json.loads(raw) if raw.strip() else {}
except Exception:
    data = {}
if state_file:
    os.makedirs(os.path.dirname(state_file), exist_ok=True)
    with open(state_file, "w", encoding="utf-8") as f:
        json.dump({
            "session_id": (data.get("session_id") or "").strip(),
            "task_id": (data.get("task_id") or "").strip(),
            "updated_at": datetime.now().astimezone().isoformat()
        }, f, ensure_ascii=False)
PY

echo "${SESSION_JSON}" | "${THEONE_BIN}" observe -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" >/dev/null 2>&1 || true
exit 0

