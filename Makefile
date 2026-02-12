.PHONY: all build test test-race test-cover test-e2e clean lint fmt vet vulncheck check verify pre-commit tools help release release-build docs docs-d2 docs-deps check-cgo check-release-toolchains verify-third-party

# ============================================================================
# VARIABLES
# ============================================================================

BINARY := btidy

GO := go
GOBUILD := $(GO) build
GOCLEAN := $(GO) clean
GOVET := $(GO) vet
GOFMT := gofmt

CGO_ENABLED := 1
HOST_CC ?= gcc

CC_linux_amd64 ?= x86_64-linux-gnu-gcc
CC_linux_arm64 ?= aarch64-linux-gnu-gcc
CC_darwin_amd64 ?= o64-clang
CC_darwin_arm64 ?= oa64-clang
CC_windows_amd64 ?= x86_64-w64-mingw32-gcc

TOOLS_DIR := $(shell pwd)/.tools
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint
GOTESTSUM := $(TOOLS_DIR)/gotestsum
GOIMPORTS := $(TOOLS_DIR)/goimports
GOVULNCHECK := $(TOOLS_DIR)/govulncheck
D2 := $(TOOLS_DIR)/d2

GOLANGCI_LINT_VERSION := v2.8.0
GOTESTSUM_VERSION := latest
GOIMPORTS_VERSION := latest
GOVULNCHECK_VERSION := v1.1.4
D2_VERSION := v0.7.1

CMD_DIR := ./cmd
PKG_DIR := ./pkg/...
ALL_GO_FILES := $(shell find . -name '*.go' -not -path './.tools/*' -not -path './vendor/*')

VERSION_FILE := VERSION
CURRENT_VERSION := $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.0.0")
LDFLAGS := -ldflags "-s -w -X main.version=$(CURRENT_VERSION)"

DIST_DIR := dist
RELEASE_PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html

# ============================================================================
# DEFAULT & HELP
# ============================================================================

all: check build

help:
	@echo "Available targets:"
	@echo "  all           - Run check and build"
	@echo "  build         - Build the binary"
	@echo "  test          - Run tests with coverage"
	@echo "  test-race     - Run tests with race detector and coverage"
	@echo "  test-cover    - Full coverage report with HTML"
	@echo "  test-e2e      - Run end-to-end CLI tests"
	@echo "  lint          - Run golangci-lint"
	@echo "  fmt           - Format code"
	@echo "  vet           - Run go vet"
	@echo "  vulncheck     - Run govulncheck"
	@echo "  check         - Run fmt, vet, lint, vulncheck, test-race"
	@echo "  verify        - Alias for check"
	@echo "  pre-commit    - Run fmt, lint, test-race (required before commit)"
	@echo "  tools         - Install development tools to .tools/"
	@echo "  docs          - Render D2 diagrams and dependency graph to docs/*.svg"
	@echo "  docs-d2       - Render D2 diagrams only (requires d2, install via make tools)"
	@echo "  docs-deps     - Render auto-generated dependency graph only (requires graphviz: apt install graphviz)"
	@echo "  check-cgo     - Verify local C compiler for CGO builds"
	@echo "  check-release-toolchains - Verify cross-compilers required by release-build"
	@echo "  verify-third-party - Verify vendored zlib files match upstream"
	@echo "  clean         - Clean build artifacts and tools"
	@echo "  release       - Tag, push, and create GitHub release with binaries"
	@echo "  release-build - Cross-compile CGO-enabled release binaries to dist/"

# ============================================================================
# BUILD
# ============================================================================

check-cgo:
	@command -v $(HOST_CC) >/dev/null 2>&1 || { \
		echo "error: required C compiler '$(HOST_CC)' not found (CGO is mandatory)"; \
		echo "hint: install build-essential (Debian/Ubuntu) or Xcode CLT (macOS)"; \
		exit 1; \
	}

build: check-cgo
	@echo "Building $(BINARY)..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(LDFLAGS) -o $(BINARY) $(CMD_DIR)

# ============================================================================
# TEST
# ============================================================================

test: check-cgo $(GOTESTSUM)
	@echo "Running tests..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOTESTSUM) --format pkgname -- -count=1 -cover $(PKG_DIR)

test-race: check-cgo $(GOTESTSUM)
	@echo "Running tests with race detector..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOTESTSUM) --format pkgname -- -race -count=1 -cover $(PKG_DIR)

