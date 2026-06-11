#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRIVERS_DIR="$(cd "${HOOK_DIR}/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${DRIVERS_DIR}/shared/theone-env.sh" claude_code
# shellcheck source=../../shared/theone-ingest.sh
source "${DRIVERS_DIR}/shared/theone-ingest.sh"

[[ -x "${THEONE_BIN}" ]] || exit 0

INGEST_JSON="$(cat | python3 "${SHARED_DIR}/theone-hook-session.py" start --agent claude_code 2>/dev/null || true)"
run_theone_ingest "${INGEST_JSON}"
exit 0
