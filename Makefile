SHELL := /bin/bash
COVER_MIN := 95.0
COVER_PROFILE := coverage.out

# Packages to measure. Exclude example workflows/demos and the CLI mains
# (thin entrypoints exercised via integration, not unit-covered by design).
PKGS := $(shell go list ./... | grep -vE '/workflows/|/demos/|/cmd/')

.PHONY: all build ship ship-mcp ship-egress install hooks test vet race cover cover-html cover-check integration pkl-test clean

all: vet test

# Opt into the version-controlled git hooks (.githooks): a fast pre-commit
# (gofmt + build + vet) and a pre-push that dogfoods `ship run ci/precommit.pkl`.
hooks:
	git config core.hooksPath .githooks
	@echo "✓ git hooks enabled (core.hooksPath=.githooks)"
	@echo "  pre-commit: gofmt + go build + go vet"
	@echo "  pre-push:   ship run ci/precommit.pkl (vet + race tests)"

build:
	go build ./...

# Build the ship CLI binary with the version stamped from git.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
ship:
	go build -ldflags "-X main.version=$(VERSION)" -o ship ./cmd/ship

# Build the MCP server binary.
ship-mcp:
	go build -ldflags "-X main.version=$(VERSION)" -o ship-mcp ./cmd/ship-mcp

# Build the egress-filtering proxy binary.
ship-egress:
	go build -ldflags "-X main.version=$(VERSION)" -o ship-egress ./cmd/ship-egress

# Install the ship CLI into GOBIN / $GOPATH/bin.
install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/ship

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

# Per-package coverage summary.
cover:
	go test -cover $(PKGS)

# Enforce a minimum coverage across measured packages. Computes per-package
# statement coverage and the statement-weighted total, then fails (non-zero
# exit) if the total is below COVER_MIN. Avoids the multi-package covdata merge
# so it works on minimal toolchains.
cover-check:
	@go test -cover $(PKGS) 2>/dev/null | awk ' \
	  /coverage:/ { \
	    for (i=1;i<=NF;i++) if ($$i=="coverage:") { gsub("%","",$$(i+1)); c=$$(i+1) } \
	    printf "  %-45s %s%%\n", $$2, c; sum+=c; n++ \
	  } \
	  END { \
	    if (n==0) { print "no coverage data"; exit 1 } \
	    avg=sum/n; \
	    printf "average package coverage: %.1f%% (min %s%%)\n", avg, "$(COVER_MIN)"; \
	    if (avg+0 < "$(COVER_MIN)"+0) { print "FAIL: below minimum"; exit 1 } \
	    print "OK: coverage gate passed" \
	  }'

# Per-package floor: no single measured package may drop below COVER_MIN.
cover-check-per-package:
	@go test -cover $(PKGS) 2>/dev/null | awk -v m=$(COVER_MIN) ' \
	  /coverage:/ { \
	    for (i=1;i<=NF;i++) if ($$i=="coverage:") { gsub("%","",$$(i+1)); c=$$(i+1) } \
	    if (c+0 < m+0) { printf "FAIL: %-45s %s%% (< %s%%)\n", $$2, c, m; fail=1 } \
	    else printf "ok:   %-45s %s%%\n", $$2, c \
	  } \
	  END { if (fail) exit 1; print "OK: all packages >= " m "%" }'

cover-html:
	go test -coverprofile=$(COVER_PROFILE) ./... >/dev/null 2>&1 || true
	go tool cover -html=$(COVER_PROFILE) -o coverage.html
	@echo "wrote coverage.html"

# Integration tests that require a real container engine (Docker/Podman/Apple
# container). Gated behind the 'docker' build tag so the default suite stays
# infra-free. Usage: make integration  [ENGINE=docker|podman|apple]
ENGINE ?= docker
integration:
	SHIP_TEST_ENGINE=$(ENGINE) go test -tags=docker -run Integration ./internal/runner/ -v

# Integration test for the real Pkl CLI (evaluates the demo pipeline). Gated
# behind the 'pkl' build tag; skips if pkl is not installed.
pkl-test:
	go test -tags=pkl -run Integration ./internal/planfile/ -v

clean:
	rm -f $(COVER_PROFILE) coverage.html ship ship-mcp ship-egress
	rm -rf dist
