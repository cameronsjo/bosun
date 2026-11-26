# Tasks

- [ ] 1. `persistState` choke point in `internal/reconcile`; route every `SaveState` in a `Reconciler` method through it; delete `undoDryRunAttempt`.
- [ ] 2. `dispatchRecovery` withholds a recovery alert on a dry run.
- [ ] 3. Daemon periodic drift check: observe-only under `ReconcileConfig.DryRun`.
- [ ] 4. Tests: state byte-identity per pipeline exit, recovery withheld, failure alert kept, daemon drift tick and restart breaker untouched; rewrite the dry-run tests that pinned the old state writes.
- [ ] 5. Docs: `docs/gitops.md`, `docs/alerting.md`, `skills/onboard/resources/gitops.md`, `skills/onboard/resources/commands.md`, AGENTS.md gotcha.
