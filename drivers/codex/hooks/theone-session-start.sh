#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRIVERS_DIR="$(cd "${HOOK_DIR}/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${DRIVERS_DIR}/shared/theone-env.sh" codex
# shellcheck source=../../shared/theone-ingest.sh
source "${DRIVERS_DIR}/shared/theone-ingest.sh"

HOOK_PAYLOAD="$(cat || true)"
if [[ -x "${THEONE_BIN}" ]]; then
  INGEST_JSON="$(printf '%s' "${HOOK_PAYLOAD}" | python3 "${SHARED_DIR}/theone-hook-session.py" start \
    --agent codex 2>/dev/null || true)"
  run_theone_ingest "${INGEST_JSON}"
fi

printf '{"continue":true}\n'
exit 0
