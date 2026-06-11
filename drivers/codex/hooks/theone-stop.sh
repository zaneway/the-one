#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRIVERS_DIR="$(cd "${HOOK_DIR}/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${DRIVERS_DIR}/shared/theone-env.sh" codex
# shellcheck source=../../shared/theone-ingest.sh
source "${DRIVERS_DIR}/shared/theone-ingest.sh"

if [[ ! -f "${BUILD_INGEST_SCRIPT}" ]]; then
  printf '{"continue":true}\n'
  exit 0
fi

HOOK_PAYLOAD="$(cat || true)"
HOOK_TMP="$(mktemp "${STATE_DIR}/hook-payload.XXXXXX")"
printf '%s' "${HOOK_PAYLOAD}" >"${HOOK_TMP}"

export THEONE_AGENT_TYPE=codex
INGEST_JSON="$(python3 "${BUILD_INGEST_SCRIPT}" \
  --mode codex-stop \
  --hook-stdin-file "${HOOK_TMP}" \
  --prompt-cache "${PROMPT_CACHE_FILE}" \
  --session-state "${BINDING_FILE}" \
  --inject-cache "${INJECT_CACHE_FILE}" 2>/dev/null || true)"
rm -f "${HOOK_TMP}" >/dev/null 2>&1 || true

run_theone_ingest "${INGEST_JSON}"
printf '{"continue":true}\n'
exit 0
