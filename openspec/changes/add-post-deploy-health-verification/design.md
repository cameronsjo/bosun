## Context

Bosun's reconcile pipeline runs `docker compose up -d` and considers the deploy "done" after the process exits. The existing `verifyPostDeploy` waits a fixed `StartupGracePeriod` (30s), does a single drift check, and logs warnings — but the reconciliation always reports success regardless of container health.

GitHub #118 and the March 2026 cascade failure (2,238 restart cycles on gitea-backup) demonstrated that this gap allows silent failures to persist indefinitely.

## Goals / Non-Goals

- **Goal**: Close the deploy→verify loop so Bosun knows whether containers are actually healthy after a deploy
- **Goal**: Make verification configurable — operators with large stacks can increase the timeout
- **Goal**: Preserve backwards compatibility — operators who don't set health checks get current behavior
- **Non-Goal**: Automatic rollback on health failure (that's a future capability; this change fails the deploy and lets the circuit breaker + alerts handle it)
- **Non-Goal**: Restart circuit breaker (bosun-0ht, depends on this change)
- **Non-Goal**: Deploy status webhooks (bosun-b0o, depends on this change)

## Decisions

### Decision: Polling loop replaces static grace period

The `StartupGracePeriod` (30s fixed sleep) is replaced by a poll loop with configurable timeout and interval. The loop checks every `HealthCheckInterval` (5s default) until all containers are healthy or `HealthCheckTimeout` (60s default) expires.

**Why**: A fixed sleep is wasteful for fast starts and insufficient for slow starts. Polling adapts to actual container behavior.

**Alternatives considered**:
- **`docker compose up --wait`**: Rejected. It exits non-zero when *any* container is unhealthy, including pre-existing ones unrelated to the current deploy. This is already documented in the codebase (deploy.go:900-902).
- **Docker events stream**: More complex, requires a persistent connection, harder to test. Polling `CollectActualState` is simpler and reuses existing infrastructure.

### Decision: Verification failure returns an error but does NOT trigger rollback

When health verification times out, `verifyPostDeploy` returns an error. The pipeline treats this as a deployment failure (failure alert, circuit breaker increment). However, **rollback is not attempted** — the containers are running (just unhealthy), and rolling back to previous compose files could make things worse.

**Why**: Rolling back an unhealthy-but-running service could destroy in-flight connections or database migrations. The safer path is: fail the deploy, alert the operator, let the circuit breaker prevent re-deploys of the same commit.

**Alternative considered**: Automatic rollback. Rejected because unhealthy ≠ stopped — the service may be partially functioning. Rollback is a separate capability (bosun-18m) that needs operator opt-in.

### Decision: Containers without HEALTHCHECK are considered healthy once running

If a container has no Docker `HEALTHCHECK` directive, it's treated as healthy as soon as its state is "running". Only containers with an explicit healthcheck can cause a verification failure.

**Why**: Most homelab services don't have healthchecks. Requiring them would be a breaking change. Operators can progressively add healthchecks to services they want verified.

### Decision: StartupGracePeriod is deprecated but still respected

If `StartupGracePeriod` is set and `HealthCheckTimeout` is zero (not configured), the old behavior is preserved — fixed sleep + single-shot check + warn-only. When `HealthCheckTimeout` is explicitly set, it takes precedence and the grace period is ignored.

**Why**: Backwards compatibility. Existing deployments that haven't configured the new env vars get the same behavior they have today.

## Risks / Trade-offs

- **Risk**: Polling adds latency to every deploy (up to 60s if containers are slow to become healthy)
  - **Mitigation**: Early exit on all-healthy. Default 5s interval means fast stacks add <10s
- **Risk**: Services with flaky health checks could block deploys
  - **Mitigation**: Configurable timeout; operators can increase or disable per-deployment
- **Risk**: First-time deploys with no healthchecks may behave differently
  - **Mitigation**: No-healthcheck containers are instantly "healthy" — no behavior change

## Open Questions

1. Should there be a `BOSUN_HEALTH_CHECK_ENABLED` toggle, or is setting timeout to 0 sufficient to disable?
2. Should the health verification result be included in the deploy state file (beyond the drift items already stored)?
