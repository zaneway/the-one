#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "${SCRIPT_DIR}/doc/cursor/mcp.json" ]]; then
  PACKAGE_DIR="${SCRIPT_DIR}"
else
  PACKAGE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
fi

PROJECT_DIR="${PWD}"
FORCE=0

usage() {
  cat <<'EOF'
Usage:
  install-cursor.sh [--project /path/to/project] [--force]

Installs The One Cursor runtime into an existing project directory.
The user project does not need Go or this source repository.

Options:
  --project DIR  Target project opened by Cursor. Defaults to current directory.
  --force        Overwrite existing runtime files under the target project.
  -h, --help     Show this help.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)
      [[ $# -ge 2 ]] || { echo "[install-cursor] --project requires a value" >&2; exit 2; }
      PROJECT_DIR="$2"
      shift 2
      ;;
    --force)
      FORCE=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "[install-cursor] unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

PROJECT_DIR="$(cd "${PROJECT_DIR}" && pwd)"

THEONE_BIN="${PACKAGE_DIR}/bin/theone"
MCP_TEMPLATE="${PACKAGE_DIR}/doc/cursor/mcp.json"
HOOKS_TEMPLATE="${PACKAGE_DIR}/doc/cursor/hooks.json"
RULE_TEMPLATE="${PACKAGE_DIR}/doc/cursor/rule/theone-memory-observe.mdc"
CONTEXT_TEMPLATE="${PACKAGE_DIR}/doc/cursor/context/theone-injected-context.mdc.template"
CONFIG_TEMPLATE="${PACKAGE_DIR}/theone.yaml"

for required in "${THEONE_BIN}" "${MCP_TEMPLATE}" "${HOOKS_TEMPLATE}" "${RULE_TEMPLATE}" "${CONTEXT_TEMPLATE}" "${CONFIG_TEMPLATE}"; do
  if [[ ! -e "${required}" ]]; then
    echo "[install-cursor] missing package file: ${required}" >&2
    exit 1
  fi
done

if [[ ! -x "${THEONE_BIN}" ]]; then
  echo "[install-cursor] binary is not executable: ${THEONE_BIN}" >&2
  echo "[install-cursor] use the official release package, or rebuild the package before installing" >&2
  exit 1
fi

INSTALL_ROOT="${PROJECT_DIR}"
DATA_DIR="${PROJECT_DIR}/.theone-data"
MCP_TARGET="${PROJECT_DIR}/.cursor/mcp.json"
HOOKS_TARGET="${PROJECT_DIR}/.cursor/hooks.json"
RULE_TARGET="${PROJECT_DIR}/.cursor/rules/theone-memory-observe.mdc"
SURFACE="${PROJECT_DIR}/.cursor/rules/theone-injected-context.mdc"

mkdir -p \
  "${PROJECT_DIR}/.cursor/rules" \
  "${DATA_DIR}" \
  "${PROJECT_DIR}/bin" \
  "${PROJECT_DIR}/drivers/cursor" \
  "${PROJECT_DIR}/drivers/shared"

copy_file() {
  local src="$1"
  local dst="$2"
  if [[ -e "${dst}" && "${FORCE}" != "1" ]]; then
    echo "[install-cursor] keep existing ${dst} (use --force to overwrite)"
    return 0
  fi
  mkdir -p "$(dirname "${dst}")"
  cp "${src}" "${dst}"
}

copy_tree() {
  local src="$1"
  local dst="$2"
  if [[ -e "${dst}" && "${FORCE}" != "1" ]]; then
    echo "[install-cursor] refresh ${dst} without deleting user files"
  fi
  mkdir -p "${dst}"
  cp -R "${src}/." "${dst}/"
}

copy_file "${THEONE_BIN}" "${PROJECT_DIR}/bin/theone"
if [[ ! -e "${PROJECT_DIR}/theone.yaml" || "${FORCE}" == "1" ]]; then
  python3 - <<'PY' "${CONFIG_TEMPLATE}" "${PROJECT_DIR}/theone.yaml"
import pathlib
import sys

src, dst = sys.argv[1], sys.argv[2]
lines = pathlib.Path(src).read_text(encoding="utf-8").splitlines()
out = []
in_processor = False
in_openai = False
for line in lines:
    stripped = line.strip()
    if line and not line.startswith((" ", "\t")):
        in_processor = stripped == "processor:"
        in_openai = False
    if in_processor and stripped == "openai:":
        in_openai = True
    if in_processor and in_openai and stripped.startswith("api_key:"):
        indent = line[: len(line) - len(line.lstrip())]
        out.append(f'{indent}api_key: ""')
        continue
    out.append(line)
pathlib.Path(dst).write_text("\n".join(out) + "\n", encoding="utf-8")
PY
else
  echo "[install-cursor] keep existing ${PROJECT_DIR}/theone.yaml (use --force to overwrite)"
fi
copy_tree "${PACKAGE_DIR}/drivers/cursor" "${PROJECT_DIR}/drivers/cursor"
copy_tree "${PACKAGE_DIR}/drivers/shared" "${PROJECT_DIR}/drivers/shared"

chmod +x "${PROJECT_DIR}/bin/theone" 2>/dev/null || true
chmod +x "${PROJECT_DIR}/drivers/cursor/entry.sh" 2>/dev/null || true
chmod +x "${PROJECT_DIR}/drivers/cursor/hooks/"*.sh 2>/dev/null || true
chmod +x "${PROJECT_DIR}/drivers/shared/"*.sh 2>/dev/null || true
chmod +x "${PROJECT_DIR}/drivers/shared/"*.py 2>/dev/null || true

MCP_RENDERED="$(python3 - <<'PY' "${MCP_TEMPLATE}" "${INSTALL_ROOT}"
import json
import pathlib
import sys

template_path, install_root = sys.argv[1], sys.argv[2]
text = pathlib.Path(template_path).read_text(encoding="utf-8").replace("__THEONE_REPO__", install_root)
json.loads(text)
print(text, end="")
PY
)"

