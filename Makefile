.PHONY: build test test-p1-sqlite test-p2-capture test-p3-sqlite test-p4-retrieval test-p5-mvp run-health run-status

DATA_DIR ?= /tmp/theone-p0
GO_TAGS ?= sqlite_fts5
BIN_DIR ?= bin

build:
	go build -tags "$(GO_TAGS)" -o "$(BIN_DIR)/theone" ./cmd/theone

test:
	go test -tags "$(GO_TAGS)" ./...

test-p1-sqlite:
	go test -tags "sqlite_fts5" ./internal/storage/sqlite

test-p2-capture:
	scripts/acceptance/p2_capture.sh

test-p3-sqlite:
	scripts/acceptance/p3_sqlite.sh

test-p4-retrieval:
	scripts/acceptance/p4_retrieval.sh

test-p5-mvp:
	scripts/acceptance/p5_mvp.sh

run-health:
	go run -tags "$(GO_TAGS)" ./cmd/theone health --data-dir "$(DATA_DIR)"

run-status:
	go run -tags "$(GO_TAGS)" ./cmd/theone status --data-dir "$(DATA_DIR)" --include-config
