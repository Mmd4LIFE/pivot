# Pivot — development tasks
#
# Run `make` or `make help` for the task list.

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ── Paths ────────────────────────────────────────────────────────────────────
BIN_DIR    := bin
TOOLS_DIR  := $(CURDIR)/$(BIN_DIR)
BINARY     := $(BIN_DIR)/pivot
PKG        := ./...

export GOBIN := $(TOOLS_DIR)
export PATH  := $(TOOLS_DIR):$(PATH)

# ── Version stamping ─────────────────────────────────────────────────────────
# Overridden by the release pipeline (Part 13). `dev` locally.
VERSION    ?= dev
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILT_BY   ?= $(shell whoami 2>/dev/null || echo unknown)

VERSION_PKG := github.com/Mmd4LIFE/pivot/internal/version
LDFLAGS := -s -w \
	-X '$(VERSION_PKG).version=$(VERSION)' \
	-X '$(VERSION_PKG).commit=$(COMMIT)' \
	-X '$(VERSION_PKG).date=$(DATE)' \
	-X '$(VERSION_PKG).builtBy=$(BUILT_BY)'

# ── Pinned tool versions ─────────────────────────────────────────────────────
# Installed from the upstream release archive rather than `go install`:
# building golangci-lint from source pulls ~400 modules, and its checksum-db
# lookups are the first thing to fail on a slow or flaky network. The release
# archive is one download, and we verify it against the published checksums.
# sqlc publishes no checksums file, so we pin a hash we verified ourselves.
# Bumping SQLC_VERSION means recomputing SQLC_SHA256 — deliberately manual, so
# a new binary is never installed unreviewed.
SQLC_VERSION := 1.31.1
SQLC_SHA256  := 497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354
SQLC_DIST    := sqlc_$(SQLC_VERSION)_linux_amd64.tar.gz
SQLC_URL     := https://github.com/sqlc-dev/sqlc/releases/download/v$(SQLC_VERSION)/$(SQLC_DIST)

GOLANGCI_VERSION  := v2.13.2
GOLANGCI_SEMVER   := $(patsubst v%,%,$(GOLANGCI_VERSION))
GOLANGCI_OS       := $(shell go env GOOS)
GOLANGCI_ARCH     := $(shell go env GOARCH)
GOLANGCI_DIST     := golangci-lint-$(GOLANGCI_SEMVER)-$(GOLANGCI_OS)-$(GOLANGCI_ARCH)
GOLANGCI_BASE_URL := https://github.com/golangci/golangci-lint/releases/download/$(GOLANGCI_VERSION)

# ── Help ─────────────────────────────────────────────────────────────────────
.PHONY: help
help: ## Show this help
	@echo "Pivot — development tasks"
	@echo ""
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@echo ""

# ── Build ────────────────────────────────────────────────────────────────────
.PHONY: build
build: ## Build the pivot binary into ./bin
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/pivot
	@echo "built $(BINARY) ($(VERSION), $(COMMIT))"

.PHONY: run
run: build ## Build and run pivot
	@$(BINARY) $(ARGS)

.PHONY: clean
clean: ## Remove build artifacts and caches
	@rm -rf $(BIN_DIR) dist coverage.out coverage.html
	@go clean -testcache
	@echo "cleaned"

# ── Development services ─────────────────────────────────────────────────────
COMPOSE_DEV := deploy/docker-compose.dev.yml
DEV_PG_URL  := postgres://pivot:pivot@localhost:5433/pivot?sslmode=disable

.PHONY: dev-db
dev-db: ## Start the development Postgres container
	docker compose -f $(COMPOSE_DEV) up -d --wait
	@echo "postgres ready: $(DEV_PG_URL)"

.PHONY: dev-db-stop
dev-db-stop: ## Stop the development Postgres container
	docker compose -f $(COMPOSE_DEV) down

