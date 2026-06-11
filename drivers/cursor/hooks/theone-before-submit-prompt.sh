#!/usr/bin/env bash
set -u

_DRIVERS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${_DRIVERS_DIR}/shared/theone-env.sh" cursor

mkdir -p "${STATE_DIR}"

HOOK_OUTPUT='{"continue": true}'

if [[ -x "${THEONE_BIN}" ]]; then
  PREFETCH_REQ="$(cat | python3 "${SHARED_DIR}/theone-hook-prefetch.py" prepare \
    --agent cursor \
    --prompt-cache "${PROMPT_CACHE_FILE}" \
    --surface "${SURFACE_FILE}" \
    --config "${CONFIG_PATH}" 2>/dev/null || true)"
  if [[ -n "${PREFETCH_REQ}" ]]; then
    PREFETCH_OUT="$(
      echo "${PREFETCH_REQ}" | "${THEONE_BIN}" prefetch-context \
        -config "${CONFIG_PATH}" \
        -data-dir "${DATA_DIR}" 2>/dev/null || true
    )"
    if [[ -n "${PREFETCH_OUT}" ]]; then
      HOOK_OUTPUT="$(printf '%s' "${PREFETCH_OUT}" | python3 "${SHARED_DIR}/theone-hook-prefetch.py" format-response --agent cursor 2>/dev/null || echo '{"continue": true}')"
    fi
  fi
fi

printf '%s\n' "${HOOK_OUTPUT}"
exit 0
