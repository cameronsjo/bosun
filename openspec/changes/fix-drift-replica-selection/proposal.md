# Change: Select one stable container per service for drift, health and the restart breaker

## Why

Bosun keeps one observed container per declared service, but chooses it by
whatever order Docker lists containers in (`CollectActualState`,
cameronsjo/bosun#799). Two things go wrong:

- A stopped leftover that lists before a running container reports the
  service as `missing` (`state=exited`), forces the commit-already-deployed
  skip check to re-run the pipeline, and fails the post-deploy health poll.
- The restart breaker watches only the selected container, so which replica
  it watches changes with list order.

A state-based "worst replica wins" rule would fix the first and break the
second: a crash-looping replica alternates between `restarting` and
`running`, so its rank would flip the selected container every sample. The
breaker resets its baseline whenever the selected container changes (the
`ContainerID` rule in Restart Breaker Baseline Integrity), so the loop would
never accumulate enough restarts to trip.

## What Changes

- New requirement Service State Selection. Per service, a live container
  (`running` or `restarting`) is selected over a stopped one, and among
  candidates the oldest container wins, with the smallest container ID as the
  tie-break. Health and the `restarting`/`running` alternation never change
  the pick, so the breaker keeps one baseline for a looping replica.
- The new requirement narrows Declared vs Actual State Comparison and
  State-Based Skip Logic by stating which container they read. Neither is
  modified: Declared vs Actual State Comparison is over 500 characters on
  `main`, so restating it whole would not validate under openspec 2.2.0
  strict (cameronsjo/bosun#828), and State-Based Skip Logic needs no
  rewording once the selection rule is stated beside it. This follows how
  cameronsjo/bosun#819 handled the same cap.
- A breaker-stopped replica hands the service to a live sibling. The tracked
  entry then sees a different container, keeps its tripped state through one
  grace sample, and resolves after a second unchanged sample, even though the
  stopped replica stays stopped. This was list-order dependent before; the
  scenarios pin it.

## Not changed

- An unhealthy or restarting replica that is not the selected one is not
  reported. Reporting every replica needs a per-replica drift item and is a
  separate change; the current model has one row per service.
- `docker compose run` one-off containers (`com.docker.compose.oneoff`) are
  still matched to their service. Excluding them is a separate change with its
  own `matchContainer` tests.
- Restart-breaker tracking stays per service. A tripped entry does not stick
  to the container that tripped it.

## Impact

- Affected specs: reconcile (new requirement Service State Selection;
  Declared vs Actual State Comparison and State-Based Skip Logic are
  unchanged and now read through it)
- Affected code: `internal/reconcile/drift.go` (`CollectActualState` and a
  `preferContainer` helper over `docker.ContainerInfo`); nine production call
  sites read the result unchanged (`internal/cmd/drift.go`,
  `internal/reconcile/drift.go`, `health.go`, `reconcile.go` x5,
  `internal/daemon/daemon.go`)
- Affected docs: `docs/gitops.md`, `docs/commands.md`, and
  `skills/onboard/resources/gitops.md` (container matching)
