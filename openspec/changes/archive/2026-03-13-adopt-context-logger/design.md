# Adopt Context-Aware Logger

## Context

Bosun's `internal/log` package has two logger construction paths:

1. `log.Component(name)` — creates a logger with a component field but no correlation IDs
2. `log.FromContext(ctx)` — creates a logger with correlation IDs but no component field

Every call site in the codebase uses path 1. Path 2 has zero callers. The correlation ID infrastructure (reconcile_id, request_id) exists on context but is never surfaced in logs from sub-operations.

## Goals / Non-Goals

- **Goal:** Every structured log line from a reconcile run carries reconcile_id without explicit per-callsite wiring
- **Goal:** Every structured log line from a daemon API request carries request_id
- **Goal:** Tunnel provider subprocess calls are visible in logs with start/success/failure + duration
- **Goal:** Daemon API handlers produce structured logs for all operations
- **Non-Goal:** Adding new correlation IDs (deploy_id, stack_name) — the pattern enables this later
- **Non-Goal:** Removing `log.Component()` — it remains valid for code paths without context (CLI commands, init)
- **Non-Goal:** Adding OpenTelemetry tracing spans — this is structured logging only

## Decisions

### Decision: Single convenience function over chainable builder

Add `log.ComponentCtx(ctx, component) zerolog.Logger` rather than a fluent builder API.

**Why:** One function that combines the two existing paths (component + context). Returns a standard `zerolog.Logger` that callers can chain with `.With()` for additional fields. Composable without API surface growth.

**Alternatives considered:**
- Fluent builder (`log.FromContext(ctx).WithComponent("git").Build()`) — more API surface, no real benefit since zerolog already supports chaining
- Pass logger as function parameter — breaks Go context convention, changes all signatures

### Decision: Enrich-and-stash at pipeline entry points

At each pipeline boundary (webhook receipt, reconcile trigger, tunnel operation), build a logger with all known context fields and stash it on context via `logger.WithContext(ctx)`.

**Why:** Zerolog's `WithContext`/`Ctx` is the designed propagation mechanism. Enriching once means sub-operations don't need to know which fields to extract — they just call `ComponentCtx` and get everything.

**Entry points:**
- `daemon.TriggerReconcile()` — stash logger with reconcile_id + source
- `daemon.loggingMiddleware()` — already has request_id, stash the enriched logger
- `reconcile.Run()` — read enriched logger from context, verify reconcile_id present
- Tunnel provider methods — stash logger with component=tunnel + operation

### Decision: Subprocess stderr at debug level for tunnels

Capture stdout+stderr from `exec.CommandContext` calls in tunnel providers. Log stderr at debug level on success, at error level on failure.

**Why:** Tunnel issues are the hardest to diagnose because the subprocess output is currently discarded. Debug-level capture is free in production (filtered out) and invaluable when troubleshooting.

### Decision: Migrate incrementally, not all-at-once

Migrate call sites that receive `context.Context` to `ComponentCtx`. Leave call sites without context (CLI commands, package init) on `log.Component()`.

**Why:** Some code paths genuinely don't have context (e.g., `internal/docker/client.go` methods that don't take ctx). Forcing context everywhere would be a larger refactor with no correlation benefit.

## Risks / Trade-offs

- **Risk:** Stale logger on context if a sub-operation enriches context but doesn't propagate it back up
  - **Mitigation:** Enrichment only happens at pipeline entry points, not in sub-operations. Sub-operations read, never write.
- **Risk:** `log.Component()` and `log.ComponentCtx()` coexist, creating confusion about which to use
  - **Mitigation:** Convention is clear: use `ComponentCtx` when you have ctx, `Component` when you don't. Document in log package godoc.

## Open Questions

None — design decisions are resolved.
