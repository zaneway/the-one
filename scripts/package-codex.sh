#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
VERSION="${VERSION:-dev}"
OS_NAME="$(go env GOOS)"
ARCH_NAME="$(go env GOARCH)"
PACKAGE_NAME="theone-codex-${VERSION}-${OS_NAME}-${ARCH_NAME}"
DIST_DIR="${ROOT_DIR}/dist"
PACKAGE_DIR="${DIST_DIR}/${PACKAGE_NAME}"

rm -rf "${PACKAGE_DIR}"
mkdir -p \
  "${PACKAGE_DIR}/bin" \
  "${PACKAGE_DIR}/doc" \
  "${PACKAGE_DIR}/drivers" \
  "${PACKAGE_DIR}/scripts"

go build -tags "${GO_TAGS}" -o "${PACKAGE_DIR}/bin/theone" ./cmd/theone

python3 - <<'PY' theone.yaml "${PACKAGE_DIR}/theone.yaml"
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
    if in_processor and stripped.startswith("provider:"):
        indent = line[: len(line) - len(line.lstrip())]
        out.append(f'{indent}provider: "rule_based"')
        continue
    if in_processor and in_openai and stripped.startswith("api_key:"):
        indent = line[: len(line) - len(line.lstrip())]
        out.append(f'{indent}api_key: ""')
        continue
    out.append(line)
pathlib.Path(dst).write_text("\n".join(out) + "\n", encoding="utf-8")
PY
cp scripts/install-codex.sh "${PACKAGE_DIR}/install-codex.sh"
cp scripts/install-codex.sh "${PACKAGE_DIR}/scripts/install-codex.sh"
cp -R doc/codex "${PACKAGE_DIR}/doc/codex"
mkdir -p "${PACKAGE_DIR}/doc/shared"
cp doc/shared/content-summary-structured.md "${PACKAGE_DIR}/doc/shared/content-summary-structured.md"
cp -R drivers/codex "${PACKAGE_DIR}/drivers/codex"
cp -R drivers/shared "${PACKAGE_DIR}/drivers/shared"

find "${PACKAGE_DIR}" -name ".DS_Store" -delete
find "${PACKAGE_DIR}/drivers" -name "*_test.py" -delete
chmod +x "${PACKAGE_DIR}/install-codex.sh" "${PACKAGE_DIR}/scripts/install-codex.sh"
chmod +x "${PACKAGE_DIR}/bin/theone"
chmod +x "${PACKAGE_DIR}/drivers/codex/hooks/"*.sh 2>/dev/null || true
chmod +x "${PACKAGE_DIR}/drivers/shared/"*.sh 2>/dev/null || true
chmod +x "${PACKAGE_DIR}/drivers/shared/"*.py 2>/dev/null || true

(
  cd "${DIST_DIR}"
  tar -czf "${PACKAGE_NAME}.tar.gz" "${PACKAGE_NAME}"
)

echo "[package-codex] package: ${PACKAGE_DIR}"
echo "[package-codex] archive: ${DIST_DIR}/${PACKAGE_NAME}.tar.gz"
