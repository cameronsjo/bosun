# Design: enforcing git network timeouts

## Context

`internal/reconcile/git.go:475` already wraps the fetch in
`context.WithTimeout(ctx, GitFetchTimeout)`. The context is real; nothing
downstream honours it. Two independent severings:

- **Dial.** go-git's SSH transport builds its dial context from
  `context.Background()` (`plumbing/transport/ssh/common.go`), and bosun never
  sets `ssh.ClientConfig.Timeout`. A TCP connect or SSH handshake against an
  unresponsive peer blocks until the kernel gives up.
- **Transfer.** `FetchContext` consults the context only *between* protocol
  steps. A packfile read that stalls mid-stream is inside a step, so the
  deadline is never observed.

The result is a timeout that names a bound it does not impose. On 2026-09-08 a
fetch declared `2m0s` and ran `16m30s`, holding the reconcile lock the whole
time and queueing three triggers behind it.

## Decision 1 — wrap the auth method, do not install a protocol

The obvious fix is `client.InstallProtocol("ssh", gitssh.NewClient(&xssh.ClientConfig{Timeout: d}))`.
It takes GitOps offline.

`overrideConfig` (`ssh/common.go:290`) reflects over every `ssh.ClientConfig`
field and assigns unconditionally — zero values included — at `common.go:141`,
*after* the nil-`HostKeyCallback` repair at `:127`. A config that sets only
`Timeout` therefore blanks `User`, `Auth`, `HostKeyCallback`, and
`HostKeyAlgorithms`, and every fetch dies with `ssh: must specify
HostKeyCallback`. It is also a process-global mutation.

**Instead:** wrap the resolved `transport.AuthMethod` so its `ClientConfig()`
returns the auth's *own* config with `Timeout` filled in. Every field the auth
resolved survives; nothing global is mutated.

The requirement that a wrapped fetch still authenticates is stated in the spec
rather than left to the implementer, because a broken wrap fails in exactly the
way a working one looks — the timeout is present, the code compiles, and only a
live fetch reveals the missing callback.

## Decision 2 — bound the transfer at the connection, not by abandonment

Decision 1 bounds dial and handshake only. A stalled packfile read is the
failure mode the incident is attributed to, and it remains unbounded.

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
is to ship Decision 1 alone and **state the residual** — dial and handshake
bounded, transfer not — in the PR body and `docs/troubleshooting.md`. A stated
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

The current trigger sits on the deploy path and is unreachable for a
docs-only recovery. Both skip branches (`reconcile.go:608` already-deployed,
`:637-655` no deploy-relevant files) reset the failure counters and return
before `reconcile.go:944`. Moving the trigger to the run boundary — dispatch
when a run ends clean and a failure alert is outstanding, then clear — makes the
retraction independent of whether the recovering commit happened to touch a
deploy path.

Dropping the `AttemptCount > 1` condition follows from `state.go:167`:
`alertThresholds = []int{1, 3, 10, 30}` means failure alerts fire at attempt 1,
so requiring two attempts before retracting guarantees the common case never
retracts.

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
