#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

DATA_DIR="${DATA_DIR:-/tmp/theone-p6-codex-hooks}"
STATE_DIR="${DATA_DIR}/runtime-state"
PROMPT_CACHE="${STATE_DIR}/prompt-cache.codex.json"
BINDING="${STATE_DIR}/binding.codex.json"
INJECT_CACHE="${STATE_DIR}/inject-cache.codex.json"
SHARED_BUILD="${ROOT_DIR}/drivers/shared/theone-build-ingest.py"

rm -rf "${DATA_DIR}"
mkdir -p "${STATE_DIR}"

test -f "${ROOT_DIR}/.codex/hooks.json"
test -x "${ROOT_DIR}/drivers/codex/hooks/theone-user-prompt-submit.sh"
test -x "${ROOT_DIR}/drivers/codex/hooks/theone-post-tool-use.sh"
test -x "${ROOT_DIR}/drivers/codex/hooks/theone-stop.sh"

export THEONE_BIN="${ROOT_DIR}/bin/theone-missing-for-hook-shape-test"
export THEONE_DATA_DIR="${DATA_DIR}"
export THEONE_AGENT_TYPE=codex

echo "[p6-codex] UserPromptSubmit shape"
USER_OUT="$("${ROOT_DIR}/drivers/codex/hooks/theone-user-prompt-submit.sh" <<'JSON'
{
  "session_id": "sess_p6_codex",
  "turn_id": "turn_p6_codex",
  "hook_event_name": "UserPromptSubmit",
  "prompt": "请验证 Codex hook-first 记忆接入"
}
JSON
)"

python3 - <<'PY' "${USER_OUT}" "${PROMPT_CACHE}"
import json
import pathlib
import sys

out = json.loads(sys.argv[1])
cache = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
if "hookSpecificOutput" not in out:
    raise SystemExit("Codex UserPromptSubmit must return hookSpecificOutput")
if out["hookSpecificOutput"].get("hookEventName") != "UserPromptSubmit":
    raise SystemExit("wrong hookEventName")
if cache.get("session_id") != "sess_p6_codex":
    raise SystemExit("prompt cache session mismatch")
print("[p6-codex] UserPromptSubmit OK")
PY

python3 - <<'PY' "${BINDING}"
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(
    json.dumps(
        {
            "agent_type": "codex",
            "session_id": "sess_p6_codex",
            "conversation_id": "sess_p6_codex",
            "task_id": "task_p6_codex",
        },
        ensure_ascii=False,
        indent=2,
    ),
    encoding="utf-8",
)
PY

echo "[p6-codex] PostToolUse envelope"
TOOL_JSON="$(python3 "${SHARED_BUILD}" \
  --mode codex-post-tool \
  --prompt-cache "${PROMPT_CACHE}" \
  --session-state "${BINDING}" <<'JSON'
{
  "session_id": "sess_p6_codex",
  "turn_id": "turn_p6_codex",
  "hook_event_name": "PostToolUse",
  "tool_name": "Bash",
  "tool_input": {"command": "go test ./internal/capture/..."},
  "tool_response": {"stdout": "ok github.com/zaneway/theone/internal/capture", "exit_code": 0}
}
JSON
)"

python3 - <<'PY' "${TOOL_JSON}"
import json
import sys

data = json.loads(sys.argv[1])
event = data["events"][0]
payload = event["payload"]
if data.get("producer") != "codex_hook:PostToolUse":
    raise SystemExit(f"producer mismatch: {data.get('producer')}")
if event["event_type"] != "tool.result.summary":
    raise SystemExit("expected tool.result.summary")
if "【事件】" not in payload.get("content_summary", ""):
    raise SystemExit("content_summary is not structured")
print("[p6-codex] PostToolUse envelope OK")
PY

echo "[p6-codex] Stop turn.completed envelope"
TURN_JSON="$(python3 "${SHARED_BUILD}" \
  --mode codex-stop \
  --prompt-cache "${PROMPT_CACHE}" \
  --session-state "${BINDING}" \
  --inject-cache "${INJECT_CACHE}" <<'JSON'
{
  "session_id": "sess_p6_codex",
  "turn_id": "turn_p6_codex",
  "hook_event_name": "Stop",
  "last_assistant_message": "已完成 Codex hook-first 记忆接入验证"
}
JSON
)"

python3 - <<'PY' "${TURN_JSON}"
import json
import sys

data = json.loads(sys.argv[1])
event = data["events"][0]
payload = event["payload"]
if data.get("producer") != "codex_hook:Stop":
    raise SystemExit(f"producer mismatch: {data.get('producer')}")
if event["kind"] != "turn.completed":
    raise SystemExit("expected turn.completed")
if "【结论" not in payload.get("agent_summary", ""):
    raise SystemExit("agent_summary is not structured with conclusion first")
print("[p6-codex] Stop envelope OK")
PY

echo "[p6-codex] PASS"
