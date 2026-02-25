## 1. Foundation (log package)

- [x] 1.1 Add `ComponentCtx(ctx context.Context, component string) zerolog.Logger` to `internal/log/context.go`
- [x] 1.2 Add `ComponentTunnel = "tunnel"` constant to `internal/log/fields.go`
- [x] 1.3 Add godoc to `Component()` and `ComponentCtx()` clarifying when to use each
- [x] 1.4 Write tests for `ComponentCtx` — verify component field present, correlation IDs inherited from context, empty context fallback

## 2. Pipeline entry point enrichment

- [x] 2.1 `daemon.TriggerReconcile()` — after `NewReconcileContext`, build enriched logger and stash on ctx via `log.WithContext`
- [x] 2.2 `daemon.loggingMiddleware()` — after request_id is on context, build enriched logger and stash on ctx
- [x] 2.3 `reconcile.Run()` — switch from `log.Component()` to `log.ComponentCtx(ctx, ...)`, add story logging (start/success/failure with duration_ms)

## 3. Reconcile sub-operation migration

- [x] 3.1 `reconcile/git.go` — migrate 4 `log.Component()` calls to `log.ComponentCtx(ctx, ...)`
- [x] 3.2 `reconcile/deploy.go` — migrate 5 `log.Component()` calls to `log.ComponentCtx(ctx, ...)`
- [x] 3.3 `reconcile/hooks.go` — migrate 1 `log.Component()` call to `log.ComponentCtx(ctx, ...)`
- [x] 3.4 `reconcile/drift.go` — migrate 2 `log.Component()` calls to `log.ComponentCtx(ctx, ...)`
- [x] 3.5 `reconcile/reconcile.go` — migrate remaining `log.Component()` calls in helper methods (executePostSyncHooks, retryWithBackoff, etc.)
- [x] 3.6 Add warn-level retry logging to `retryWithBackoff` (attempt number, max, backoff, previous error)

## 4. Daemon API and webhook logging

- [x] 4.1 `daemon/api.go` — add structured logging to all handlers (start + outcome for each)
- [x] 4.2 `daemon/server.go` — verify loggingMiddleware stashes enriched logger (task 2.2)
- [x] 4.3 `daemon/daemon.go` — migrate `log.Component()` calls to `log.ComponentCtx(ctx, ...)` where ctx is available
- [x] 4.4 Log TCP auth failures at warn level with source IP (security-relevant)

## 5. Tunnel provider logging

- [x] 5.1 `tunnel/cloudflare.go` — add story logging to all subprocess/HTTP calls, capture stderr at debug level
- [x] 5.2 `tunnel/tailscale.go` — add story logging to all subprocess calls, capture stderr at debug level

## 6. Remaining consumers (where ctx is available)

- [x] 6.1 `docker/client.go` — audit 6 call sites, migrate those that receive ctx
- [x] 6.2 `manifest/render.go` — audit 2 call sites, migrate those that receive ctx

## 7. Verification

- [x] 7.1 Run `make test` — all existing tests pass
- [x] 7.2 Verify with `BOSUN_LOG_FORMAT=json BOSUN_LOG_LEVEL=debug` that reconcile_id appears in sub-operation logs
- [x] 7.3 Verify daemon API handlers produce structured log output
- [x] 7.4 Verify tunnel provider subprocess output appears at debug level
