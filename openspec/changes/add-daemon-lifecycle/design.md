## Context

The daemon runs a single-flight reconcile loop with dirty-flag coalescing. Five
entry points can trigger a reconcile: startup, poll, HTTP webhook, Unix socket,
and TCP API (plus the `/api/trigger` handler). These evolved independently, so
the timeout/cancellation/queueing contract differs per entry point. The webhook
path wraps `context.WithTimeout`; startup and poll do not. The socket/TCP/api
paths spawn `context.Background()` goroutines that outlive shutdown. The
coalescing logic uses a boolean dirty-flag that loses concurrent triggers.

## Goals / Non-Goals

- Goals:
  - One uniform reconcile execution envelope: bounded, cancellable, queue-safe.
  - Enforce the timeout at a single chokepoint so it cannot be bypassed.
  - Make shutdown deterministic — no orphaned reconcile after exit.
- Non-Goals:
  - Reordering or prioritizing triggers beyond "don't lose them."
  - SSH/tar subprocess orphan cleanup (Cluster H).
  - Lock-directory bootstrap (#241, plain bug fix).

## Decisions

- **Decision: Enforce `ReconcileTimeout` inside `executeReconcile`.**
  Wrapping at each call site is the source of the #231 gap — a new entry point
  silently lacks the bound. Centralizing makes the invariant structural.
  - Alternatives: add the wrap at the two missing call sites only (fragile,
    invites recurrence).

- **Decision: Daemon-wide cancellation context + WaitGroup for all spawns.**
  The webhook path already uses `d.wg`; extend the same pattern to socket/TCP/api
  and derive every spawned context from a daemon root context that Shutdown
  cancels. Reuses the existing graceful-shutdown machinery in `Server.Shutdown`.

- **Decision: Counter/queue coalescing over dirty-bit.**
  A monotonically-checked counter (or a bounded source-capturing queue) closes
  the lost-update window between `executeReconcile` completion and clearing
  `reconciling`. Source attribution becomes an aggregate, not a last-writer.

- **Decision: `cmd.Cancel` with SIGTERM→grace→SIGKILL for docker.**
  The docker CLI is a thin gRPC client; SIGKILLing it abandons server-side work.
  A graceful cancel (or compose-down on timeout) makes "cancelled" mean the
  docker daemon actually stopped. Trade-off: a small extra shutdown delay.

## Risks / Trade-offs

- **Centralizing the timeout could double-bound paths that already wrap it** →
  audit and remove redundant wraps; nested identical timeouts are harmless but
  untidy.
- **Counter-based coalescing adds concurrency surface** → cover with a stress
  test (100 concurrent triggers during a long reconcile).
- **Active docker abort adds shutdown latency** → bound it by `ShutdownTimeout`.

## Migration Plan

No config or API change. Behavior change: wedged reconciles now self-cancel at
`ReconcileTimeout` instead of hanging; operators relying on the old "hang forever"
behavior (none expected) would notice timeouts in logs. Rollback is reverting the
code; no state migration.

## Open Questions

- Should coalescing aggregate sources into a single string or keep a small ring
  of the last N? (Lean: aggregate distinct sources, cap length.)
- Does compose-down-on-timeout risk tearing down healthy containers from a prior
  good deploy? (Need to scope abort to the in-flight `up` only.)
