#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRIVERS_DIR="$(cd "${HOOK_DIR}/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${DRIVERS_DIR}/shared/theone-env.sh" claude_code
# shellcheck source=../../shared/theone-ingest.sh
source "${DRIVERS_DIR}/shared/theone-ingest.sh"

[[ -x "${THEONE_BIN}" ]] || exit 0

END_JSON="$(cat | python3 "${SHARED_DIR}/theone-hook-session.py" end \
  --agent claude_code \
  --binding "${BINDING_FILE}" 2>/dev/null || true)"
run_theone_ingest "${END_JSON}"

python3 "${SHARED_DIR}/theone-hook-session.py" cleanup-runtime \
  --agent claude_code \
  --state-dir "${STATE_DIR}" \
  --binding "${BINDING_FILE}" \
  --surface "${SURFACE_FILE}" >/dev/null 2>&1 || true

exit 0
