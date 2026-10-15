## MODIFIED Requirements

### Requirement: Restart Breaker Baseline Integrity

The restart circuit breaker SHALL NOT silently reset its restart-count baseline merely because the evaluation window elapsed while restarts are still accumulating, so that a sustained slow restart loop still trips.

When evaluating a tracked service whose current restart count exceeds its
baseline (`delta > 0`), the breaker SHALL preserve the earliest unresolved-restart
baseline (its count and timestamp) when the elapsed time exceeds the configured
window, rather than resetting the baseline to the current observation. The breaker SHALL advance the baseline normally only when no new restarts occurred since the last check (`delta <= 0`). A service that restarts repeatedly across intervals longer than `BOSUN_RESTART_WINDOW` SHALL still accumulate toward the threshold and trip.

At configuration load, the daemon SHALL warn when `BOSUN_DRIFT_INTERVAL` is
greater than `BOSUN_RESTART_WINDOW`, because the breaker observes restart counts
on the drift-check cadence and a window-bounded delta would otherwise be
unobservable.

The destructive breaker SHALL stop containers only within the effective Compose
project scope of the target being checked. An empty project scope SHALL fail
closed before Docker inspection or stop calls: the breaker SHALL stop nothing
and SHALL preserve that target's existing restart tracking unchanged. The
read-only drift comparison MAY retain its existing broad matching behavior.

The daemon SHALL derive the scope from the existing effective target config. A
named local target with no project name SHALL retain the existing `compose`
fallback used by the target drift path. An implicit default target with no
resolved project name SHALL remain unscoped and therefore inactive for the
destructive breaker. Multi-target checks SHALL retain independent state files,
effective configs, and project namespaces; one target's breaker evaluation
SHALL NOT update another target's state.

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

#### Scenario: Foreign-project container is never stopped
- **WHEN** a restart-looping container outside the selected target's resolved Compose project crosses the restart threshold
- **THEN** the breaker does not inspect or stop it
- **AND** it does not enter that target's restart tracking

#### Scenario: Target state remains independent
- **WHEN** a multi-target daemon checks one target and a restart loop is present in another target's Compose project
- **THEN** only the selected target's project is inspected and eligible for stopping
- **AND** the sibling target's state file remains unchanged

#### Scenario: Unresolved implicit target fails closed
- **WHEN** the restart breaker is enabled for an implicit default target with no resolved project name
- **THEN** the breaker performs no Docker inspection or stop
- **AND** it preserves the target's existing restart tracking unchanged
