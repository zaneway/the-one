.PHONY: build test test-p1-sqlite test-p2-capture test-p3-sqlite test-p4-retrieval test-p5-mvp run-health run-status

DATA_DIR ?= /tmp/memoryd-p0
GO_TAGS ?=
BIN_DIR ?= bin

build:
	go build -tags "$(GO_TAGS)" -o "$(BIN_DIR)/memoryd" ./cmd/memoryd

test:
	go test -tags "$(GO_TAGS)" ./...

test-p1-sqlite:
	go test -tags "sqlite_fts5" ./internal/storage/sqlite

test-p2-capture:
	GO_TAGS="$(GO_TAGS)" BIN_DIR="$(BIN_DIR)" scripts/acceptance/p2_capture.sh

test-p3-sqlite:
	GO_TAGS="sqlite_fts5" BIN_DIR="$(BIN_DIR)" scripts/acceptance/p3_sqlite.sh

test-p4-retrieval:
	GO_TAGS="sqlite_fts5" BIN_DIR="$(BIN_DIR)" scripts/acceptance/p4_retrieval.sh

test-p5-mvp:
	GO_TAGS="sqlite_fts5" BIN_DIR="$(BIN_DIR)" scripts/acceptance/p5_mvp.sh

run-health:
	go run -tags "$(GO_TAGS)" ./cmd/memoryd health --data-dir "$(DATA_DIR)"

run-status:
	go run -tags "$(GO_TAGS)" ./cmd/memoryd status --data-dir "$(DATA_DIR)" --include-config
