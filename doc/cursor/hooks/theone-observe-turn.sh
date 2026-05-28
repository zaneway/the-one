#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
THEONE_BIN="${ROOT_DIR}/bin/theone"
CONFIG_PATH="${ROOT_DIR}/theone.yaml"
DATA_DIR="${ROOT_DIR}/.theone-data"
PROMPT_CACHE_FILE="${DATA_DIR}/runtime-state/prompt-cache.json"

if [[ ! -x "${THEONE_BIN}" ]]; then
  exit 0
fi

HOOK_PAYLOAD="$(cat || true)"

TURN_JSON="$(python3 - <<'PY' "${HOOK_PAYLOAD}" "${PROMPT_CACHE_FILE}"
import json
import hashlib
import sys
from datetime import datetime, timezone

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

response = pick(data, ["response", "content", "assistantMessage", "output", "text"], "")
if not response:
    response = "Agent 已完成本轮响应"

user_summary = pick(data, ["prompt", "userMessage", "input", "lastUserMessage"], "")
if not user_summary:
    try:
        with open(sys.argv[3], "r", encoding="utf-8") as f:
            cache = json.load(f)
            cached = (cache.get("user_summary") or "").strip()
            if cached:
                user_summary = cached
    except Exception:
        pass
if not user_summary:
    user_summary = "用户输入摘要未直接可见"

session_id = pick(data, ["session_id", "sessionId"], "")
if not session_id:
    # 基于仓库和日期构造稳定会话前缀
    session_id = "sess_cursor_auto_" + datetime.now().strftime("%Y%m%d")

task_id = pick(data, ["task_id", "taskId"], "")
if not task_id:
    task_id = "task_cursor_auto"

base = f"{session_id}|{user_summary}|{response}|{datetime.now().strftime('%Y%m%d%H%M%S')}"
turn_id = "turn_" + hashlib.sha1(base.encode("utf-8")).hexdigest()[:16]

payload = {
    "workspace_id": "local_default_workspace",
    "project_id": "the-one",
    "repo_id": "the-one",
    "agent_type": "cursor",
    "session_id": session_id,
    "task_id": task_id,
    "turn_id": turn_id,
    "user_summary": user_summary[:1000],
    "agent_summary": response[:1800],
    "is_substantive": True,
    "started_at": datetime.now(timezone.utc).isoformat(),
    "completed_at": datetime.now(timezone.utc).isoformat(),
    "keywords": ["cursor", "hook", "observe-turn"],
    "salient_spans": [response[:200]]
}

print(json.dumps(payload, ensure_ascii=False))
PY
)"

if [[ -z "${TURN_JSON}" ]]; then
  exit 0
fi

SESSION_META="$(python3 - <<'PY' "${TURN_JSON}"
import json
import sys

raw = sys.argv[1] if len(sys.argv) > 1 else ""
try:
    data = json.loads(raw) if raw.strip() else {}
except Exception:
    data = {}
session_id = (data.get("session_id") or "").strip()
task_id = (data.get("task_id") or "").strip()
if not task_id:
    task_id = "task_cursor_auto"
print(session_id + "\t" + task_id)
PY
)"
SESSION_ID="${SESSION_META%%$'\t'*}"
TASK_ID="${SESSION_META#*$'\t'}"

if [[ -n "${SESSION_ID}" ]]; then
  "${THEONE_BIN}" observe -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" <<EOF >/dev/null 2>&1 || true
{"session_id":"${SESSION_ID}","task_id":"${TASK_ID}","agent_type":"cursor","workspace_id":"local_default_workspace","project_id":"the-one","repo_id":"the-one","event_type":"session.start","source_channel":"agent_session","actor":"agent","content_summary":"Cursor hook auto session start","capture_capabilities":{"conversation_capture":true,"tool_call_capture":true,"tool_output_capture":true,"file_edit_capture":true,"session_lifecycle":true,"mcp_observe":true},"session":{"goal_summary":"Cursor hook observe session","status":"active"},"task":{"task_summary":"${TASK_ID}","status":"active","outcome_summary":""}}
EOF
fi

echo "${TURN_JSON}" | "${THEONE_BIN}" observe-turn -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" >/dev/null 2>&1 || true
exit 0

