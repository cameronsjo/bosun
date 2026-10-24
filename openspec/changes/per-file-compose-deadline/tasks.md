# Tasks

- [x] 1. `internal/deadline`: a cycle timeout that can pause, with an injectable
  clock; unit tests for expiry, pause/resume budget, nested pauses, parent
  cancellation, and derived contexts reporting `DeadlineExceeded`.
- [x] 2. Daemon cycle context uses the pausable timeout.
- [x] 3. Isolated compose up pauses the cycle clock for the compose phase and
  gives each file's `up` its own `ComposeUpTimeout` deadline;
  `ComposeUpMultiple` always applies its timeout.
- [x] 4. Pipeline tests through `Run()` with a fake clock: files that together
  exceed the cycle budget all succeed; a hung file fails at its own deadline,
  later files still run, and the orphan pass is skipped when it was not rolled
  back. Staged break (no pause) turns the first test red.
- [x] 5. Spec delta: `specs/reconcile/spec.md`, ADDED requirement. Docs:
  env-var table and onboard skill.
- [x] 6. Hard cycle cap `ReconcileMaxTimeout` (`BOSUN_RECONCILE_MAX_TIMEOUT`,
  default 30m) that runs while the budget is paused. Fake-clock tests: the cap
  runs while paused; three slow files hit the cap (b and c fail, rollbacks
  fail, orphan pass skipped); one slow file does not starve the rest. Staged break (cap ignored) turns the cap tests red.
- [x] 7. Fake clock moved to the test-helper package `internal/deadline/deadlinetest`.
- [x] 8. Review round: after the cap fires, files not yet started are skipped
  without rollback, and extraction/rollback for the cut-off file run under a
  shared `ReconcileRollbackGrace` (default 5m) from the cap's deadline. Fake-clock
  tests: rollback fits in the grace and succeeds; the grace cuts a slow cleanup
  off; never-started files are not rolled back; elapsed time stays within
  cap + grace. Staged breaks (no skip; no grace) turn them red.
- [x] 9. Review round 2: every extraction and rollback (per-file and the
  health-gate `RollbackFromBackupSet`) uses `deadline.CleanupContext`:
  detached, ending at min(start + `ComposeUpTimeout`, cap deadline + grace),
  whether started before or after the cap. Fake-clock tests: cleanup started at
  cap − 1m with a wedged extraction ends at cap + grace; a health-gate rollback
  with wedged docker ends at cap + grace; shutdown still rolls back. Staged
  breaks (compose cleanup unbounded; health-gate rollback unbounded; cleanup not
  detached) each turn tests red.
