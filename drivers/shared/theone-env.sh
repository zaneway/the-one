#!/usr/bin/env bash
# 由 Agent Hook 脚本 source；勿直接执行。
# 用法: source drivers/shared/theone-env.sh [agent_type]
# THEONE_DATA_DIR 可由 hooks.json / mcp.json 注入，优先级最高。

set -u

THEONE_AGENT_TYPE="${THEONE_AGENT_TYPE:-${1:-cursor}}"
export THEONE_AGENT_TYPE

SHARED_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRIVERS_DIR="$(cd "${SHARED_DIR}/.." && pwd)"
INSTALL_ROOT="$(cd "${DRIVERS_DIR}/.." && pwd)"
DATA_PARENT="$(basename "${INSTALL_ROOT}")"

if [[ "${DATA_PARENT}" == ".theone-data" ]]; then
  DATA_DIR="${INSTALL_ROOT}"
  ROOT_DIR="$(cd "${INSTALL_ROOT}/.." && pwd)"
  if [[ -f "${DATA_DIR}/theone-install.env" ]]; then
    # shellcheck source=/dev/null
    source "${DATA_DIR}/theone-install.env"
    THEONE_BIN="${THEONE_BIN:-${THEONE_PACKAGE_DIR}/bin/theone}"
    CONFIG_PATH="${THEONE_CONFIG:-${THEONE_PACKAGE_DIR}/theone.yaml}"
  else
    THEONE_BIN="${THEONE_BIN:-${DATA_DIR}/bin/theone}"
    CONFIG_PATH="${THEONE_CONFIG:-${DATA_DIR}/theone.yaml}"
  fi
elif [[ -z "${ROOT_DIR:-}" ]]; then
  ROOT_DIR="${INSTALL_ROOT}"
  DATA_DIR="${ROOT_DIR}/.theone-data"
  THEONE_BIN="${THEONE_BIN:-${ROOT_DIR}/bin/theone}"
  CONFIG_PATH="${THEONE_CONFIG:-${ROOT_DIR}/theone.yaml}"
else
  DATA_DIR="${ROOT_DIR}/.theone-data"
  THEONE_BIN="${THEONE_BIN:-${ROOT_DIR}/bin/theone}"
  CONFIG_PATH="${THEONE_CONFIG:-${ROOT_DIR}/theone.yaml}"
fi

if [[ -n "${THEONE_DATA_DIR:-}" ]]; then
  DATA_DIR="$(cd "${THEONE_DATA_DIR}" && pwd)"
fi

export ROOT_DIR DATA_DIR
STATE_DIR="${DATA_DIR}/runtime-state"
BUILD_INGEST_SCRIPT="${SHARED_DIR}/theone-build-ingest.py"

export STATE_DIR THEONE_BIN CONFIG_PATH SHARED_DIR BUILD_INGEST_SCRIPT

BINDING_FILE="${STATE_DIR}/binding.${THEONE_AGENT_TYPE}.json"
export BINDING_FILE

if [[ "${THEONE_AGENT_TYPE}" == "cursor" ]]; then
  PROMPT_CACHE_FILE="${STATE_DIR}/prompt-cache.json"
  INJECT_CACHE_FILE="${STATE_DIR}/inject-cache.json"
  SURFACE_FILE="${ROOT_DIR}/.cursor/rules/theone-injected-context.mdc"
elif [[ "${THEONE_AGENT_TYPE}" == "codex" ]]; then
  PROMPT_CACHE_FILE="${STATE_DIR}/prompt-cache.codex.json"
  INJECT_CACHE_FILE="${STATE_DIR}/inject-cache.codex.json"
  SURFACE_FILE="${ROOT_DIR}/.codex/theone-context.md"
else
  PROMPT_CACHE_FILE="${STATE_DIR}/prompt-cache.${THEONE_AGENT_TYPE}.json"
  INJECT_CACHE_FILE="${STATE_DIR}/inject-cache.${THEONE_AGENT_TYPE}.json"
  SURFACE_FILE="${ROOT_DIR}/.claude/theone-context.md"
fi
export PROMPT_CACHE_FILE INJECT_CACHE_FILE SURFACE_FILE

mkdir -p "${STATE_DIR}" "${ROOT_DIR}/.claude" "${ROOT_DIR}/.codex" 2>/dev/null || true
