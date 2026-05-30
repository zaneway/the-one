#!/usr/bin/env bash
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)/drivers/cursor/hooks/theone-session-start.sh" "$@"
