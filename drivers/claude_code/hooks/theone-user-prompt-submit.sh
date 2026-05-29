#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export ROOT_DIR="$(cd "${HOOK_DIR}/../../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${ROOT_DIR}/drivers/shared/theone-env.sh" claude_code

mkdir -p "${STATE_DIR}" "${ROOT_DIR}/.claude"

HOOK_OUTPUT='{}'

if [[ -x "${THEONE_BIN}" ]]; then
  PREFETCH_REQ="$(cat | python3 "${SHARED_DIR}/theone-hook-prefetch.py" prepare \
    --agent claude_code \
    --prompt-cache "${PROMPT_CACHE_FILE}" \
    --surface "${SURFACE_FILE}" 2>/dev/null || true)"
  if [[ -n "${PREFETCH_REQ}" ]]; then
    PREFETCH_OUT="$(
      echo "${PREFETCH_REQ}" | "${THEONE_BIN}" prefetch-context \
        -config "${CONFIG_PATH}" \
        -data-dir "${DATA_DIR}" 2>/dev/null || true
    )"
    if [[ -n "${PREFETCH_OUT}" ]]; then
      HOOK_OUTPUT="$(printf '%s' "${PREFETCH_OUT}" | python3 "${SHARED_DIR}/theone-hook-prefetch.py" format-response --agent claude_code 2>/dev/null || echo '{}')"
    fi
  fi
fi

printf '%s\n' "${HOOK_OUTPUT}"
exit 0
