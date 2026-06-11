#!/usr/bin/env bash
set -u

_DRIVERS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${_DRIVERS_DIR}/shared/theone-env.sh" cursor
# shellcheck source=../../shared/theone-ingest.sh
source "${_DRIVERS_DIR}/shared/theone-ingest.sh"

[[ -x "${THEONE_BIN}" ]] || exit 0

INGEST_JSON="$(cat | python3 "${SHARED_DIR}/theone-hook-session.py" start --agent cursor 2>/dev/null || true)"
if [[ -n "${INGEST_JSON}" ]]; then
  run_theone_ingest "${INGEST_JSON}"
fi
exit 0
