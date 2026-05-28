# Deploy-Chain Hardening: Managed-Set Prune, Rollback Recovery, and the Bind-Mount That Wasn't a Split — Field Report

**Date:** 2026-05-26
**Type:** investigation + architecture
**Project:** bosun

## Goal

Close the two remaining default-path bugs from the GH#330 "success/safety before proven" sweep — #331 (`removeStaleFiles` deletes live data) and #332/#335 (rollback is a silent no-op) — and answer the P0 gating question behind #331 (bosun-n4x): was the homelab actually at risk, or safe by accident? The fixes shipped in PR #366; the lasting value is *why* the danger was real and two CI-mechanics discoveries made along the way.

## What Shipped

- **#331 — managed-set prune.** Replaced rsync-`--delete` mirror semantics with a persisted manifest of files bosun wrote on the last successful deploy (`DeployState.deployed_files`, appdata-relative). A target file is pruned **iff** it was in the previous manifest **and** is absent from current source. Files bosun never wrote (runtime data) are never in the manifest, so never pruned — topology-independent. Plus an empty-source render-failure guard and backward-compatible empty-manifest bootstrap.
- **#332/#335 — rollback consumes the backup.** `Backup()` writes a single `configs.tar.gz` (absolute paths inside), but both rollback paths looked for *loose* files at `filepath.Join(backupPath, base(f))` — which never existed. Replaced with extract-to-temp + `resolveBackupFile`, accounting for tar's leading-`/` stripping. `ComposeUpIsolated` extracts lazily (happy path pays nothing) and shares the root with the orphan pass.

Both were straightforward once the contract was clear. The investigation below is the part worth remembering.

## Root Cause: the "path split" was a bind-mount illusion

The n4x hypothesis (carried in CLAUDE.md and the original PR description) was that `removeStaleFiles` was safe by accident because bosun's `/mnt/appdata` and the containers' `/mnt/user/appdata` were *distinct storage*. The planned verification was an on-box `stat -c '%d:%i %n'` comparing the two paths.

Tracing the actual homelab compose files refuted it:

- **authelia** (`manifests/services/authelia.yml:26`): host `/mnt/user/appdata/authelia` → container `/config`; DB at `/config/db.sqlite3` (`configuration.yml.tmpl:53`) → host `/mnt/user/appdata/authelia/db.sqlite3`.
- **bosun** (`unraid/appdata/bosun/docker-compose.yml:92`): host `/mnt/user/appdata` → container `/mnt/appdata`, with `local_appdata_path: /mnt/appdata`.

So bosun's authelia deploy target resolves to host `/mnt/user/appdata/authelia` — **the same directory** authelia writes its DB to. The repo source for authelia is config-only (`configuration.yml.tmpl` is the only tracked file), so the pre-fix prune compared a config-only staging tree against a target holding `db.sqlite3`/`users.yml` and would delete them. The storage was **shared**; #331 was genuinely dangerous, not safe by accident.

The kicker: the originally-planned `stat /mnt/appdata/...` check would have *misled*. `/mnt/appdata` is a container-only bind-mount path — it doesn't exist on the Unraid host, so `stat` would return ENOENT, which reads like "distinct storage" when it actually means "you're looking at the wrong layer." The equivalence is definitional via the bind mount; no on-box check was needed once the compose files were read.

