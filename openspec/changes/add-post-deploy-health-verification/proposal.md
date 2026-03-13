# Change: Add post-deploy health verification with polling

## Why

After `docker compose up -d`, Bosun has no way to confirm containers actually started healthy. The existing `verifyPostDeploy` does a single-shot drift check after a fixed 30s grace period — it warns but never fails. During the March 2026 cascade failure, this allowed gitea-backup to hit 2,238 restart cycles and norish to hit 477 before anyone noticed. The deploy→verify loop is open.

## What Changes

- Replace the static `StartupGracePeriod` sleep + single-shot check in `verifyPostDeploy` with a **polling loop** that repeatedly checks container health until all declared services are healthy or a timeout expires
- When the timeout expires with unhealthy/restarting containers, the deploy **fails** (returns an error) instead of just logging a warning
- Add configurable `BOSUN_HEALTH_CHECK_TIMEOUT` (default 60s) and `BOSUN_HEALTH_CHECK_INTERVAL` (default 5s) env vars
- Deprecate `StartupGracePeriod` — the polling loop subsumes its purpose
- Containers without a `HEALTHCHECK` are considered healthy once running (current behavior preserved)

## Impact

- Affected specs: `reconcile` (Post-Deploy Verification requirement)
- Affected code:
  - `internal/reconcile/reconcile.go` — `verifyPostDeploy` rewritten to poll, returns error
  - `internal/reconcile/reconcile.go:466-469` — caller must handle error from `verifyPostDeploy`
  - `internal/reconcile/reconcile.go:32-98` — `Config` gains `HealthCheckTimeout` and `HealthCheckInterval` fields
  - `internal/reconcile/drift.go` — `CollectActualState` and `CompareDrift` unchanged (reused as-is)
  - `internal/daemon/daemon.go` — parse `BOSUN_HEALTH_CHECK_TIMEOUT` and `BOSUN_HEALTH_CHECK_INTERVAL`
  - `internal/daemon/daemon_test.go` — env var parsing tests
  - `internal/reconcile/reconcile_test.go` — `TestVerifyPostDeploy` rewritten for polling
- All consumers of `verifyPostDeploy`:
  - `internal/reconcile/reconcile.go:468` — only call site (in `Run` pipeline)
- All consumers of `StartupGracePeriod`:
  - `internal/reconcile/reconcile.go:613` — grace period sleep in `verifyPostDeploy`
  - `internal/reconcile/reconcile.go:150` — default value in `DefaultConfig()`
  - `internal/daemon/daemon.go` — not currently exposed as env var (only set in DefaultConfig)
