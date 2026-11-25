# Change: Select one stable container per service for drift and health; track every container in the restart breaker

## Why

Bosun keeps one observed container per declared service, but chooses it by
whatever order Docker lists containers in (`CollectActualState`,
cameronsjo/bosun#799). A stopped leftover that lists before a running
container reports the service as `missing` (`state=exited`), forces the
commit-already-deployed skip check to re-run the pipeline, and fails the
post-deploy health poll.

The first design also made the restart breaker watch only the selected
container. Review showed that is wrong for a scaled service:

- When the breaker stops the selected replica, selection moves to a running
  sibling, and the tracked entry resolved after two quiet samples while the
  stopped replica stayed stopped. The next deploy restarts it, and the breaker
  trips again.
- When every replica crash-loops, the breaker stops one and never the others.
- A crash-looping replica that is not the selected one is never watched.

All three come from one cause: the breaker evaluates one container per
service. A state-based "worst replica wins" selection would also flip the
selected container on every crash-loop sample and reset the breaker baseline
each time the observed container ID changes (`evaluateRestartBreaker`,
`internal/reconcile/restart_breaker.go`).

## What Changes

- New requirement Service State Selection. Per service, a live container
  (`running` or `restarting`) is selected over a stopped one, and among
  candidates the oldest container wins, with the smallest container ID as the
  tie-break. Health and the `restarting`/`running` alternation never change
  the pick. It covers drift, the commit-already-deployed skip check and the
  post-deploy health poll, and each has a scenario.
- New requirement Restart Breaker Replica Tracking. The breaker evaluates
  every running or restarting container of a service, each with its own
  baseline keyed by container ID, and stops each one that reaches the
  threshold, within the project scope it already enforces. Selection does not
  affect the breaker. A service with one container behaves as before.
- New requirement Restart Breaker Tripped Entry Lifecycle. A tripped entry
  stays tied to the container that tripped, so a breaker-stopped replica stays
  tripped, with no resolved alert, while a sibling runs. It resolves only when
  that container, or the container that replaces it, runs stably. "Replaces"
  is defined: a new container takes the entry over only when the tripped
  container no longer exists and the new one has the same Compose service and
  container number (`com.docker.compose.container-number`); a sibling with
  another number never inherits it. A tripped container that is gone with no
  replacement clears its entry without a resolved alert, logged at info level,
  and the next deploy starts fresh; this replaces the earlier behavior of
  keeping the entry until a container returned. A service is reported resolved
  only when none of its containers is still tripped, so one replica recovering
  does not hide another that is still tripped. The skip check counts a service
  as present when one of its stopped containers is tripped.
- The requirements supersede wording in existing requirements instead of
  modifying them. Service State Selection supersedes the per-container
  wording in Declared vs Actual State Comparison and State-Based Skip Logic;
  neither is restated, because Declared vs Actual State Comparison is over 500
  characters on `main` and would not validate under openspec 2.2.0 strict
  (cameronsjo/bosun#828), the same cap cameronsjo/bosun#819 worked around.
  The superseded text is the "Non-running service detected as missing"
  scenario, the stopped-state clause of the `missing` drift type, and the
  sentence in State-Based Skip Logic that a container "in any other state ...
  is not present". Restart Breaker Replica Tracking narrows Restart Breaker
  Baseline Integrity and Restart Breaker Resolution Attribution, whose
  per-service wording describes the one-container case. Restart Breaker
  Resolution Attribution still holds for a replacement container: it stays
  tripped for a grace sample before it resolves.

## Not changed

- An unhealthy or restarting replica that is not the selected one is not
  reported by drift or the health poll. Reporting every replica needs a
  per-replica drift item (cameronsjo/bosun#853); the breaker no longer depends
  on it.
- `docker compose run` one-off containers (`com.docker.compose.oneoff`) are
  still matched to their service. Excluding them is a separate change with its
  own `matchContainer` tests.
- A tripped container that is started again and keeps looping is not stopped a
  second time; the tripped path only watches for stability, as before.

## Impact

- Affected specs: reconcile (new requirements Service State Selection,
  Restart Breaker Replica Tracking and Restart Breaker Tripped Entry
  Lifecycle; the existing requirements named above are unchanged and now read
  through them)
- Affected code: `internal/reconcile/drift.go` (`CollectActualState`, a
  `preferContainer` helper over `docker.ContainerInfo`, and the containers of
  each service on `ActualService`); `internal/reconcile/restart_breaker.go`
  (per-container observations and tracking keys); the readers of tripped
  state by service (`ServiceIsPresent` callers in `pure.go` and
  `compose_select.go`). Eight direct production callers of
  `CollectActualState` read the selected container unchanged
  (`internal/cmd/drift.go`, `internal/reconcile/drift.go`, `health.go`,
  `reconcile.go` x5); the daemon reaches it through `RunDriftCheckWithState`.
- Affected docs: `docs/gitops.md`, `docs/commands.md`, and
  `skills/onboard/resources/gitops.md` (container matching and the breaker)
