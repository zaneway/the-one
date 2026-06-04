#!/usr/bin/env bash
# 由 Agent Hook 脚本 source；勿直接执行。
# 用法: export ROOT_DIR=...; source drivers/shared/theone-env.sh [agent_type]

set -u

THEONE_AGENT_TYPE="${THEONE_AGENT_TYPE:-${1:-cursor}}"
export THEONE_AGENT_TYPE

if [[ -z "${ROOT_DIR:-}" ]]; then
  ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
export ROOT_DIR

DATA_DIR="${THEONE_DATA_DIR:-${ROOT_DIR}/.theone-data}"
STATE_DIR="${DATA_DIR}/runtime-state"
THEONE_BIN="${THEONE_BIN:-${ROOT_DIR}/bin/theone}"
CONFIG_PATH="${THEONE_CONFIG:-${ROOT_DIR}/theone.yaml}"
SHARED_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_INGEST_SCRIPT="${SHARED_DIR}/theone-build-ingest.py"

export DATA_DIR STATE_DIR THEONE_BIN CONFIG_PATH SHARED_DIR BUILD_INGEST_SCRIPT

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
