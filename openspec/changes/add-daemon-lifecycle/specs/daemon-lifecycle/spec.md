## ADDED Requirements

### Requirement: Bounded reconcile execution

Every reconcile SHALL execute under a finite timeout derived from
`ReconcileTimeout`, regardless of which trigger initiated it. The bound SHALL be
enforced at a single chokepoint (`executeReconcile`) so that startup, poll,
webhook, socket, TCP, and manual triggers are all bounded uniformly and a new
entry point cannot bypass it. A reconcile that exceeds the timeout SHALL be
cancelled and reported as a timeout error, and the daemon's `reconciling` state
SHALL be cleared so subsequent triggers can run.

#### Scenario: Poll-triggered reconcile is bounded
- **WHEN** a poll-triggered reconcile exceeds `ReconcileTimeout`
- **THEN** the reconcile is cancelled and reported as a timeout error
- **AND** the daemon accepts and runs the next trigger rather than wedging

#### Scenario: Startup-triggered reconcile is bounded
- **WHEN** the startup reconcile exceeds `ReconcileTimeout`
- **THEN** the reconcile is cancelled and reported as a timeout error
- **AND** the daemon continues to its poll/serve loop

#### Scenario: A wedged reconcile does not block future cycles
- **WHEN** one reconcile is cancelled at the timeout boundary
- **THEN** `reconciling` is cleared and a later webhook or manual trigger executes normally

### Requirement: Graceful shutdown of in-flight reconciles

Every code path that spawns a reconcile SHALL register with the daemon's
WaitGroup and SHALL derive its context from the daemon-wide cancellation context,
so that daemon shutdown cancels in-flight reconciles and waits for them to unwind
within the shutdown timeout. No reconcile SHALL be spawned from a detached
`context.Background()` that ignores shutdown.

#### Scenario: SIGTERM cancels a socket-triggered reconcile
- **WHEN** a reconcile triggered via the Unix socket is in flight and the daemon receives SIGTERM
- **THEN** the reconcile's context is cancelled
- **AND** shutdown waits for the goroutine to return before exiting

#### Scenario: SIGTERM cancels a TCP-triggered reconcile
- **WHEN** a reconcile triggered via the TCP API is in flight and the daemon receives SIGTERM
- **THEN** the reconcile's context is cancelled and shutdown waits for it

#### Scenario: SIGTERM cancels an API-triggered reconcile
- **WHEN** a reconcile triggered via `/api/trigger` is in flight and the daemon receives SIGTERM
- **THEN** the reconcile's context is cancelled and shutdown waits for it

### Requirement: Lossless trigger coalescing

The daemon SHALL NOT silently drop a trigger that arrives while a reconcile is
running. When triggers are coalesced, the daemon SHALL guarantee that at least
one further reconcile runs after the current one if any trigger arrived during
it, and SHALL preserve the source attribution of coalesced triggers rather than
overwriting it with the most recent value.

#### Scenario: Trigger arriving mid-reconcile is not lost
- **WHEN** a reconcile is running and a new trigger arrives before it completes
- **THEN** the daemon runs a follow-up reconcile after the current one completes

#### Scenario: Concurrent triggers preserve source attribution
- **WHEN** multiple triggers from different sources arrive during one reconcile
- **THEN** the coalesced run records the contributing sources rather than only the last writer

### Requirement: Cancellation aborts external work

When a reconcile context expires or is cancelled, the daemon SHALL actively
abort in-flight external subprocess work rather than relying solely on SIGKILL of
the local CLI client. For `docker compose` invocations, the daemon SHALL ensure
the underlying docker operation is stopped (e.g. graceful SIGTERM-then-SIGKILL or
an explicit compose-down) so that actual container state does not continue to
diverge after the reconcile is considered cancelled.

#### Scenario: Timeout during compose up does not leak background work
- **WHEN** `docker compose up` is in flight and the reconcile timeout fires
- **THEN** the docker operation is actively aborted
- **AND** the docker daemon does not continue bringing up containers after the reconcile is reported cancelled