test-cover: check-cgo $(GOTESTSUM)
	@echo "Running tests with coverage report..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOTESTSUM) --format pkgname -- -race -count=1 -coverprofile=$(COVERAGE_FILE) -covermode=atomic $(PKG_DIR)
	@echo ""
	@echo "Coverage by function:"
	@$(GO) tool cover -func=$(COVERAGE_FILE)
	@$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo ""
	@echo "HTML report: $(COVERAGE_HTML)"

test-e2e: check-cgo
	@echo "Running end-to-end tests..."
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test ./e2e -v -count=1

# ============================================================================
# LINT & FORMAT
# ============================================================================

lint: $(GOLANGCI_LINT)
	@echo "Running golangci-lint..."
	$(GOLANGCI_LINT) run --timeout 5m

fmt: $(GOIMPORTS)
	@echo "Formatting code..."
	$(GOFMT) -w -s $(ALL_GO_FILES)
	$(GOIMPORTS) -w -local btidy $(ALL_GO_FILES)

vet: check-cgo
	@echo "Running go vet..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOVET) $(PKG_DIR)

vulncheck: $(GOVULNCHECK)
	@echo "Running govulncheck..."
	$(GOVULNCHECK) ./...

# ============================================================================
# COMBINED CHECKS
# ============================================================================

check: fmt vet lint vulncheck test-race
	@echo "All checks passed!"

verify: check

pre-commit: fmt lint test-race
	@echo "Pre-commit checks passed!"

# ============================================================================
# TOOLS
# ============================================================================

tools: $(GOLANGCI_LINT) $(GOTESTSUM) $(GOIMPORTS) $(GOVULNCHECK) $(D2)

$(TOOLS_DIR):
	@mkdir -p $(TOOLS_DIR)

$(GOLANGCI_LINT): | $(TOOLS_DIR)
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@GOBIN=$(TOOLS_DIR) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOTESTSUM): | $(TOOLS_DIR)
	@echo "Installing gotestsum $(GOTESTSUM_VERSION)..."
	@GOBIN=$(TOOLS_DIR) $(GO) install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)

$(GOIMPORTS): | $(TOOLS_DIR)
	@echo "Installing goimports..."
	@GOBIN=$(TOOLS_DIR) $(GO) install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)

$(GOVULNCHECK): | $(TOOLS_DIR)
	@echo "Installing govulncheck $(GOVULNCHECK_VERSION)..."
	@GOBIN=$(TOOLS_DIR) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

$(D2): | $(TOOLS_DIR)
	@echo "Installing d2 $(D2_VERSION)..."
	@GOBIN=$(TOOLS_DIR) $(GO) install oss.terrastruct.com/d2@$(D2_VERSION)

# ============================================================================
# DOCS
# ============================================================================

D2_FILES := $(wildcard docs/*.d2)
D2_SVGS := $(D2_FILES:.d2=.svg)

docs: docs-d2 docs-deps ## Render D2 diagrams and dependency graph to docs/*.svg

docs-d2: $(D2) $(D2_SVGS) ## Render D2 diagrams only

docs/%.svg: docs/%.d2 $(D2)
	@echo "Rendering $<..."
	$(D2) $< $@

docs-deps: ## Render auto-generated dependency graph only
	@echo "Generating dependency graph..."
	@scripts/depgraph.sh docs/deps.svg

verify-third-party:
	@echo "Verifying vendored zlib files against upstream tag..."
	@$(GO) run ./scripts/verify_zlib_vendor.go

# ============================================================================
# CLEAN
# ============================================================================

clean:
	$(GOCLEAN)
	rm -f $(BINARY)
	rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	rm -rf $(TOOLS_DIR)
	rm -rf $(DIST_DIR)

# ============================================================================
# RELEASE
# ============================================================================

check-release-toolchains:
	@missing=0; \
	for platform in $(RELEASE_PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform#*/}; \
		case "$$GOOS/$$GOARCH" in \
			linux/amd64) CC_BIN="$(CC_linux_amd64)" ;; \
			linux/arm64) CC_BIN="$(CC_linux_arm64)" ;; \
			darwin/amd64) CC_BIN="$(CC_darwin_amd64)" ;; \
			darwin/arm64) CC_BIN="$(CC_darwin_arm64)" ;; \
			windows/amd64) CC_BIN="$(CC_windows_amd64)" ;; \
			*) echo "error: unsupported platform $$GOOS/$$GOARCH"; exit 1 ;; \
		esac; \
		if ! command -v "$$CC_BIN" >/dev/null 2>&1; then \
			echo "error: required cross-compiler '$$CC_BIN' not found for $$GOOS/$$GOARCH"; \
			missing=1; \
		fi; \
	done; \
	if [ $$missing -ne 0 ]; then exit 1; fi

