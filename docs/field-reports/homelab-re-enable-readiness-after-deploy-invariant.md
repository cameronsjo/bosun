# Homelab Re-Enable Readiness After the Deploy Invariant — Field Report

**Date:** 2026-05-28
**Type:** investigation
**Project:** bosun (+ homelab GitOps repo)

## Goal

Bosun had been turned **off** in the homelab after `#214` — local-deploy mode
reported `success: true` for 13 days while `/mnt/appdata/**` never changed (see
`gh214-local-deploy-silent-no-op.md` for the root cause). Since then the
deploy-invariant work landed (`#371`, v0.37.4) and the backup-scope fix shipped
(`#375`, v0.37.5). The question this session answered: **is it safe to re-release
and turn bosun back on against the live Unraid host?** This report captures the
readiness *assessment* — what changed the risk calculus, how the open-issue
backlog was triaged closed-loop, and the verify-first re-enable method handed to
the homelab session. It deliberately does not re-derive the `#214` root cause.

## What Changed the Risk Calculus

The blocker for re-enabling was never "is the config fixed" — `BOSUN_INFRA_DIR`
was already corrected `"." → "unraid"` in the homelab repo (homelab PR #33). The
blocker was **trust**: the exact failure mode that bit us is the one bosun was
structurally unable to detect, because every pipeline stage agreed on the wrong
root and reported green. Re-enabling a tool that can silently lie for 13 days is a
different decision than re-enabling one that fails loud.

The deploy invariant (`#371`) is what moved the needle. It checks the *outcome* —
did bytes with the expected content actually land on disk, with fresh mtimes,
matching `WrittenFiles` — rather than intra-stage consistency. That converts the
`#214` class from "ships green, stale indefinitely" into "fails the reconcile and
rolls back." The backup-scope fix (`#375`) is the complement: when a deploy *does*
need rollback, the pre-deploy backup now captures the deployed config footprint
(not whole appdata dirs), so the 5-minute `BOSUN_BACKUP_TIMEOUT` no longer burns on
media/DB/cache and the archive isn't a "corrupted" partial. **Invariant + scoped
backup together** are what make the rollback path credible, and a credible rollback
path is what makes auto-reconcile safe to re-arm.

## Decisions Made

- **Re-enable is gated on a first-reconcile host-side proof, not on the daemon's
  green.** The daemon reporting `success: true` is exactly the signal that lied
  during `#214`. The go/no-go is the host `mtime`/`grep` check after the first live
  reconcile — same ground truth that originally exposed the bug. The invariant
  makes a *bad* outcome loud; it does not relieve the operator of confirming the
  *good* outcome once.
- **Triage the deploy-sprint backlog closed-loop before re-enabling, not after.**
  Re-enabling with unknown open risk is how `#214` shipped. Every open
  deploy-sprint issue was classified as fixed, not-applicable-in-this-topology, or
  irrelevant-unless-exposed before giving the green light.
- **Re-release on the normal release-please path.** The three PRs this session
  (`chore(openspec)` / `fix(reconcile)` / `docs`) produced a single clean **0.37.5**
  patch bump from the lone `fix:` — no manual version touch, no special re-release
  ceremony. A boring release is the right kind of release for a trust-rebuilding
  deploy.

## Open-Issue Triage (the closed-loop check)

The re-enable verdict rests on this classification of the deploy-sprint backlog:

| Issue | Status | Bearing on re-enable |
|---|---|---|
| GH#214 (silent no-op) | Root cause fixed (config) + invariant safety net | Cleared — the originating bug |
| GH#215 / #221 / #220 | Closed | No residual risk |
| GH#218 | Open | **N/A in this topology** — local deploy mode; the issue is remote-path specific |
| GH#294–#297 | Open (webhook hardening) | **Irrelevant unless internet-exposed** — homelab daemon is LAN-only, poll-driven (`BOSUN_POLL_INTERVAL: 3600`) |

The discriminator that made #218 and #294–#297 safe to defer is the homelab's
actual shape: **local deploy mode, LAN-only, poll-driven.** Remote-SSH-path bugs
and webhook-surface hardening don't touch a daemon that copies files locally and
never accepts an inbound webhook. Reading the homelab compose file
(`BOSUN_INFRA_DIR: "unraid"`, local mode, `DRY_RUN: "false"`, port 8080, polls
hourly, mounts `/mnt/user/appdata:/mnt/appdata`) is what let those issues be
deferred with confidence rather than hand-waved.

## Gotchas

- **The auto-mode sandbox can't reach the Unraid host, and its silence reads as a
  clean check.** Per the global CLAUDE.md rule: sandboxed Bash `ssh`/`docker` into
  `192.168.1.8`/`unraid` returns empty, not an error — and empty output looks like
  "nothing wrong." A re-enable verdict built on a sandboxed host read is built on
  fabricated success. The host-side proof **must** run via the user's `!`-prefix
  (real shell, real authority), never a sandboxed tool call.
- **"Re-release" and "turn on" are two decisions, not one.** Shipping 0.37.5 to
  GHCR doesn't deploy anything — the homelab daemon pulls `:latest` on its own
  cadence, and the config change only takes effect on the next reconcile. The
  handoff had to spell out the ordering: release lands → homelab pulls → first
  reconcile → *then* the host-side proof. Conflating them is how you "verify" a
  release that the live host hasn't even pulled yet.
- **The invariant has an escape hatch that defeats the whole safety case.**
  `BOSUN_SKIP_DEPLOY_INVARIANT=true` exists for diagnostic deploys. If it's set in
  the homelab compose, re-enabling buys none of the protection this assessment
  relies on. Confirming it's *unset* is part of the readiness check, not an
  afterthought.

## Recommendations

- **Re-enable behind a one-time host proof.** First reconcile after turn-on:
  `ssh unraid`, then `stat`/`grep` a known-rendered file under
  `/mnt/appdata/compose/` and confirm a fresh mtime + expected content. Green daemon
  + fresh host bytes = proven. Green daemon + stale bytes = the invariant should
  have already failed it; if it didn't, that's a `#371` regression, not a config
  issue.
- **When assessing readiness, classify every open issue against the deployment's
  actual topology, not its worst-case topology.** "Webhook hardening is open" is
  only a blocker if you accept webhooks. Read the live config first; let the real
  shape retire the irrelevant risks.
- **Treat a tool that previously failed silently as guilty until host-proven.** The
  invariant earns back trust by making bad outcomes loud — but the operator still
  confirms the first good outcome by hand. Automate the loudness; verify the
  silence once.

## Key Takeaways

- The re-enable decision hinged on a safety net, not a config fix: the config was
  fixed weeks ago, but `#371`'s outcome-checking invariant + `#375`'s scoped backup
  are what make a silent-no-op-class failure loud and recoverable — that's what made
  turning bosun back on a defensible call.
- A daemon's `success: true` is the wrong ground truth for the one failure mode that
  reports green while doing nothing. The host-side `mtime`/`grep` after the first
  live reconcile is the only proof that counts.
- Open issues are only blockers relative to the deployment's real topology. LAN-only
  local-mode poll-driven homelab → remote-path (#218) and webhook-hardening
  (#294–#297) issues are deferrable, and reading the live compose file is what
  licenses that call.
- In auto/background mode, a sandboxed read into the homelab returns empty, which
  masquerades as a passing check. Verification reads against the live host must go
  through the `!`-prefix real shell.