.PHONY: dev-db-reset
dev-db-reset: ## Destroy and recreate the development Postgres volume
	docker compose -f $(COMPOSE_DEV) down -v
	$(MAKE) dev-db

.PHONY: dev-db-url
dev-db-url: ## Print the development Postgres URL
	@echo '$(DEV_PG_URL)'

# ── Frontend ─────────────────────────────────────────────────────────────────
#
# Vite builds static assets into web/dist, which web/embed.go compiles into the
# binary. Nothing here runs in production - ADR-0002 rejects an SSR framework
# precisely so that no Node process has to be deployed.
#
# The Go build does not depend on these targets. web/dist ships with a
# committed placeholder, so `make build` works on a machine with no Node and
# the server explains itself rather than failing. `make all` is the one that
# produces a binary with a real frontend in it.
WEB_DIR := web

.PHONY: web-install
web-install: ## Install frontend dependencies (large download, run once)
	cd $(WEB_DIR) && npm install

$(WEB_DIR)/node_modules:
	@echo "frontend dependencies are missing; run 'make web-install'" && exit 1

.PHONY: web-build
web-build: $(WEB_DIR)/node_modules ## Type-check and build the browser application
	cd $(WEB_DIR) && npm run build
	@$(MAKE) --no-print-directory web-keep

# web-keep restores the embed placeholder.
#
# Vite's emptyOutDir wipes web/dist on every build, .gitkeep included. Without
# it `//go:embed all:dist` fails on a fresh clone, so a developer who built the
# frontend and then committed would break the Go build for everyone who had
# not. Restoring it after each build makes that impossible rather than
# remembered.
.PHONY: web-keep
web-keep:
	@mkdir -p $(WEB_DIR)/dist
	@[ -f $(WEB_DIR)/dist/.gitkeep ] || printf '%s\n' \
		'Keeps this directory in git so that `//go:embed all:dist` in ../embed.go has' \
		'something to embed on a fresh clone. Vite'"'"'s emptyOutDir deletes it on every' \
		'build, so `make web-build` and `make web-clean` both put it back -- without it,' \
		'`go build ./...` fails on a machine that has never run the frontend build.' \
		> $(WEB_DIR)/dist/.gitkeep

.PHONY: web-typecheck
web-typecheck: $(WEB_DIR)/node_modules ## Type-check the frontend without building
	cd $(WEB_DIR) && npm run typecheck

.PHONY: web-test
web-test: $(WEB_DIR)/node_modules ## Run the component accessibility suite (axe over every story)
	cd $(WEB_DIR) && npm test

# ── End to end ───────────────────────────────────────────────────────────────
#
# Against the binary, never the dev server. What ships is one Go process
# serving the embedded bundle, and the differences between that and Vite are
# exactly where bugs hide: the SPA fallback, the asset caching headers, the
# document CSP, and a same-origin session cookie a proxied dev setup would
# handle differently.
#
# The suite provisions its own throwaway SQLite instance on :8099 and throws it
# away afterwards. It never touches a database anybody is using.
#
# Browsers are a separate ~650 MB download, once per machine:
#
#     cd web && npx playwright install chromium
#
# `install-deps` fails on Ubuntu 24.10 -- its package list is keyed to 24.04 --
# and can be skipped there; the libraries are already present.
.PHONY: e2e
e2e: all $(WEB_DIR)/node_modules ## Run the browser end-to-end suite against the built binary
	cd $(WEB_DIR) && npm run e2e

.PHONY: e2e-ui
e2e-ui: all $(WEB_DIR)/node_modules ## Run the end-to-end suite in Playwright's inspector
	cd $(WEB_DIR) && npm run e2e:ui

