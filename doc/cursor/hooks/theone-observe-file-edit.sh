#!/usr/bin/env bash
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)/drivers/cursor/hooks/theone-observe-file-edit.sh" "$@"
