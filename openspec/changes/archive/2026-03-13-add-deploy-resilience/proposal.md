# Change: Improve deployment resilience

## Why

A single unhealthy container blocked ALL deployments for 7+ days with no notification.
The cascade: `docker compose up --wait` exits 1 on unhealthy containers, Bosun treats this
as a deploy failure, the circuit breaker trips after 3 attempts, and the circuit breaker
path skips failure alerting entirely. The only way to discover the outage was manual log inspection.

Separately, Traefik doesn't pick up route changes after Bosun syncs config files because
Unraid's FUSE mount breaks inotify. This requires manual container restarts after every deploy.

## What Changes

1. **Compose up tolerates unhealthy containers** (Fixes #37)
   - Remove `--wait` from `docker compose up`
   - Perform our own post-deploy health inspection via Docker SDK
   - Distinguish "failed to start" (deploy failure) from "running but unhealthy" (warning + alert)

2. **Alert on all failure paths** (Fixes #39)
   - Add failure alert to circuit breaker path (currently skips alerting)
   - Add alert throttling: notify on first failure, suppress until recovery or every Nth failure
   - Add unhealthy container alert from post-deploy verification

3. **Post-sync container restart hooks** (Fixes #38)
   - Generic config-driven mechanism: "when files in path X change, restart container Y"
   - Solves Traefik inotify issue and generalizes to any file-watching container

## Impact

- Affected specs: reconcile (modified), alerting (modified)
- Affected code:
  - `internal/reconcile/deploy.go` - Remove `--wait`, add health inspection
  - `internal/reconcile/reconcile.go` - Circuit breaker alerting, post-deploy alert on unhealthy
  - `internal/reconcile/hooks.go` - New file, post-sync restart hooks
  - `internal/alert/alert.go` - Throttling, unhealthy container alert method
  - `internal/daemon/daemon.go` - Hook configuration, restart execution
  - `internal/config/config.go` - Post-sync hook config schema
