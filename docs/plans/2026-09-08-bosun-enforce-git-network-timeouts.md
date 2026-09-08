---
updated: "2026-09-08"
body_sha256: "10848d676a292950329fce4df4385fe4c8a01356f62f45458222989b0b4c0bd1"
session: "stone-mallet"
session_id: "8b6ece91-f829-49d5-9a30-2b4b32b00b62"
model: "claude-opus-5"
harness: "claude-code 2.1.263"
machine: "cf6e768835c7"
approved_in: "ember-fugue"
approved_session_id: "20a7397f-5bc8-4ae0-b9fe-c27829ae80db"
status: in_progress
branch: spec/update-git-timeouts-and-alert-recovery
implementation_branch: fix/git-timeout-enforcement
repo: bosun
next: Task 2b — drive the spec PR to the ready-to-build label; Tasks 3-5 are gated on it
---

# Bosun: enforce git network timeouts, make failure alerts retractable, close the 404

## Context

A Sentinel alert on 2026-09-08 at 10:49 reported `Deployment Failed [unraid]` —
`failed to sync repository: git fetch timed out after 2m0s`, duration `16m30s`.

The deploy was never in danger. The bosun daemon logs on `unraid` show the next
cycle pulled clean 5 seconds later (`de54cd6` → `1b78097`, `duration_ms: 5009`)
and every cycle since has been green. GitHub reported `All Systems Operational`.
The changed files were docs and skills only, so bosun correctly logged
`No deploy-relevant files changed, skipping reconciliation`.

What the alert exposed is three real defects in `bosun`, plus a stale webhook
outside it:

1. **The git network timeouts are declared but never enforced.** The alert says
   `2m0s`; the log says `duration_ms: 989971` — 16m30s, eight times the stated
   bound. `internal/reconcile/git.go:475` wraps the fetch in
   `context.WithTimeout(ctx, GitFetchTimeout)`, but go-git severs that context at
   the SSH transport: `plumbing/transport/ssh/common.go` builds its dial context
   from `context.Background()`, and bosun never sets `ssh.ClientConfig.Timeout`.
   `FetchContext` consults the context only *between* protocol steps, so a
   stalled dial or a stalled packfile read blocks indefinitely. The deadline is a
   label on the error message, not a ceiling on the operation. `Clone`
   (`git.go:366`, `:406`) has the identical defect. The stalled run held the
   reconcile lock for the full 16m30s, queueing three triggers behind it.

2. **A failure alert cannot be retracted, and the reason is not the gate it
   looks like.** Three suppressors sit in front of `sendRecoveryAlert`:
   `alerts.go:174` returns early on `!r.config.OnSuccess`; `reconcile.go:944`
   only calls it when `state.AttemptCount > 1`, while failure alerts fire at
   attempt 1 (`state.go:167`, `alertThresholds = []int{1, 3, 10, 30}`); and the
   deploy-path skip at `reconcile.go:637-655` — the branch this very incident
   took — resets `AttemptCount` and `LastAlertedAttempt` to 0 and returns before
   reaching line 944 at all. The "already deployed" branch at `:608` does the
   same. So a single failure followed by a docs-only recovery can never produce a
   retraction, whatever the config says.

3. **Bosun's HTTP request log carries no client address.** The one field that
   attributes an unexplained request is absent from the `HTTP request completed`
   line (`internal/daemon/server.go`), and the `tailscale-gateway` container logs
   no request paths either. This is worth fixing on its own merit; it is no
   longer what unblocks the 404 below.

4. **A stale webhook on `cameronsjo/dotfiles` has been 404ing since
   2025-12-23.** Its push hook posts to
   `https://gateway.<tailnet>.ts.net/hooks/github-push`;
   `homelab/unraid/appdata/tailscale-gateway/serve.json` proxies `/hooks/` to
   `http://bosun:8080/webhook`, so it resolves to `/webhook/github-push`, which
   `server.go:72-74` never registers (only `/webhook`, `/webhook/github`,
   `/webhook/manual`). GitHub's recorded delivery timestamps match the three
   bosun 404s exactly. `dotfiles` pushes have therefore never triggered a
   reconcile. Bosun's own docs still print that path in four places, which is the
   likely origin.

Intended outcome: a declared timeout is an actual ceiling; a failure alert always
gets a retraction; `dotfiles` pushes reach bosun; and the next unexplained request
names its own sender.

