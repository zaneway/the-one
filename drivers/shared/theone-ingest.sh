#!/usr/bin/env bash
# shellcheck source=theone-env.sh
# 依赖 THEONE_BIN / CONFIG_PATH / DATA_DIR

run_theone_ingest() {
  local json="${1:-}"
  if [[ -z "${json}" || ! -x "${THEONE_BIN}" ]]; then
    return 0
  fi
  echo "${json}" | "${THEONE_BIN}" ingest -config "${CONFIG_PATH}" -data-dir "${DATA_DIR}" >/dev/null 2>&1 || true
}
