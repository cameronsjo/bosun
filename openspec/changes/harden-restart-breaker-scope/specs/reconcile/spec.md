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

Before a container can trip or be stopped, the breaker SHALL confirm from that
container's inspected labels that `com.docker.compose.project` equals the
target's project and `com.docker.compose.service` equals the service it was
matched as. A container name, including a `<project>-` name prefix, SHALL NOT
count as ownership. A container whose labels do not prove ownership SHALL NOT
be stopped and SHALL NOT advance or reset that service's restart tracking. A
trip SHALL stop the container by the full ID that was inspected and
ownership-checked, never by name, so a container that took over the name in
between is not stopped.

The daemon SHALL derive the scope from the existing effective target config. A
named local target with no project name SHALL retain the existing `compose`
fallback used by the target drift path. Because any Compose stack started from
a directory named `compose` derives that same name, the breaker SHALL also
require a fallback-scoped container's `com.docker.compose.project.working_dir`
label to equal the target's local compose directory (`<local appdata>/compose`),
either literally or after resolving symlinks on both paths;
when the target has no local appdata path, the breaker SHALL fail closed before
inspection. An explicitly configured `project_name` of `compose` SHALL NOT
require the extra marker. An implicit default target with no
resolved project name SHALL remain unscoped and therefore inactive for the
destructive breaker. Multi-target checks SHALL retain independent state files,
effective configs, and project namespaces; one target's breaker evaluation
SHALL NOT update another target's state.

When the restart breaker is enabled for an implicit default target with no
resolved project name, daemon startup SHALL emit one warning naming the
unresolved `project_name` condition and instructing the operator to set
`project_name`. `bosun doctor` SHALL report the same condition and fix. The
per-cycle empty-scope message SHALL be debug-level so the condition does not
flood logs. A root-level `project_name` supplied by the local configuration is
diagnostic input only: it SHALL NOT replace the existing per-target deployment
or restart-breaker namespace, and therefore SHALL NOT suppress the startup or
doctor warning. Until a reconcile loads `project_name` from the repository
configuration, the breaker for that target stops nothing; when the local
configuration has a root-level name, the warning SHALL say that the name does
not scope the breaker at startup.

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

#### Scenario: Unlabeled container sharing the project prefix is never stopped
- **WHEN** an unlabeled container named `<project>-<name>` restart-loops past the threshold
- **THEN** the breaker does not stop it
- **AND** that service's existing restart tracking is preserved unchanged

#### Scenario: Foreign stack named compose is not acted on
- **WHEN** a named local target has no project name and a restart-looping container labeled with project `compose` was created from a directory other than the target's local compose directory
- **THEN** the breaker does not stop it
- **AND** a container labeled `compose` whose working directory is the target's compose directory can still trip and be stopped

#### Scenario: Target state remains independent
- **WHEN** a multi-target daemon checks one target and a restart loop is present in another target's Compose project
- **THEN** only the selected target's project is inspected and eligible for stopping
- **AND** the sibling target's state file remains unchanged

#### Scenario: Unresolved implicit target fails closed
- **WHEN** the restart breaker is enabled for an implicit default target with no resolved project name
- **THEN** the breaker performs no Docker inspection or stop
- **AND** it preserves the target's existing restart tracking unchanged

#### Scenario: Unresolved implicit target reports its fix
- **WHEN** the restart breaker is enabled for an implicit default target with no
  resolved project name
- **THEN** daemon startup emits one warning naming the missing `project_name`
  and telling the operator to set it
- **AND** `bosun doctor` reports the same warning and fix
- **AND** recurring drift checks do not emit the condition at warning level

#### Scenario: Root-level project name does not silence the posture warning
- **WHEN** the restart breaker is enabled for an implicit default target with no
  target-level project name
- **AND** the local configuration sets only a root-level `project_name`
- **THEN** a drift check before any repository config reload stops nothing
- **AND** daemon startup and `bosun doctor` still warn, stating that the
  root-level name does not scope the breaker until a reconcile loads it
