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
tool_name = pick(data, ["tool_name", "toolName", "tool"], "unknown_tool")
input_summary = pick(data, ["input_summary", "input", "arguments"], "")
output_summary = pick(data, ["output_summary", "output", "result", "summary"], "")
if not output_summary:
    output_summary = "工具执行完成"
exit_code = data.get("exit_code", data.get("exitCode", 0))
if not isinstance(exit_code, int):
    try:
        exit_code = int(exit_code)
    except Exception:
        exit_code = 0

base = f"{session_id}|{tool_name}|{datetime.now().strftime('%Y%m%d%H%M%S')}"
turn_id = "turn_" + hashlib.sha1(base.encode("utf-8")).hexdigest()[:16]

payload = {
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "agent_type": "cursor",
    "session_id": session_id,
    "task_id": task_id,
    "turn_id": turn_id,
    "user_summary": "本轮执行了工具",
    "agent_summary": f"工具 {tool_name} 执行完成",
    "is_substantive": True,
    "started_at": datetime.now().astimezone().isoformat(),
    "completed_at": datetime.now().astimezone().isoformat(),
    "keywords": ["cursor", "tool-result", "hook"],
    "salient_spans": [f"{tool_name}: {output_summary[:160]}"],
    "tool_results": [
        {
            "tool_name": tool_name,
            "input_summary": str(input_summary)[:1000],
            "output_summary": str(output_summary)[:1800],
            "exit_code": exit_code
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

