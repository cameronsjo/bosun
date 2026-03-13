# Change: Adopt context-logger pattern across reconcile, daemon, and tunnel

## Why

The logging infrastructure has context propagation plumbing (`FromContext`, `WithContext`, `NewReconcileContext`) that zero call sites use. Every sub-operation calls `log.Component()` which creates a standalone logger without correlation IDs. This breaks request-to-reconcile tracing and makes daemon API handlers, tunnel providers, and reconcile sub-operations invisible in structured logs.

## What Changes

- Add `log.ComponentCtx(ctx, component)` that returns a logger with correlation IDs from context plus the component field
- Change pipeline entry points (daemon.TriggerReconcile, reconcile.Run, HTTP middleware) to build enriched loggers and stash them on context via `log.WithContext`
- Migrate all `log.Component()` call sites to `log.ComponentCtx(ctx, ...)` where context is available
- Add story logging (start/success/failure with duration) to reconcile.Run(), daemon API handlers, webhook processing, and tunnel provider subprocess calls
- Capture subprocess stderr at debug level for tunnel providers
- Add `ComponentTunnel` constant to log package

## Impact

- Affected specs: `observability`
- Affected code:
  - `internal/log/context.go` — add `ComponentCtx()`
  - `internal/log/fields.go` — add `ComponentTunnel` constant
  - `internal/reconcile/reconcile.go` — enrich context in Run(), migrate to ComponentCtx
  - `internal/reconcile/git.go` — migrate 4 call sites
  - `internal/reconcile/deploy.go` — migrate 5 call sites
  - `internal/reconcile/hooks.go` — migrate 1 call site
  - `internal/reconcile/drift.go` — migrate 2 call sites
  - `internal/daemon/daemon.go` — enrich context in TriggerReconcile(), migrate 6 call sites
  - `internal/daemon/api.go` — add handler logging (currently zero)
  - `internal/daemon/server.go` — enrich context in loggingMiddleware
  - `internal/tunnel/cloudflare.go` — add story logging + stderr capture
  - `internal/tunnel/tailscale.go` — add story logging + stderr capture
  - `internal/docker/client.go` — migrate 6 call sites (where ctx available)
  - `internal/manifest/render.go` — migrate 2 call sites (where ctx available)
- All consumers of `log.Component()`: 30 call sites across 10 files (see grep above)
- All consumers of `log.FromContext()`: 0 call sites (never adopted)
