## ADDED Requirements

### Requirement: Per-File Compose Up Deadline

During isolated compose up, each compose file's `up` SHALL run under its own
deadline of `ComposeUpTimeout` (default 10 minutes, `BOSUN_COMPOSE_UP_TIMEOUT`),
measured from when that file starts, so a slow file cannot use a later file's
time.

The daemon's per-cycle `ReconcileTimeout` SHALL NOT elapse while isolated
compose up runs: the cycle clock pauses when the compose phase starts and
resumes with the same remaining budget when it returns. The compose phase stays
bounded by the per-file deadlines and the existing rollback, extraction, and
orphan-pass timeouts, so a hung deploy still ends. Caller cancellation (daemon
shutdown) SHALL still stop the compose phase immediately.

A file whose `up` exceeds its deadline SHALL be treated as a failed file:
rolled back when a verified backup exists, and counted as unrecovered for the
orphan-pass safety rule when it is not.

#### Scenario: Files together exceed the cycle budget

- **WHEN** a full run selects several compose files whose `up` calls together take longer than `ReconcileTimeout`
- **AND** each file finishes within `ComposeUpTimeout`
- **THEN** every file succeeds and none is rolled back
- **AND** the steps after compose up still have the cycle budget that remained when compose up started

#### Scenario: One file hangs

- **WHEN** one file's `up` does not return
- **THEN** that file fails when its own `ComposeUpTimeout` elapses
- **AND** the files after it still run, each with its full `ComposeUpTimeout`
- **AND** when the hung file is not rolled back, no command in that deploy passes `--remove-orphans`

#### Scenario: Shutdown during compose up

- **WHEN** the daemon shuts down while isolated compose up runs
- **THEN** the in-flight compose call is canceled without waiting for its deadline
