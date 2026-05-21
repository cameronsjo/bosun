# Change: Daemon reconcile lifecycle — bounded execution, graceful shutdown, lossless coalescing

## Why

The April 2026 reconcile-path bug hunt found that the daemon's reconcile
lifecycle has no uniform contract for how a reconcile is bounded, cancelled, or
queued. The webhook, socket, and TCP entry points each wrap reconcile in
`context.WithTimeout(bgCtx, ReconcileTimeout)`, but the **startup** and **poll**
triggers pass the bare daemon context with no timeout (#231) — a single wedged
`docker compose up` or stuck SSH stalls every future GitOps cycle forever, with
`d.reconciling` stuck true so later triggers coalesce but never run. Separately,
the socket/TCP/`/api/trigger` paths spawn fire-and-forget goroutines with
`context.Background()` that ignore daemon shutdown (#256), so SIGTERM mid-deploy
leaves a reconcile running against shutting-down state. The dirty-flag coalescing
loses triggers that arrive during a run and overwrites source attribution (#257),
silently dropping real upstream pushes under load. And when a reconcile times
out, Go SIGKILLs only the thin docker CLI client — the docker daemon keeps
pulling/starting in the background (#264), diverging actual state from what bosun
believes it cancelled.

No spec describes the daemon's lifecycle guarantees, so these are uncovered
implementation gaps. This proposal establishes a `daemon-lifecycle` capability:
**every reconcile is bounded, every in-flight reconcile is cancellable at
shutdown, no trigger is silently dropped, and cancellation aborts external work.**

## What Changes

- **Bounded reconcile execution on every trigger** — the `ReconcileTimeout`
  bound SHALL apply uniformly to all triggers (startup, poll, webhook, socket,
  TCP, manual), preferably enforced inside `executeReconcile` so the bound cannot
  be bypassed by a new entry point.
- **Graceful shutdown of in-flight reconciles** — every reconcile-spawning path
  SHALL join the daemon WaitGroup and derive its context from the daemon-wide
  cancellation context, so shutdown cancels and waits for in-flight work.
- **Lossless trigger coalescing** — concurrent triggers arriving during an
  active reconcile SHALL NOT be lost, and the source attribution of a coalesced
  run SHALL be preserved rather than overwritten.
- **Cancellation aborts external work** — when a reconcile context expires or is
  cancelled, the daemon SHALL actively abort in-flight subprocess work (docker
  compose) rather than only SIGKILLing the local CLI client.

## Impact

- Affected specs: NEW capability `daemon-lifecycle`. Adjacent to `reconcile`
  (which owns the pipeline steps) but distinct — this is the orchestration
  envelope around a reconcile run.
- Affected code:
  - `internal/daemon/daemon.go` — startup trigger (`:318`), poll trigger
    (`:674`), `executeReconcile`, dirty-flag coalescing (`:451-523`), `d.wg`,
    daemon cancellation context
  - `internal/daemon/server.go` / `api.go` / `socket.go` / `tcp.go` — the
    reconcile-spawning goroutines (`server.go:231`, `api.go:309`,
    `socket.go:175`, `tcp.go:169`)
  - `internal/reconcile/compose.go` — `exec.CommandContext` sites (`:77`, `:235`,
    `:326`, `:360`) needing `cmd.Cancel` / active abort
- All trigger consumers (each needs its own scenario + task):
  - startup trigger (`daemon.go:318`)
  - poll trigger (`daemon.go:674`)
  - webhook / socket / TCP / API trigger goroutines
- New config / env vars: none new — formalizes use of existing
  `BOSUN_RECONCILE_TIMEOUT` and `BOSUN_SHUTDOWN_TIMEOUT`.
- Out of scope (handled elsewhere):
  - SSH/tar orphan-process cleanup on cancel (#251, #276) — Cluster H
    (reconcile/SSH lifecycle)
  - Lock-directory bootstrap (#241) — plain bug fix (restores intended
    behavior), no spec proposal
- Docs: `skills/onboard/resources/gitops.md` (daemon reconcile lifecycle),
  `docs/troubleshooting.md` (wedged-reconcile recovery).
