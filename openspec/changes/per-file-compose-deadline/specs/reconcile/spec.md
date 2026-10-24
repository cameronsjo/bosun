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
orphan-pass timeouts. Caller cancellation (daemon shutdown) SHALL still stop
the compose phase immediately.

A hard cap, `ReconcileMaxTimeout` (default 30 minutes,
`BOSUN_RECONCILE_MAX_TIMEOUT`), SHALL bound each cycle's run, compose up
included; it SHALL keep running while the cycle budget is paused. A cap below
`ReconcileTimeout` SHALL be raised to `ReconcileTimeout`. When the cap fires,
the file running at that moment SHALL fail, and every later file SHALL NOT be
started and SHALL NOT be rolled back (its containers were never touched); it
SHALL count as failed and unrecovered, so the orphan pass is skipped.

Every backup extraction and rollback in the cycle (isolated compose up's
per-file rollback and the health-gate rollback) SHALL run detached from the
cycle's cancellation and SHALL end at the earlier of its start plus
`ComposeUpTimeout` and the cap's deadline plus `ReconcileRollbackGrace`
(default 5 minutes, `BOSUN_RECONCILE_ROLLBACK_GRACE`), whether it started
before or after the cap fired. The cap's deadline is fixed when the cycle
starts.

A cycle's worst case is therefore `ReconcileMaxTimeout` plus
`ReconcileRollbackGrace`. Caller cancellation (daemon shutdown) is not a cap:
files it interrupts SHALL still be rolled back, within the same bound.

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

#### Scenario: Many slow files reach the hard cap

- **WHEN** the selected files' `up` calls together run past `ReconcileMaxTimeout`
- **THEN** the file running when the cap fires fails, and its rollback ends by the cap's deadline plus `ReconcileRollbackGrace`
- **AND** no later file is started or rolled back
- **AND** no command in that deploy passes `--remove-orphans`, and the deploy error names the files not started

#### Scenario: Rollback grace runs out

- **WHEN** backup extraction or a rollback is still running at the cap's deadline plus `ReconcileRollbackGrace`, whether it started before or after the cap fired
- **THEN** that work is canceled and the file counts as not rolled back

#### Scenario: Health-gate rollback after the cap

- **WHEN** the cap fires during the health gate and the gate rolls the deploy back
- **THEN** the rollback ends by the cap's deadline plus `ReconcileRollbackGrace`

#### Scenario: Shutdown during compose up

- **WHEN** the daemon shuts down while isolated compose up runs
- **THEN** the in-flight compose call is canceled without waiting for its deadline