if [[ ! -f "${MCP_TARGET}" ]]; then
  printf '%s' "${MCP_RENDERED}" >"${MCP_TARGET}"
  echo "[install-cursor] created ${MCP_TARGET}"
elif command -v jq >/dev/null 2>&1; then
  tmp="$(mktemp)"
  jq -s '
    (.[0].mcpServers // {}) as $old |
    (.[1].mcpServers // {}) as $new |
    .[0] * .[1] | .mcpServers = ($old * $new)
  ' "${MCP_TARGET}" <(printf '%s' "${MCP_RENDERED}") >"${tmp}"
  mv "${tmp}" "${MCP_TARGET}"
  echo "[install-cursor] merged theone into ${MCP_TARGET}"
else
  echo "[install-cursor] ${MCP_TARGET} exists; install jq or merge theone MCP block manually"
fi

if [[ ! -f "${HOOKS_TARGET}" ]]; then
  cp "${HOOKS_TEMPLATE}" "${HOOKS_TARGET}"
  echo "[install-cursor] created ${HOOKS_TARGET}"
elif command -v jq >/dev/null 2>&1; then
  tmp="$(mktemp)"
  jq -s '
    (.[0].hooks // {}) as $old |
    (.[1].hooks // {}) as $new |
    .[0] * .[1] | .hooks = ($old * $new)
  ' "${HOOKS_TARGET}" "${HOOKS_TEMPLATE}" >"${tmp}"
  mv "${tmp}" "${HOOKS_TARGET}"
  echo "[install-cursor] merged hooks into ${HOOKS_TARGET}"
else
  echo "[install-cursor] ${HOOKS_TARGET} exists; install jq or merge hooks manually"
fi

cp "${RULE_TEMPLATE}" "${RULE_TARGET}"
if [[ ! -f "${SURFACE}" ]]; then
  cp "${CONTEXT_TEMPLATE}" "${SURFACE}"
fi

"${PROJECT_DIR}/bin/theone" health -config "${PROJECT_DIR}/theone.yaml" -data-dir "${DATA_DIR}" >/dev/null

echo "[install-cursor] installed The One Cursor runtime"
echo "[install-cursor] project: ${PROJECT_DIR}"
echo "[install-cursor] data:    ${DATA_DIR}"
echo "[install-cursor] next:    Reload Cursor, then check Settings -> MCP -> theone"
