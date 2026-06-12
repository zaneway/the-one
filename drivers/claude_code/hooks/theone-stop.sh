#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRIVERS_DIR="$(cd "${HOOK_DIR}/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${DRIVERS_DIR}/shared/theone-env.sh" claude_code
# shellcheck source=../../shared/theone-ingest.sh
source "${DRIVERS_DIR}/shared/theone-ingest.sh"

if [[ ! -x "${THEONE_BIN}" || ! -f "${BUILD_INGEST_SCRIPT}" ]]; then
  exit 0
fi

HOOK_PAYLOAD="$(cat || true)"
HOOK_TMP="$(mktemp "${STATE_DIR}/hook-payload.XXXXXX")"
BUILD_ERR="$(mktemp "${STATE_DIR}/build-ingest-stderr.XXXXXX")"
printf '%s' "${HOOK_PAYLOAD}" >"${HOOK_TMP}"

export THEONE_AGENT_TYPE
INGEST_JSON="$(python3 "${BUILD_INGEST_SCRIPT}" \
  --mode turn-agent \
  --hook-stdin-file "${HOOK_TMP}" \
  --prompt-cache "${PROMPT_CACHE_FILE}" \
  --session-state "${BINDING_FILE}" \
  --inject-cache "${INJECT_CACHE_FILE}" 2>"${BUILD_ERR}" || true)"
if [[ -s "${BUILD_ERR}" ]]; then
  log_theone_hook_error "build ingest failed hook=Stop agent_type=${THEONE_AGENT_TYPE}" "${BUILD_ERR}"
fi
rm -f "${HOOK_TMP}" >/dev/null 2>&1 || true
rm -f "${BUILD_ERR}" >/dev/null 2>&1 || true

run_theone_ingest "${INGEST_JSON}"
exit 0
