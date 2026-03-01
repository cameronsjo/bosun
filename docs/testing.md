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

## Coverage

Current coverage as of 2026-03-01 (sorted by coverage):

| Package | Coverage | Notes |
|---------|-------:|-------|
| ui | **100.0%** | Console + JSON mode, injectable `exitFn` |
| config | **98.4%** | Getters + `Load()` integration |
| alert | **97.6%** | Manager methods + provider HTTP |
| docker | **94.3%** | Compose runner injection + interface |
| manifest | 84.6% | Render + merge + interpolation |
| lock | 83.8% | File-based locking |
| daemon | 82.6% | Lifecycle, roundtrip, Docker API, drift loops |
| sentry | 79.4% | Integration setup |
| snapshot | 77.4% | Snapshot management |
| reconcile | 77.3% | Pure functions, deploy, drift, hooks, git |
| log | 75.8% | Init, format detection |
| fileutil | 75.5% | Copy, atomic write |
| preflight | 73.3% | Doctor checks |
| tunnel | 69.2% | Provider abstraction |
| cmd | 40.0% | Orchestrators — E2E territory |
| update | 0.0% | Self-update from GitHub — network-dependent |

*Higher coverage is better.*

### Coverage Targets

| Tier | Threshold | Packages |
|------|--------:|----------|
| Critical | 90%+ | ui, config, alert, docker |
| Core | 75%+ | manifest, daemon, reconcile, lock, sentry, snapshot, log, fileutil |
| Best effort | 60%+ | preflight, tunnel |
| E2E only | — | cmd, update |

### Testing Patterns

See `docs/field-reports/pure-function-extraction-sprint.md` for the full coverage sprint methodology: pure function extraction, agent teams, worktree isolation, and the functional core principle.

## Writing Tests

- Use table-driven tests for multiple scenarios
- Test behavior, not implementation
- Use testify/require for setup failures
- Use testify/assert for assertions

## Mocking

- **Docker**: `internal/docker/dockertest.MockDockerAPI` — injectable via `docker.NewClientWithAPI(mockAPI)`
- **Exit functions**: `var exitFn = os.Exit` pattern for Fatal-like functions
- **Command runners**: `commandRunner` injection on structs that shell out
- **Endpoints**: Injectable URL fields on HTTP provider structs
