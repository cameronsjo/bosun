# Change: Give each compose file its own deadline

## Why

The daemon bounds every reconcile cycle with `ReconcileTimeout` (default 10
minutes). Isolated compose up runs the selected files one after another under
that same cycle context, and the per-call compose timeout
(`BOSUN_COMPOSE_UP_TIMEOUT`, default 10 minutes) applied only when the context
had no deadline, so under the daemon it never applied. Once the cycle's budget
was spent, every remaining file failed within seconds with
`docker compose up timed out`, was rolled back, and the deploy reported a
partial failure for stacks the commit never touched (#727). No single file hung:
the files together needed more than one cycle's budget.

## What Changes

- Each compose file's `up` in isolated compose up SHALL run under its own
  deadline of `ComposeUpTimeout`, measured from when that file starts. A slow
  file can use at most its own budget; it cannot consume a later file's.
- The daemon's cycle timeout SHALL NOT run while isolated compose up runs. The
  cycle clock pauses for the compose phase and resumes, with the same remaining
  budget, when compose up returns. `ReconcileTimeout` therefore bounds the rest
  of the pipeline (git, render, backup, deploy, health gate, hooks), and the
  compose phase is bounded by the per-file deadlines (plus the existing
  per-file rollback, extraction, and orphan-pass timeouts). A hung deploy still
  ends: overall time is at most `ReconcileTimeout` plus those per-file bounds
  for the files actually selected.
- Caller cancellation (daemon shutdown) still stops the compose phase at once;
  only the cycle deadline pauses.
- A file whose `up` hits its own deadline is a failed file, as today: it is
  rolled back when a verified backup exists, and when it is not rolled back the
  orphan pass is skipped (`skip-orphan-pass-on-unrecovered-failure`).
- `ComposeUpMultiple` applies `ComposeUpTimeout` even when the caller's context
  already has a deadline (the earlier of the two wins), instead of only when it
  has none.

## Impact

- Affected specs: `reconcile` — ADDED: Per-File Compose Up Deadline.
- Affected code: new `internal/deadline` (a cycle timeout that can pause),
  `internal/daemon/daemon.go` (cycle context uses it),
  `internal/reconcile/compose.go` (pause the cycle clock for the compose phase;
  per-file deadline).
- No new env vars or config. `BOSUN_RECONCILE_TIMEOUT` now excludes the compose
  phase; `BOSUN_COMPOSE_UP_TIMEOUT` is per file.
- Remote (SSH) deploys run one compose up for the whole directory and are
  unchanged.

## Scope

Spec delta and implementation ship together in one PR.
