#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

GO_TAGS="${GO_TAGS:-sqlite_fts5}"
BIN_DIR="${BIN_DIR:-bin}"
P5_MVP_MODE="${P5_MVP_MODE:-synthetic}"

if [[ "${P5_MVP_MODE}" != "synthetic" ]]; then
  echo "[p5] unsupported P5_MVP_MODE=${P5_MVP_MODE}; P5-C only supports synthetic"
  exit 1
fi

echo "[p5] running mvp model and fixture tests"
go test -tags "${GO_TAGS}" ./internal/mvp

echo "[p5] running mvp repository tests"
go test -tags "${GO_TAGS}" ./internal/storage/sqlite -run 'TestP5'

echo "[p5] running synthetic MVP acceptance"
go test -tags "${GO_TAGS}" ./internal/app -run 'TestP5CSyntheticMVPAcceptance' -count=1

echo "[p5] running app MVP tool regression"
go test -tags "${GO_TAGS}" ./internal/app -run 'TestAppRegistersMVPTools' -count=1

echo "[p5] running full sqlite_fts5 regression suite"
go test -tags "${GO_TAGS}" ./...

echo "[p5] building theone"
go build -tags "${GO_TAGS}" -o "${BIN_DIR}/theone" ./cmd/theone

echo "[p5] acceptance passed"
