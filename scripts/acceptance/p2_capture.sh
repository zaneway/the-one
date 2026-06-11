#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

echo "[p2] running focused capture tests"
go test -tags "${GO_TAGS:-}" ./internal/capture ./internal/storage/sqlite ./internal/app

echo "[p2] running full regression suite"
go test -tags "${GO_TAGS:-}" ./...

echo "[p2] building theone"
go build -tags "${GO_TAGS:-}" -o "${BIN_DIR:-bin}/theone" ./cmd/theone

echo "[p2] acceptance passed"
