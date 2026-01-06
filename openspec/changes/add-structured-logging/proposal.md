# Change: Add Structured Logging

## Why

Bosun currently uses human-readable console output via the `ui` package. While this works for CLI usage, it creates observability gaps:

- Logs cannot be parsed by log aggregation tools (Loki, Splunk, etc.)
- No log levels for filtering in production
- No correlation IDs for tracing requests through the daemon
- No way to switch between human-friendly CLI output and machine-parseable daemon logs

Structured logging is a foundational requirement for production observability.

## What Changes

- **New `internal/log` package** using zerolog for structured logging
- **Dual output modes**: Pretty console output (CLI) vs JSON (daemon/production)
- **Log levels**: DEBUG, INFO, WARN, ERROR with runtime configuration
- **Context propagation**: Request IDs, reconcile run IDs, source tracking
- **Refactor `internal/ui`**: Thin wrapper around structured logger for CLI commands
- **Environment-based configuration**: `BOSUN_LOG_FORMAT` (json/console), `BOSUN_LOG_LEVEL`

### Not Included (Future Work)

- OpenTelemetry tracing integration (separate proposal)
- Log shipping/aggregation configuration
- Metrics collection

## Impact

- Affected specs: `specs/observability/` (new)
- Affected code:
  - `internal/log/` (new package)
  - `internal/ui/color.go` (refactor to use structured logger)
  - `internal/daemon/` (add request context, structured logging)
  - `internal/reconcile/` (add run context, structured logging)
  - `internal/cmd/` (configure logger on startup)
  - `go.mod` (add zerolog dependency)
