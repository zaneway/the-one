#!/usr/bin/env bash
# Cursor 统一 Hook 入口（P5）：hooks.json 传入事件名，转调 drivers/cursor/hooks/*.sh
set -u

HOOK_EVENT="${1:-${CURSOR_HOOK_EVENT:-}}"
if [[ -z "${HOOK_EVENT}" ]]; then
  echo "theone cursor entry: missing hook event (arg1 or CURSOR_HOOK_EVENT)" >&2
  exit 0
fi
shift || true

ENTRY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOKS_DIR="${ENTRY_DIR}/hooks"

case "${HOOK_EVENT}" in
  sessionStart) exec "${HOOKS_DIR}/theone-session-start.sh" "$@" ;;
  beforeSubmitPrompt) exec "${HOOKS_DIR}/theone-before-submit-prompt.sh" "$@" ;;
  afterAgentResponse) exec "${HOOKS_DIR}/theone-observe-turn.sh" "$@" ;;
  afterFileEdit) exec "${HOOKS_DIR}/theone-observe-file-edit.sh" "$@" ;;
  afterMCPExecution) exec "${HOOKS_DIR}/theone-observe-tool-result.sh" "$@" ;;
  sessionEnd) exec "${HOOKS_DIR}/theone-session-end.sh" "$@" ;;
  stop) exec "${HOOKS_DIR}/theone-stop.sh" "$@" ;;
  *)
    echo "theone cursor entry: unknown event ${HOOK_EVENT}" >&2
    exit 0
    ;;
esac
