#!/usr/bin/env bash
set -u

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRIVERS_DIR="$(cd "${HOOK_DIR}/../.." && pwd)"
# shellcheck source=../../shared/theone-env.sh
source "${DRIVERS_DIR}/shared/theone-env.sh" codex

HOOK_PAYLOAD="$(cat || true)"
HOOK_OUTPUT='{"continue":true,"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":""}}'

PREFETCH_REQ="$(printf '%s' "${HOOK_PAYLOAD}" | python3 "${SHARED_DIR}/theone-hook-prefetch.py" prepare \
  --agent codex \
  --prompt-cache "${PROMPT_CACHE_FILE}" \
  --surface "${SURFACE_FILE}" \
  --config "${CONFIG_PATH}" 2>/dev/null || true)"

if [[ -x "${THEONE_BIN}" && -n "${PREFETCH_REQ}" ]]; then
  PREFETCH_OUT="$(
    printf '%s' "${PREFETCH_REQ}" | "${THEONE_BIN}" prefetch-context \
      -config "${CONFIG_PATH}" \
      -data-dir "${DATA_DIR}" 2>/dev/null || true
  )"
  if [[ -n "${PREFETCH_OUT}" ]]; then
    HOOK_OUTPUT="$(printf '%s' "${PREFETCH_OUT}" | python3 "${SHARED_DIR}/theone-hook-prefetch.py" format-response --agent codex 2>/dev/null || printf '%s' "${HOOK_OUTPUT}")"
  fi
fi

printf '%s\n' "${HOOK_OUTPUT}"
exit 0
