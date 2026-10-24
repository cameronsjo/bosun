## ADDED Requirements

### Requirement: Pre-Deploy Validation Hooks

The `pre_deploy_hooks` configuration field (root level, overridable per
target, including through `BOSUN_TARGETS`) SHALL list validation commands
that the reconciler runs against the rendered staging tree before it changes
anything on the target. Each hook SHALL have a `command` (a non-empty list of
arguments), an optional `name`, and an optional `timeout` that defaults to
two minutes.

The reconciler SHALL run the hooks in order after templates render and
declared state is extracted, and before the pre-deploy backup, the file sync,
and compose up. Each hook SHALL run with its working directory set to the
staging tree, without a shell, and with no part of its command templated. Its
environment SHALL contain only `PATH`, `BOSUN_STAGING_DIR`, `BOSUN_TARGET`,
and `BOSUN_COMMIT` (plus the variables the operating system requires to start
a process).

A hook that exits with a non-zero status, cannot be started, or is still
running when its timeout or the reconcile cycle deadline expires SHALL abort
the deploy. An aborted deploy SHALL NOT sync files, run compose up, or run
the orphan pass; SHALL leave the last deployed commit and the redeploy marker
unchanged; SHALL count as a failed attempt for the circuit breaker; and SHALL
send a failure alert naming the hook. Hooks after the failing one SHALL NOT
run. A timed-out hook's process group SHALL be terminated, and after any
hook exits the reconciler SHALL kill whatever remains in its process group.
Hook names, output, and start-failure text SHALL have control characters
removed before they reach terminal output, logs, or alerts, and the failure
alert reason SHALL be a single line.

Configuration loading, daemon and CLI startup, a repository configuration
reload, and the start of each reconcile SHALL reject a hook with an empty
command, an empty program name, a NUL byte in its name or arguments, or a
negative timeout. A rejected reload SHALL abort the reconcile instead of
keeping or dropping the previous hooks. A hot reload of a configuration file
without the field SHALL clear the hooks. An explicit empty list on a target
SHALL disable the hooks for that target.

The hooks SHALL also run on a dry run. A dry run blocked by a hook SHALL NOT
send a failure alert or count as a failed attempt for the circuit breaker.

#### Scenario: Passing hook

- **WHEN** every pre-deploy hook exits with status 0
- **THEN** the deploy continues to the backup, file sync, and compose up
- **AND** each hook ran with the staging tree as its working directory

#### Scenario: Failing hook blocks the deploy

- **WHEN** a pre-deploy hook exits with a non-zero status
- **THEN** no files are synced to the target
- **AND** compose up and the orphan pass do not run
- **AND** the last deployed commit is unchanged
- **AND** a failure alert names the hook
- **AND** later hooks do not run

#### Scenario: Hook timeout blocks the deploy

- **WHEN** a pre-deploy hook runs past its timeout
- **THEN** the hook is stopped and the deploy is blocked as for a failing hook

#### Scenario: Cycle deadline blocks the deploy

- **WHEN** the reconcile cycle deadline expires while a pre-deploy hook runs
- **THEN** the hook is stopped and the deploy is blocked

#### Scenario: bosun's environment is not passed to the hook

- **WHEN** bosun runs with a Git token in its environment
- **THEN** a pre-deploy hook's environment does not contain it

#### Scenario: Dry run blocked by a hook

- **WHEN** a pre-deploy hook fails during a dry run
- **THEN** the run reports the block
- **AND** no failure alert is sent
- **AND** the circuit-breaker attempt count is unchanged

#### Scenario: Timed-out hook's children are stopped

- **WHEN** a pre-deploy hook that started a background child times out
- **THEN** the child is stopped along with the hook

#### Scenario: Daemon rejects invalid hooks at startup

- **WHEN** the daemon starts with a `pre_deploy_hooks` entry that has an empty command
- **THEN** daemon startup fails with an error naming the entry

#### Scenario: CLI rejects invalid hooks at startup

- **WHEN** `bosun reconcile` starts with a `pre_deploy_hooks` entry that has an empty command
- **THEN** the CLI fails before reconciling, with an error naming the entry

#### Scenario: Example hook does not match its own config

- **WHEN** the documented `<no value>` hook runs against a staging tree that contains `bosun.yaml`
- **AND** no rendered file contains `<no value>`
- **THEN** the hook passes

#### Scenario: Example hook fails closed on a grep error

- **WHEN** the documented `<no value>` hook runs and `grep` exits with an error
- **THEN** the hook fails and the deploy is blocked

#### Scenario: Invalid hook configuration

- **WHEN** `pre_deploy_hooks` holds a hook with an empty command
- **THEN** configuration loading fails
- **AND** a reconcile that reaches the hook list fails before rendering or deploying
