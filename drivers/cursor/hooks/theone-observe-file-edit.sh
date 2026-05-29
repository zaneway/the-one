#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${ROOT_DIR}/drivers/shared/theone-env.sh" cursor
# shellcheck source=../../shared/theone-ingest.sh
source "${ROOT_DIR}/drivers/shared/theone-ingest.sh"

[[ -x "${THEONE_BIN}" && -f "${BUILD_INGEST_SCRIPT}" ]] || exit 0

HOOK_TMP="$(mktemp "${STATE_DIR}/hook-payload.XXXXXX")"
cat >"${HOOK_TMP}" || true

export THEONE_AGENT_TYPE=cursor
INGEST_JSON="$(python3 "${BUILD_INGEST_SCRIPT}" \
  --mode atomic-file \
  --hook-stdin-file "${HOOK_TMP}" \
  --prompt-cache "${PROMPT_CACHE_FILE}" \
  --session-state "${BINDING_FILE}" 2>/dev/null || true)"
rm -f "${HOOK_TMP}" >/dev/null 2>&1 || true

run_theone_ingest "${INGEST_JSON}"
exit 0
