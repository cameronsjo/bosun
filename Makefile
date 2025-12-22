.PHONY: build install clean test lint run

# Binary name
BINARY := bosun

# Build directory
BUILD_DIR := ./build

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Build flags
LDFLAGS := -s -w

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

# Help
help:
	@echo "Available targets:"
	@echo "  build      - Build the binary"
	@echo "  install    - Install to GOPATH/bin"
	@echo "  run        - Run without building (use ARGS=... for arguments)"
	@echo "  test       - Run tests"
	@echo "  test-cover - Run tests with coverage"
	@echo "  tidy       - Tidy go.mod"
	@echo "  deps       - Download dependencies"
	@echo "  clean      - Remove build artifacts"
	@echo "  build-all  - Build for all platforms"
	@echo "  dev        - Development build"
