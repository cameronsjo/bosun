## Context

Bosun has two independent breaker mechanisms that the spec conflates by name but
which are distinct in code:

1. The **deploy-failure circuit breaker** (`### Requirement: Circuit Breaker` in
   the reconcile spec) counts consecutive pipeline failures per commit and stops
   retrying a bad commit. This change does **not** touch it.
2. The **restart circuit breaker** (`internal/reconcile/restart_breaker.go`)
   watches Docker container restart-count velocity and trips when a container
   restarts too fast within a window. It has no spec requirement today.

Separately, **drift self-heal** (`maybeSelfHeal`, gated by
`BOSUN_DRIFT_SELF_HEAL`) reconciles when periodic drift is detected. It also has
no spec requirement.

All three findings are state-machine bugs: an unbounded loop, a baseline reset
that erases accumulated signal, and a resolution check that misreads Docker's
restart-count reset on recreation. Because these are interacting state machines
with subtle transition edges, a design doc is warranted.

## Goals / Non-Goals

- Goals:
  - Self-heal converges to a terminal "exhausted" state for unfixable
    (out-of-band) drift, with operator-visible signal.
  - The restart breaker trips for sustained slow loops, not only bursty ones
    inside a single window.
  - Resolution detection distinguishes bosun-initiated recreation from external
    operator recovery and from continued looping.
- Non-Goals:
  - Replacing the breaker with a generic rate limiter or backoff library.
  - Auto-remediating out-of-band drift causes (the daemon cannot fix what keeps
    mutating state outside git — bounding the attempts is the correct posture).
  - Changing the deploy-failure circuit breaker.

## Decisions

- **Decision: Bound self-heal by attempts per drift signature, not wall-clock.**
  A drift "signature" is the stable set of `service:type` items currently in
  drift. Counting attempts per signature means a *new* problem resets the budget
  while a *persistent unfixable* one exhausts and stops. Wall-clock backoff alone
  would still loop forever at a slower cadence.
  - Alternatives considered: global attempt cap (penalizes unrelated future
    drift); pure exponential backoff (never terminates; still flaps).

- **Decision: Carry forward the earliest unresolved-restart baseline.**
  The bug is that `elapsed > window` resets the baseline to "now", discarding the
  fact that the container has been restart-looping the whole time. Instead, when
  restarts are still accumulating (`delta > 0`), the breaker keeps the earliest
  baseline so a slow loop eventually crosses the threshold. A clean check
  (`delta <= 0`) still advances the baseline normally.
  - Alternatives considered: shrink the default window (doesn't fix the
    misconfiguration class); rate-of-restart over absolute delta (larger change,
    deferred).

- **Decision: Track container identity + require post-recreate stability.**
  Docker zeroes `RestartCount` on recreation, so `count <= prev.RestartCount`
  cannot by itself mean "operator fixed it". Recording the container ID lets the
  breaker recognize a recreation (identity changed) versus a same-container
  recovery. After a recreation, the breaker requires at least one clean check
  cycle (no new restarts) before declaring `Resolved`.
  - Alternatives considered: ignore resolution entirely until manual reset (too
    sticky); time-only grace period without identity (still misreads a recreate
    that immediately resumes looping).

- **Decision: Warn, do not hard-fail, on `drift_interval > restart_window`.**
  The breaker observes restart counts on the drift-check cadence. If the drift
  interval exceeds the restart window, a window-bounded delta is unobservable.
  Carrying the baseline forward (above) makes the breaker robust to this, but the
  config is still a smell, so config-load warns rather than silently degrading.

## Risks / Trade-offs

- **Exhausted self-heal could mask a transient drift that would have cleared on
  the next attempt** → mitigated by resetting the counter on any signature change
  and re-arming when the exhausted signature later clears.
- **Carrying the restart baseline forward could delay a clean-state reset** →
  mitigated by advancing the baseline normally whenever `delta <= 0`.
- **Container-identity tracking adds a state field** → mitigated by safe decode
  of legacy state (unknown identity treated conservatively).

## Migration Plan

1. New state fields (self-heal attempt counters, restart container identity)
   default to zero values; legacy state files decode as "no prior attempts" /
   "identity unknown".
2. New env var `BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS` has a safe default; operators
   need not set anything.
3. Rollback: removing the bound reverts to current loop behavior; no state schema
   break (new fields are additive).

## Open Questions

- Default value for `BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS` (lean: 3, matching the
  deploy-failure breaker's `MaxAttempts`).
- Whether the post-recreate stability grace period is expressed in check cycles
  or a duration (lean: one full check cycle for simplicity).
