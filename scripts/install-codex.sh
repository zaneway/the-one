#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "${SCRIPT_DIR}/doc/codex/hooks.json" ]]; then
  PACKAGE_DIR="${SCRIPT_DIR}"
else
  PACKAGE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
fi

PROJECT_DIR="${PWD}"
FORCE=0

usage() {
  cat <<'EOF'
Usage:
  install-codex.sh [--project /path/to/project] [--force]

Installs The One Codex runtime into an existing project directory.
The user project does not need Go or this source repository.

MCP is configured via ~/.codex/config.toml; this script writes a rendered
fragment under .codex/theone-mcp.toml for manual merge.

Options:
  --project DIR  Target project used by Codex. Defaults to current directory.
  --force        Overwrite existing runtime files under the target project.
  -h, --help     Show this help.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)
      [[ $# -ge 2 ]] || { echo "[install-codex] --project requires a value" >&2; exit 2; }
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
      echo "[install-codex] unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

PROJECT_DIR="$(cd "${PROJECT_DIR}" && pwd)"

THEONE_BIN="${PACKAGE_DIR}/bin/theone"
HOOKS_TEMPLATE="${PACKAGE_DIR}/doc/codex/hooks.json"
MCP_TEMPLATE="${PACKAGE_DIR}/doc/codex/config.toml.template"
CONTEXT_TEMPLATE="${PACKAGE_DIR}/doc/codex/context/theone-context.md.template"
CONFIG_TEMPLATE="${PACKAGE_DIR}/theone.yaml"

for required in "${THEONE_BIN}" "${HOOKS_TEMPLATE}" "${MCP_TEMPLATE}" "${CONTEXT_TEMPLATE}" "${CONFIG_TEMPLATE}"; do
  if [[ ! -e "${required}" ]]; then
    echo "[install-codex] missing package file: ${required}" >&2
    exit 1
  fi
done

if [[ ! -x "${THEONE_BIN}" ]]; then
  echo "[install-codex] binary is not executable: ${THEONE_BIN}" >&2
  echo "[install-codex] use the official release package, or rebuild the package before installing" >&2
  exit 1
fi

DATA_DIR="${PROJECT_DIR}/.theone-data"
HOOKS_TARGET="${PROJECT_DIR}/.codex/hooks.json"
MCP_FRAGMENT="${PROJECT_DIR}/.codex/theone-mcp.toml"
SURFACE="${PROJECT_DIR}/.codex/theone-context.md"

mkdir -p \
  "${PROJECT_DIR}/.codex" \
  "${DATA_DIR}" \
  "${PROJECT_DIR}/bin" \
  "${PROJECT_DIR}/drivers/codex" \
  "${PROJECT_DIR}/drivers/shared"

copy_file() {
  local src="$1"
  local dst="$2"
  if [[ -e "${dst}" && "${FORCE}" != "1" ]]; then
    echo "[install-codex] keep existing ${dst} (use --force to overwrite)"
    return 0
  fi
  mkdir -p "$(dirname "${dst}")"
  cp "${src}" "${dst}"
}

copy_tree() {
  local src="$1"
  local dst="$2"
  if [[ -e "${dst}" && "${FORCE}" != "1" ]]; then
    echo "[install-codex] refresh ${dst} without deleting user files"
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
  echo "[install-codex] keep existing ${PROJECT_DIR}/theone.yaml (use --force to overwrite)"
fi
copy_tree "${PACKAGE_DIR}/drivers/codex" "${PROJECT_DIR}/drivers/codex"
copy_tree "${PACKAGE_DIR}/drivers/shared" "${PROJECT_DIR}/drivers/shared"

chmod +x "${PROJECT_DIR}/bin/theone" 2>/dev/null || true
chmod +x "${PROJECT_DIR}/drivers/codex/hooks/"*.sh 2>/dev/null || true
chmod +x "${PROJECT_DIR}/drivers/shared/"*.sh 2>/dev/null || true
chmod +x "${PROJECT_DIR}/drivers/shared/"*.py 2>/dev/null || true

if [[ ! -f "${HOOKS_TARGET}" ]]; then
  cp "${HOOKS_TEMPLATE}" "${HOOKS_TARGET}"
  echo "[install-codex] created ${HOOKS_TARGET}"
elif command -v jq >/dev/null 2>&1; then
  tmp="$(mktemp)"
  jq -s '
    (.[0].hooks // {}) as $old |
    (.[1].hooks // {}) as $new |
    .[0] * .[1] | .hooks = ($old * $new)
  ' "${HOOKS_TARGET}" "${HOOKS_TEMPLATE}" >"${tmp}"
  mv "${tmp}" "${HOOKS_TARGET}"
  echo "[install-codex] merged hooks into ${HOOKS_TARGET}"
else
  echo "[install-codex] ${HOOKS_TARGET} exists; install jq or merge hooks manually"
fi

sed "s|__THEONE_REPO__|${PROJECT_DIR}|g" "${MCP_TEMPLATE}" >"${MCP_FRAGMENT}"
echo "[install-codex] wrote MCP fragment: ${MCP_FRAGMENT}"
echo "[install-codex] merge it into ~/.codex/config.toml (see doc/codex/README.md)"

if [[ ! -f "${SURFACE}" ]]; then
  cp "${CONTEXT_TEMPLATE}" "${SURFACE}"
fi

"${PROJECT_DIR}/bin/theone" health -config "${PROJECT_DIR}/theone.yaml" -data-dir "${DATA_DIR}" >/dev/null

echo "[install-codex] installed The One Codex runtime"
echo "[install-codex] project: ${PROJECT_DIR}"
echo "[install-codex] data:    ${DATA_DIR}"
echo "[install-codex] next:    merge .codex/theone-mcp.toml into ~/.codex/config.toml, then restart Codex"
