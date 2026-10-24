# Tasks

- [x] 1. Pure `orphanPassSkipReason` with table tests (success, unhealthy-only,
  verified rollback, no rollback, rolled back without a backup tree, several
  unrecovered files).
- [x] 2. Skip the orphan pass in isolated compose up when the reason is
  non-empty; log it and carry it on the summary and the returned errors.
- [x] 3. Pipeline test through `Run()`: a full run where one file fails and its
  rollback fails runs no `--remove-orphans` and returns an error naming the
  skip; positive control where the rollback succeeds keeps the pass.
- [x] 4. Staged break (skip disabled) turns the unit and `Run()` tests red.
- [x] 5. Spec delta: `specs/reconcile/spec.md`, ADDED requirement.
