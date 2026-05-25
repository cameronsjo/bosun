# GH#214 Local-Deploy Silent No-Op — Field Report

**Date:** 2026-05-21
**Type:** investigation
**Project:** bosun (+ homelab GitOps repo)

## Goal

Bosun's local-deploy mode reported `success: true` on every reconcile while the
rendered Compose/appdata files at `/mnt/appdata/**` never changed. Push after
push, the daemon, the webhook, and `docker compose up` all returned green; only a
host-side `mtime`/`grep` check revealed the files were 13 days stale. The goal:
make this failure mode impossible to ship silently, then find and fix the
underlying cause.

## Root Cause

`BOSUN_INFRA_DIR` was set to `"."` in the homelab daemon's compose file, but the
infrastructure (`compose/`, `appdata/`) lives under `unraid/`, not at the repo
root. Bosun therefore treated the **entire repo** as the infra root, and every
stage of the pipeline did exactly what it was told:

- `ExtractDeclaredState` globbed `staging/compose/*.yml` — which didn't exist at
  the root — and returned zero declared services.
- `discoverDeployTargets` walked the staging root and treated every top-level
  directory (`.beads`, `.claude`, `unraid`) as a deploy target.
- The sync mapped `staging/unraid → /mnt/appdata/unraid`, so rendered output
  landed at `/mnt/appdata/unraid/compose/*.yml` while Docker kept reading the
  stale `/mnt/appdata/compose/*.yml`.

The misconfiguration was **internally consistent**: render, discovery, and deploy
all resolved to the same wrong root, so no single stage could detect a
contradiction. That consistency is exactly why every diagnostic surface reported
success — there was no disagreement to flag.

## What Worked

A three-layer response, each independently valuable:

- **Layer 1 — defensive invariants** (shipped earlier in v0.34.0): promote
  `declared_services: 0` to a hard error (`ErrNoDeclaredServices`, overridable via
  `BOSUN_ALLOW_EMPTY_DECLARED_STATE`), distinguish a missing compose dir
  (`ErrComposeDirMissing`, fatal/non-overridable) from an empty one, and add a
  post-deploy mtime + `WrittenFiles` invariant (`BOSUN_SKIP_DEPLOY_INVARIANT` to
  bypass). A silent no-op now fails loudly instead of reporting false success.
- **Layer 3 — self-diagnosing hint** (this session's PR #307): when the staging
  compose dir is absent, `findComposeCandidates` scans sibling directories for a
  `compose/` subdir; `suggestInfraDir` turns any candidates into an actionable
  remediation (`did you mean BOSUN_INFRA_DIR="unraid"?`). The error now points at
  its own fix.
- **Root-cause config fix** (homelab PR #33): `BOSUN_INFRA_DIR: "." → "unraid"`,
  plus a `deploy_sync_paths` allowlist (`compose/**`, `appdata/**`) so siblings
  like `boot/` and `scripts/` aren't synced into `/mnt/appdata`, plus dropping the
  `unraid/` prefix from post-sync hook globs (hooks match staging-relative
  `WrittenFiles`, not repo-relative paths).

## Decisions Made

- **Merged bosun #307 over a CHANGES_REQUESTED review.** CodeRabbit raised three
  actionable findings (quote `BOSUN_INFRA_DIR` values with `%q`; label a fenced
  block `text`; H1→H2 in an OpenSpec delta file). All were fixed and locally
  verified. CodeRabbit then ran out of review credits and could not re-review the
  fix commit, leaving a *stale* CHANGES_REQUESTED it was structurally unable to
  clear. With substantive CI (Build/Lint/Test/WebUI/codecov) green and findings
  addressed, the merge overrode the stale review rather than waiting on a reviewer
  that couldn't run.
- **Staged the homelab production change separately and merged it deliberately.**
  Branch push and deploy are decoupled, so the fix sat inert on a feature branch
  until an explicit merge-to-main triggered the live reconcile.

## Gotchas

- **`gofmt -l` reports per-file, not per-edit.** It flagged both files I touched,
  but `origin/main` already carried the same struct-alignment drift — the files
  were dirty before my branch. Running `gofmt -w` would have swept ~227 lines of
  unrelated reformatting into the diff. The reliable check is `gofmt -d <file> |
  grep` scoped to your actual hunks: empty output means your edits conform,
  regardless of the file's overall state.
- **CodeRabbit credit exhaustion is a hard merge blocker disguised as a review
  state.** A stale CHANGES_REQUESTED against a pre-fix commit can't be cleared if
  the bot can't re-review. `mergeStateStatus: UNSTABLE` (not `BLOCKED`) is the
  tell that nothing hard-blocks and the override is available.
- **The llms.txt freshness check fails as `HttpError: Resource not accessible by
  integration`.** That's the restricted `GITHUB_TOKEN` being denied the
  comment-write, not a real staleness signal — touching `llms.txt` would only game
  the heuristic.
- **A CodeRabbit finding points at a class, not its full extent.** It flagged only
  the single-candidate `%q` line; fixing just that would have left the
  multi-candidate branch inconsistent. The fix had to cover both branches, the
  test expectations, and the doc example that mirrors the output.

## Key Takeaways

- An internally-consistent misconfiguration is the hardest kind to detect — no
  stage disagrees, so add invariants that check the *outcome* (did bytes land on
  disk?), not just intra-stage consistency.
- Branch push ≠ production deploy in this GitOps setup. The webhook fires only on
  merge to `main`; feature branches are safe to push and stage indefinitely.
- When a fix improves an error path, also update the docs example that quotes that
  error's output — they drift together.
- `#214` is merged but not *proven* fixed until the Unraid reconcile lands and
  host files update. The Layer-1 invariants guarantee a bad outcome surfaces
  loudly rather than silently, but the host-side `mtime`/`grep` check is still the
  ground truth.
