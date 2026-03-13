## Context

Bosun's reconciliation pipeline has three resilience gaps exposed by the obsidian healthcheck incident:

1. `docker compose up -d --remove-orphans --wait` (deploy.go:714) waits for healthchecks
   and exits non-zero if ANY container is unhealthy. This treats a pre-existing broken
   healthcheck as a deploy failure, blocking all future deployments.

2. The circuit breaker (reconcile.go:240-250) returns an error but never calls
   `sendFailureAlert()`, creating a silent failure path. All other failure points
   (decrypt, template, deploy) correctly alert.

3. Traefik's file provider watches via inotify, but Unraid's FUSE mount doesn't emit
   inotify events for sed-i/rename operations. Bosun needs to restart affected containers
   after syncing their config files.

## Goals / Non-Goals

- Goals:
  - Unhealthy containers don't block deployments of other services
  - Every failure path sends a notification (no silent failures)
  - Config file changes automatically restart watching containers
  - Alert throttling prevents notification spam during extended outages

- Non-Goals:
  - Automatic healthcheck remediation (restarting unhealthy containers)
  - Per-service compose up (targeted deploys) - future improvement
  - Replacing Docker Compose healthchecks with Bosun-native checks

## Decisions

### D1: Remove `--wait`, inspect health ourselves

**Decision:** Remove `--wait` from `docker compose up` and use the Docker SDK to inspect
container health status after deployment.

**Rationale:** `--wait` conflates "container failed to start" with "container running but
unhealthy." By removing it, `compose up` only fails on actual startup failures (image pull
errors, port conflicts, OOM). We then use the existing Docker client to check health status
separately, matching the pattern already used in `verifyPostDeploy()` and drift detection.

**Alternatives considered:**
- Parse compose stderr to distinguish failure types - brittle, output format is unstable
- Use `--wait-timeout` with short timeout - still exits non-zero on pre-existing unhealthy containers
- Per-service `compose up --no-deps` - larger change, saves for future work

### D2: Alert throttling with exponential backoff

**Decision:** Alert on first failure, then on attempts 3, 10, 30 (roughly exponential).
Always alert on recovery (success after failure).

**Rationale:** Without throttling, a commit stuck in the circuit breaker could generate alerts
every poll interval (5 min default = 288 alerts/day). But zero alerts is worse (current state).
Exponential spacing surfaces the issue without drowning the channel.

**Implementation:** Track `LastAlertedAttempt` in deploy state file. Compare against current
`AttemptCount` to decide whether to alert.

### D3: Generic post-sync hooks via config

**Decision:** Add a `post_sync_hooks` config section that maps file path globs to container
restart actions.

**Rationale:** While this is motivated by Traefik, the pattern applies to any container that
watches config files (Prometheus, Grafana, Gatus, nginx). A generic mechanism avoids
special-casing each tool.

**Config schema:**
```yaml
# bosun.yml or env: BOSUN_POST_SYNC_HOOKS
post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
  - paths: ["gatus/config.yaml"]
    action: restart
    container: gatus
```

**Implementation:** After deploy step completes, diff deployed files against previous state.
For each hook, check if any changed files match the glob patterns. If so, execute the action
(restart container via Docker SDK).

## Risks / Trade-offs

- **Removing `--wait` changes exit semantics**: Compose up will succeed even if a new service
  fails its healthcheck. Mitigated by post-deploy verification + unhealthy alert.
- **Post-sync hooks add restart latency**: Each matched hook adds a container restart to deploy
  time. Mitigated by running restarts concurrently.
- **Alert throttling may miss escalation**: If a problem worsens, throttled alerts won't reflect
  urgency. Mitigated by always including attempt count and duration in alert body.

## Open Questions

- Should post-sync hooks support actions beyond `restart`? (e.g., `reload`, `exec`)
  Recommend starting with `restart` only and extending later.
