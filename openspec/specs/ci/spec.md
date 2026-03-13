# CI Specification

## Purpose

The CI system provides a Dagger-based pipeline that implements test, lint, build, and release stages as Go code. This enables identical execution locally and in GitHub Actions, with content-addressed caching for performance.

## Requirements

### Requirement: Dagger CI Pipeline

The system SHALL provide a Dagger module that implements CI/CD pipelines as Go code, enabling local execution and CI platform portability.

#### Scenario: Local CI execution

- **WHEN** a developer runs `dagger call ci --source .` from the project root
- **THEN** the system executes test, lint, and build stages in containers
- **AND** produces build artifacts for all target platforms (linux/darwin × amd64/arm64)

#### Scenario: GitHub Actions CI execution

- **WHEN** a push or pull request targets the main branch
- **THEN** the CI workflow installs Dagger and calls the CI function
- **AND** the pipeline executes identically to local runs

### Requirement: Test Function

The system SHALL provide a `test` Dagger function that runs the Go test suite with race detection and coverage.

#### Scenario: Running tests

- **WHEN** `dagger call test --source .` is executed
- **THEN** the system runs `go test -v -race -coverprofile=coverage.out ./...`
- **AND** returns the container with test results

#### Scenario: Test failure handling

- **WHEN** any test fails
- **THEN** the Dagger function returns a non-zero exit code
- **AND** the CI pipeline fails

### Requirement: Lint Function

The system SHALL provide a `lint` Dagger function that runs golangci-lint.

#### Scenario: Running linter

- **WHEN** `dagger call lint --source .` is executed
- **THEN** the system runs golangci-lint with project configuration
- **AND** reports any linting violations

#### Scenario: Lint failure handling

- **WHEN** linting finds violations
- **THEN** the Dagger function returns a non-zero exit code
- **AND** the CI pipeline fails

### Requirement: Build Function

The system SHALL provide a `build` Dagger function that creates binaries for all target platforms.

#### Scenario: Multi-platform build

- **WHEN** `dagger call build --source .` is executed
- **THEN** the system builds binaries for linux-amd64, linux-arm64, darwin-amd64, darwin-arm64
- **AND** applies ldflags for version, commit, and date injection
- **AND** returns a directory containing all binaries

### Requirement: Release Function

The system SHALL provide a `release` Dagger function that executes GoReleaser for creating releases.

#### Scenario: Creating a release

- **WHEN** `dagger call release --source . --github-token env:GITHUB_TOKEN` is executed
- **THEN** the system runs GoReleaser with the existing `.goreleaser.yaml` configuration
- **AND** publishes release artifacts to GitHub

### Requirement: Makefile CI Target

The system SHALL provide a `make ci` target for convenient local CI execution.

#### Scenario: Running CI via Make

- **WHEN** a developer runs `make ci` from the project root
- **THEN** the system executes `dagger call ci --source .`
- **AND** reports success or failure

### Requirement: Dagger Caching

The system SHALL leverage Dagger's content-addressed caching for build performance.

#### Scenario: Cached dependency resolution

- **WHEN** the CI pipeline runs with unchanged go.mod/go.sum
- **THEN** Go module downloads are retrieved from Dagger cache
- **AND** subsequent runs complete faster than initial runs

#### Scenario: Cache invalidation

- **WHEN** source files change
- **THEN** only affected pipeline stages re-execute
- **AND** unchanged stages use cached results
