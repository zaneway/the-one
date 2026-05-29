#!/usr/bin/env bash
set -u

# PostToolUseFailure：与成功路径相同，exit_code 由 normalize 从 tool_response / error 推断。
HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export ROOT_DIR="$(cd "${HOOK_DIR}/../../.." && pwd)"
exec "${HOOK_DIR}/theone-post-tool-use.sh"
