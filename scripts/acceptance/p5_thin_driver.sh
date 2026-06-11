#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

fail() {
  echo "[p5-thin-driver] FAIL: $*" >&2
  exit 1
}

echo "[p5-thin-driver] no inline python heredocs in thin hook dirs"
for dir in drivers/cursor/hooks drivers/claude_code/hooks .cursor/hooks doc/cursor/hooks; do
  if [[ -d "${dir}" ]]; then
    while IFS= read -r -d '' shfile; do
      if grep -qF "python3 - <<'PY'" "${shfile}" 2>/dev/null; then
        fail "inline PY in ${shfile}"
      fi
    done < <(find "${dir}" -maxdepth 1 -name '*.sh' -print0 2>/dev/null)
  fi
done

echo "[p5-thin-driver] hooks.json uses drivers/cursor/entry.sh"
if ! grep -q 'drivers/cursor/entry.sh' .cursor/hooks.json; then
  fail ".cursor/hooks.json must reference drivers/cursor/entry.sh"
fi

echo "[p5-thin-driver] stop must not trigger session.end cleanup"
if grep -E '"stop".*sessionEnd|entry\.sh stop' .cursor/hooks.json >/dev/null 2>&1; then
  if grep -q 'sessionEnd' .cursor/hooks.json && grep -A2 '"stop"' .cursor/hooks.json | grep -q 'sessionEnd'; then
    fail "stop hook must not point to sessionEnd"
  fi
fi
if grep -q 'sessionEnd | stop' drivers/cursor/entry.sh 2>/dev/null; then
  fail "drivers/cursor/entry.sh must not map stop to session-end"
fi

echo "[p5-thin-driver] v1 scripts removed from .cursor/hooks"
for gone in theone-build-turn.py theone-inject-context.py theone-runtime-lib.py; do
  if [[ -f ".cursor/hooks/${gone}" ]]; then
    fail ".cursor/hooks/${gone} should be archived/removed"
  fi
done

echo "[p5-thin-driver] shared hook-session start envelope"
OUT="$(echo '{"conversation_id":"conv_p5","session_id":"conv_p5"}' | python3 drivers/shared/theone-hook-session.py start --agent cursor)"
python3 - <<'PY' "${OUT}"
import json, sys
data = json.loads(sys.argv[1])
assert data["agent_type"] == "cursor"
assert data["events"][0]["kind"] == "session.lifecycle"
print("[p5-thin-driver] session.start envelope OK")
PY

echo "[p5-thin-driver] cursor entry.sh dispatches"
[[ -x drivers/cursor/entry.sh ]] || chmod +x drivers/cursor/entry.sh drivers/cursor/hooks/*.sh drivers/shared/*.sh 2>/dev/null || true
echo '{"conversation_id":"conv_p5_entry"}' | drivers/cursor/entry.sh sessionStart >/dev/null || true

echo "[p5-thin-driver] PASS"
