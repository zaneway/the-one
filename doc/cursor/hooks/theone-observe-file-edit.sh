#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
THEONE_BIN="${ROOT_DIR}/bin/theone"
CONFIG_PATH="${ROOT_DIR}/theone.yaml"
DATA_DIR="${ROOT_DIR}/.theone-data"

if [[ ! -x "${THEONE_BIN}" ]]; then
  exit 0
fi

HOOK_PAYLOAD="$(cat || true)"

TURN_JSON="$(python3 - <<'PY' "${HOOK_PAYLOAD}"
import json
import hashlib
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

session_id = pick(data, ["session_id", "sessionId"], "sess_cursor_auto_" + datetime.now().strftime("%Y%m%d"))
task_id = pick(data, ["task_id", "taskId"], "task_cursor_auto")
file_path = pick(data, ["file_path", "path", "relativePath", "target_file"], "unknown_file")
change_type = pick(data, ["change_type", "changeType"], "modify")
summary = pick(data, ["summary", "content_summary", "description"], "")
if not summary:
    summary = f"文件修改：{file_path}"

base = f"{session_id}|{file_path}|{datetime.now().strftime('%Y%m%d%H%M%S')}"
turn_id = "turn_" + hashlib.sha1(base.encode("utf-8")).hexdigest()[:16]

payload = {
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "agent_type": "cursor",
    "session_id": session_id,
    "task_id": task_id,
    "turn_id": turn_id,
    "user_summary": "本轮触发了文件修改",
    "agent_summary": summary[:1600],
    "is_substantive": True,
    "started_at": datetime.now().astimezone().isoformat(),
    "completed_at": datetime.now().astimezone().isoformat(),
    "keywords": ["cursor", "file-edit", "hook"],
    "salient_spans": [summary[:200]],
    "file_edits": [
        {
            "file_path": file_path,
            "content_summary": summary[:1600],
            "change_type": change_type
        }
    ]
}

print(json.dumps(payload, ensure_ascii=False))
PY
)"

if [[ -z "${TURN_JSON}" ]]; then
  exit 0
fi

echo "${TURN_JSON}" | "${THEONE_BIN}" observe-turn -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" >/dev/null 2>&1 || true
exit 0

