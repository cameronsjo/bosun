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
