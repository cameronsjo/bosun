# Design: enforcing git network timeouts

## Context

`internal/reconcile/git.go:475` already wraps the fetch in
`context.WithTimeout(ctx, GitFetchTimeout)`. The context is real; nothing
downstream honours it. Two independent severings:

- **Dial.** go-git's SSH transport builds its dial context from
  `context.Background()` (`plumbing/transport/ssh/common.go:159`), and bosun
  never sets `ssh.ClientConfig.Timeout`. A TCP connect against an unresponsive
  peer blocks until the kernel gives up.
- **Handshake.** Even *with* `Timeout` set, `dial` applies it to the dial context
  and then calls `ssh.NewClientConn(conn, addr, config)` (`common.go:197`), which
  takes no context and honors no deadline. A peer that completes the TCP accept
  and then never speaks stalls in the handshake indefinitely. This is the part
  that reads as fixed when it is not, and it is why the field alone is not enough.
- **Transfer.** `FetchContext` consults the context only *between* protocol
  steps. A packfile read that stalls mid-stream is inside a step, so the
  deadline is never observed.

A fourth, found while reviewing this change: `Clone` applies `GitCloneTimeout`
**only when the caller context carries no deadline** (`git.go:362-367`), and
`daemon.go:1008` sets a `ReconcileTimeout` deadline on every reconcile cycle. On
the daemon path — the only path that runs unattended — `GitCloneTimeout` is
never applied, while the error text still names it.

The result is a timeout that names a bound it does not impose.

## What the incident does and does not establish

The measured facts are: the error text said `2m0s`, and the run logged
`duration_ms: 989971`. That is the **run** duration, which also covers lock
acquisition, `validateBranch`, `IsDirty`, `GetLatestCommit` and `PlainOpen`
(`git.go:433-472`), plus everything after the fetch error.

So the incident establishes that **the error text reports the declared bound
rather than the measured one** — that inference needs no assumption about where
the time went. It does *not* establish which layer stalled. A fetch that ran ~2m
and expired correctly, with 14m30s spent elsewhere in the run, produces the same
two observations. The discriminating measurement is a fetch-scoped elapsed time,
which is exactly the instrument this change adds and which did not exist when
the incident happened.

Both layers are unbounded by code-reading, and both are bounded here. Neither
gets to borrow the incident's authority as its confirmed cause.

## Decision 1 — wrap the auth method, do not install a protocol

The obvious fix is `client.InstallProtocol("ssh", gitssh.NewClient(&xssh.ClientConfig{Timeout: d}))`.
It takes GitOps offline.

`overrideConfig` (`ssh/common.go:290`) reflects over every `ssh.ClientConfig`
field and assigns unconditionally — zero values included — at `common.go:141`,
*after* the nil-`HostKeyCallback` repair at `:127`. A config that sets only
`Timeout` therefore blanks `User`, `Auth`, `HostKeyCallback`, and
`HostKeyAlgorithms`, and every fetch dies with `ssh: must specify
HostKeyCallback`.

Field-blanking is the whole disqualifier and it stands on its own. An earlier
draft also listed "it is a process-global mutation" — that reason is withdrawn,
because Decision 2's preferred design registers a transport through the same
global registry. Rejecting one for globalness while preferring the other would
be incoherent.

**Instead:** wrap the resolved `transport.AuthMethod` so its `ClientConfig()`
returns the auth's *own* config with `Timeout` filled in. Every field the auth
resolved survives. This is safe because `overrideConfig` returns early when the
client's own config is nil (`common.go:291-293`), which is the default client's
case.

The requirement that a wrapped fetch still authenticates is stated in the spec
rather than left to the implementer, because a broken wrap fails in exactly the
way a working one looks — the timeout is present, the code compiles, and only a
live fetch reveals the missing callback.

**Scope, stated plainly:** this bounds the **TCP dial only**. `dial` applies
`config.Timeout` to the dial context and then calls
`ssh.NewClientConn(conn, addr, config)` (`common.go:197`), which takes no context
and honors no deadline. Decision 2 is therefore not optional hardening on top of
Decision 1 — it is what bounds the handshake and the transfer.

## Decision 2 — bound the handshake and transfer at the connection, not by abandonment

Decision 1 bounds the TCP dial. Everything after it — handshake, reference
negotiation, packfile transfer — remains unbounded, and any of those layers
could have produced the observed incident. Bounding them is justified by the
code, not by an attribution the evidence cannot support.

Decision 2 **subsumes** Decision 1 where both apply: once a custom transport
owns the dial, `ClientConfig().Timeout` is no longer the thing enforcing it.
Decision 1's auth-preservation requirement survives the subsumption unchanged,
because the custom transport must replicate the same auth config faithfully —
it is the same trap one layer down.

Preferred: register a custom `transport.Transport` for `ssh` that dials the TCP
connection itself, wraps it in a `net.Conn` refreshing a read deadline on every
`Read`, builds the client via `xssh.NewClientConn`, and replicates the auth
config faithfully. This removes the leak, the corruption risk, and the need for
any in-flight guard at once.

