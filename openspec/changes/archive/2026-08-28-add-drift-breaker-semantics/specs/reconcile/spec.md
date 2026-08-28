## ADDED Requirements

### Requirement: Drift Self-Heal Attempt Bounding

Drift self-heal SHALL track reconciliation attempts per drift signature and SHALL stop attempting after a bounded number of attempts when reconciling does not resolve the drift, rather than looping indefinitely.

When `BOSUN_DRIFT_SELF_HEAL` is enabled, a periodic drift check that finds drift
MAY trigger a reconciliation. The daemon SHALL maintain, in the deploy state
file, a per-drift-signature attempt counter, where a drift signature is the
stable set of currently-drifted `service:type` items. The attempt bound SHALL be
configurable via `BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS` with a small positive
default.

Each self-heal trigger for the current signature SHALL increment the counter.
When the counter reaches the bound, the daemon SHALL mark the signature exhausted,
SHALL stop triggering self-heal for that signature, and SHALL emit a
`self-heal-exhausted` alert at most once per exhausted signature. When the drift
signature changes, or the drifted items clear, the counter SHALL reset and an
exhausted signature SHALL re-arm.

#### Scenario: Out-of-band drift exhausts the self-heal bound
- **WHEN** `BOSUN_DRIFT_SELF_HEAL` is enabled and a drift caused by out-of-band container state persists across self-heal attempts
- **THEN** self-heal triggers up to the configured maximum number of attempts for that signature
- **AND** after the bound is reached the daemon stops triggering self-heal for that signature
- **AND** a `self-heal-exhausted` alert is emitted once

#### Scenario: New drift signature resets the attempt counter
- **WHEN** a different set of services enters drift after a prior signature was exhausted
- **THEN** the attempt counter for the new signature starts at zero
- **AND** self-heal may attempt reconciliation again for the new signature

#### Scenario: Resolved drift re-arms an exhausted signature
- **WHEN** a previously exhausted drift signature later clears (drift items become empty)
- **THEN** the exhausted state for that signature is cleared
- **AND** a subsequent recurrence of the same signature is eligible for self-heal again

#### Scenario: Self-heal disabled performs no attempts
- **WHEN** `BOSUN_DRIFT_SELF_HEAL` is disabled and a periodic drift check finds drift
- **THEN** no self-heal reconciliation is triggered
- **AND** no attempt counter is incremented

### Requirement: Restart Breaker Baseline Integrity

The restart circuit breaker SHALL NOT silently reset its restart-count baseline merely because the evaluation window elapsed while restarts are still accumulating, so that a sustained slow restart loop still trips.

When evaluating a tracked service whose current restart count exceeds its
baseline (`delta > 0`), the breaker SHALL preserve the earliest unresolved-restart
baseline (its count and timestamp) when the elapsed time exceeds the configured
window, rather than resetting the baseline to the current observation. The breaker
SHALL advance the baseline normally only when no new restarts occurred since the
last check (`delta <= 0`). A service that restarts repeatedly across intervals
longer than `BOSUN_RESTART_WINDOW` SHALL still accumulate toward the threshold and
trip.

At configuration load, the daemon SHALL warn when `BOSUN_DRIFT_INTERVAL` is
greater than `BOSUN_RESTART_WINDOW`, because the breaker observes restart counts
on the drift-check cadence and a window-bounded delta would otherwise be
unobservable.

#### Scenario: Slow restart loop trips despite long drift interval
- **WHEN** `BOSUN_DRIFT_INTERVAL` is greater than `BOSUN_RESTART_WINDOW` and a container restarts repeatedly across successive drift checks
- **THEN** the breaker preserves the accumulating baseline rather than resetting it each interval
- **AND** the service eventually trips the restart breaker

#### Scenario: Clean check advances the baseline
- **WHEN** a tracked service shows no new restarts since the last check (`delta <= 0`)
- **THEN** the breaker advances the baseline to the current count and timestamp

#### Scenario: Misconfigured intervals warn at load
- **WHEN** the daemon loads configuration with `BOSUN_DRIFT_INTERVAL` greater than `BOSUN_RESTART_WINDOW`
- **THEN** a warning is logged identifying the interval/window mismatch

### Requirement: Restart Breaker Resolution Attribution

The restart circuit breaker SHALL distinguish a container recreation caused by bosun's own deploy from external operator recovery and SHALL NOT record a deploy-driven recreation as resolution of a tripped service.

The breaker SHALL track a container-identity field (such as the container ID)
alongside the restart count for each tracked service. Because Docker resets a
container's restart count to zero on recreation, a lower-or-equal restart count
SHALL NOT by itself be treated as operator recovery. When the tracked
container identity changes, the breaker SHALL treat the event as recreation and
SHALL require a post-recreate stability grace period — at least one check cycle
with no further restarts — before marking the service `Resolved`. A service that
resumes restart-looping after recreation SHALL remain tripped.

#### Scenario: Deploy-driven recreation does not falsely resolve
- **WHEN** a tripped service is recreated by a reconcile and immediately resumes restart-looping
- **THEN** the breaker recognizes the changed container identity as recreation
- **AND** does not emit a `Resolved` event
- **AND** the service remains tripped

#### Scenario: Operator recovery resolves after stability
- **WHEN** a tripped service is recreated or recovered and then runs with no further restarts across at least one check cycle
- **THEN** the breaker marks the service `Resolved`

#### Scenario: Same-container recovery requires stability
- **WHEN** a tripped service shows a restart count that is lower or equal but the container identity is unchanged
- **THEN** the breaker does not mark the service `Resolved` until a clean check cycle confirms stability
