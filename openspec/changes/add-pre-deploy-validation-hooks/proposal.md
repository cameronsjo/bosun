# Change: Pre-deploy validation hooks that can block a deploy

## Why

No hook phase runs before containers start and can stop the deploy (#477).
`post_sync_hooks` run after the file sync and after compose up, and a failing
`exec` hook is logged as a warning while the reconcile still reports success.
A check on the rendered tree, such as a search for Go's `<no value>` template
sentinel that a mistyped secret key produces, can only report the problem
after the containers have already read the bad value. For a service that
reads a value once on first start (a database superuser password consumed by
`initdb`), that is not recoverable by fixing the template later.

## What Changes

- A new `pre_deploy_hooks` config field (root level and per target, also
  accepted in `BOSUN_TARGETS`) SHALL list validation commands. Each hook has
  a `command` (exact argv), an optional `name`, and an optional `timeout`
  (default 2 minutes).
- The hooks SHALL run in order after templates render and declared state is
  extracted, and before the backup, the file sync, and compose up, with the
  working directory set to the rendered staging tree.
- A hook that exits non-zero, cannot start, or runs past its timeout SHALL
  abort the deploy: no files are synced, compose up does not run, the orphan
  pass does not run, `LastDeployedCommit` and `NeedsRedeploy` are unchanged,
  the attempt counts toward the circuit breaker, and a failure alert is sent
  through the existing failure-alert path. Later hooks do not run.
- The reconcile cycle deadline also bounds the hooks: the cycle ending stops
  a running hook and blocks the deploy. Timeouts fail closed.
- The command SHALL run without a shell, and no part of it is templated, so
  repository content cannot change what runs. A relative `command[0]` with a
  path separator resolves against the staging tree.
- The hook environment SHALL be minimal: `PATH` plus `BOSUN_STAGING_DIR`,
  `BOSUN_TARGET`, and `BOSUN_COMMIT`. Nothing else from bosun's environment
  (Git token, age key, alert credentials) is passed.
- A timed-out hook's process group is sent SIGTERM, then killed after a
  5-second grace.
- Hook output (last 4 KiB) goes to the reconcile log on failure. The alert
  names the hook and the exit status only.
- Invalid hooks (empty command, empty program name, NUL byte, negative
  timeout) SHALL be rejected at config load, at daemon and CLI startup, on a
  repository reload (which aborts the reconcile), and at the start of
  `Run()`.
- A config file without the key reloads as an empty list, so removing
  `pre_deploy_hooks` from `bosun.yaml` clears the gate. An explicit empty
  list on a target disables the gate for that target.
- The hooks also run on a dry run, which then reports whether the real
  deploy would be blocked. Hooks must therefore be read-only checks.

## Impact

- Affected specs: `reconcile` — ADDED: Pre-Deploy Validation Hooks.
- Affected code: `internal/reconcile/predeploy.go` (new: type, validation,
  runner), `internal/reconcile/reconcile.go` (config field, pipeline step),
  `internal/reconcile/target.go` and `config_reload.go` (per-target override,
  hot reload), `internal/config/config.go` (YAML field, load-time
  validation), `internal/cmd/reconcile.go` and `internal/daemon/daemon.go`
  (hydration, startup rejection).
- `post_sync_hooks` are unchanged. The alternative in #477 (a `required`
  flag that turns a failing post-sync hook into a failed reconcile) is not
  part of this change: it would still run after compose up.
- No new environment variables.
- Trust: hooks run on the bosun host as bosun's user and are read from the
  tracked branch on every reconcile, so a branch writer can run commands
  there. For remote targets this is a different host from the one compose up
  runs on. Documented in the onboard GitOps guide.

## Scope

Spec delta and implementation ship together in one PR.
