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
  per-file rollback, extraction, and orphan-pass timeouts).
- A new hard cap, `ReconcileMaxTimeout` (`BOSUN_RECONCILE_MAX_TIMEOUT`, default
  30 minutes), bounds the whole cycle's wall-clock time and keeps running while
  the cycle budget is paused, so a run of many slow files still ends. A value
  below `ReconcileTimeout` is raised to it with a warning. The 30-minute default
  is three times the cycle budget: 10 minutes for the steps outside compose up
  plus about 20 minutes of compose up, room for a full run in which a few files
  pull large images, while hung files still end the cycle within half an hour.
- When the cap fires, the file running at that moment fails, and every later
  file fails at once on the ended cycle. Each goes through the existing per-file
  rollback (which has its own timeout, as on shutdown today). Any of them not
  rolled back counts as unrecovered, so the orphan pass is skipped.
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
- Affected code: new `internal/deadline` (a cycle timeout that can pause, a
  hard cap, and `WithCycleBudget` combining them; test clock in
  `internal/deadline/deadlinetest`), `internal/daemon/daemon.go` (cycle context
  uses it; `ReconcileMaxTimeout` config),
  `internal/reconcile/compose.go` (pause the cycle clock for the compose phase;
  per-file deadline).
- New env var `BOSUN_RECONCILE_MAX_TIMEOUT` (daemon). `BOSUN_RECONCILE_TIMEOUT`
  now excludes the compose phase; `BOSUN_COMPOSE_UP_TIMEOUT` is per file.
- Not included: running changed files before unchanged ones on a full run. The
  per-file budgets remove the starvation that ordering would have reduced, and
  passing per-file change reasons out of selection is a larger change.
- Remote (SSH) deploys run one compose up for the whole directory and are
  unchanged.

## Scope

Spec delta and implementation ship together in one PR.
