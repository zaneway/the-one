.PHONY: build test test-p1-sqlite run-health run-status

DATA_DIR ?= /tmp/memoryd-p0
GO_TAGS ?=
BIN_DIR ?= bin

build:
	go build -tags "$(GO_TAGS)" -o "$(BIN_DIR)/memoryd" ./cmd/memoryd

test:
	go test -tags "$(GO_TAGS)" ./...

test-p1-sqlite:
	go test -tags "sqlite_fts5" ./internal/storage/sqlite

run-health:
	go run -tags "$(GO_TAGS)" ./cmd/memoryd health --data-dir "$(DATA_DIR)"

run-status:
	go run -tags "$(GO_TAGS)" ./cmd/memoryd status --data-dir "$(DATA_DIR)" --include-config