release-build: check-release-toolchains
	@echo "building release binaries v$(CURRENT_VERSION)..."
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)
	@for platform in $(RELEASE_PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform#*/}; \
		case "$$GOOS/$$GOARCH" in \
			linux/amd64) CC_BIN="$(CC_linux_amd64)" ;; \
			linux/arm64) CC_BIN="$(CC_linux_arm64)" ;; \
			darwin/amd64) CC_BIN="$(CC_darwin_amd64)" ;; \
			darwin/arm64) CC_BIN="$(CC_darwin_arm64)" ;; \
			windows/amd64) CC_BIN="$(CC_windows_amd64)" ;; \
			*) echo "error: unsupported platform $$GOOS/$$GOARCH"; exit 1 ;; \
		esac; \
		output="$(DIST_DIR)/$(BINARY)-$${GOOS}-$${GOARCH}"; \
		if [ "$$GOOS" = "windows" ]; then output="$${output}.exe"; fi; \
		echo "  $$GOOS/$$GOARCH (CC=$$CC_BIN) -> $$output"; \
		CGO_ENABLED=1 CC=$$CC_BIN GOOS=$$GOOS GOARCH=$$GOARCH $(GOBUILD) $(LDFLAGS) -o "$$output" $(CMD_DIR) || exit 1; \
	done
	@echo "binaries written to $(DIST_DIR)/"

release:
	@echo "preparing release..."
	@# check for staged changes (nothing should be staged)
	@if ! git diff --cached --quiet; then \
		echo "error: staged changes detected"; \
		echo "unstage or commit changes before releasing"; \
		exit 1; \
	fi
	@# check for unstaged changes (excluding VERSION which we may modify)
	@if ! git diff --quiet -- . ':!VERSION'; then \
		echo "error: unstaged changes detected"; \
		echo "commit or stash changes before releasing"; \
		exit 1; \
	fi
	@# check gh is available
	@command -v gh >/dev/null 2>&1 || { echo "error: gh (github cli) not found"; exit 1; }
	@# check we're on main branch
	@if [ "$$(git branch --show-current)" != "main" ]; then \
		echo "error: must be on main branch to release"; \
		exit 1; \
	fi
	@# pull latest changes and verify we're in sync with remote
	@echo "pulling latest changes..."
	@git fetch origin main
	@if [ "$$(git rev-parse HEAD)" != "$$(git rev-parse origin/main)" ]; then \
		echo "error: local main is not in sync with origin/main"; \
		echo "run 'git pull' or 'git push' to sync before releasing"; \
		exit 1; \
	fi
	@# determine version: if tag exists, bump patch; otherwise use VERSION as-is
	@VERSION=$(CURRENT_VERSION); \
	if git rev-parse "v$$VERSION" >/dev/null 2>&1; then \
		echo "tag v$$VERSION exists, bumping patch version..."; \
		MAJOR=$$(echo $$VERSION | cut -d. -f1); \
		MINOR=$$(echo $$VERSION | cut -d. -f2); \
		PATCH=$$(echo $$VERSION | cut -d. -f3); \
		PATCH=$$((PATCH + 1)); \
		VERSION="$$MAJOR.$$MINOR.$$PATCH"; \
		echo "$$VERSION" > $(VERSION_FILE); \
		echo "updated VERSION to $$VERSION"; \
	else \
		echo "using version $$VERSION from VERSION file"; \
	fi && \
	\
	echo "committing version bump..." && \
	git add $(VERSION_FILE) && \
	(git commit -m "chore: release v$$VERSION" || true) && \
	\
	echo "creating tag v$$VERSION..." && \
	git tag -a "v$$VERSION" -m "Release v$$VERSION" && \
	\
	echo "pushing to origin..." && \
	git push origin main --tags && \
	\
	echo "building release binaries..." && \
	$(MAKE) release-build CURRENT_VERSION=$$VERSION && \
	\
	echo "creating github release..." && \
	gh release create "v$$VERSION" $(DIST_DIR)/* --generate-notes --title "v$$VERSION" && \
	\
	echo "" && \
	echo "release v$$VERSION complete!"
