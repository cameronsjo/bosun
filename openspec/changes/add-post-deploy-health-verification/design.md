## Context

Bosun's reconcile pipeline runs `docker compose up -d` and considers the deploy "done" after the process exits. The existing `verifyPostDeploy` waits a fixed `StartupGracePeriod` (30s), does a single drift check, and logs warnings — but the reconciliation always reports success regardless of container health.

GitHub #118 and the March 2026 cascade failure (2,238 restart cycles on gitea-backup) demonstrated that this gap allows silent failures to persist indefinitely.

## Goals / Non-Goals

- **Goal**: Close the deploy→verify loop so Bosun knows whether containers are actually healthy after a deploy
- **Goal**: Make verification configurable — operators with large stacks can increase the timeout
- **Non-Goal**: Automatic rollback on health failure (that's a future capability; this change fails the deploy and lets the circuit breaker + alerts handle it)
- **Non-Goal**: Restart circuit breaker (bosun-0ht, depends on this change)
- **Non-Goal**: Deploy status webhooks (bosun-b0o, depends on this change)

## Decisions

### Decision: Polling is the default behavior

The `StartupGracePeriod` (30s fixed sleep) is replaced by a poll loop. Default timeout is 60s, default interval is 5s. This is enabled out of the box — no opt-in required.

Setting `BOSUN_HEALTH_CHECK_TIMEOUT=0` disables verification entirely. No separate `ENABLED` toggle — one knob is sufficient.

**Why**: A fixed sleep is wasteful for fast starts and insufficient for slow starts. Polling adapts to actual container behavior.

**Alternatives considered**:
- **`docker compose up --wait`**: Rejected. It exits non-zero when *any* container is unhealthy, including pre-existing ones unrelated to the current deploy. Already documented in codebase (deploy.go:900-902).
- **Docker events stream**: More complex, requires a persistent connection, harder to test. Polling `CollectActualState` is simpler and reuses existing infrastructure.
- **Separate ENABLED toggle**: Rejected. Creates a 2x2 config matrix that someone will misconfigure. `timeout=0` is clear enough.

### Decision: Health failures count toward the circuit breaker

When health verification times out, the pipeline treats it as a deployment failure — same as a compose up failure. The circuit breaker increments, alerts fire per the throttle schedule.

**Why**: The circuit breaker's job is "stop retrying the same broken commit." A commit whose containers fail health checks 3 times is broken. The operator fix is the same: push a new commit or `bosun trigger -f`. Making health failures a softer category adds complexity for no safety gain.

### Decision: Verification failure does NOT trigger rollback

When health verification times out, `verifyPostDeploy` returns an error but rollback is NOT attempted. The containers are running (just unhealthy), and rolling back to previous compose files could make things worse.

**Why**: Unhealthy ≠ stopped. A service failing health checks may still be serving requests (slow startup, flaky check, database migration in progress). Rolling back could kill in-flight work. Automatic rollback is a separate capability (bosun-18m) that needs operator opt-in.

### Decision: Containers without HEALTHCHECK are considered healthy once running

If a container has no Docker `HEALTHCHECK` directive, it's treated as healthy as soon as its state is "running". Only containers with an explicit healthcheck can cause a verification failure.

**Why**: Most homelab services don't have healthchecks. The polling loop only blocks on containers that have explicit health checks. Operators progressively add healthchecks to services they want verified.

### Decision: Add lightweight health verdict to deploy state

Add `health_verified_at` timestamp and `health_verification_passed` boolean to the deploy state file. The drift items already capture detail — the verdict fields let `bosun status` show "last deploy: health verified ✓" without parsing drift items.

## Risks / Trade-offs

- **Risk**: Polling adds latency to every deploy (up to 60s if containers are slow to become healthy)
  - **Mitigation**: Early exit on all-healthy. Default 5s interval means fast stacks add <10s
- **Risk**: Services with flaky health checks could block deploys
  - **Mitigation**: Configurable timeout; operators can increase or set to 0 to disable
- **Risk**: Breaking change for operators with accepted-unhealthy containers
  - **Mitigation**: Single known operator (Cameron). Set `BOSUN_HEALTH_CHECK_TIMEOUT=0` to restore old behavior
