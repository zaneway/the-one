#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export ROOT_DIR="$(cd "${HOOK_DIR}/../../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${ROOT_DIR}/drivers/shared/theone-env.sh" claude_code
# shellcheck source=../../shared/theone-ingest.sh
source "${ROOT_DIR}/drivers/shared/theone-ingest.sh"

if [[ ! -x "${THEONE_BIN}" || ! -f "${BUILD_INGEST_SCRIPT}" ]]; then
  exit 0
fi

HOOK_PAYLOAD="$(cat || true)"
HOOK_TMP="$(mktemp "${STATE_DIR}/hook-payload.XXXXXX")"
printf '%s' "${HOOK_PAYLOAD}" >"${HOOK_TMP}"

export THEONE_AGENT_TYPE
INGEST_JSON="$(python3 "${BUILD_INGEST_SCRIPT}" \
  --mode claude-post-tool \
  --hook-stdin-file "${HOOK_TMP}" \
  --prompt-cache "${PROMPT_CACHE_FILE}" \
  --session-state "${BINDING_FILE}" 2>/dev/null || true)"
rm -f "${HOOK_TMP}" >/dev/null 2>&1 || true

run_theone_ingest "${INGEST_JSON}"
exit 0
