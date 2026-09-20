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

# ── Quality ──────────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run all tests with race detection (SQLite only; Postgres tests skip)
	go test -race -count=1 $(PKG)

.PHONY: test-all
test-all: dev-db ## Run all tests against BOTH SQLite and Postgres
	PIVOT_TEST_POSTGRES_URL='$(DEV_PG_URL)' go test -race -count=1 $(PKG)

.PHONY: cover
cover: ## Run tests and open an HTML coverage report
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic $(PKG)
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
.PHONY: tools
tools: $(TOOLS_DIR)/golangci-lint ## Install pinned dev tooling into ./bin

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
