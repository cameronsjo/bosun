## Context

Bosun's reconcile pipeline runs `docker compose up` (stage 9) and then performs post-deploy verification (stage 13) via drift detection. The verification logs warnings and sends alerts for unhealthy containers but never fails the deployment. For auth-ingress containers, this is insufficient -- a reverse proxy serving traffic without auth middleware is a security hole, not a warning.

The `VerifyContainerHealth` stub in `deploy.go` already shells out to `docker compose ps --format json` but discards the result. This proposal fills in that stub with Docker API-based health inspection and wires it into the pipeline as a rollback trigger.

## Goals / Non-Goals

- **Goals:**
  - Gate deployments on critical container health (configurable list)
  - Trigger rollback when any critical container is unhealthy or missing after compose up
  - Allow containers without healthcheck definitions to pass (can't gate on undefined checks)
  - Provide configurable timeout for containers that need startup time before health checks pass
  - Support both `bosun.yaml` config and env var override

- **Non-Goals:**
  - Runtime auth middleware verification (testing that authelia actually intercepts requests)
  - Adding healthcheck definitions to containers that lack them (user responsibility)
  - Changing existing post-deploy verification behavior for non-critical containers
  - Remote deploy support for the health gate (Docker API is local-only; remote deploys use SSH)

## Decisions

- **Docker API inspect over compose ps** -- `docker compose ps --format json` shells out and parses JSON. Docker SDK `ContainerInspect` is already used by `EnrichUnhealthyItems` in the drift system. Reusing the SDK is more reliable, testable (mock injection), and consistent with existing patterns.

- **Health gate runs before post-deploy verification** -- the health gate is a hard failure that triggers rollback. It runs after the startup grace period but before the drift-based post-deploy verification. Pipeline flow: compose up -> startup grace period -> health gate (fail = rollback) -> record state -> post-sync hooks -> drift verification (warn only).

- **Containers without healthcheck pass the gate** -- Docker reports health as empty string when no healthcheck is configured. The gate only fails on explicit "unhealthy" status or missing/non-running containers. This prevents the gate from blocking deployments when users haven't configured healthchecks yet.

- **Separate timeout from StartupGracePeriod** -- `StartupGracePeriod` (default 30s) controls the wait before drift verification. The health gate timeout (`BOSUN_HEALTH_GATE_TIMEOUT`, default 60s) controls how long to poll critical containers before declaring failure. The gate polls every 5 seconds during this window. The health gate runs after the startup grace period has elapsed, so the total wait is `StartupGracePeriod` + up to `HealthGateTimeout`.

- **Rollback reuses existing mechanism** -- `ComposeUpMultipleWithRollback` already handles rollback to backup compose files. The health gate failure triggers the same rollback path, returning `ErrRollbackSucceeded` or `ErrRollbackFailed`.

- **Config follows existing patterns** -- `critical_containers` in `bosun.yaml` follows the same pattern as `post_sync_hooks` and `deploy_paths`. `BOSUN_CRITICAL_CONTAINERS` env var (JSON array) completely replaces the config file value when set, consistent with `BOSUN_POST_SYNC_HOOKS` precedence behavior.

- **Config reloaded from repo** -- like `PostSyncHooks` and `DeployPaths`, `CriticalContainers` is reloaded from the repo's `bosun.yaml` after each git pull, unless the env var override is set. This means the critical container list can be managed in the same repo as the compose files.

## Risks / Trade-offs

- **False positive rollbacks** -- a container with a flaky healthcheck could cause unnecessary rollbacks. Mitigation: the polling timeout gives containers time to stabilize, and users control which containers are in the critical list.

- **Extended deploy time** -- the health gate adds up to `HealthGateTimeout` seconds to every deployment when critical containers are configured. Mitigation: the gate polls every 5 seconds and returns as soon as all critical containers are healthy, so the typical case adds only a few seconds.

- **Local-only limitation** -- Docker API inspect requires a local Docker socket. Remote deploys (SSH) cannot use this gate. Mitigation: document this limitation. Remote deploys can still use the existing drift-based verification for warnings.

- **Rollback may not help** -- if the new compose config is what caused authelia to be unhealthy, rolling back to the previous config is the right move. But if authelia is unhealthy for an unrelated reason (e.g., upstream LDAP down), rollback won't fix it and may revert unrelated changes. Mitigation: the failure alert includes which containers failed the health gate, giving the operator context to decide whether to force-deploy.

## Open Questions

None -- the design follows established patterns in the codebase and the insertion point (`VerifyContainerHealth`) is already identified.
