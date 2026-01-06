# Design: Structured Logging

## Context

Bosun runs in two modes:
1. **CLI mode** - Interactive commands (doctor, status, yacht) where users expect colored, human-readable output
2. **Daemon mode** - Long-running process handling webhooks and reconciliation where logs need machine parsing

The logging system must serve both contexts while maintaining a consistent API.

## Goals

- Single logging API used throughout the codebase
- Automatic format switching based on context (CLI vs daemon)
- Structured fields for all log entries
- Minimal performance overhead
- Easy migration from current `ui.Info/Error/Success` calls

## Non-Goals

- OpenTelemetry integration (future proposal)
- Log shipping configuration
- Log rotation (handled externally)

## Decisions

### Decision 1: Use zerolog

**Choice**: zerolog over slog or zap

**Rationale**:
- Zero-allocation JSON encoding (best performance)
- Built-in `ConsoleWriter` for pretty CLI output
- Simpler API than zap
- Context propagation via `zerolog.Ctx(ctx)`
- Active maintenance, widely adopted

**Alternatives considered**:
- `log/slog` - Good stdlib option but less feature-rich, no built-in pretty printer
- `zap` - More configuration complexity, larger dependency

### Decision 2: Package Structure

```
internal/log/
├── log.go       # Package-level logger, Init(), global functions
├── context.go   # Context helpers (WithRequestID, FromContext)
└── fields.go    # Common field constants and builders
```

### Decision 3: Output Format Detection

```go
// Automatic detection based on:
// 1. BOSUN_LOG_FORMAT env var (explicit override)
// 2. IsTerminal check (tty = console, pipe = json)
// 3. Running as daemon = json

func Init(opts ...Option) {
    format := detectFormat()
    // ...
}
```

### Decision 4: Preserve ui Package for CLI

Keep `ui.Success`, `ui.Error`, etc. as thin wrappers:

```go
// internal/ui/color.go
func Success(format string, args ...any) {
    log.Info().Msgf(format, args...)
    // Console writer handles the checkmark/color
}
```

This preserves the CLI experience while adding structure.

### Decision 5: Structured Field Standards

Standard fields across all log entries:

| Field | Type | Description |
|-------|------|-------------|
| `component` | string | Package/subsystem (daemon, reconcile, git) |
| `request_id` | string | HTTP request correlation ID |
| `reconcile_id` | string | Reconcile run correlation ID |
| `source` | string | Trigger source (webhook, poll, manual) |
| `duration_ms` | int64 | Operation duration |
| `error` | string | Error message (when applicable) |

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Breaking existing output parsing | Console mode preserves current format |
| Performance overhead | zerolog is zero-allocation |
| Migration effort | Phased approach, ui package first |

## Migration Plan

1. Add `internal/log` package with zerolog
2. Update `internal/ui` to use structured logger
3. Add context propagation to daemon/reconcile
4. Migrate direct `ui.*` calls in packages to use context-aware logging
5. Add log level configuration to CLI flags

## Open Questions

None - requirements are clear.
