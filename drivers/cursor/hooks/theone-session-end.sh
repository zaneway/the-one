#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${ROOT_DIR}/drivers/shared/theone-env.sh" cursor
# shellcheck source=../../shared/theone-ingest.sh
source "${ROOT_DIR}/drivers/shared/theone-ingest.sh"

[[ -x "${THEONE_BIN}" ]] || exit 0

END_JSON="$(cat | python3 "${SHARED_DIR}/theone-hook-session.py" end \
  --agent cursor \
  --binding "${BINDING_FILE}" 2>/dev/null || true)"
run_theone_ingest "${END_JSON}"

python3 "${SHARED_DIR}/theone-hook-session.py" cleanup-runtime \
  --agent cursor \
  --state-dir "${STATE_DIR}" \
  --binding "${BINDING_FILE}" \
  --surface "${SURFACE_FILE}" >/dev/null 2>&1 || true

exit 0
