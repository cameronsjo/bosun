# Tasks: Add Structured Logging

## 1. Foundation

- [x] 1.1 Add zerolog dependency to go.mod
- [x] 1.2 Create `internal/log/log.go` with Init(), global logger, format detection
- [x] 1.3 Create `internal/log/context.go` with context helpers
- [x] 1.4 Create `internal/log/fields.go` with standard field constants
- [x] 1.5 Write unit tests for log package

## 2. UI Package Migration

- [x] 2.1 Refactor `internal/ui/color.go` to use structured logger internally
- [x] 2.2 Configure ConsoleWriter for CLI-friendly output (icons, colors)
- [x] 2.3 Preserve existing ui.Success/Error/Info/Warning API
- [x] 2.4 Update ui tests

## 3. Daemon Integration

- [x] 3.1 Initialize JSON logger in daemon startup
- [x] 3.2 Add request ID middleware to HTTP server
- [x] 3.3 Add request context to webhook handlers
- [x] 3.4 Update daemon logging calls to use structured fields

## 4. Reconcile Integration

- [x] 4.1 Add reconcile run ID generation
- [x] 4.2 Pass context through reconcile workflow
- [x] 4.3 Add structured fields to reconcile steps (git, sops, template, deploy)
- [x] 4.4 Log durations for each reconcile phase

## 5. CLI Configuration

- [x] 5.1 Add `--log-level` global flag to root command
- [x] 5.2 Add `--log-format` global flag (json/console/auto)
- [x] 5.3 Add BOSUN_LOG_LEVEL and BOSUN_LOG_FORMAT env var support
- [x] 5.4 Document logging configuration in README

## 6. Package Migration

- [x] 6.1 Update `internal/docker` to use structured logging
- [x] 6.2 Update `internal/manifest` to use structured logging
- [x] 6.3 Update `internal/alert` to use structured logging (no logging needed)
- [x] 6.4 Update `internal/cmd` commands to use structured logging (no logging needed)

## 7. Validation

- [x] 7.1 Run full test suite
- [x] 7.2 Manual test: CLI commands show colored output
- [x] 7.3 Manual test: Daemon outputs JSON logs
- [x] 7.4 Manual test: Log level filtering works
