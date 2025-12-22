.PHONY: build install clean test lint run release release-dry-run completion

# Binary name
BINARY := bosun

# Build directory
BUILD_DIR := ./build

# Completion directory
COMPLETION_DIR := ./completions

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Version info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS := -s -w \
	-X github.com/cameronsjo/bosun/internal/cmd.version=$(VERSION) \
	-X github.com/cameronsjo/bosun/internal/cmd.commit=$(COMMIT) \
	-X github.com/cameronsjo/bosun/internal/cmd.date=$(DATE)

# Default target
all: build

# Build the binary
build:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/bosun

# Install to GOPATH/bin
install:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(GOPATH)/bin/$(BINARY) ./cmd/bosun

# Run without building
run:
	$(GOCMD) run ./cmd/bosun $(ARGS)

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with coverage
test-cover:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Tidy dependencies
tidy:
	$(GOMOD) tidy

# Download dependencies
deps:
	$(GOMOD) download

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Build for multiple platforms
build-all: clean
	GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/bosun
	GOOS=linux GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/bosun
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 ./cmd/bosun
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 ./cmd/bosun

# Development build (no optimizations)
dev:
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY) ./cmd/bosun

# Release - dry run (test locally without publishing)
release-dry-run:
	@echo "Running goreleaser in dry-run mode..."
	goreleaser release --snapshot --clean --skip=publish

# Release - create and publish (requires GITHUB_TOKEN)
release:
	@if [ -z "$$GITHUB_TOKEN" ]; then \
		echo "Error: GITHUB_TOKEN is required"; \
		exit 1; \
	fi
	goreleaser release --clean

# Check goreleaser config
release-check:
	goreleaser check

# Generate shell completion scripts
completion: build
	@mkdir -p $(COMPLETION_DIR)
	$(BUILD_DIR)/$(BINARY) completion bash > $(COMPLETION_DIR)/$(BINARY).bash
	$(BUILD_DIR)/$(BINARY) completion zsh > $(COMPLETION_DIR)/_$(BINARY)
	$(BUILD_DIR)/$(BINARY) completion fish > $(COMPLETION_DIR)/$(BINARY).fish
	$(BUILD_DIR)/$(BINARY) completion powershell > $(COMPLETION_DIR)/$(BINARY).ps1
	@echo "Completions generated in $(COMPLETION_DIR)/"

# Help
help:
	@echo "Available targets:"
	@echo "  build           - Build the binary"
	@echo "  install         - Install to GOPATH/bin"
	@echo "  run             - Run without building (use ARGS=... for arguments)"
	@echo "  test            - Run tests"
	@echo "  test-cover      - Run tests with coverage"
	@echo "  tidy            - Tidy go.mod"
	@echo "  deps            - Download dependencies"
	@echo "  clean           - Remove build artifacts"
	@echo "  build-all       - Build for all platforms"
	@echo "  dev             - Development build"
	@echo "  release-dry-run - Test release locally (no publish)"
	@echo "  release         - Create and publish release (requires GITHUB_TOKEN)"
	@echo "  release-check   - Validate goreleaser config"
	@echo "  completion      - Generate shell completion scripts"