# ── Storybook ────────────────────────────────────────────────────────────────
#
# A development and review tool, never shipped. It is not part of `make all`
# and its output never reaches web/dist, so nothing it produces can end up
# inside the binary.
#
# Reviewing a component in Storybook is not a substitute for `make web-test`:
# the panel shows one story at a time and only while someone is looking, and
# the suite checks all of them on every run.
.PHONY: storybook
storybook: $(WEB_DIR)/node_modules ## Browse the design system at :6006
	cd $(WEB_DIR) && npm run storybook

.PHONY: storybook-build
storybook-build: $(WEB_DIR)/node_modules ## Build a static Storybook into web/storybook-static
	cd $(WEB_DIR) && npm run storybook:build

.PHONY: web-clean
web-clean: ## Remove built frontend assets, keeping the embed placeholder
	@find $(WEB_DIR)/dist -mindepth 1 ! -name '.gitkeep' -delete 2>/dev/null || true
	@rm -rf $(WEB_DIR)/storybook-static $(WEB_DIR)/.playwright $(WEB_DIR)/playwright-report
	@$(MAKE) --no-print-directory web-keep
	@echo "cleaned $(WEB_DIR)/dist"

.PHONY: all
all: web-build build ## Build the frontend and embed it in the binary

.PHONY: dev
dev: $(WEB_DIR)/node_modules ## Run both sides with reload: Vite on :5173, Go on :8080
	@echo "Vite  http://localhost:5173  (proxies /api to :8080)"
	@echo "Go    http://localhost:8080"
	@echo "Ctrl-C stops both."
	@trap 'kill 0' EXIT INT TERM; \
	( cd $(WEB_DIR) && npm run dev ) & \
	$(MAKE) --no-print-directory dev-go & \
	wait

# dev-go restarts the server whenever a .go file changes.
#
# Polling with find rather than adding a file-watcher dependency: it is a
# second of latency on a rebuild nobody is waiting on, and it works the same on
# every machine without another tool to install.
.PHONY: dev-go
dev-go:
	@stamp=$$(mktemp); \
	trap 'rm -f $$stamp; kill 0' EXIT INT TERM; \
	while true; do \
		go run ./cmd/pivot serve & \
		pid=$$!; \
		touch $$stamp; \
		while [ -z "$$(find . -name '*.go' -newer $$stamp -not -path './web/node_modules/*' -print -quit)" ]; do \
			sleep 1; \
			kill -0 $$pid 2>/dev/null || break; \
		done; \
		kill $$pid 2>/dev/null || true; \
		wait $$pid 2>/dev/null || true; \
		echo "--- restarting after a change ---"; \
	done

# ── Quality ──────────────────────────────────────────────────────────────────
#
# TEST_FLAGS carries -p 1, which serializes *packages* (subtests inside a
# package still run in parallel). Without it, `go test ./...` starts one test
# binary per package at once — up to GOMAXPROCS of them — and several of ours
# hash passwords with Argon2 at 64 MiB a time under the race detector, whose
# shadow memory multiplies that several fold. On a 22-core machine that was
# enough to get the run killed outright, and a 2-core CI runner with 7 GiB has
# far less headroom. Serialized, the whole suite is about 30 seconds.
TEST_FLAGS := -race -count=1 -p 1

.PHONY: test
test: ## Run all tests with race detection (SQLite only; Postgres tests skip)
	go test $(TEST_FLAGS) $(PKG)

.PHONY: test-all
test-all: dev-db ## Run all tests against BOTH SQLite and Postgres
	PIVOT_TEST_POSTGRES_URL='$(DEV_PG_URL)' go test $(TEST_FLAGS) $(PKG)

.PHONY: cover
cover: ## Run tests and open an HTML coverage report
	go test $(TEST_FLAGS) -coverprofile=coverage.out -covermode=atomic $(PKG)
	go tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

.PHONY: lint
lint: $(TOOLS_DIR)/golangci-lint ## Run the linters
	$(TOOLS_DIR)/golangci-lint run

.PHONY: lint-fix
lint-fix: $(TOOLS_DIR)/golangci-lint ## Run the linters and auto-fix what can be fixed
	$(TOOLS_DIR)/golangci-lint run --fix

