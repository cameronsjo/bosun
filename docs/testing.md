# Testing Strategy

## Overview

Bosun uses Go's standard testing framework with testify for assertions.

## Running Tests

```bash
go test ./...                    # Run all tests
go test ./... -cover             # With coverage
go test ./... -race              # With race detection
go test -v ./internal/manifest/  # Verbose, specific package
```

## Test Organization

- Unit tests: `*_test.go` alongside source files
- Test helpers: `test_helpers_test.go` in cmd package
- Mocks: `mock_test.go` in docker package

## Coverage Expectations

| Package | Target |
|---------|--------|
| manifest | >90% |
| docker | >70% |
| reconcile | >60% |
| cmd | >50% |

## Writing Tests

- Use table-driven tests for multiple scenarios
- Test behavior, not implementation
- Use testify/require for setup failures
- Use testify/assert for assertions

## Mocking

The docker package provides MockDockerAPI for testing container operations without a real Docker daemon.
