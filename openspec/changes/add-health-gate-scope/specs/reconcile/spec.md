## ADDED Requirements

### Requirement: Health Gate Scope

The reconciler SHALL support a configurable `health_gate_scope` that selects which containers the post-compose-up health gate polls and rolls back on. The scope SHALL be one of `critical`, `declared`, or `off`, configured via `health_gate_scope` in `bosun.yaml` and overridable via the `BOSUN_HEALTH_GATE_SCOPE` environment variable. An empty or unset value SHALL resolve to `critical`.

- **critical** (default): the gate polls only the configured `critical_containers` members, exactly as the Critical Container Health Gate requirement describes. An empty `critical_containers` list skips the gate. A declared-but-non-critical service coming up unhealthy SHALL NOT trigger rollback.
- **declared**: the gate polls all declared services (those extracted from the staging compose files). A service that was already unhealthy BEFORE this deploy (a pre-existing casualty) SHALL be exempt and SHALL NOT trigger rollback; only a service this deploy made unhealthy triggers the gate. An empty declared set skips the gate.
- **off**: the health gate SHALL be skipped entirely.

An unknown `health_gate_scope` value SHALL be rejected by validation, and the validation error SHALL name the three valid values. When an invalid value nonetheless reaches the gate at deploy time, the gate SHALL fall back to `critical` and log the invalid value rather than failing the deployment.

Regardless of scope, the gate SHALL be skipped when the deploy is a dry run, the deploy is remote (the Docker API is local-only and cannot observe the remote host's containers), or no Docker client is available.

On a gate failure under any scope, the reconciler SHALL trigger rollback to the backup compose files before post-sync hooks run, SHALL skip post-sync hooks when a rollback ran (the working tree is a hybrid of old compose and new config), and SHALL NOT record the deployment as successful.

Alerting on a gate failure differs by scope, so a flapping healthcheck under `declared` does not spam:

- **critical**: SHALL send only the existing throttled failure alert on the attempt-count schedule — byte-for-byte the prior behavior, with NO rollback-specific alert.
- **declared**: SHALL send the throttled failure alert AND, when a rollback ran, a rollback alert (success or failure) — both on the SAME attempt-count throttle window, so they fire on the established cadence rather than once per cycle.

`BOSUN_HEALTH_GATE_SCOPE` SHALL take precedence over the config file value. An invalid env value SHALL be ignored with a warning, leaving the config-file (or default) scope in effect.

#### Scenario: Declared scope rolls back on a service this deploy broke

- **WHEN** `health_gate_scope` is `declared`
- **AND** a declared service is healthy before the deploy but reports unhealthy after compose up within the health gate timeout
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback to the backup compose files
- **AND** post-sync hooks are skipped
- **AND** the deployment is NOT recorded as successful
- **AND** a throttled failure alert AND a throttled rollback alert are sent on the same attempt-count window

#### Scenario: Declared scope exempts a pre-existing unhealthy service

- **WHEN** `health_gate_scope` is `declared`
- **AND** a declared service was already unhealthy before the deploy and remains unhealthy after compose up
- **THEN** the health gate does NOT fail on that service
- **AND** no rollback is triggered
- **AND** the deployment is recorded as successful

#### Scenario: Critical scope ignores a non-critical declared service

- **WHEN** `health_gate_scope` is `critical` (the default) with no `critical_containers` configured
- **AND** a declared-but-non-critical service reports unhealthy after compose up
- **THEN** the health gate is skipped
- **AND** no rollback is triggered
- **AND** the deployment is recorded as successful

#### Scenario: Off scope runs no gate

- **WHEN** `health_gate_scope` is `off`
- **AND** a declared service reports unhealthy after compose up
- **THEN** the health gate does not run
- **AND** no rollback is triggered
- **AND** the deployment is recorded as successful

#### Scenario: Unknown scope value is rejected

- **WHEN** `health_gate_scope` (or `BOSUN_HEALTH_GATE_SCOPE`) is set to a value other than `critical`, `declared`, or `off`
- **THEN** validation returns an error naming the three valid values
- **AND** if the invalid value reaches the gate at deploy time, the gate falls back to `critical` and logs the invalid value