**Out of scope, filed separately:** the `no declared services in state file`
drift warning every 5 minutes ([bosun#478](https://github.com/cameronsjo/bosun/issues/478));
`GitLocalTimeout` sharing the unenforced-label defect on local operations; a
timeout-bearing HTTP transport for the private-HTTPS git path (homelab uses SSH).

**Unsettled, and it blocks nothing here.** The `Reloaded project config from repo`
line prints `on_failure: false` while a Discord failure alert was delivered
seconds earlier through a path that returns early on `!r.config.OnFailure`
(`alerts.go:87`). `homelab/bosun.yaml` has no `alerts` block and the spec says
`on_failure` defaults true. Either the log line misreports the gate or a failure
path is ungated. Task 6 files it; do not cite that log line as the effective
config anywhere in this work.

## Panel

Panel: cadence:plan-reviewer, cadence:operability-reviewer, cadence:red-team-reviewer ran — 43 findings, 39 folded in, 4 declined

## Alternatives declined

- **`client.InstallProtocol("ssh", gitssh.NewClient(&xssh.ClientConfig{Timeout}))`.**
  The obvious way to set a dial timeout, and it takes GitOps offline. `overrideConfig`
  (`ssh/common.go:290`) reflects over every `ssh.ClientConfig` field and assigns
  unconditionally, zero values included, at `common.go:141` — *after* the
  nil-`HostKeyCallback` repair at `:127`. Installing a config that sets only
  `Timeout` therefore blanks `User`, `Auth`, `HostKeyCallback`, and
  `HostKeyAlgorithms`, and every fetch dies `ssh: must specify HostKeyCallback`.
  Both the plan-review and red-team seats caught this independently.
- **Shell out to `git fetch` and kill the process on timeout.** Trivially
  killable, and the container has git. Declined: breaks design principle 2
  (*single binary — no Python, uv, or bash dependencies on target*).
- **Raise `GitFetchTimeout`.** The bound was never the problem; nothing enforced
  it. A larger unenforced number is the same defect with a worse label.
- **Abandon the fetch in a goroutine behind an in-flight guard.** My first
  design. Declined on three counts the panel raised: nothing bounds the
  abandoned goroutine, so the guard is fail-*stuck* and only `docker restart
  bosun` clears it — strictly worse than the 16-minute stall it replaces;
  `Clone`'s `os.RemoveAll(g.Dir)` cleanup (`git.go:396-400`) would delete the
  directory out from under a live writer; and `defer closeGitAuth(auth)`
  (`git.go:441-445`) closes the ssh-agent socket the goroutine is still using.
- **Flip `on_success: true` in `homelab/bosun.yaml`.** Config-only, but buys
  retraction at the price of a Discord message on every deploy — and per
  finding 2 it would not have retracted this alert anyway.

## Panel review findings declined

1. **Register the abandoned goroutine on `Server.wg` for graceful shutdown**
   (operability). Moot — the abandonment design is declined above.
2. **Add an unmatched-path metric counter so 404 evidence survives log rotation**
   (operability). The sender is now identified, so the motivating need is gone.
   Noted in the PR body as optional hardening.
3. **Install a timeout-bearing HTTP transport for the private-HTTPS git path**
   (plan review). Real, but homelab authenticates over SSH; filed as follow-up
   rather than widening this change.
4. **Fix the duplicated stale docs under `bosun/.claude/worktrees/`**
   (red team). Those are worktree checkouts of other branches, not files to edit
   — they resolve when their own branches rebase.

## Work

All bosun work lands in a worktree under
`bosun/.claude/worktrees/git-timeout-enforcement/`, pushed `-u` with a draft PR
at entry (`bosun` is branch-mode). Agent-run local gates must be wrapped:
`scripts/agent-go-gate.sh go test ...`.

Task 1 is independent of everything else and should ship today.

### Task 1 — Repoint the `dotfiles` webhook (no code, no PR)

`cameronsjo/dotfiles` hook `587540215` posts to `/hooks/github-push`. Repoint it
to `https://gateway.<tailnet>.ts.net/hooks` — the path `cameronsjo/homelab`
uses, which returns 202. Then redeliver a recent push and confirm bosun logs a
`Generic webhook received` rather than a 404.

Decide first whether `dotfiles` pushes *should* trigger a homelab reconcile at
all. If not, delete the hook instead. It has been inert for nine months, so
nothing depends on the current behaviour either way.

### Task 2 — Author the OpenSpec proposal (gates Tasks 3-5)

This is mandatory, not a question. `openspec/specs/alerting/spec.md:209-218`
specifies `on_success`/`on_failure` and their defaults; `:232` specifies the
Deploy Recovery alert shape; `openspec/specs/reconcile/spec.md:121-122` and
`:517-526` specify pull/fetch behaviour and the timeout family. Both Task 3 and
Task 4 change specified behaviour.

Per `bosun/CLAUDE.md` § Spec Review Workflow this runs on its **own**
`spec/<change-id>` branch and PR, through CodeRabbit convergence, and needs the
`ready-to-build` label before implementation starts. Budget days, not hours — the
implementation worktree does not open until the label lands.

### Task 3 — Make the git network timeouts real

Files: `internal/reconcile/git.go`, tests in `internal/reconcile/git_test.go`.

**3a — bound the dial and handshake, safely.** Wrap the resolved
`transport.AuthMethod` so its `ClientConfig()` returns the auth's own config with
`Timeout` filled in. This preserves `User`, `Auth`, `HostKeyCallback`, and
`HostKeyAlgorithms` — which the declined `InstallProtocol` approach destroys —
and needs no process-global mutation. Add `GitSSHDialTimeout` (suggest
`30 * time.Second`).

Add a test asserting a fetch **still authenticates** after the wrap. That test is
what would have caught the declined approach.

**3b — spike the read-deadline injection point, time-boxed to half a day.**
3a bounds dial and handshake only; a stalled packfile read remains unbounded, and
that is the failure mode the incident is attributed to. Two candidates, in
preference order:

- A custom `transport.Transport` registered for `ssh` that dials the TCP
  connection itself, wraps it in a `net.Conn` that refreshes a read deadline on
  every `Read`, builds the client via `xssh.NewClientConn`, and replicates the
  auth config faithfully. This is the only design that removes the leak, the
  corruption risk, and the guard at once.
- Failing that, bound the fetch externally *and* force-close the underlying
  connection so the goroutine actually dies, with the auth's `io.Closer` owned by
  the goroutine rather than the caller's `defer`.

If neither is workable inside the box, ship 3a alone and **state the residual in
the PR body and `docs/troubleshooting.md`**: dial is bounded, transfer is not.
Do not ship the abandonment design.

**3c — make the failure legible.** `git.go:486-488` returns the timeout error
with no log line at all (`Clone` at `:394-400` does log one). Add the
`logger.Error()` at the throw site carrying `operation`, sanitized URL, branch,
`elapsed_ms`, and `timeout_ms`, and put the **actual elapsed time** in the error
text — `git fetch timed out after 2m0s` describing a 16m30s stall is the single
most misleading fact in the incident.

**Test fixtures.** `GitFetchTimeout`, `GitCloneTimeout`, and `GitSSHDialTimeout`
are package constants (`git.go:77-83`); a test cannot shorten them. Make them
`GitOps` fields defaulting to those constants. `Pull` also runs `validateBranch`,
`IsDirty`, `GetLatestCommit`, and `PlainOpen` before the fetch (`git.go:433-472`),
so the fixture needs a real initialized repo with a commit and an `origin` remote.

Two tests, and they test different layers — a listener that accepts TCP and never
speaks is a stalled *handshake*, which 3a alone bounds, so it proves nothing
about 3b:

- stalled handshake → bounded by 3a
- handshake completes, transfer stalls mid-packfile → bounded by 3b

Run the red-proof against `origin/main` with an explicit `-timeout 30s` on that
single package; the expected red is a bounded panic dump, not a 10-minute wait
that the gate script scores as an aborted run.

### Task 4 — Make recovery actually fire

Files: `internal/reconcile/reconcile.go`, `internal/reconcile/alerts.go`,
`internal/config/config.go`, `internal/reconcile/config_reload.go`,
`internal/reconcile/target.go`, `internal/daemon/daemon.go`,
`internal/cmd/alert.go`.

**The gate change is the small half.** Add `on_recovery`, defaulting **true** —
a system that alerts on failure and cannot retract is worse than one that never
alerted. Note that `AlertConfig.OnSuccess` (`config.go:162`) is a plain `bool`;
the tri-state lives only in the raw DTO and collapses at `extractAlertConfig`, so
"mirror `OnSuccess`" yields default-*false*. Default-true needs explicit defaults
in three places, following how `OnFailure` does it: `extractAlertConfig`
(`config.go:1188-1193`), `alertConfigFromEnv` (`config.go:1157-1158`), and
`DefaultConfig()` (`target.go:233`). And `daemon.go:2349-2350` copies the gates
into the reconciler config at startup — omit it and `on_recovery` never reaches
the daemon path, which is the only path the incident happened on.

**The trigger change is the real fix.** Move retraction to the run boundary:
when `state.LastAlertedAttempt > 0` and the run ends clean, send the recovery
alert and reset — including from the deploy-path skip (`reconcile.go:637-655`)
and the already-deployed skip (`:608`), both of which currently reset the
counters and return before reaching `reconcile.go:944`. Drop the
`AttemptCount > 1` condition; one failure that alerted deserves one retraction.

**The test must drive this incident's exact sequence:** one sync failure that
alerts, then a success whose changed files are all deploy-irrelevant so it takes
the skip branch. Under today's code that produces no recovery alert. If the test
passes before the trigger change, it is testing the wrong thing.

Also update the now-false doc comment at `reconcile.go:219-221` ("OnSuccess gates
success **and recovery** alert dispatch"), `internal/cmd/alert.go:166-171` which
reports the gates, and `skills/onboard/resources/configuration.md` (mandatory per
§ Skill Maintenance). There is no `BOSUN_ALERT_ON_*` env var today — if
`on_recovery` gets one, it needs a row in `bosun/CLAUDE.md`'s table and an entry
in `alertConfigFromEnv`; if not, no CLAUDE.md change.

### Task 5 — Log the client address, unforgeably

File: `internal/daemon/server.go` (the middleware at `:155-187`).

Log **two** fields, never collapsed into one:

- `remote_addr` — always `r.RemoteAddr`. The observed fact.
- `forwarded_for` — the validated first `X-Forwarded-For` entry, and only when
  `r.RemoteAddr` is the known gateway peer. The claim.

`BOSUN_LISTEN_ADDR` binds all interfaces by design (`server.go:94-100`), so any
container on the docker bridge can send a well-formed `X-Forwarded-For` and be
recorded as the sender. Parsing as an IP does not make a forged value true.
Preferring the header would have made the next investigation confidently wrong.

Use a `log.FieldRemoteAddr` constant, matching the middleware's existing style.
Confirm tailscale `serve` actually sets `X-Forwarded-For` on this path before
depending on it — and note `serve.json` carries `"AllowFunnel": true`, so
`/hooks/` is reachable from the public internet and the sender population was
never limited to repos we own.

### Task 6 — Correct the stale webhook paths, and file the loose ends

Docs, in the Task 3-5 PR. The gateway-fronted vs. direct-to-port distinction is
what picks the replacement, so these are not all the same edit:

| File | Current | Correct |
|---|---|---|
| `docs/guides/unraid-setup.md:236` | `/hooks/github-push` (via gateway) | `/hooks` |
| `docs/guides/unraid-setup.md:315` | `localhost:8080/hooks/github-push` | `/webhook/github` |
| `unraid-templates/README.md:80` | `your-unraid:8080/hooks/github-push` | `/webhook/github` |
| `unraid-templates/README.md:169` | `unraid:8080/hooks/test` | `/webhook` |
| `docs/adr/0005-tunnel-providers.md:70` | `"Proxy": "http://bosun:8080"` | needs the `/webhook` suffix |

File as separate issues: the `on_failure: false`-yet-alert-delivered
contradiction (§ Context, Unsettled); `GitLocalTimeout`'s unenforced label; the
HTTPS git transport timeout.

### Task 7 — Changelog, PR, release, deploy

`[Unreleased]` entry in `bosun/CHANGELOG.md` (standalone repo, root changelog),
`fix:` subject so release-please cuts a patch. Run `cadence-forge:polish` before
flipping out of draft.

**Deploying it needs a named actor.** `bosun` runs `ghcr.io/cameronsjo/bosun:latest`,
a moving tag — an unchanged compose file means bosun's own reconcile never
recreates the container ([bosun reconcile does not repull a moving tag]). The
release PR `bosun#640` (`chore(main): release 0.42.2`) is currently open, so
`main` is not released either. Sequence: merge this PR → merge #640 → wait for
the GHCR push → **manually** `docker compose pull bosun && docker compose up -d bosun`
on `unraid` → confirm the new `StartedAt`, not `RestartCount`.

## Verification

Local, from the worktree:

```bash
scripts/agent-go-gate.sh go vet ./...
scripts/agent-go-gate.sh go test -race -timeout 120s ./internal/reconcile ./internal/daemon ./internal/config
```

The two timeout tests and the docs-only-recovery test are the load-bearing ones.
Each must be shown failing against `origin/main` before it counts — a test that
could not have gone red proves nothing here.

**Timeout enforcement is proven in the Go tests, not on `unraid`.** The earlier
draft proposed dropping egress to `github.com:22` on the host; that reaches all
78 containers including `traefik`, `authelia`, and `tailscale`, and there is no
bosun subcommand that does an ad-hoc sync, so the sibling suggestion had no entry
point either. A blackholed address in the test fixture measures the same thing
with no blast radius.

On `unraid`, after the deploy in Task 7 — capture these in
`scripts/verify-git-timeout.sh` rather than leaving them as prose that dies with
the session:

1. **Recovery retracts.** Push a commit that fails to sync, restore, then push a
   docs-only commit so the recovery takes the skip branch. Expect a Discord
   retraction with `on_success` still `false`.
2. **Client address appears.** `docker logs --since 1h bosun | grep github-push`
   — grep the 404, not the field name; grepping `remote_addr` matches every
   request line once the field exists and passes whether or not it was populated.

Add a `docs/troubleshooting.md` section: how to tell git sync is wedged, what the
log line and elapsed time look like, and what clears it.

Baseline captured 2026-09-08:

```bash
ssh unraid 'docker logs --since 2h --timestamps bosun' | grep -E 'timed out|github-push'
```

## Global Constraints

- `bosun` is branch-mode: all work lands in a worktree under `bosun/.claude/worktrees/`, pushed `-u` with a draft PR at entry.
- Agent-run local gates MUST be wrapped: `scripts/agent-go-gate.sh go test ...`.
- Task 2's `ready-to-build` label gates Tasks 3–5. Implementation does not start before the label lands.
- Every load-bearing test MUST be shown failing against `origin/main` before it counts.
- Do not ship the abandon-the-fetch-in-a-goroutine design under any circumstance; a stated partial bound is the acceptable fallback.

## Orchestrator

Driver: Opus family — the change decides what an `X-Forwarded-For` header is allowed to assert in an audit log, and rewrites a fail-open alert gate. Both are security-posture calls, not pattern application.

## Tasks

- [x] 1. Repoint or delete the `dotfiles` webhook — **deleted** (hook `587540215`, `dotfiles` now has 0 hooks). `homelab/bosun.yaml` declares one target, so a `dotfiles` push could only ever trigger a no-op homelab fetch.
- [x] 2. Author the OpenSpec proposal — `openspec/changes/update-git-timeouts-and-alert-recovery/`, `openspec validate --strict` passes.
- [ ] 2b. Drive the spec PR through CodeRabbit convergence to the `ready-to-build` label.
- [ ] 3. Make the git network timeouts real (`internal/reconcile/git.go`).
- [ ] 4. Make recovery actually fire (`reconcile.go`, `alerts.go`, `config.go`, `target.go`, `daemon.go`, `cmd/alert.go`).
- [ ] 5. Log the client address, unforgeably (`internal/daemon/server.go`).
- [ ] 6. Correct the stale webhook paths in docs (rides the Task 3-5 PR, so gated on `ready-to-build`). Loose-end issues **filed**: [#652](https://github.com/cameronsjo/bosun/issues/652) `on_failure` contradiction, [#653](https://github.com/cameronsjo/bosun/issues/653) `GitLocalTimeout`, [#654](https://github.com/cameronsjo/bosun/issues/654) HTTPS transport.
- [ ] 7. Changelog, PR, release, manual deploy on `unraid`, confirm new `StartedAt`.

## Deviations

- **2026-09-08 — Task 2, reconcile spec delta is ADDED, not MODIFIED.** The plan cites `openspec/specs/reconcile/spec.md:517-526` as "the timeout family"; those lines specify `BackupTimeout` only. `GitFetchTimeout`/`GitCloneTimeout` appear nowhere in any spec, so there is no requirement to modify. Written as two ADDED requirements (Git Network Timeout Enforcement, Git Timeout Configuration) instead.
- **2026-09-08 — Task 2 adds an `observability` spec delta the plan did not name.** Task 5's client-address logging changes behaviour the `observability` capability owns (`Structured Fields`, `:107`), so it needs its own delta or the spec-before-code rule is only half-satisfied.
- **2026-09-08 — Task 5 requires a trusted-proxy config surface.** The plan says to emit `forwarded_for` "only when `r.RemoteAddr` is the known gateway peer", but bosun has no trusted-proxy notion today — `grep` for `trusted.?prox` returns nothing. Specified as an operator-configured set defaulting **empty**, so `forwarded_for` is never emitted until a proxy is named.
- **2026-09-08 — `remote_addr` already exists elsewhere in `server.go`.** Six auth-failure sites hardcode the string (`:587`, `:593`, `:608`, `:658`, `:664`, `:695`); only the request-completion middleware lacks it. Task 5's `FieldRemoteAddr` constant now also has those six call sites to converge.

## Learnings

- **The spec cited as governing a behaviour may not govern it at all.** Two of the three plan-named spec anchors were wrong in the same direction: the reconcile timeout family was `BackupTimeout`, and the alerting spec specifies the recovery alert's *shape* without ever saying when it fires. Both defects survived precisely because no requirement covered them — which is the same reason the plan could not find the right line to cite.

## Learnings

_None yet._
