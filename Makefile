.PHONY: all build test test-race test-cover test-e2e clean lint fmt vet check pre-commit tools help

# Binary name
BINARY := btidy

# Go parameters
GO := go
GOBUILD := $(GO) build
GOCLEAN := $(GO) clean
GOVET := $(GO) vet
GOFMT := gofmt

# Tools directory
TOOLS_DIR := $(shell pwd)/.tools
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint
GOTESTSUM := $(TOOLS_DIR)/gotestsum
GOIMPORTS := $(TOOLS_DIR)/goimports

# Tool versions
GOLANGCI_LINT_VERSION := v1.64.8
GOTESTSUM_VERSION := latest
GOIMPORTS_VERSION := latest

# Directories
CMD_DIR := ./cmd
PKG_DIR := ./pkg/...
ALL_GO_FILES := $(shell find . -name '*.go' -not -path './.tools/*' -not -path './vendor/*')

# Coverage
COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html

# Default target
all: check build

help:
	@echo "Available targets:"
	@echo "  all          - Run check and build"
	@echo "  build        - Build the binary"
	@echo "  test         - Run tests with coverage"
	@echo "  test-race    - Run tests with race detector and coverage"
	@echo "  test-cover   - Full coverage report with HTML"
	@echo "  test-e2e     - Run end-to-end CLI tests"
	@echo "  lint         - Run golangci-lint (54 linters)"
	@echo "  fmt          - Format code"
	@echo "  vet          - Run go vet"
	@echo "  check        - Run fmt, vet, lint, test-race"
	@echo "  pre-commit   - Run fmt, lint, test-race (required before commit)"
	@echo "  tools        - Install development tools to .tools/"
	@echo "  clean        - Clean build artifacts and tools"

# Tools installation
tools: $(GOLANGCI_LINT) $(GOTESTSUM) $(GOIMPORTS)

$(TOOLS_DIR):
	@mkdir -p $(TOOLS_DIR)

$(GOLANGCI_LINT): | $(TOOLS_DIR)
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@GOBIN=$(TOOLS_DIR) $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOTESTSUM): | $(TOOLS_DIR)
	@echo "Installing gotestsum $(GOTESTSUM_VERSION)..."
	@GOBIN=$(TOOLS_DIR) $(GO) install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)

$(GOIMPORTS): | $(TOOLS_DIR)
	@echo "Installing goimports..."
	@GOBIN=$(TOOLS_DIR) $(GO) install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)

# Build
build:
	@echo "Building $(BINARY)..."
	$(GOBUILD) -o $(BINARY) $(CMD_DIR)

# Tests (using gotestsum)
test: $(GOTESTSUM)
	@echo "Running tests..."
	$(GOTESTSUM) --format pkgname -- -count=1 -cover $(PKG_DIR)

test-race: $(GOTESTSUM)
	@echo "Running tests with race detector..."
	$(GOTESTSUM) --format pkgname -- -race -count=1 -cover $(PKG_DIR)

test-cover: $(GOTESTSUM)
	@echo "Running tests with coverage report..."
	$(GOTESTSUM) --format pkgname -- -race -count=1 -coverprofile=$(COVERAGE_FILE) -covermode=atomic $(PKG_DIR)
	@echo ""
	@echo "Coverage by function:"
	@$(GO) tool cover -func=$(COVERAGE_FILE)
	@$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo ""
	@echo "HTML report: $(COVERAGE_HTML)"

# End-to-end tests
test-e2e:
	@echo "Running end-to-end tests..."
	$(GO) test ./e2e -v -count=1

# Linting
lint: $(GOLANGCI_LINT)
	@echo "Running golangci-lint..."
	$(GOLANGCI_LINT) run --timeout 5m

# Formatting
fmt: $(GOIMPORTS)
	@echo "Formatting code..."
	$(GOFMT) -w -s $(ALL_GO_FILES)
	$(GOIMPORTS) -w -local btidy $(ALL_GO_FILES)

# Vet
vet:
	@echo "Running go vet..."
	$(GOVET) $(PKG_DIR)

# Combined checks
check: fmt vet lint test-race
	@echo "All checks passed!"

# Pre-commit hook - run before every commit
pre-commit: fmt lint test-race
	@echo "Pre-commit checks passed!"

# Clean
clean:
	$(GOCLEAN)
	rm -f $(BINARY)
	rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	rm -rf $(TOOLS_DIR)
