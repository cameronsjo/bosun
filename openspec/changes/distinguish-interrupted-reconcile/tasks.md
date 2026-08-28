## 1. Persist Interruption Outcome

- [ ] 1.1 Add an optional, backward-compatible `last_attempt_outcome` state
  object for the canonical `interrupted` outcome, commit, and timestamp.
- [ ] 1.2 Add state round-trip, omitted-field, legacy-file, and subsequent
  terminal-outcome clearing tests.

## 2. Classify and Account for Cancellation

- [ ] 2.1 Add a pure classifier that requires both a cancelled caller context
  and a returned error wrapping `context.Canceled`; keep deadline and ambiguous
  race cases on the failure path.
- [ ] 2.2 Snapshot and restore the pre-attempt commit, count, and alert-throttle
  budget for classified interruptions without clearing an existing
  `needs_redeploy` marker.
- [ ] 2.3 Apply interruption finalization once at the run boundary across sync,
  decrypt, render, backup, deploy, and health cancellation paths without
  changing panic or ordinary failure accounting; explicitly preserve swallowed
  post-sync hook errors and cancellation as best-effort, non-terminal behavior.
- [ ] 2.4 Cover same-commit prior failures, first run of a new commit, no-effect
  cancellation, partial deploy cancellation, deadline expiry, real-error races,
  force mode, swallowed hook cancellation, and per-target state isolation.
- [ ] 2.5 Stop multi-target iteration whenever the cycle context reports caller
  cancellation, including after a real target error races with shutdown, and
  stop without target finalization when cancellation occurs between targets;
  verify later target state and alerts remain untouched while ordinary target
  failures continue only under a live cycle context.

## 3. Deliver Interruption Alert

- [ ] 3.1 Send exactly one existing-format deploy-failure alert with a canonical
  interrupted reason for each classified interruption when failure alerts are
  enabled.
- [ ] 3.2 Make the run-boundary finalizer the exclusive owner of interruption
  alerts: pass causal errors through every stage alert site and suppress both
  `sendThrottledFailureAlert` and `sendGateFailureAlerts` (including rollback
  companion notifications) when propagated caller cancellation is detected.
- [ ] 3.3 Preserve context values while detaching caller cancellation and
  enforcing the 30-second maximum delivery budget.
- [ ] 3.4 Verify interruption delivery bypasses attempt throttling, does not
  mutate `last_alerted_attempt`, and leaves persisted outcome intact when a
  provider fails.
- [ ] 3.5 Add deterministic stage-table and health-gate tests proving propagated
  cancellation attempts exactly one finalizer-owned alert, real errors keep
  their existing stage-owned alerts, and a multi-target shutdown spends at most
  one 30-second alert budget.

## 4. Documentation and Validation

- [ ] 4.1 Update `docs/alerting.md` and
  `skills/onboard/resources/gitops.md` with interruption, retry, breaker, and
  bounded alert-delivery behavior.
- [ ] 4.2 Run focused reconcile and daemon lifecycle tests, full tests, race
  tests, build, vet, lint, and strict OpenSpec validation through the required
  shared agent resource gate where applicable.
