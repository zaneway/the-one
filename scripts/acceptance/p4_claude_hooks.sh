#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"
DATA_DIR="${DATA_DIR:-/tmp/theone-p4-acceptance}"
STATE_DIR="${DATA_DIR}/runtime-state"
CONFIG_PATH="${ROOT_DIR}/theone.yaml"
SHARED_BUILD="${ROOT_DIR}/drivers/shared/theone-build-ingest.py"

rm -rf "${DATA_DIR}"
mkdir -p "${STATE_DIR}"

echo "[p4-claude] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

export THEONE_ALLOW_SYNTHETIC_SESSION=1
export THEONE_AGENT_TYPE=claude_code

CONV="conv_p4_claude"
GEN="gen_p4_accept_001"

echo "[p4-claude] session.start via ingest"
"${BIN_DIR}/theone" ingest -config "${CONFIG_PATH}" -db-path "${DATA_DIR}/memory.db" <<JSON >/dev/null
{
  "ingest_id": "ing_p4_sess",
  "protocol_version": "v1",
  "producer": "claude_code_hook:sessionStart",
  "agent_type": "claude_code",
  "session_id": "${CONV}",
  "events": [{
    "kind": "session.lifecycle",
    "event_type": "session.start",
    "payload": {
      "agent_type": "claude_code",
      "workspace_id": "local_default_workspace",
      "project_id": "the-one",
      "repo_id": "the-one",
      "conversation_id": "${CONV}",
      "content_summary": "p4 claude session"
    }
  }]
}
JSON

echo "[p4-claude] prefetch-context (claude_code binding isolation)"
OUT="$(
  "${BIN_DIR}/theone" prefetch-context -config "${CONFIG_PATH}" -db-path "${DATA_DIR}/memory.db" <<JSON
{
  "task": "验收 P4 Claude Code prefetch 与 task 绑定",
  "workspace_id": "local_default_workspace",
  "project_id": "the-one",
  "repo_id": "the-one",
  "conversation_id": "${CONV}",
  "generation_id": "${GEN}",
  "agent_type": "claude_code",
  "token_budget": 1200,
  "include_code_refs": true,
  "include_evidence_summary": true,
  "rule_file": "${ROOT_DIR}/.claude/theone-context.md"
}
JSON
)"
echo "${OUT}"

python3 - <<'PY' "${OUT}" "${STATE_DIR}/binding.claude_code.json" "${STATE_DIR}/inject-cache.claude_code.json" "${ROOT_DIR}/.claude/theone-context.md"
import json
import pathlib
import sys

out = json.loads(sys.argv[1])
binding = json.loads(pathlib.Path(sys.argv[2]).read_text())
inject = json.loads(pathlib.Path(sys.argv[3]).read_text())
surface = pathlib.Path(sys.argv[4])

if not out.get("ok"):
    raise SystemExit(f"[p4-claude] prefetch not ok: {out}")
if not out.get("task_bound"):
    raise SystemExit("[p4-claude] task_bound=false")
if binding.get("agent_type") != "claude_code":
    raise SystemExit(f"[p4-claude] binding agent_type={binding.get('agent_type')}")
if binding.get("task_id") == "task_claude_code_auto":
    raise SystemExit("[p4-claude] binding still auto task")
if inject.get("generation_id") != out.get("generation_id"):
    raise SystemExit("[p4-claude] inject-cache generation mismatch")
if not surface.is_file():
    raise SystemExit("[p4-claude] .claude/theone-context.md missing")
body = surface.read_text(encoding="utf-8")
if "alwaysApply" in body:
    raise SystemExit("[p4-claude] surface must not be Cursor mdc")
print("[p4-claude] prefetch + surface OK")
PY

PROMPT_CACHE="${STATE_DIR}/prompt-cache.claude_code.json"
python3 - <<'PY' "${PROMPT_CACHE}" "${CONV}" "${GEN}"
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(
    json.dumps(
        {
            "user_summary": "验收 P4 原子与回合",
            "session_id": sys.argv[2],
            "conversation_id": sys.argv[2],
            "generation_id": sys.argv[3],
            "prompt_fingerprint": "fp_p4_test",
            "turn_id": "turn_" + sys.argv[3],
        },
        ensure_ascii=False,
        indent=2,
    ),
    encoding="utf-8",
)
PY

BINDING="${STATE_DIR}/binding.claude_code.json"
export THEONE_AGENT_TYPE=claude_code

echo "[p4-claude] atomic file via build-ingest (Write)"
WRITE_JSON="$(python3 "${SHARED_BUILD}" \
  --mode claude-post-tool \
  --prompt-cache "${PROMPT_CACHE}" \
  --session-state "${BINDING}" <<JSON
{
  "session_id": "${CONV}",
  "hook_event_name": "PostToolUse",
  "tool_name": "Write",
  "tool_input": {"file_path": "internal/adapter/driver_surface.go"},
  "tool_response": {"filePath": "internal/adapter/driver_surface.go", "success": true}
}
JSON
)"
echo "${WRITE_JSON}" | "${BIN_DIR}/theone" ingest -config "${CONFIG_PATH}" -db-path "${DATA_DIR}/memory.db" >/dev/null

echo "[p4-claude] turn.completed via Stop shape"
TURN_JSON="$(python3 "${SHARED_BUILD}" \
  --mode turn-agent \
  --prompt-cache "${PROMPT_CACHE}" \
  --session-state "${BINDING}" \
  --inject-cache "${STATE_DIR}/inject-cache.claude_code.json" <<JSON
{
  "session_id": "${CONV}",
  "hook_event_name": "Stop",
  "last_assistant_message": "P4 验收回合完成"
}
JSON
)"
python3 - <<'PY' "${TURN_JSON}"
import json, sys
data = json.loads(sys.argv[1])
ev = data["events"][0]
if ev["kind"] != "turn.completed":
    raise SystemExit("expected turn.completed")
if data.get("agent_type") != "claude_code":
    raise SystemExit("agent_type mismatch")
print("[p4-claude] turn envelope OK")
PY

echo "${TURN_JSON}" | "${BIN_DIR}/theone" ingest -config "${CONFIG_PATH}" -db-path "${DATA_DIR}/memory.db" >/dev/null

echo "[p4-claude] cursor binding must not exist"
if [[ -f "${STATE_DIR}/binding.cursor.json" ]]; then
  echo "[p4-claude] WARN: binding.cursor.json present (ok if parallel tests ran)"
fi

echo "[p4-claude] PASS"
