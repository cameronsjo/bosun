# Change: Add critical container health gate to post-deploy verification

## Why

A partial deploy where traefik is up but authelia is down is a silent security hole -- an open reverse proxy without auth middleware. The current post-deploy verification (stage 13) detects unhealthy containers and sends alerts, but does not fail the deployment or trigger rollback. For auth-ingress containers (traefik, authelia, tailscale-gateway), an unhealthy state is not a warning -- it is a security-critical failure that demands rollback.

The `VerifyContainerHealth` stub in `deploy.go` already exists as the intended insertion point but only runs `docker compose ps` without inspecting results.

## What Changes

- **Critical container health gate** -- configurable list of containers that MUST be healthy (or have no healthcheck defined) after compose up. If any critical container is unhealthy or missing, the reconciler triggers rollback
- **Config surface** -- `critical_containers` list in `bosun.yaml` + `BOSUN_CRITICAL_CONTAINERS` env var (JSON array, replaces config when set)
- **Health check method** -- Docker API container inspect (not compose ps) for reliable health status
- **Timeout/retry** -- configurable health check timeout with polling retries before declaring failure, separate from the existing `StartupGracePeriod`
- **Backwards compatible** -- empty `critical_containers` list (default) means no health gate; existing behavior unchanged

## Impact

- Affected specs: reconcile (modified)
- Affected code:
  - `internal/reconcile/deploy.go` -- fill in `VerifyContainerHealth` stub with Docker API inspect
  - `internal/reconcile/reconcile.go` -- wire health gate into pipeline between compose up and state save; trigger rollback on failure
  - `internal/reconcile/drift.go` -- reuse `CollectActualState` and health inspection patterns
  - `internal/config/config.go` -- add `critical_containers` field to `configFile` struct, `extractCriticalContainers()`, `CriticalContainers()` getter
  - `internal/daemon/daemon.go` -- parse `BOSUN_CRITICAL_CONTAINERS` env var
- All consumers:
  - `internal/reconcile/reconcile.go:Run()` -- pipeline orchestrator (primary consumer)
  - `internal/daemon/daemon.go:ConfigFromEnv()` -- env var parsing
  - `internal/config/config.go:loadConfigFile()` -- YAML config parsing
  - `internal/config/config.go:loadConfigDir()` -- directory-based config parsing
