# Change: Alert on reconciliation failure

## Why

When reconciliation fails (git sync failure, dirty repo, compose exit 1, etc.), Bosun logs the error and returns 503 on `/health`, but does NOT send a failure alert via Discord or other providers. Successful deploys DO send alerts. This asymmetry caused a 7+ day outage to go unnoticed -- Bosun was stuck at 503, and only a manual health check discovered it.

Two bugs contribute to this:

1. **Missing alert calls on early pipeline failures.** The `syncRepo` failure path (the most common failure mode) and the lock acquisition failure path both return errors without calling `sendThrottledFailureAlert`. Only failures in decrypt, template, and deploy stages send alerts.

2. **`on_success`/`on_failure` config flags are dead config.** The spec defines these flags and `extractAlertConfig()` defaults `on_failure` to true, but neither the reconciler nor the daemon checks these flags before sending alerts. The success alert always fires; the failure alert fires whenever the alerter is wired (for stages that call it).

## What Changes

- Clarify the reconcile spec's Pipeline Orchestration requirement to mandate that most pipeline stage failures (including git sync) send throttled failure alerts; lock acquisition failures remain excluded as they are transient and lack state context
- Clarify the alerting spec's Reconciliation Lifecycle Alerts to state that the `on_failure` flag SHALL gate failure alert dispatch, and `on_success` SHALL gate success alert dispatch
- Add scenarios covering the pre-state-load git sync failure path which currently has no alert coverage

## Impact

- Affected specs: `alerting`, `reconcile`
- Affected code:
  - `internal/reconcile/reconcile.go` -- add alert calls to `syncRepo` and lock failure paths; pass `on_failure`/`on_success` config to reconciler
  - `internal/reconcile/reconcile.go` -- gate `sendSuccessAlert` on `on_success`, gate `sendThrottledFailureAlert` on `on_failure`
  - `internal/config/config.go` -- no changes needed (defaults are already correct)
  - `internal/daemon/daemon.go` -- pass alert config flags to reconciler
  - `internal/cmd/daemon.go` -- wire `on_failure`/`on_success` from alert config into reconciler config
- All consumers:
  - `internal/reconcile/reconcile.go:Run()` -- success alert path, 4 failure alert paths, 2 missing failure paths
  - `internal/daemon/daemon.go:executeReconcile()` -- calls `reconciler.Run()`, handles errors
  - `internal/cmd/daemon.go:createDaemonAlertManager()` -- creates alert manager, does not read config flags
  - `internal/cmd/alert.go:displayAlertStatus()` -- displays `on_success`/`on_failure` (read-only, no change needed)
  - `internal/config/config.go:extractAlertConfig()` -- sets defaults (already correct)