**Rejected: abandon the fetch in a goroutine behind an in-flight guard.** Three
independent defects, any one disqualifying:

- Nothing bounds the abandoned goroutine, so the guard is fail-*stuck* — only a
  container restart clears it. Strictly worse than the 16-minute stall it
  replaces.
- `Clone`'s `os.RemoveAll(g.Dir)` cleanup (`git.go:396-400`) would delete the
  directory out from under a live writer.
- `defer closeGitAuth(auth)` (`git.go:441-445`) closes the ssh-agent socket the
  abandoned goroutine is still using.

If the preferred transport proves unworkable inside its time box, the fallback
is to ship Decision 1 alone and **state the residual** — TCP dial bounded,
handshake and transfer not — in the PR body and `docs/troubleshooting.md`. A stated
partial bound is honest; an abandonment guard is a regression wearing a fix's
label.

**Rejected: shell out to `git fetch` and kill the process.** Trivially killable,
and the container has git — but it breaks design principle 2 (single binary, no
shell dependencies on target).

**Rejected: raise `GitFetchTimeout`.** The bound was never the problem. A larger
unenforced number is the same defect with a worse label.

## Decision 3 — timeouts become fields, and the error reports elapsed

Package constants cannot be shortened by a test, so a timeout test would have to
wait the real bound. Moving them to `GitOps` fields with constant defaults makes
the behaviour testable in milliseconds.

A non-positive configured value falls back to the default rather than being
honoured, so a partially-populated struct cannot silently produce an unbounded
or instantly-expiring operation.

The error text takes the *measured* elapsed time. `git fetch timed out after
2m0s` describing a 16m30s stall was the single most misleading fact in the
incident: it made an unenforced bound look like an enforced one, which is
precisely the defect being fixed.

## Decision 4 — recovery is gated on its own flag, defaulting true

`on_recovery` could have mirrored `on_success`. It must not: a system that
alerts on failure and cannot retract leaves the operator worse informed than one
that never alerted, so the safe default is on.

Default-true costs more than it looks. `AlertConfig.OnSuccess` (`config.go:162`)
is a plain `bool`; the tri-state that would carry "unset" lives only in the raw
DTO and collapses at `extractAlertConfig`. Mirroring `OnSuccess` therefore
yields default-*false*. Getting default-true requires explicit defaults in three
construction sites, following `OnFailure`'s existing pattern — plus the copy at
`daemon.go:2349-2350`, without which the gate never reaches the daemon path,
which is the only path the incident took.

## Decision 5 — recovery is evaluated at the run boundary

The current trigger sits on the deploy path and is unreachable for a docs-only
recovery. Both skip branches return before `reconcile.go:944`, but they are
**not** the same bug and a fix that treats them as one is wrong:

- `:637-655` (no deploy-relevant files) zeroes `AttemptCount`,
  `LastAttemptedCommit` and `LastAlertedAttempt`, then returns. The evidence
  that a retraction is owed is destroyed. Dispatch must be inserted *above* the
  reset.
- `:608-616` (already deployed) zeroes only `AttemptCount` and
  `LastAttemptedCommit`, and only under a guard. `LastAlertedAttempt` survives.
  The evidence is intact; the defect is purely the early return. But because
  nothing clears it, dispatching here *without* also clearing
  `LastAlertedAttempt` re-fires the retraction on every subsequent
  already-deployed run — a fix that converts a missing alert into a repeating
  one.

Moving the trigger to the run boundary — dispatch when a run ends clean and a
failure alert is outstanding, then clear on every clean path — makes the
retraction independent of whether the recovering commit happened to touch a
deploy path.

**The predicate is `LastAlertedAttempt > 0`, not `AttemptCount > 0`.** A failure
that never crossed an alert threshold owes no retraction. The three candidate
fields clear differently per branch, so leaving the choice to the implementer
means some scenarios pass and others silently do not.

Dropping the `AttemptCount > 1` condition follows from `state.go:167`:
`alertThresholds = []int{1, 3, 10, 30}` means failure alerts fire at attempt 1,
so requiring two attempts before retracting guarantees the common case never
retracts.

The `-1` at `reconcile.go:945` goes with it. It exists only because the call was
gated on `> 1`; keeping it while removing the gate reports **0 prior failures**
in exactly the single-failure case this change exists to serve — and the
retained Reconciliation Lifecycle Alerts requirement says the Deploy Recovery
message carries a count of prior failures.

## Decision 6 — two address fields, never one

`remote_addr` is what the connection observed. `forwarded_for` is what the
sender claimed. Collapsing them, or preferring the header, converts a forgeable
claim into a logged fact.

`BOSUN_LISTEN_ADDR` binds all interfaces by design (`server.go:94-100`), so any
container on the docker bridge can send a well-formed `X-Forwarded-For`. Parsing
a value as an IP does not make it true. `serve.json` additionally carries
`"AllowFunnel": true`, so `/hooks/` is reachable from the public internet — the
sender population was never limited to repos we own.

The trusted-proxy set defaults empty, so `forwarded_for` is never emitted until
an operator names a proxy. An empty set means "trust nothing", not "trust
everything" — the inverted reading is how this control usually fails.
