#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"

echo "[p3] running automation pipeline tests"
go test -tags "${GO_TAGS}" ./internal/processor ./internal/automation ./internal/retention

echo "[p3] running sqlite repository tests"
go test -tags "${GO_TAGS}" ./internal/storage/sqlite

echo "[p3] running app/MCP diagnostics tests"
go test -tags "${GO_TAGS}" ./internal/app

echo "[p3] running full regression suite"
go test -tags "${GO_TAGS}" ./...

echo "[p3] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

echo "[p3] acceptance passed"
