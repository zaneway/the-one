#!/usr/bin/env bash
# shellcheck source=theone-env.sh
# 依赖 THEONE_BIN / CONFIG_PATH / DATA_DIR

log_theone_hook_error() {
  local message="${1:-hook error}"
  local detail_file="${2:-}"
  local log_dir="${THEONE_HOOK_LOG_DIR:-${DATA_DIR}/logs/${THEONE_AGENT_TYPE:-agent}}"
  local log_file="${THEONE_HOOK_LOG_PATH:-${log_dir}/hook.log}"
  mkdir -p "${log_dir}" 2>/dev/null || true
  {
    printf '%s ' "$(date -Iseconds 2>/dev/null || date)"
    printf '%s' "${message}"
    if [[ -n "${detail_file}" && -s "${detail_file}" ]]; then
      printf ' detail='
      tr '\n' ' ' <"${detail_file}" | head -c 2000
    fi
    printf '\n'
  } >>"${log_file}" 2>/dev/null || true
}

run_theone_ingest() {
  local json="${1:-}"
  if [[ -z "${json}" || ! -x "${THEONE_BIN}" ]]; then
    return 0
  fi
  local err_file
  err_file="$(mktemp "${STATE_DIR}/ingest-stderr.XXXXXX" 2>/dev/null || mktemp "/tmp/theone-ingest-stderr.XXXXXX")"
  if ! printf '%s' "${json}" | "${THEONE_BIN}" ingest -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" >/dev/null 2>"${err_file}"; then
    log_theone_hook_error "theone ingest failed agent_type=${THEONE_AGENT_TYPE:-unknown} config=${CONFIG_PATH} data_dir=${DATA_DIR} json_chars=${#json} stderr=" "${err_file}"
    rm -f "${err_file}" >/dev/null 2>&1 || true
    return 1
  fi
  rm -f "${err_file}" >/dev/null 2>&1 || true
  return 0
}