(Why data survived despite the bug is a secondary, unconfirmed forensic: likely SQLite auto-recreates `db.sqlite3` on the post-sync authelia restart, masking loss as session resets rather than an outage. Answerable by grepping daemon logs for `Removed stale`, but it doesn't change the fix.)

## The CI "freshness check" that was really a comment-poster

PR #366 went red on `Check llms.txt freshness`. It was not a content check. The `llms-txt-sync.yml` workflow's only job is to POST an advisory "does llms.txt need updating?" nudge comment. It got `403 Resource not accessible by integration` and the unhandled throw failed the step.

Two-layer fix:

1. **Root cause — permission.** Commenting on a PR goes through `POST /issues/{n}/comments`, but when `{n}` is a *pull request* GitHub requires **both** `issues: write` **and** `pull-requests: write`. The job had `issues: write` + `pull-requests: read`. The `x-accepted-github-permissions: issues=write; pull_requests=write` response header was the authoritative tell (semicolon-joined = all required). Granted `pull-requests: write`.
2. **Blast radius — `safeMutate`.** An advisory step must never gate a PR. Wrapped all three comment mutations in a helper that `core.warning()`s on failure instead of throwing. Now a future permission regression or API hiccup degrades to a warning.

## Gotchas

- **`filepath.Join(root, "/abs/path")` discards `root`.** Per the documented `Join` behavior, an absolute second arg wins. The backup resolver must `strings.TrimPrefix(originalPath, "/")` first (tar already stripped the leading `/` from the member, so this is also what matches the extracted layout).
- **`filepath.ToSlash` is a no-op off Windows.** It only converts `os.PathSeparator`, which is `/` on Linux/macOS. So a backslash-normalization regression test isn't portable to assert on CI — only the trailing-slash trim is. Kept `ToSlash` for symmetry with `recordManaged`; tested only the portable behavior.
- **For `pull_request` events, the workflow file comes from the PR's head ref.** Committing the CI permission fix onto the #366 branch self-healed #366's own check on the next push — a separate PR wouldn't have fixed #366 until it rebased past the merge.
- **Dry-run still populates `ManagedFiles`.** `recordManaged` → `listManagedFiles` walks the *source*, independent of whether files were written, and the success-state `SaveState` has no dry-run guard. So a dry-run persisted `deployed_files` and could make the next real reconcile prune untouched paths. CodeRabbit caught this (Major); guarded with `&& !r.config.DryRun`.
- **Three path conventions in one package.** staging-relative (`appdata/authelia/x`, for hook globs) vs appdata/TargetPath-relative (`authelia/x`, the manifest keys) vs absolute-with-stripped-slash (`mnt/appdata/...`, inside tarballs). A test asserted the wrong one and caught a real confusion — cheap insurance.

## Decisions Made

- **#331 = managed-set, not protect-list.** A protect-list of runtime patterns (`*.sqlite3`, `data/`) would be perpetually incomplete. Persisting what bosun *wrote* inverts the question to one bosun can answer authoritatively.
- **#332/#335 = fix rollback only (conservative).** Kept "unhealthy-on-compose-up is recoverable, no auto-rollback." Only the broken loose-file lookup was fixed; the `ErrComposeUnhealthy` skip is untouched.
- **CodeRabbit triage: verify before fixing.** All 4 findings checked against source first. The dry-run seeding (Major) and `targetPath` normalization (Major) were real; the normalization was a latent Linux-safe asymmetry with `recordManaged` worth fixing for symmetry even though it can't bite on the daemon's actual Linux runtime.

## Key Takeaways

- **When verifying storage equivalence across containers, read the bind mounts, not the in-container paths.** A container-internal path can alias shared host storage; checking the alias on the host returns ENOENT and reads as a false negative. The compose `volumes:` are the ground truth.
- **A non-gating advisory step should never be able to fail the check.** Wrap its side effects (`core.warning` on failure). Fix the root cause (permission) *and* the blast radius (wrapper) — fixing only one leaves the workflow brittle or silences a real misconfiguration.
- **`pull_request` runs the workflow from the PR head ref** — commit CI fixes onto the feature branch to self-heal that PR's checks.
- **"Success before proven" hides in state-write ordering and effect assumptions.** #331 assumed "absent from source ⇒ safe to delete"; #332 assumed backups were loose files. Both cures replaced an inference with verified durable state (a persisted manifest; extract-then-resolve). When touching reconcile success/state, consult on-disk/persisted truth, not counters or current filesystem shape.
- **Guard persisted state against dry-runs.** If a code path populates state-bearing structures regardless of dry-run, the persistence site needs the `!DryRun` guard, not just the deploy site.
