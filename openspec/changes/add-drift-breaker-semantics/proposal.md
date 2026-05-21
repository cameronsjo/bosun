# Change: Bounded drift self-heal and restart-breaker state-machine integrity

## Why

The April 2026 reconcile-path bug hunt (Cluster F) found three correctness gaps
in the drift self-heal and restart circuit-breaker state machines. None of these
mechanisms has an authoritative spec requirement to regress against.

- **Self-heal has no max-attempts breaker (#259).** `maybeSelfHeal`
  (`internal/daemon/daemon.go:923`) honors only skip-if-reconciling and
  skip-if-cooldown. When the drift cause is out-of-band — something keeps
  mutating Docker state outside git — `docker compose up` cannot fix it, so the
  daemon reconciles forever at cooldown cadence: sustained alert flap, log
  volume, zero remediation.
- **Restart breaker resets its baseline when the window elapses (#265).**
  `evaluateRestartBreaker` (`internal/reconcile/restart_breaker.go:71`)
  unconditionally resets the baseline when `delta > 0` but `elapsed > window`. If
  `BOSUN_DRIFT_INTERVAL > BOSUN_RESTART_WINDOW`, the breaker is mathematically
  incapable of tripping — a slow restart loop never accumulates a window-bounded
  delta. A silent no-op of a safety net.
- **Deploy-driven recreation is mis-attributed as operator resolution (#266).**
  The resolution branch (`restart_breaker.go:50`) marks a tripped service
  `Resolved` when `count <= prev.RestartCount`. Docker resets `RestartCount` to 0
  on container recreation, so a reconcile-driven recreate trivially satisfies the
  check and fires a false "Resolved" alert even though the container has not
  stabilized.

This proposal establishes spec requirements for **bounded self-heal** and
**restart-breaker state-machine integrity** so the daemon's automated remediation
is correct, observable, and fail-safe.

## What Changes

- **Self-heal attempt bounding** — drift self-heal SHALL track attempts per
  drift signature and stop after a bounded number of attempts when reconciling
  does not resolve the drift, emitting an operator-visible exhausted state and
  alert rather than looping indefinitely. This is the first spec requirement for
  `BOSUN_DRIFT_SELF_HEAL`.
- **Restart-breaker baseline integrity** — the restart circuit breaker SHALL NOT
  silently reset its baseline merely because the evaluation window elapsed while
  restarts are still accumulating; a sustained slow restart loop SHALL still
  trip. Config-load SHALL warn when `BOSUN_DRIFT_INTERVAL > BOSUN_RESTART_WINDOW`.
- **Drift resolution attribution** — a container recreation caused by bosun's own
  deploy SHALL NOT be recorded as operator/external resolution of a tripped
  restart breaker; resolution detection SHALL distinguish bosun-initiated change
  from external change and require post-recreate stability before declaring
  resolved.

## Impact

- Affected specs: `reconcile` — three ADDED requirements (Drift Self-Heal
  Attempt Bounding, Restart Breaker Baseline Integrity, Restart Breaker
  Resolution Attribution). No existing requirement changes behavior: the spec's
  `### Requirement: Circuit Breaker` describes the **deploy-failure** breaker
  (consecutive pipeline failures per commit), a distinct mechanism from the
  **restart** breaker in `restart_breaker.go`. Self-heal has no prior requirement.
- Affected code:
  - `internal/daemon/daemon.go` — `maybeSelfHeal` (`:923`), self-heal cooldown
    and trigger path
  - `internal/reconcile/restart_breaker.go` — `evaluateRestartBreaker`
    (resolution branch `:50`, window-reset branch `:71`),
    `RestartTrackingEntry`, `collectRestartCounts`
  - `internal/reconcile/reconcile.go` / state types — `DeployState` /
    drift-status fields used to persist self-heal attempt counters keyed by drift
    signature
  - config-load / `ConfigFromEnv` — drift-interval vs restart-window warning
- All consumers of the affected state:
  - Daemon self-heal loop (`maybeSelfHeal`) — reads/writes self-heal attempt
    state, needs its own scenarios
  - Restart-breaker evaluation (`evaluateRestartBreaker`) — reads/writes
    `RestartTrackingEntry`, needs its own scenarios
  - Drift status persistence — the state file gains self-heal counters and a
    container-identity field on restart tracking
- New config / env vars (settled during implementation):
  - `BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS` (int, bounded self-heal attempts)
  - Restart tracking gains a container-identity field and a post-recreate
    stability grace period
- Docs: `skills/onboard/resources/gitops.md` (self-heal + breaker behavior),
  `CLAUDE.md` env-var table.
