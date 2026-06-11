#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"

echo "[p4] running retrieval unit coverage targets"
go test -tags "${GO_TAGS}" ./internal/retrieval ./internal/codeindex ./internal/docindex

echo "[p4] running retrieval repository tests"
go test -tags "${GO_TAGS}" ./internal/storage/sqlite

echo "[p4] running app integration and diagnostics tests"
go test -tags "${GO_TAGS}" ./internal/app

echo "[p4] running full sqlite_fts5 regression suite"
go test -tags "${GO_TAGS}" ./...

echo "[p4] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

echo "[p4] acceptance passed"