.PHONY: fmt
fmt: ## Format Go code and tidy modules
	go fmt $(PKG)
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: check
check: fmt vet lint test ## Run everything CI runs

# ── Tooling ──────────────────────────────────────────────────────────────────
OPENAPI_SPEC := api/openapi.yaml
TS_CLIENT    := web/src/api/schema.d.ts

.PHONY: gen
gen: $(TOOLS_DIR)/sqlc ## Generate typed query code from SQL
	$(TOOLS_DIR)/sqlc generate
	@echo "generated; run 'git diff --exit-code' to confirm it is committed"

.PHONY: gen-client
gen-client: ## Generate the TypeScript client from the OpenAPI spec
	@mkdir -p $(dir $(TS_CLIENT))
	npx --yes openapi-typescript@7 $(OPENAPI_SPEC) -o $(TS_CLIENT)
	@echo "generated $(TS_CLIENT)"

.PHONY: validate-spec
validate-spec: ## Check that the OpenAPI spec parses and is self-consistent
	go test ./internal/api/ -run TestOpenAPISpec -count=1 -v 2>&1 | grep -E "^(=== RUN|--- |ok|FAIL)"

.PHONY: gen-check
gen-check: gen ## Fail if generated code is out of date (for CI)
	@git diff --exit-code -- internal/store/ \
		|| (echo "ERROR: generated code is stale; run 'make gen' and commit" && exit 1)

.PHONY: tools
tools: $(TOOLS_DIR)/golangci-lint $(TOOLS_DIR)/sqlc ## Install pinned dev tooling into ./bin

$(TOOLS_DIR)/sqlc:
	@mkdir -p $(TOOLS_DIR)
	@echo "installing sqlc $(SQLC_VERSION)..."
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
		curl -sSfL --retry 5 --retry-delay 3 --retry-all-errors \
			-o "$$tmp/$(SQLC_DIST)" "$(SQLC_URL)" && \
		echo "$(SQLC_SHA256)  $$tmp/$(SQLC_DIST)" | sha256sum -c - && \
		tar -xzf "$$tmp/$(SQLC_DIST)" -C "$$tmp" && \
		install -m 0755 "$$tmp/sqlc" "$(TOOLS_DIR)/sqlc"
	@$(TOOLS_DIR)/sqlc version

$(TOOLS_DIR)/golangci-lint:
	@mkdir -p $(TOOLS_DIR)
	@echo "installing golangci-lint $(GOLANGCI_VERSION) ($(GOLANGCI_OS)/$(GOLANGCI_ARCH))..."
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
		curl -sSfL --retry 5 --retry-delay 3 --retry-all-errors \
			-o "$$tmp/$(GOLANGCI_DIST).tar.gz" \
			"$(GOLANGCI_BASE_URL)/$(GOLANGCI_DIST).tar.gz" && \
		curl -sSfL --retry 5 --retry-delay 3 --retry-all-errors \
			-o "$$tmp/checksums.txt" \
			"$(GOLANGCI_BASE_URL)/golangci-lint-$(GOLANGCI_SEMVER)-checksums.txt" && \
		(cd "$$tmp" && grep " $(GOLANGCI_DIST).tar.gz$$" checksums.txt | sha256sum -c -) && \
		tar -xzf "$$tmp/$(GOLANGCI_DIST).tar.gz" -C "$$tmp" && \
		install -m 0755 "$$tmp/$(GOLANGCI_DIST)/golangci-lint" "$(TOOLS_DIR)/golangci-lint"
	@$(TOOLS_DIR)/golangci-lint --version

# ── Housekeeping ─────────────────────────────────────────────────────────────
.PHONY: tidy
tidy: ## Tidy and verify go.mod
	go mod tidy
	go mod verify

.PHONY: deps
deps: ## Download module dependencies
	go mod download
