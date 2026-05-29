#!/usr/bin/env bash
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)/drivers/claude_code/hooks/theone-post-tool-use-failure.sh" "$@"
