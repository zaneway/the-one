.PHONY: build package-cursor package-claude package-codex install-cursor install-claude install-codex test test-p1-sqlite test-p2-capture test-p2-envelope test-p3-sqlite test-p4-retrieval test-p5-mvp run-health run-status

DATA_DIR ?= /tmp/theone-p0
GO_TAGS ?= sqlite_fts5
BIN_DIR ?= bin
VERSION ?= dev

build:
	go build -tags "$(GO_TAGS)" -o "$(BIN_DIR)/theone" ./cmd/theone

package-cursor:
	GO_TAGS="$(GO_TAGS)" VERSION="$(VERSION)" scripts/package-cursor.sh

package-claude:
	GO_TAGS="$(GO_TAGS)" VERSION="$(VERSION)" scripts/package-claude.sh

package-codex:
	GO_TAGS="$(GO_TAGS)" VERSION="$(VERSION)" scripts/package-codex.sh

install-cursor: build
	chmod +x scripts/install-cursor.sh
	./scripts/install-cursor.sh

install-claude: build
	chmod +x scripts/install-claude.sh scripts/install-claude-hooks.sh
	./scripts/install-claude.sh

install-codex: build
	chmod +x scripts/install-codex.sh
	./scripts/install-codex.sh

test:
	go test -tags "$(GO_TAGS)" ./...

test-p1-sqlite:
	go test -tags "sqlite_fts5" ./internal/storage/sqlite

test-p2-capture:
	scripts/acceptance/p2_capture.sh

test-p2-envelope:
	scripts/acceptance/p2_envelope.sh

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
